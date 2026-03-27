package gitutil

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
)

// ErrEmptyRepo is returned when the repository has no commits (HEAD is invalid).
var ErrEmptyRepo = errors.New("repository has no commits")

// CreateWorktree creates a new branch and checks it out as a worktree at worktreePath.
// If branchName already exists (e.g. the worktree directory was lost after a server
// restart but the branch was preserved), it checks out the existing branch instead.
func CreateWorktree(repoPath, worktreePath, branchName string) error {
	// Verify HEAD is resolvable; an empty repo (git init with no commits) has
	// no valid HEAD and git-worktree-add will fail with "invalid reference: HEAD".
	if err := cmdexec.Git(repoPath, "rev-parse", "--verify", "HEAD").Run(); err != nil {
		return ErrEmptyRepo
	}

	// Check if the branch already exists. If it does, reattach it directly
	// rather than trying "add -b ... HEAD" which would either fail (branch
	// exists) or — if the branch was deleted by a race — create a fresh
	// branch from HEAD and silently lose all committed work on the old branch.
	if cmdexec.Git(repoPath, "rev-parse", "--verify", branchName).Run() == nil {
		// Prune stale worktree tracking so the --force add below doesn't
		// fail with "already registered worktree" for a directory that no
		// longer exists.
		_ = cmdexec.Git(repoPath, "worktree", "prune").Run()
		out, err := cmdexec.Git(repoPath, "worktree", "add", "--force", worktreePath, branchName).Combined()
		if err != nil {
			return fmt.Errorf("git worktree add (existing branch) in %s: %w\n%s", repoPath, err, out)
		}
		return nil
	}

	// Create a new branch from HEAD and check it out in the worktree.
	out, err := cmdexec.Git(repoPath, "worktree", "add", "-b", branchName, worktreePath, "HEAD").Combined()
	if err != nil {
		// Race condition: branch may have been created between the check and
		// the add, or a stale worktree entry triggers "already registered
		// worktree". Reattach so in-progress commits are preserved.
		if strings.Contains(out, "already exists") ||
			strings.Contains(out, "already registered worktree") {
			_ = cmdexec.Git(repoPath, "worktree", "prune").Run()
			out2, err2 := cmdexec.Git(repoPath, "worktree", "add", "--force", worktreePath, branchName).Combined()
			if err2 != nil {
				return fmt.Errorf("git worktree add (existing branch) in %s: %w\n%s", repoPath, err2, out2)
			}
			return nil
		}
		return fmt.Errorf("git worktree add in %s: %w\n%s", repoPath, err, out)
	}
	return nil
}

// CreateWorktreeAt creates a new branch at baseCommit and checks it out as a
// worktree at worktreePath. baseCommit can be any git revision (hash, branch, tag).
// Unlike CreateWorktree, on conflict it deletes the stale branch and retries,
// since the caller specifies an explicit base commit rather than preserving
// existing branch state.
func CreateWorktreeAt(repoPath, worktreePath, branchName, baseCommit string) error {
	out, err := cmdexec.Git(repoPath, "worktree", "add", "-b", branchName, worktreePath, baseCommit).Combined()
	if err != nil && strings.Contains(out, "already exists") {
		if delErr := cmdexec.Git(repoPath, "branch", "-D", branchName).Run(); delErr != nil {
			slog.Default().With("component", "git").Debug("branch delete before retry (best-effort)", "repo", repoPath, "branch", branchName, "error", delErr)
		}
		out, err = cmdexec.Git(repoPath, "worktree", "add", "-b", branchName, worktreePath, baseCommit).Combined()
	}
	if err != nil {
		if strings.Contains(out, "already exists") ||
			strings.Contains(out, "already registered worktree") {
			out2, err2 := cmdexec.Git(repoPath, "worktree", "add", "--force", worktreePath, branchName).Combined()
			if err2 != nil {
				return fmt.Errorf("git worktree add (existing branch) in %s: %w\n%s", repoPath, err2, out2)
			}
			return nil
		}
		return fmt.Errorf("git worktree add in %s: %w\n%s", repoPath, err, out)
	}
	return nil
}

// ResolveHead returns the full commit hash of HEAD in the given directory
// (works for both main worktrees and linked worktrees).
func ResolveHead(dir string) (string, error) {
	out, err := cmdexec.Git(dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD in %s: %w", dir, err)
	}
	return out, nil
}

// RemoveWorktree removes a worktree and deletes the associated branch.
func RemoveWorktree(repoPath, worktreePath, branchName string) error {
	out, err := cmdexec.Git(repoPath, "worktree", "remove", "--force", worktreePath).Combined()
	if err != nil {
		// If the directory is already gone, prune stale refs and carry on so
		// that the branch deletion below still runs.
		if strings.Contains(out, "not a worktree") ||
			strings.Contains(out, "not a working tree") ||
			strings.Contains(out, "not found") {
			if pruneErr := cmdexec.Git(repoPath, "worktree", "prune").Run(); pruneErr != nil {
				slog.Default().With("component", "git").Debug("worktree prune (best-effort)", "repo", repoPath, "error", pruneErr)
			}
		} else {
			return fmt.Errorf("git worktree remove %s: %w\n%s", worktreePath, err, out)
		}
	}
	// Delete the branch (best-effort) — always attempted so stale branches
	// are cleaned up even when the worktree directory was already missing.
	if delErr := cmdexec.Git(repoPath, "branch", "-D", branchName).Run(); delErr != nil {
		slog.Default().With("component", "git").Debug("branch delete after worktree remove (best-effort)", "repo", repoPath, "branch", branchName, "error", delErr)
	}
	return nil
}
