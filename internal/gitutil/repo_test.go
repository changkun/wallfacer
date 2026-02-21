package gitutil

import (
	"path/filepath"
	"testing"
)

func TestIsGitRepo(t *testing.T) {
	repo := setupRepo(t)
	plain := t.TempDir()
	missing := filepath.Join(t.TempDir(), "no-such-dir")

	if !IsGitRepo(repo) {
		t.Errorf("IsGitRepo(%q) = false, want true", repo)
	}
	if IsGitRepo(plain) {
		t.Errorf("IsGitRepo(%q) = true, want false (plain dir)", plain)
	}
	if IsGitRepo(missing) {
		t.Errorf("IsGitRepo(%q) = true, want false (missing path)", missing)
	}
}

func TestDefaultBranch(t *testing.T) {
	t.Run("local HEAD branch without remote", func(t *testing.T) {
		repo := setupRepo(t)
		branch, err := DefaultBranch(repo)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if branch != "main" {
			t.Errorf("got %q, want %q", branch, "main")
		}
	})

	t.Run("with origin/HEAD configured", func(t *testing.T) {
		origin := t.TempDir()
		gitRun(t, origin, "init", "--bare", "-b", "main")
		repo := setupRepo(t)
		gitRun(t, repo, "remote", "add", "origin", origin)
		gitRun(t, repo, "push", "origin", "main")
		gitRun(t, repo, "remote", "set-head", "origin", "main")

		branch, err := DefaultBranch(repo)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if branch != "main" {
			t.Errorf("got %q, want %q", branch, "main")
		}
	})

	t.Run("detached HEAD falls back to main", func(t *testing.T) {
		repo := setupRepo(t)
		hash := gitRun(t, repo, "rev-parse", "HEAD")
		gitRun(t, repo, "checkout", hash)

		branch, err := DefaultBranch(repo)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if branch != "main" {
			t.Errorf("got %q, want %q", branch, "main")
		}
	})
}

func TestGetCommitHash(t *testing.T) {
	t.Run("valid repo returns 40-char SHA", func(t *testing.T) {
		repo := setupRepo(t)
		hash, err := GetCommitHash(repo)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(hash) != 40 {
			t.Errorf("hash len = %d, want 40; got %q", len(hash), hash)
		}
	})

	t.Run("non-git directory returns error", func(t *testing.T) {
		if _, err := GetCommitHash(t.TempDir()); err == nil {
			t.Error("expected error for non-git path, got nil")
		}
	})
}
