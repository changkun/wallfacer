package gitutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStashIfDirty(t *testing.T) {
	t.Run("clean tree returns false", func(t *testing.T) {
		if StashIfDirty(setupRepo(t)) {
			t.Error("StashIfDirty on clean tree = true, want false")
		}
	})

	t.Run("untracked file returns true", func(t *testing.T) {
		repo := setupRepo(t)
		writeFile(t, filepath.Join(repo, "untracked.txt"), "new\n")
		if !StashIfDirty(repo) {
			t.Error("StashIfDirty with untracked file = false, want true")
		}
	})

	t.Run("modified tracked file returns true", func(t *testing.T) {
		repo := setupRepo(t)
		writeFile(t, filepath.Join(repo, "file.txt"), "modified\n")
		if !StashIfDirty(repo) {
			t.Error("StashIfDirty with modified file = false, want true")
		}
	})
}

func TestStashPop(t *testing.T) {
	t.Run("restores stashed file", func(t *testing.T) {
		repo := setupRepo(t)
		writeFile(t, filepath.Join(repo, "stash-me.txt"), "stashed\n")
		if !StashIfDirty(repo) {
			t.Fatal("expected stash to be created")
		}
		if err := StashPop(repo); err != nil {
			t.Fatalf("StashPop returned unexpected error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(repo, "stash-me.txt")); os.IsNotExist(err) {
			t.Error("stashed file not restored after StashPop")
		}
	})

	t.Run("no stash entry returns error", func(t *testing.T) {
		if err := StashPop(setupRepo(t)); err == nil {
			t.Error("StashPop with no stash entry should return error")
		}
	})

	t.Run("conflict preserves stash entry and cleans worktree", func(t *testing.T) {
		repo := setupRepo(t) // file.txt = "initial\n"

		// Create an uncommitted change to file.txt.
		writeFile(t, filepath.Join(repo, "file.txt"), "uncommitted\n")
		if !StashIfDirty(repo) {
			t.Fatal("expected stash to be created")
		}

		// Modify the same file and commit, so popping the stash will conflict.
		writeFile(t, filepath.Join(repo, "file.txt"), "modified after stash\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "diverge from stash")

		// StashPop should fail due to the conflict.
		if err := StashPop(repo); err == nil {
			t.Fatal("expected StashPop to fail due to conflict")
		}

		// The stash entry must be preserved so data is recoverable.
		out := gitRun(t, repo, "stash", "list")
		if out == "" {
			t.Error("stash entry should be preserved after failed pop")
		}

		// The worktree should be clean (StashPop cleans up on failure).
		status := gitRun(t, repo, "status", "--porcelain")
		if status != "" {
			t.Errorf("worktree should be clean after StashPop conflict, got: %q", status)
		}
	})
}
