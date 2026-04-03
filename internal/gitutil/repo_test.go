package gitutil

import (
	"path/filepath"
	"testing"
)

// TestIsGitRepo validates detection of git repos vs plain directories and
// missing paths.
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

// TestHasOriginRemote validates origin remote detection for repos with and
// without a configured remote.
func TestHasOriginRemote(t *testing.T) {
	t.Run("no remote", func(t *testing.T) {
		repo := setupRepo(t)
		if HasOriginRemote(repo) {
			t.Error("HasOriginRemote should be false for a repo without a remote")
		}
	})
	t.Run("with remote", func(t *testing.T) {
		origin := t.TempDir()
		gitRun(t, origin, "init", "--bare")
		repo := setupRepo(t)
		gitRun(t, repo, "remote", "add", "origin", origin)
		if !HasOriginRemote(repo) {
			t.Error("HasOriginRemote should be true for a repo with an origin remote")
		}
	})
	t.Run("non-git dir", func(t *testing.T) {
		if HasOriginRemote(t.TempDir()) {
			t.Error("HasOriginRemote should be false for a non-git directory")
		}
	})
}

// TestDefaultBranch validates branch resolution priority: current local branch
// is preferred over origin/HEAD, with "main" as final fallback.
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

	t.Run("prefers current branch over origin/HEAD", func(t *testing.T) {
		origin := t.TempDir()
		gitRun(t, origin, "init", "--bare", "-b", "main")
		repo := setupRepo(t)
		gitRun(t, repo, "remote", "add", "origin", origin)
		gitRun(t, repo, "push", "origin", "main")
		gitRun(t, repo, "remote", "set-head", "origin", "main")

		// Switch to a different branch — DefaultBranch should return it,
		// not origin/HEAD (which is "main").
		gitRun(t, repo, "checkout", "-b", "develop")
		branch, err := DefaultBranch(repo)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if branch != "develop" {
			t.Errorf("got %q, want %q", branch, "develop")
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

// TestGetCommitHashForRef validates ref resolution across branches, invalid refs,
// and non-git directories.
func TestGetCommitHashForRef(t *testing.T) {
	t.Run("returns main HEAD when on different branch", func(t *testing.T) {
		repo := setupRepo(t)
		mainHash := gitRun(t, repo, "rev-parse", "main")

		// Create and checkout a new branch with an extra commit.
		gitRun(t, repo, "checkout", "-b", "feature")
		writeFile(t, filepath.Join(repo, "feature.txt"), "feature\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "feature commit")

		// GetCommitHashForRef("main") should return main's HEAD, not feature's.
		hash, err := GetCommitHashForRef(repo, "main")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hash != mainHash {
			t.Errorf("got %q, want main HEAD %q", hash, mainHash)
		}

		// Verify it differs from HEAD (which is on feature).
		headHash, _ := GetCommitHash(repo)
		if hash == headHash {
			t.Error("main hash should differ from HEAD (feature branch)")
		}
	})

	t.Run("error for invalid ref", func(t *testing.T) {
		repo := setupRepo(t)
		if _, err := GetCommitHashForRef(repo, "nonexistent-ref-xyz"); err == nil {
			t.Error("expected error for invalid ref")
		}
	})

	t.Run("non-git directory returns error", func(t *testing.T) {
		if _, err := GetCommitHashForRef(t.TempDir(), "main"); err == nil {
			t.Error("expected error for non-git path")
		}
	})
}

// TestGetCommitHash validates HEAD hash retrieval for valid and non-git paths.
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

// TestHasCommits validates commit detection for repos with commits and non-git dirs.
func TestHasCommits(t *testing.T) {
	t.Run("repo with commits returns true", func(t *testing.T) {
		repo := setupRepo(t)
		if !HasCommits(repo) {
			t.Error("expected HasCommits=true for repo with commits")
		}
	})

	t.Run("empty non-git dir returns false", func(t *testing.T) {
		if HasCommits(t.TempDir()) {
			t.Error("expected HasCommits=false for non-git directory")
		}
	})
}

// TestConflictError_Error validates that the error message includes the file count.
func TestConflictError_Error(t *testing.T) {
	e := &ConflictError{
		WorktreePath:    "/repo/wt",
		ConflictedFiles: []string{"a.go", "b.go"},
	}
	msg := e.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	if !contains(msg, "2") {
		t.Errorf("error message should mention 2 conflicted files, got: %q", msg)
	}
}

// TestDefaultBranch_DetachedWithOriginHEAD verifies that when HEAD is detached
// and origin/HEAD is configured, the branch name from origin/HEAD is returned.
func TestDefaultBranch_DetachedWithOriginHEAD(t *testing.T) {
	origin := t.TempDir()
	gitRun(t, origin, "init", "--bare", "-b", "develop")
	repo := setupRepo(t)
	gitRun(t, repo, "remote", "add", "origin", origin)
	// Push main to origin under the name "develop".
	gitRun(t, repo, "push", "origin", "main:develop")
	gitRun(t, repo, "remote", "set-head", "origin", "develop")

	// Detach HEAD so branch --show-current returns empty.
	hash := gitRun(t, repo, "rev-parse", "HEAD")
	gitRun(t, repo, "checkout", hash)

	branch, err := DefaultBranch(repo)
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if branch != "develop" {
		t.Errorf("got %q, want %q", branch, "develop")
	}
}

// TestRemoteDefaultBranch validates probing fallback logic for origin/main and
// origin/master when origin/HEAD is not configured.
func TestRemoteDefaultBranch(t *testing.T) {
	t.Run("returns branch from origin/HEAD", func(t *testing.T) {
		origin := t.TempDir()
		gitRun(t, origin, "init", "--bare", "-b", "develop")
		repo := setupRepo(t)
		gitRun(t, repo, "remote", "add", "origin", origin)
		gitRun(t, repo, "push", "origin", "main:develop")
		gitRun(t, repo, "remote", "set-head", "origin", "develop")

		got := RemoteDefaultBranch(repo)
		if got != "develop" {
			t.Errorf("RemoteDefaultBranch = %q, want %q", got, "develop")
		}
	})

	t.Run("probes origin/main when origin/HEAD not set", func(t *testing.T) {
		origin := t.TempDir()
		gitRun(t, origin, "init", "--bare", "-b", "main")
		repo := setupRepo(t)
		gitRun(t, repo, "remote", "add", "origin", origin)
		gitRun(t, repo, "push", "origin", "main")
		// Do NOT set origin/HEAD — force probing path.

		got := RemoteDefaultBranch(repo)
		if got != "main" {
			t.Errorf("RemoteDefaultBranch = %q, want %q", got, "main")
		}
	})

	t.Run("probes origin/master fallback", func(t *testing.T) {
		origin := t.TempDir()
		gitRun(t, origin, "init", "--bare", "-b", "master")
		repo := setupRepo(t)
		gitRun(t, repo, "remote", "add", "origin", origin)
		gitRun(t, repo, "push", "origin", "main:master")
		// No origin/HEAD, no origin/main → should find origin/master.

		got := RemoteDefaultBranch(repo)
		if got != "master" {
			t.Errorf("RemoteDefaultBranch = %q, want %q", got, "master")
		}
	})

	t.Run("defaults to main without any remote", func(t *testing.T) {
		repo := setupRepo(t)
		got := RemoteDefaultBranch(repo)
		if got != "main" {
			t.Errorf("RemoteDefaultBranch = %q, want %q", got, "main")
		}
	})
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
