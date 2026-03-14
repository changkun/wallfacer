package gitutil

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

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

	out, err := exec.Command("git", "-C", worktreePath, "rebase", defBranch).CombinedOutput()
	if err != nil {
		// Abort so the repo is not stuck mid-rebase.
		if abortErr := exec.Command("git", "-C", worktreePath, "rebase", "--abort").Run(); abortErr != nil {
			slog.Default().With("component", "git").Debug("rebase abort after failure", "path", worktreePath, "error", abortErr)
		}
		if IsConflictOutput(string(out)) || IsRebaseNeedsMergeOutput(string(out)) {
			return &ConflictError{
				WorktreePath:    worktreePath,
				ConflictedFiles: parseConflictedFiles(string(out)),
				RawOutput:       string(out),
			}
		}
		return fmt.Errorf("git rebase in %s: %w\n%s", worktreePath, err, out)
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
	if err := exec.Command("git", "-C", worktreePath, "rebase", "--abort").Run(); err != nil {
		slog.Default().With("component", "git").Debug("rebase abort (expected if not in rebase)", "path", worktreePath, "error", err)
	}
	if err := exec.Command("git", "-C", worktreePath, "merge", "--abort").Run(); err != nil {
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
	if _, err := exec.Command("git", "-C", worktreePath, "rev-parse", "--verify", "-q", "REBASE_HEAD").Output(); err == nil {
		return true, nil
	}
	if _, err := exec.Command("git", "-C", worktreePath, "rev-parse", "--verify", "-q", "MERGE_HEAD").Output(); err == nil {
		return true, nil
	}
	if _, err := exec.Command("git", "-C", worktreePath, "rev-parse", "--verify", "-q", "CHERRY_PICK_HEAD").Output(); err == nil {
		return true, nil
	}
	return false, nil
}

// clearConflictedPaths removes unmerged markers introduced by stale conflict
// states while preserving tracked content. This is a no-op when nothing is
// blocked.
func clearConflictedPaths(worktreePath string) error {
	if err := exec.Command("git", "-C", worktreePath, "reset", "--merge").Run(); err == nil {
		return nil
	}
	if err := exec.Command("git", "-C", worktreePath, "restore", "--staged", "--worktree", "--source=HEAD", "--", ".").Run(); err == nil {
		return nil
	}
	if err := exec.Command("git", "-C", worktreePath, "reset", "--hard", "HEAD").Run(); err == nil {
		return nil
	}
	return fmt.Errorf("git clean failed in %s: clear conflicted state", worktreePath)
}

// FFMerge fast-forward merges branchName into the default branch of repoPath.
func FFMerge(repoPath, branchName string) error {
	defBranch, err := DefaultBranch(repoPath)
	if err != nil {
		return err
	}
	if out, err := exec.Command("git", "-C", repoPath, "checkout", defBranch).CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout %s in %s: %w\n%s", defBranch, repoPath, err, out)
	}
	out, err := exec.Command("git", "-C", repoPath, "merge", "--ff-only", branchName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git merge --ff-only %s in %s: %w\n%s", branchName, repoPath, err, out)
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
	out, err := exec.Command(
		"git", "-C", worktreePath,
		"rev-list", "--count", "HEAD.."+defHash,
	).Output()
	if err != nil {
		return 0, fmt.Errorf("git rev-list in %s: %w", worktreePath, err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n, nil
}

func defaultBranchCommitHash(repoPath, defBranch string) (string, error) {
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
	out, err := exec.Command(
		"git", "-C", worktreePath,
		"rev-list", "--count", baseBranch+"..HEAD",
	).Output()
	if err != nil {
		return false, fmt.Errorf("git rev-list in %s: %w", worktreePath, err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n > 0, nil
}

// MergeBase returns the best common ancestor (merge-base) of two refs,
// evaluated in the given repository/worktree path.
func MergeBase(repoPath, ref1, ref2 string) (string, error) {
	out, err := exec.Command("git", "-C", repoPath, "merge-base", ref1, ref2).Output()
	if err != nil {
		return "", fmt.Errorf("git merge-base %s %s in %s: %w", ref1, ref2, repoPath, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// BranchTipCommit returns the hash, subject, and author timestamp of the most
// recent commit on branch in repoPath. It runs:
//
//	git -C <repoPath> log -1 --format=%H|%s|%aI <branch>
//
// Returns an error if the branch does not exist or the path is not a git repo.
func BranchTipCommit(repoPath, branch string) (hash, subject string, ts time.Time, err error) {
	out, cmdErr := exec.Command(
		"git", "-C", repoPath,
		"log", "-1", "--format=%H|%s|%aI", branch,
	).Output()
	if cmdErr != nil {
		err = fmt.Errorf("git log in %s for branch %s: %w", repoPath, branch, cmdErr)
		return
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		err = fmt.Errorf("branch %s not found or has no commits in %s", branch, repoPath)
		return
	}
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

// IsConflictOutput reports whether git output text indicates a merge conflict.
func IsConflictOutput(s string) bool {
	return strings.Contains(s, "CONFLICT") ||
		strings.Contains(s, "Merge conflict") ||
		strings.Contains(s, "conflict")
}

// HasConflicts reports whether the worktree at worktreePath has any unresolved
// merge/rebase conflicts (files with conflict-marker status codes in git status).
func HasConflicts(worktreePath string) (bool, error) {
	out, err := exec.Command("git", "-C", worktreePath, "status", "--porcelain").Output()
	if err != nil {
		return false, fmt.Errorf("git status in %s: %w", worktreePath, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 2 {
			continue
		}
		// Conflict status codes: UU, AA, DD, AU, UA, DU, UD
		xy := line[:2]
		switch xy {
		case "UU", "AA", "DD", "AU", "UA", "DU", "UD":
			return true, nil
		}
	}
	return false, nil
}
