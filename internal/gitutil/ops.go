package gitutil

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
)

// GlobalIdentityOverrides returns git `-c user.name=... -c user.email=...`
// flags sourced from the caller's global git config. Attach them to a `git`
// invocation (before the subcommand) to force the global identity even when a
// per-repo or per-worktree config sets different values — for example, when a
// sandbox container has written its own user.name / user.email into a
// worktree's .git/config. Returns an empty slice when no global identity is
// set.
func GlobalIdentityOverrides(ctx context.Context) []string {
	var overrides []string
	if n, err := cmdexec.New("git", "config", "--global", "user.name").WithContext(ctx).Output(); err == nil && n != "" {
		overrides = append(overrides, "-c", "user.name="+n)
	}
	if e, err := cmdexec.New("git", "config", "--global", "user.email").WithContext(ctx).Output(); err == nil && e != "" {
		overrides = append(overrides, "-c", "user.email="+e)
	}
	return overrides
}

// RebaseOntoDefault rebases the task branch (currently checked out in worktreePath)
// onto the default branch of repoPath. On conflict it aborts the rebase and returns
// ErrConflict so the caller can invoke conflict resolution and retry.
func RebaseOntoDefault(repoPath, worktreePath string) error {
	defBranch, err := DefaultBranch(repoPath)
	if err != nil {
		return err
	}

	if conflictErr := recoverRebaseState(worktreePath); conflictErr != nil {
		return conflictErr
	}

	// Use a transactional command: attempt the rebase, and if it fails,
	// automatically abort it so the worktree is left in a clean state.
	tx := cmdexec.NewTx()
	tx.AddWithRollback(
		cmdexec.Git(worktreePath, "rebase", defBranch),
		cmdexec.Git(worktreePath, "rebase", "--abort"),
	)
	if txErr := tx.Run(); txErr != nil {
		te, ok := txErr.(*cmdexec.TxError)
		if !ok || te.Step == nil {
			return txErr
		}
		out := te.Step.Output
		if len(te.RollbackErrors) > 0 {
			slog.Default().With("component", "git").Debug("rebase abort after failure", "path", worktreePath, "error", te.RollbackErrors)
		}
		if IsConflictOutput(out) || IsRebaseNeedsMergeOutput(out) {
			return &ConflictError{
				WorktreePath:    worktreePath,
				ConflictedFiles: parseConflictedFiles(out),
				RawOutput:       out,
			}
		}
		return fmt.Errorf("git rebase in %s: %w\n%s", worktreePath, te.Step.Err, out)
	}
	return nil
}

// recoverRebaseState aborts any stale merge/rebase state before retrying.
// It only fails when Git metadata is corrupted or a cleanly recoverable state
// cannot be restored; common non-rebase states are ignored.
func recoverRebaseState(worktreePath string) error {
	inRebaseOrMerge, err := hasRebaseOrMergeState(worktreePath)
	if err != nil {
		return fmt.Errorf("check stale rebase state in %s: %w", worktreePath, err)
	}
	if !inRebaseOrMerge {
		return nil
	}

	// Clear stale merge/rebase metadata so the next attempt starts clean.
	// Both are attempted; only the one matching the current state will succeed,
	// so errors from the other are expected and intentionally ignored.
	if err := cmdexec.Git(worktreePath, "rebase", "--abort").Run(); err != nil {
		slog.Default().With("component", "git").Debug("rebase abort (expected if not in rebase)", "path", worktreePath, "error", err)
	}
	if err := cmdexec.Git(worktreePath, "merge", "--abort").Run(); err != nil {
		slog.Default().With("component", "git").Debug("merge abort (expected if not in merge)", "path", worktreePath, "error", err)
	}

	if err := clearConflictedPaths(worktreePath); err != nil {
		return fmt.Errorf("clear stale rebase state in %s: %w", worktreePath, err)
	}

	if hasConflicts, statusErr := HasConflicts(worktreePath); statusErr == nil {
		if hasConflicts {
			return &ConflictError{
				WorktreePath: worktreePath,
				RawOutput:    "pre-rebase cleanup still reports unmerged files",
			}
		}
	}

	return nil
}

// hasRebaseOrMergeState checks whether Git currently has leftover rebase or
// merge state under .git/rebase-apply, .git/rebase-merge, or .git/MERGE_HEAD.
func hasRebaseOrMergeState(worktreePath string) (bool, error) {
	if _, err := cmdexec.Git(worktreePath, "rev-parse", "--verify", "-q", "REBASE_HEAD").Output(); err == nil {
		return true, nil
	}
	if _, err := cmdexec.Git(worktreePath, "rev-parse", "--verify", "-q", "MERGE_HEAD").Output(); err == nil {
		return true, nil
	}
	if _, err := cmdexec.Git(worktreePath, "rev-parse", "--verify", "-q", "CHERRY_PICK_HEAD").Output(); err == nil {
		return true, nil
	}
	return false, nil
}

// clearConflictedPaths removes unmerged markers introduced by stale conflict
// states while preserving tracked content. This is a no-op when nothing is
// blocked. It uses an escalating strategy: each subsequent command is more
// destructive, but also more likely to succeed in pathological states.
func clearConflictedPaths(worktreePath string) error {
	// Attempt 1: reset --merge undoes a conflicted merge while keeping
	// unrelated uncommitted changes. Fails when both staged and unstaged
	// changes exist for the same file (index != HEAD and worktree != index).
	if err := cmdexec.Git(worktreePath, "reset", "--merge").Run(); err == nil {
		return nil
	}
	// Attempt 2: restore from HEAD clears both the index and worktree for all
	// files. Handles the staged+unstaged case that reset --merge rejects.
	if err := cmdexec.Git(worktreePath, "restore", "--staged", "--worktree", "--source=HEAD", "--", ".").Run(); err == nil {
		return nil
	}
	// Attempt 3: hard reset as a last resort. Discards all local modifications.
	if err := cmdexec.Git(worktreePath, "reset", "--hard", "HEAD").Run(); err == nil {
		return nil
	}
	return fmt.Errorf("git clean failed in %s: clear conflicted state", worktreePath)
}

// FFMerge fast-forward merges branchName into the default branch of repoPath.
// It stashes any dirty working-tree state before checkout, and restores it
// after the merge completes. Returns an error if the merge is not fast-forward.
func FFMerge(repoPath, branchName string) error {
	defBranch, err := DefaultBranch(repoPath)
	if err != nil {
		return err
	}

	// Stash any local changes in the main repo so that checkout+merge
	// does not fail with "Your local changes would be overwritten".
	stashed := StashIfDirty(repoPath)

	// Build a transactional command sequence: checkout the default branch,
	// then fast-forward merge. Deferred stash pop runs after the sequence
	// regardless of success, restoring the user's uncommitted changes.
	tx := cmdexec.NewTx()
	if stashed {
		tx.Defer(cmdexec.Git(repoPath, "stash", "pop"))
	}
	tx.Add(cmdexec.Git(repoPath, "checkout", defBranch))
	tx.Add(cmdexec.Git(repoPath, "merge", "--ff-only", branchName))

	if txErr := tx.Run(); txErr != nil {
		te, ok := txErr.(*cmdexec.TxError)
		if !ok || te.Step == nil {
			// TxError without a Step means only deferred commands (stash pop)
			// failed. The merge itself succeeded, so log and return nil.
			slog.Default().With("component", "git").Debug("ff-merge defer error", "repo", repoPath, "error", txErr)
			return nil
		}
		out := te.Step.Output
		if te.Step.Index == 0 {
			return fmt.Errorf("git checkout %s in %s: %w\n%s", defBranch, repoPath, te.Step.Err, out)
		}
		return fmt.Errorf("git merge --ff-only %s in %s: %w\n%s", branchName, repoPath, te.Step.Err, out)
	}
	return nil
}

// CommitsBehind returns the number of commits the default branch has ahead of
// the worktree's HEAD (i.e. how many commits the task branch is behind).
func CommitsBehind(repoPath, worktreePath string) (int, error) {
	defBranch, err := DefaultBranch(repoPath)
	if err != nil {
		return 0, err
	}
	defHash, err := defaultBranchCommitHash(repoPath, defBranch)
	if err != nil {
		// No resolvable ref for the default branch (e.g. empty repo with no
		// commits or no remote configured). The worktree cannot be behind a
		// branch that does not exist yet, so report 0.
		return 0, nil
	}
	out, err := cmdexec.Git(worktreePath, "rev-list", "--count", "HEAD.."+defHash).Output()
	if err != nil {
		return 0, fmt.Errorf("git rev-list in %s: %w", worktreePath, err)
	}
	n, _ := strconv.Atoi(out)
	return n, nil
}

// defaultBranchCommitHash resolves the commit hash of the default branch,
// trying multiple ref forms in order: bare name, refs/heads/, origin/ remote,
// and refs/remotes/origin/. This handles detached-HEAD repos where the local
// branch may not exist but the remote tracking ref is still available.
func defaultBranchCommitHash(repoPath, defBranch string) (string, error) {
	// Try progressively more qualified ref names. In a normal checkout the
	// bare name suffices, but in detached-HEAD repos the local branch may be
	// absent while the remote tracking ref still exists.
	candidates := []string{
		defBranch,
		"refs/heads/" + defBranch,
		"origin/" + defBranch,
		"refs/remotes/origin/" + defBranch,
	}
	var lastErr error
	for _, ref := range candidates {
		hash, err := GetCommitHashForRef(repoPath, ref)
		if err == nil {
			return hash, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("default branch %q not found in %s", defBranch, repoPath)
	}
	return "", lastErr
}

// HasCommitsAheadOf reports whether worktreePath has commits not yet in baseBranch.
func HasCommitsAheadOf(worktreePath, baseBranch string) (bool, error) {
	out, err := cmdexec.Git(worktreePath, "rev-list", "--count", baseBranch+"..HEAD").Output()
	if err != nil {
		return false, fmt.Errorf("git rev-list in %s: %w", worktreePath, err)
	}
	n, _ := strconv.Atoi(out)
	return n > 0, nil
}

// MergeBase returns the best common ancestor (merge-base) of two refs,
// evaluated in the given repository/worktree path.
func MergeBase(repoPath, ref1, ref2 string) (string, error) {
	out, err := cmdexec.Git(repoPath, "merge-base", ref1, ref2).Output()
	if err != nil {
		return "", fmt.Errorf("git merge-base %s %s in %s: %w", ref1, ref2, repoPath, err)
	}
	return out, nil
}

// BranchTipCommit returns the hash, subject, and author timestamp of the most
// recent commit on branch in repoPath. It runs:
//
//	git -C <repoPath> log -1 --format=%H|%s|%aI <branch>
//
// Returns an error if the branch does not exist or the path is not a git repo.
func BranchTipCommit(repoPath, branch string) (hash, subject string, ts time.Time, err error) {
	line, cmdErr := cmdexec.Git(repoPath, "log", "-1", "--format=%H|%s|%aI", branch).Output()
	if cmdErr != nil {
		err = fmt.Errorf("git log in %s for branch %s: %w", repoPath, branch, cmdErr)
		return
	}
	if line == "" {
		err = fmt.Errorf("branch %s not found or has no commits in %s", branch, repoPath)
		return
	}
	// Parse the pipe-delimited format: "<hash>|<subject>|<ISO8601 timestamp>".
	// SplitN with limit 3 ensures subjects containing "|" are not mangled.
	parts := strings.SplitN(line, "|", 3)
	if len(parts) != 3 {
		err = fmt.Errorf("unexpected git log output %q in %s", line, repoPath)
		return
	}
	hash = parts[0]
	subject = parts[1]
	ts, err = time.Parse(time.RFC3339, parts[2])
	if err != nil {
		err = fmt.Errorf("parse commit timestamp %q: %w", parts[2], err)
	}
	return
}

// FetchOrigin runs `git fetch origin` in the given repository path to update
// remote tracking refs. Returns an error if the fetch fails (e.g. no network,
// no remote configured). Callers should log the error and continue — stale
// refs are better than aborting the operation entirely.
func FetchOrigin(repoPath string) error {
	out, err := cmdexec.Git(repoPath, "fetch", "origin").Combined()
	if err != nil {
		return fmt.Errorf("git fetch origin in %s: %w\n%s", repoPath, err, out)
	}
	return nil
}

// IsConflictOutput reports whether git output text indicates a merge conflict.
func IsConflictOutput(s string) bool {
	return strings.Contains(s, "CONFLICT") ||
		strings.Contains(s, "Merge conflict") ||
		strings.Contains(s, "conflict")
}

// HasConflicts reports whether the worktree at worktreePath has any unresolved
// merge/rebase conflicts (files with conflict-marker status codes in git status).
func HasConflicts(worktreePath string) (bool, error) {
	out, err := cmdexec.Git(worktreePath, "status", "--porcelain").Output()
	if err != nil {
		return false, fmt.Errorf("git status in %s: %w", worktreePath, err)
	}
	// Scan each line of porcelain output for unmerged status codes. Lines
	// shorter than 2 characters are blank separators and are skipped.
	for line := range strings.SplitSeq(out, "\n") {
		if len(line) < 2 {
			continue
		}
		// Git porcelain status uses a two-character XY code. The conflict
		// status codes below correspond to unmerged entries:
		//   UU = both modified, AA = both added, DD = both deleted,
		//   AU/UA = added by us/them, DU/UD = deleted by us/them.
		xy := line[:2]
		switch xy {
		case "UU", "AA", "DD", "AU", "UA", "DU", "UD":
			return true, nil
		}
	}
	return false, nil
}
