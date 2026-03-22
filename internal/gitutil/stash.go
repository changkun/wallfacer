package gitutil

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// StashIfDirty stashes uncommitted changes in worktreePath if the working tree
// is dirty. Returns true if a stash entry was created.
func StashIfDirty(worktreePath string) bool {
	out, err := exec.Command("git", "-C", worktreePath, "status", "--porcelain").Output()
	if err != nil {
		slog.Default().With("component", "git").Warn("git status failed in StashIfDirty; assuming clean",
			"path", worktreePath, "error", err)
		return false
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return false
	}
	err = exec.Command("git", "-C", worktreePath, "stash", "--include-untracked").Run()
	return err == nil
}

// StashPop restores the most recent stash entry.
// Returns an error when the pop fails (e.g. conflicts with rebased state).
// A failed pop leaves the stash entry intact so it can be recovered.
func StashPop(worktreePath string) error {
	out, err := exec.Command("git", "-C", worktreePath, "stash", "pop").CombinedOutput()
	if err != nil {
		// Abort the conflicted pop so the stash entry is preserved and the
		// worktree returns to a clean state. A failed stash pop can leave
		// unmerged (UU) entries that "git checkout -- ." alone cannot clear.
		// Use "git reset --hard HEAD" to clear both the index conflict markers
		// and working tree changes, then "git clean -fd" for untracked files.
		_ = exec.Command("git", "-C", worktreePath, "reset", "--hard", "HEAD").Run()
		_ = exec.Command("git", "-C", worktreePath, "clean", "-fd").Run()
		slog.Default().With("component", "git").Warn("stash pop failed",
			"path", worktreePath, "error", err, "output", string(out))
		return fmt.Errorf("stash pop in %s: %w\n%s", worktreePath, err, out)
	}
	return nil
}
