package gitutil

import (
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// ErrEmptyRepo is returned when the repository has no commits (HEAD is invalid).
var ErrEmptyRepo = errors.New("repository has no commits")

// CreateWorktree creates a new branch and checks it out as a worktree at worktreePath.
// If branchName already exists (e.g. the worktree directory was lost after a server
// restart but the branch was preserved), it checks out the existing branch instead.
func CreateWorktree(repoPath, worktreePath, branchName string) error {
	// Verify HEAD is resolvable; an empty repo (git init with no commits) has
	// no valid HEAD and git-worktree-add will fail with "invalid reference: HEAD".
	if err := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "HEAD").Run(); err != nil {
		return ErrEmptyRepo
	}

	out, err := exec.Command(
		"git", "-C", repoPath,
		"worktree", "add", "-b", branchName, worktreePath, "HEAD",
	).CombinedOutput()
	if err != nil {
		// Branch may already exist when the worktree directory was deleted but the
		// git branch survived (e.g. server restart). The stale worktree entry in
		// .git/worktrees/ also triggers "missing but already registered". Reattach
		// the existing branch with --force so in-progress commits are preserved.
		if strings.Contains(string(out), "already exists") ||
			strings.Contains(string(out), "already registered worktree") {
			out2, err2 := exec.Command(
				"git", "-C", repoPath,
				"worktree", "add", "--force", worktreePath, branchName,
			).CombinedOutput()
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
// Handles the same stale-branch and missing-worktree edge cases as CreateWorktree.
func CreateWorktreeAt(repoPath, worktreePath, branchName, baseCommit string) error {
	out, err := exec.Command(
		"git", "-C", repoPath,
		"worktree", "add", "-b", branchName, worktreePath, baseCommit,
	).CombinedOutput()
	if err != nil && strings.Contains(string(out), "already exists") {
		if delErr := exec.Command("git", "-C", repoPath, "branch", "-D", branchName).Run(); delErr != nil {
			slog.Default().With("component", "git").Debug("branch delete before retry (best-effort)", "repo", repoPath, "branch", branchName, "error", delErr)
		}
		out, err = exec.Command(
			"git", "-C", repoPath,
			"worktree", "add", "-b", branchName, worktreePath, baseCommit,
		).CombinedOutput()
	}
	if err != nil {
		if strings.Contains(string(out), "already exists") ||
			strings.Contains(string(out), "already registered worktree") {
			out2, err2 := exec.Command(
				"git", "-C", repoPath,
				"worktree", "add", "--force", worktreePath, branchName,
			).CombinedOutput()
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
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD in %s: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// RemoveWorktree removes a worktree and deletes the associated branch.
func RemoveWorktree(repoPath, worktreePath, branchName string) error {
	out, err := exec.Command(
		"git", "-C", repoPath,
		"worktree", "remove", "--force", worktreePath,
	).CombinedOutput()
	if err != nil {
		// If the directory is already gone, prune stale refs and carry on so
		// that the branch deletion below still runs.
		if strings.Contains(string(out), "not a worktree") ||
			strings.Contains(string(out), "not a working tree") ||
			strings.Contains(string(out), "not found") {
			if pruneErr := exec.Command("git", "-C", repoPath, "worktree", "prune").Run(); pruneErr != nil {
				slog.Default().With("component", "git").Debug("worktree prune (best-effort)", "repo", repoPath, "error", pruneErr)
			}
		} else {
			return fmt.Errorf("git worktree remove %s: %w\n%s", worktreePath, err, out)
		}
	}
	// Delete the branch (best-effort) — always attempted so stale branches
	// are cleaned up even when the worktree directory was already missing.
	if delErr := exec.Command("git", "-C", repoPath, "branch", "-D", branchName).Run(); delErr != nil {
		slog.Default().With("component", "git").Debug("branch delete after worktree remove (best-effort)", "repo", repoPath, "branch", branchName, "error", delErr)
	}
	return nil
}
