package gitutil

import (
	"fmt"
	"os/exec"
	"strings"
)

// CreateWorktree creates a new branch and checks it out as a worktree at worktreePath.
// If branchName already exists (e.g. the worktree directory was lost after a server
// restart but the branch was preserved), it checks out the existing branch instead.
func CreateWorktree(repoPath, worktreePath, branchName string) error {
	out, err := exec.Command(
		"git", "-C", repoPath,
		"worktree", "add", "-b", branchName, worktreePath, "HEAD",
	).CombinedOutput()
	if err != nil && strings.Contains(string(out), "already exists") {
		// A stale branch was left behind by a previous failed cleanup. Force-delete
		// the orphaned branch and retry so the task can start fresh from HEAD.
		exec.Command("git", "-C", repoPath, "branch", "-D", branchName).Run()
		out, err = exec.Command(
			"git", "-C", repoPath,
			"worktree", "add", "-b", branchName, worktreePath, "HEAD",
		).CombinedOutput()
	}
	if err != nil {
		// Branch may already exist when the worktree directory was deleted but the
		// git branch survived (e.g. server restart). The stale worktree entry in
		// .git/worktrees/ also triggers "missing but already registered". Both
		// cases are resolved by checking out the existing branch with --force.
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
			exec.Command("git", "-C", repoPath, "worktree", "prune").Run()
		} else {
			return fmt.Errorf("git worktree remove %s: %w\n%s", worktreePath, err, out)
		}
	}
	// Delete the branch (best-effort) — always attempted so stale branches
	// are cleaned up even when the worktree directory was already missing.
	exec.Command("git", "-C", repoPath, "branch", "-D", branchName).Run()
	return nil
}
