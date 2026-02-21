package gitutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateWorktree(t *testing.T) {
	t.Run("creates fresh worktree and branch", func(t *testing.T) {
		repo := setupRepo(t)
		wtDir := filepath.Join(t.TempDir(), "wt")
		if err := CreateWorktree(repo, wtDir, "new-branch"); err != nil {
			t.Fatalf("CreateWorktree failed: %v", err)
		}
		if _, err := os.Stat(wtDir); os.IsNotExist(err) {
			t.Error("worktree directory was not created")
		}
		t.Cleanup(func() { RemoveWorktree(repo, wtDir, "new-branch") })
	})

	t.Run("stale branch is force-deleted then recreated", func(t *testing.T) {
		repo := setupRepo(t)
		gitRun(t, repo, "branch", "stale")
		wtDir := filepath.Join(t.TempDir(), "wt")
		if err := CreateWorktree(repo, wtDir, "stale"); err != nil {
			t.Fatalf("CreateWorktree with stale branch failed: %v", err)
		}
		t.Cleanup(func() { RemoveWorktree(repo, wtDir, "stale") })
	})

	t.Run("directory deleted externally recovers via --force", func(t *testing.T) {
		repo := setupRepo(t)
		wtDir := filepath.Join(t.TempDir(), "wt")
		if err := CreateWorktree(repo, wtDir, "orphan"); err != nil {
			t.Fatalf("initial CreateWorktree failed: %v", err)
		}
		os.RemoveAll(wtDir)
		if err := CreateWorktree(repo, wtDir, "orphan"); err != nil {
			t.Fatalf("CreateWorktree after dir removal failed: %v", err)
		}
		t.Cleanup(func() { RemoveWorktree(repo, wtDir, "orphan") })
	})
}

func TestRemoveWorktree(t *testing.T) {
	t.Run("removes existing worktree and branch", func(t *testing.T) {
		repo := setupRepo(t)
		wtDir := filepath.Join(t.TempDir(), "wt")
		if err := CreateWorktree(repo, wtDir, "rm-branch"); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := RemoveWorktree(repo, wtDir, "rm-branch"); err != nil {
			t.Errorf("RemoveWorktree failed: %v", err)
		}
		if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
			t.Error("worktree directory still exists after removal")
		}
	})

	t.Run("graceful when path was never registered", func(t *testing.T) {
		repo := setupRepo(t)
		ghost := filepath.Join(t.TempDir(), "ghost")
		if err := RemoveWorktree(repo, ghost, "ghost-branch"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("graceful when directory deleted externally", func(t *testing.T) {
		repo := setupRepo(t)
		wtDir := filepath.Join(t.TempDir(), "wt")
		if err := CreateWorktree(repo, wtDir, "del-branch"); err != nil {
			t.Fatalf("setup: %v", err)
		}
		os.RemoveAll(wtDir)
		if err := RemoveWorktree(repo, wtDir, "del-branch"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
