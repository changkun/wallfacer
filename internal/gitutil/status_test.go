package gitutil

import (
	"path/filepath"
	"testing"
)

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
