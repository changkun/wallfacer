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

	t.Run("existing branch is reused without deleting commits", func(t *testing.T) {
		repo := setupRepo(t)
		gitRun(t, repo, "checkout", "-b", "stale")
		writeFile(t, filepath.Join(repo, "stale.txt"), "keep me\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "stale commit")
		staleHead := gitRun(t, repo, "rev-parse", "HEAD")
		gitRun(t, repo, "checkout", "main")

		wtDir := filepath.Join(t.TempDir(), "wt")
		if err := CreateWorktree(repo, wtDir, "stale"); err != nil {
			t.Fatalf("CreateWorktree with stale branch failed: %v", err)
		}
		wtHead := gitRun(t, wtDir, "rev-parse", "HEAD")
		if wtHead != staleHead {
			t.Fatalf("expected existing branch head %q, got %q", staleHead, wtHead)
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

func TestCreateWorktreeAt(t *testing.T) {
	t.Run("creates worktree at specific commit", func(t *testing.T) {
		repo := setupRepo(t)
		baseCommit := gitRun(t, repo, "rev-parse", "HEAD")
		wtDir := filepath.Join(t.TempDir(), "wt-at")

		if err := CreateWorktreeAt(repo, wtDir, "at-branch", baseCommit); err != nil {
			t.Fatalf("CreateWorktreeAt: %v", err)
		}
		if _, err := os.Stat(wtDir); os.IsNotExist(err) {
			t.Error("worktree directory was not created")
		}
		t.Cleanup(func() { RemoveWorktree(repo, wtDir, "at-branch") })
	})

	t.Run("handles existing branch by delete and recreate", func(t *testing.T) {
		repo := setupRepo(t)
		baseCommit := gitRun(t, repo, "rev-parse", "HEAD")
		wtDir := filepath.Join(t.TempDir(), "wt-at2")

		// Create once.
		if err := CreateWorktreeAt(repo, wtDir, "at-branch2", baseCommit); err != nil {
			t.Fatalf("first CreateWorktreeAt: %v", err)
		}
		t.Cleanup(func() { RemoveWorktree(repo, wtDir, "at-branch2") })

		// Remove dir but keep branch — simulates server restart.
		os.RemoveAll(wtDir)

		// Create again at same commit.
		if err := CreateWorktreeAt(repo, wtDir, "at-branch2", baseCommit); err != nil {
			t.Fatalf("second CreateWorktreeAt: %v", err)
		}
	})
}

func TestResolveHead(t *testing.T) {
	t.Run("returns 40-char hash for valid repo", func(t *testing.T) {
		repo := setupRepo(t)
		hash, err := ResolveHead(repo)
		if err != nil {
			t.Fatalf("ResolveHead: %v", err)
		}
		if len(hash) != 40 {
			t.Errorf("hash len = %d, want 40; got %q", len(hash), hash)
		}
	})

	t.Run("returns error for non-git directory", func(t *testing.T) {
		if _, err := ResolveHead(t.TempDir()); err == nil {
			t.Error("expected error for non-git path")
		}
	})
}
