package gitutil

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestIsConflictOutput(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"CONFLICT (content): Merge conflict in file.txt", true},
		{"Merge conflict detected", true},
		{"auto-merging file; conflict detected", true},
		{"Already up to date.", false},
		{"Fast-forward\n file.txt | 1 +", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsConflictOutput(c.in); got != c.want {
			t.Errorf("IsConflictOutput(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCommitsBehind(t *testing.T) {
	t.Run("zero when branches are equal", func(t *testing.T) {
		repo := setupRepo(t)
		wtDir := filepath.Join(t.TempDir(), "wt")
		gitRun(t, repo, "worktree", "add", "-b", "task", wtDir, "HEAD")
		t.Cleanup(func() { RemoveWorktree(repo, wtDir, "task") })

		n, err := CommitsBehind(repo, wtDir)
		if err != nil || n != 0 {
			t.Errorf("CommitsBehind = %d, %v; want 0, nil", n, err)
		}
	})

	t.Run("two commits behind main", func(t *testing.T) {
		repo := setupRepo(t)
		wtDir := filepath.Join(t.TempDir(), "wt")
		gitRun(t, repo, "worktree", "add", "-b", "task", wtDir, "HEAD")
		t.Cleanup(func() { RemoveWorktree(repo, wtDir, "task") })

		for _, f := range []string{"m1.txt", "m2.txt"} {
			writeFile(t, filepath.Join(repo, f), f+"\n")
			gitRun(t, repo, "add", ".")
			gitRun(t, repo, "commit", "-m", f)
		}

		n, err := CommitsBehind(repo, wtDir)
		if err != nil || n != 2 {
			t.Errorf("CommitsBehind = %d, %v; want 2, nil", n, err)
		}
	})

	t.Run("non-git worktree path returns error", func(t *testing.T) {
		repo := setupRepo(t)
		if _, err := CommitsBehind(repo, t.TempDir()); err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestHasCommitsAheadOf(t *testing.T) {
	t.Run("false when at same commit", func(t *testing.T) {
		repo := setupRepo(t)
		ahead, err := HasCommitsAheadOf(repo, "main")
		if err != nil || ahead {
			t.Errorf("HasCommitsAheadOf = %v, %v; want false, nil", ahead, err)
		}
	})

	t.Run("true after task commit", func(t *testing.T) {
		repo := setupRepo(t)
		wtDir := filepath.Join(t.TempDir(), "wt")
		gitRun(t, repo, "worktree", "add", "-b", "task", wtDir, "HEAD")
		t.Cleanup(func() { RemoveWorktree(repo, wtDir, "task") })

		writeFile(t, filepath.Join(wtDir, "task.txt"), "task\n")
		gitRun(t, wtDir, "add", ".")
		gitRun(t, wtDir, "commit", "-m", "task commit")

		ahead, err := HasCommitsAheadOf(wtDir, "main")
		if err != nil || !ahead {
			t.Errorf("HasCommitsAheadOf = %v, %v; want true, nil", ahead, err)
		}
	})

	t.Run("non-git path returns error", func(t *testing.T) {
		if _, err := HasCommitsAheadOf(t.TempDir(), "main"); err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestRebaseOntoDefault(t *testing.T) {
	t.Run("clean rebase succeeds", func(t *testing.T) {
		repo := setupRepo(t)
		wtDir := filepath.Join(t.TempDir(), "wt")
		gitRun(t, repo, "worktree", "add", "-b", "task", wtDir, "HEAD")
		t.Cleanup(func() { RemoveWorktree(repo, wtDir, "task") })

		writeFile(t, filepath.Join(repo, "main-only.txt"), "main\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "main change")

		writeFile(t, filepath.Join(wtDir, "task-only.txt"), "task\n")
		gitRun(t, wtDir, "add", ".")
		gitRun(t, wtDir, "commit", "-m", "task change")

		if err := RebaseOntoDefault(repo, wtDir); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("conflicting changes return ErrConflict", func(t *testing.T) {
		repo := setupRepo(t)
		wtDir := filepath.Join(t.TempDir(), "wt")
		gitRun(t, repo, "worktree", "add", "-b", "task", wtDir, "HEAD")
		t.Cleanup(func() { RemoveWorktree(repo, wtDir, "task") })

		writeFile(t, filepath.Join(repo, "file.txt"), "main version\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "main: change file.txt")

		writeFile(t, filepath.Join(wtDir, "file.txt"), "task version\n")
		gitRun(t, wtDir, "add", ".")
		gitRun(t, wtDir, "commit", "-m", "task: change file.txt")

		err := RebaseOntoDefault(repo, wtDir)
		if !errors.Is(err, ErrConflict) {
			t.Errorf("expected ErrConflict, got %v", err)
		}
	})
}

func TestFFMerge(t *testing.T) {
	t.Run("fast-forward merge succeeds", func(t *testing.T) {
		repo := setupRepo(t)
		gitRun(t, repo, "checkout", "-b", "task")
		writeFile(t, filepath.Join(repo, "task.txt"), "task\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "task commit")
		gitRun(t, repo, "checkout", "main")

		if err := FFMerge(repo, "task"); err != nil {
			t.Errorf("FFMerge failed: %v", err)
		}
	})

	t.Run("diverged branches fail ff-only merge", func(t *testing.T) {
		repo := setupRepo(t)
		gitRun(t, repo, "checkout", "-b", "task")
		writeFile(t, filepath.Join(repo, "task.txt"), "task\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "task commit")

		gitRun(t, repo, "checkout", "main")
		writeFile(t, filepath.Join(repo, "other.txt"), "other\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "diverging main commit")

		if err := FFMerge(repo, "task"); err == nil {
			t.Error("expected error for non-ff merge, got nil")
		}
	})
}
