package gitutil

import (
	"path/filepath"
	"testing"
)

// TestWorkspaceStatus validates git status inspection across plain directories,
// repos without remotes, repos with upstream tracking, and ahead/behind counts.
func TestWorkspaceStatus(t *testing.T) {
	t.Run("plain directory is not a git repo", func(t *testing.T) {
		dir := t.TempDir()
		s := WorkspaceStatus(dir)
		if s.IsGitRepo || s.HasRemote {
			t.Errorf("expected IsGitRepo=false HasRemote=false, got %+v", s)
		}
		if s.Path != dir || s.Name != filepath.Base(dir) {
			t.Errorf("unexpected Path/Name: %+v", s)
		}
	})

	t.Run("git repo without remote tracking branch", func(t *testing.T) {
		repo := setupRepo(t)
		s := WorkspaceStatus(repo)
		if !s.IsGitRepo {
			t.Error("IsGitRepo = false, want true")
		}
		if s.Branch != "main" {
			t.Errorf("Branch = %q, want %q", s.Branch, "main")
		}
		if s.HasRemote {
			t.Error("HasRemote = true, want false")
		}
	})

	t.Run("git repo with remote tracking branch in sync", func(t *testing.T) {
		origin := t.TempDir()
		gitRun(t, origin, "init", "--bare", "-b", "main")
		repo := setupRepo(t)
		gitRun(t, repo, "remote", "add", "origin", origin)
		gitRun(t, repo, "push", "-u", "origin", "main")

		s := WorkspaceStatus(repo)
		if !s.HasRemote {
			t.Error("HasRemote = false, want true")
		}
		if s.AheadCount != 0 || s.BehindCount != 0 {
			t.Errorf("ahead=%d behind=%d, want 0 0", s.AheadCount, s.BehindCount)
		}
		if s.RemoteURL != origin {
			t.Errorf("RemoteURL = %q, want %q", s.RemoteURL, origin)
		}
	})

	t.Run("git repo with origin but no tracking branch has RemoteURL", func(t *testing.T) {
		origin := t.TempDir()
		gitRun(t, origin, "init", "--bare", "-b", "main")
		repo := setupRepo(t)
		gitRun(t, repo, "remote", "add", "origin", origin)
		// Do not push — no tracking branch set up.

		s := WorkspaceStatus(repo)
		if !s.IsGitRepo {
			t.Fatal("IsGitRepo = false, want true")
		}
		if s.HasRemote {
			t.Error("HasRemote = true, want false (no tracking branch)")
		}
		if s.RemoteURL != origin {
			t.Errorf("RemoteURL = %q, want %q", s.RemoteURL, origin)
		}
	})

	t.Run("git repo without any remote has empty RemoteURL", func(t *testing.T) {
		repo := setupRepo(t)
		s := WorkspaceStatus(repo)
		if s.RemoteURL != "" {
			t.Errorf("RemoteURL = %q, want empty", s.RemoteURL)
		}
	})

	t.Run("git repo behind main on different branch", func(t *testing.T) {
		origin := t.TempDir()
		gitRun(t, origin, "init", "--bare", "-b", "main")
		repo := setupRepo(t)
		gitRun(t, repo, "remote", "add", "origin", origin)
		gitRun(t, repo, "push", "-u", "origin", "main")

		// Create a feature branch.
		gitRun(t, repo, "checkout", "-b", "feature")
		gitRun(t, repo, "push", "-u", "origin", "feature")

		// Advance main on origin by pushing from a temporary clone.
		clone := t.TempDir()
		gitRun(t, clone, "clone", origin, ".")
		gitRun(t, clone, "config", "user.email", "test@example.com")
		gitRun(t, clone, "config", "user.name", "Test")
		writeFile(t, filepath.Join(clone, "ahead.txt"), "ahead\n")
		gitRun(t, clone, "add", ".")
		gitRun(t, clone, "commit", "-m", "advance main")
		gitRun(t, clone, "push", "origin", "main")

		// Fetch in the repo so origin/main is updated.
		gitRun(t, repo, "fetch", "origin")

		s := WorkspaceStatus(repo)
		if s.Branch != "feature" {
			t.Fatalf("Branch = %q, want %q", s.Branch, "feature")
		}
		if s.MainBranch != "main" {
			t.Fatalf("MainBranch = %q, want %q", s.MainBranch, "main")
		}
		if s.BehindMainCount != 1 {
			t.Errorf("BehindMainCount = %d, want 1", s.BehindMainCount)
		}
	})

	t.Run("git repo ahead of remote", func(t *testing.T) {
		origin := t.TempDir()
		gitRun(t, origin, "init", "--bare", "-b", "main")
		repo := setupRepo(t)
		gitRun(t, repo, "remote", "add", "origin", origin)
		gitRun(t, repo, "push", "-u", "origin", "main")

		writeFile(t, filepath.Join(repo, "local.txt"), "local\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "local commit")

		if s := WorkspaceStatus(repo); s.AheadCount != 1 {
			t.Errorf("AheadCount = %d, want 1", s.AheadCount)
		}
	})
}
