package gitutil

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
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

func TestMergeBase(t *testing.T) {
	t.Run("returns correct ancestor for diverged branches", func(t *testing.T) {
		repo := setupRepo(t)
		forkPoint := gitRun(t, repo, "rev-parse", "HEAD")

		// Create worktree with a task branch.
		wtDir := filepath.Join(t.TempDir(), "wt")
		gitRun(t, repo, "worktree", "add", "-b", "task", wtDir, "HEAD")
		t.Cleanup(func() { RemoveWorktree(repo, wtDir, "task") })

		// Advance main.
		writeFile(t, filepath.Join(repo, "m.txt"), "main\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "main advance")

		// Advance task branch.
		writeFile(t, filepath.Join(wtDir, "t.txt"), "task\n")
		gitRun(t, wtDir, "add", ".")
		gitRun(t, wtDir, "commit", "-m", "task advance")

		base, err := MergeBase(wtDir, "HEAD", "main")
		if err != nil {
			t.Fatalf("MergeBase: %v", err)
		}
		if base != forkPoint {
			t.Errorf("MergeBase = %s, want %s", base, forkPoint)
		}
	})

	t.Run("returns error for invalid refs", func(t *testing.T) {
		repo := setupRepo(t)
		_, err := MergeBase(repo, "HEAD", "nonexistent-branch")
		if err == nil {
			t.Error("expected error for invalid ref, got nil")
		}
	})
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

func TestBranchTipCommit(t *testing.T) {
	t.Run("returns hash, subject, and timestamp for existing branch", func(t *testing.T) {
		repo := setupRepo(t)
		hash, subject, ts, err := BranchTipCommit(repo, "main")
		if err != nil {
			t.Fatalf("BranchTipCommit: %v", err)
		}
		if len(hash) != 40 {
			t.Errorf("hash length = %d, want 40", len(hash))
		}
		if subject != "initial commit" {
			t.Errorf("subject = %q, want %q", subject, "initial commit")
		}
		if ts.IsZero() {
			t.Error("timestamp is zero")
		}
	})

	t.Run("timestamp reflects explicit commit date", func(t *testing.T) {
		repo := setupRepo(t)
		fixedDate := "2020-06-15T12:00:00+00:00"
		commitCmd := exec.Command("git", "-C", repo, "commit", "--allow-empty", "-m", "dated commit")
		commitCmd.Env = append(os.Environ(),
			"GIT_AUTHOR_DATE="+fixedDate,
			"GIT_COMMITTER_DATE="+fixedDate,
		)
		if out, err := commitCmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit: %v\n%s", err, out)
		}
		_, _, ts, err := BranchTipCommit(repo, "main")
		if err != nil {
			t.Fatalf("BranchTipCommit: %v", err)
		}
		want, _ := time.Parse(time.RFC3339, fixedDate)
		if !ts.Equal(want) {
			t.Errorf("ts = %v, want %v", ts, want)
		}
	})

	t.Run("returns error for nonexistent branch", func(t *testing.T) {
		repo := setupRepo(t)
		_, _, _, err := BranchTipCommit(repo, "nonexistent-branch")
		if err == nil {
			t.Error("expected error for nonexistent branch, got nil")
		}
	})

	t.Run("returns error for non-git path", func(t *testing.T) {
		_, _, _, err := BranchTipCommit(t.TempDir(), "main")
		if err == nil {
			t.Error("expected error for non-git path, got nil")
		}
	})
}
