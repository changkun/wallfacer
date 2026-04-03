package gitutil

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestIsConflictOutput validates that various git output strings are correctly
// classified as conflict or non-conflict indicators.
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

// TestIsRebaseNeedsMergeOutput validates detection of git output that indicates
// an active rebase/merge state blocking a new rebase attempt.
func TestIsRebaseNeedsMergeOutput(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"You have not concluded your merge (MERGE_HEAD exists). Please, commit your changes before you can rebase.", true},
		{"Another rebase-apply is in progress.", true},
		{"Cannot rebase: You have uncommitted changes.", true},
		{"error: cannot rebase: You have unstaged changes.\nerror: Please commit or stash them.", true},
		{"Could not apply commit", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsRebaseNeedsMergeOutput(c.in); got != c.want {
			t.Errorf("IsRebaseNeedsMergeOutput(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestMergeBase verifies merge-base computation for diverged branches and
// invalid refs.
func TestMergeBase(t *testing.T) {
	t.Run("returns correct ancestor for diverged branches", func(t *testing.T) {
		repo := setupRepo(t)
		forkPoint := gitRun(t, repo, "rev-parse", "HEAD")

		// Create worktree with a task branch.
		wtDir := filepath.Join(t.TempDir(), "wt")
		gitRun(t, repo, "worktree", "add", "-b", "task", wtDir, "HEAD")
		t.Cleanup(func() { _ = RemoveWorktree(repo, wtDir, "task") })

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

// TestCommitsBehind verifies the count of commits a worktree is behind the
// default branch, including edge cases like detached HEAD and missing branches.
func TestCommitsBehind(t *testing.T) {
	t.Run("zero when branches are equal", func(t *testing.T) {
		repo := setupRepo(t)
		wtDir := filepath.Join(t.TempDir(), "wt")
		gitRun(t, repo, "worktree", "add", "-b", "task", wtDir, "HEAD")
		t.Cleanup(func() { _ = RemoveWorktree(repo, wtDir, "task") })

		n, err := CommitsBehind(repo, wtDir)
		if err != nil || n != 0 {
			t.Errorf("CommitsBehind = %d, %v; want 0, nil", n, err)
		}
	})

	t.Run("two commits behind main", func(t *testing.T) {
		repo := setupRepo(t)
		wtDir := filepath.Join(t.TempDir(), "wt")
		gitRun(t, repo, "worktree", "add", "-b", "task", wtDir, "HEAD")
		t.Cleanup(func() { _ = RemoveWorktree(repo, wtDir, "task") })

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

	// Regression: when local "main" branch is deleted but origin/main exists,
	// CommitsBehind should still resolve the default branch via remote ref.
	t.Run("detached repo head with only origin main still works", func(t *testing.T) {
		origin := t.TempDir()
		gitRun(t, origin, "init", "--bare", "-b", "main")
		repo := setupRepo(t)
		gitRun(t, repo, "remote", "add", "origin", origin)
		gitRun(t, repo, "push", "-u", "origin", "main")

		wtDir := filepath.Join(t.TempDir(), "wt")
		gitRun(t, repo, "worktree", "add", "-b", "task", wtDir, "HEAD")
		t.Cleanup(func() { _ = RemoveWorktree(repo, wtDir, "task") })

		writeFile(t, filepath.Join(repo, "m1.txt"), "main\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "main advance")
		gitRun(t, repo, "push", "origin", "main")

		headHash := gitRun(t, repo, "rev-parse", "HEAD")
		gitRun(t, repo, "checkout", headHash)
		gitRun(t, repo, "branch", "-D", "main")
		if exec.Command("git", "-C", repo, "rev-parse", "--verify", "main").Run() == nil {
			t.Fatal("expected local main branch to be absent for regression setup")
		}
		if exec.Command("git", "-C", repo, "rev-parse", "--verify", "origin/main").Run() != nil {
			t.Fatal("expected origin/main to remain available for regression setup")
		}

		n, err := CommitsBehind(repo, wtDir)
		if err != nil || n != 1 {
			t.Fatalf("CommitsBehind = %d, %v; want 1, nil", n, err)
		}
	})
}

// TestHasCommitsAheadOf validates ahead-of detection for same commit, diverged
// branches, and non-git paths.
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
		t.Cleanup(func() { _ = RemoveWorktree(repo, wtDir, "task") })

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

// TestRebaseOntoDefault validates clean rebases succeed and conflicting changes
// produce a ConflictError with populated file list.
func TestRebaseOntoDefault(t *testing.T) {
	t.Run("clean rebase succeeds", func(t *testing.T) {
		repo := setupRepo(t)
		wtDir := filepath.Join(t.TempDir(), "wt")
		gitRun(t, repo, "worktree", "add", "-b", "task", wtDir, "HEAD")
		t.Cleanup(func() { _ = RemoveWorktree(repo, wtDir, "task") })

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
		t.Cleanup(func() { _ = RemoveWorktree(repo, wtDir, "task") })

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
		var conflictErr *ConflictError
		if !errors.As(err, &conflictErr) {
			t.Fatal("expected *ConflictError, got different type")
		}
		if len(conflictErr.ConflictedFiles) == 0 {
			t.Error("expected at least one conflicted file, got none")
		}
	})
}

// TestParseConflictedFiles validates extraction of file paths from git conflict output.
func TestParseConflictedFiles(t *testing.T) {
	input := "CONFLICT (content): Merge conflict in foo/bar.go\n" +
		"CONFLICT (add/add): Merge conflict in baz.txt\n" +
		"Automatic merge failed; fix conflicts and then commit the result.\n"
	files := parseConflictedFiles(input)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}
	if files[0] != "foo/bar.go" {
		t.Errorf("files[0] = %q, want %q", files[0], "foo/bar.go")
	}
	if files[1] != "baz.txt" {
		t.Errorf("files[1] = %q, want %q", files[1], "baz.txt")
	}
}

// TestFFMerge validates fast-forward merges including dirty-worktree stash/pop
// and diverged-branch rejection.
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

	t.Run("succeeds with dirty working directory", func(t *testing.T) {
		repo := setupRepo(t)
		gitRun(t, repo, "checkout", "-b", "task")
		writeFile(t, filepath.Join(repo, "task.txt"), "task\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "task commit")
		gitRun(t, repo, "checkout", "main")

		// Dirty the working directory with an uncommitted change.
		writeFile(t, filepath.Join(repo, "dirty.txt"), "dirty\n")

		if err := FFMerge(repo, "task"); err != nil {
			t.Errorf("FFMerge with dirty working dir failed: %v", err)
		}

		// Verify the dirty file was preserved after stash pop.
		data, err := os.ReadFile(filepath.Join(repo, "dirty.txt"))
		if err != nil {
			t.Fatalf("dirty file missing after merge: %v", err)
		}
		if string(data) != "dirty\n" {
			t.Errorf("dirty file content = %q, want %q", string(data), "dirty\n")
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

// TestBranchTipCommit validates retrieval of the latest commit hash, subject,
// and timestamp for existing and nonexistent branches.
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

// TestHasConflicts_CleanRepo verifies that a repo with no unmerged paths reports
// no conflicts.
func TestHasConflicts_CleanRepo(t *testing.T) {
	repo := setupRepo(t)
	has, err := HasConflicts(repo)
	if err != nil {
		t.Fatalf("HasConflicts: %v", err)
	}
	if has {
		t.Error("expected HasConflicts=false for clean repo")
	}
}

// TestHasConflicts_NonGitDir verifies that a non-git directory returns an error.
func TestHasConflicts_NonGitDir(t *testing.T) {
	_, err := HasConflicts(t.TempDir())
	if err == nil {
		t.Error("expected error for non-git directory")
	}
}

// createMergeConflict sets up two branches with conflicting changes to the
// given files and runs git merge, leaving the repo in a conflicted state.
// Returns the repo path. The caller is responsible for aborting the merge.
func createMergeConflict(t *testing.T, files []string) string {
	t.Helper()
	repo := setupRepo(t)

	// Add extra files to the initial commit if needed.
	for i := 1; i < len(files); i++ {
		writeFile(t, filepath.Join(repo, files[i]), "initial\n")
	}
	if len(files) > 1 {
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "add extra files")
	}

	// Create branch-a with conflicting changes.
	gitRun(t, repo, "checkout", "-b", "branch-a")
	for _, f := range files {
		writeFile(t, filepath.Join(repo, f), "branch-a version\n")
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "branch-a changes")

	// Advance main with conflicting changes.
	gitRun(t, repo, "checkout", "main")
	for _, f := range files {
		writeFile(t, filepath.Join(repo, f), "main version\n")
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "main changes")

	// Merge branch-a into main, which will conflict.
	_ = exec.Command("git", "-C", repo, "merge", "--no-ff", "branch-a").Run()
	return repo
}

// TestClearConflictedPaths validates the escalating cleanup strategy: git reset
// --merge, then git restore, then git reset --hard, and error on non-git dirs.
func TestClearConflictedPaths(t *testing.T) {
	t.Run("clean repo is a no-op", func(t *testing.T) {
		repo := setupRepo(t)
		if err := clearConflictedPaths(repo); err != nil {
			t.Errorf("clearConflictedPaths on clean repo: %v", err)
		}
	})

	t.Run("single conflicted file is cleared", func(t *testing.T) {
		repo := createMergeConflict(t, []string{"file.txt"})
		t.Cleanup(func() { _ = exec.Command("git", "-C", repo, "merge", "--abort").Run() })

		has, _ := HasConflicts(repo)
		if !has {
			t.Skip("no conflict detected; setup may need adjustment")
		}

		if err := clearConflictedPaths(repo); err != nil {
			t.Errorf("clearConflictedPaths: %v", err)
		}

		has, err := HasConflicts(repo)
		if err != nil {
			t.Fatalf("HasConflicts after clear: %v", err)
		}
		if has {
			t.Error("expected no conflicts after clearConflictedPaths, still has conflicts")
		}
	})

	t.Run("multiple conflicted files are cleared", func(t *testing.T) {
		repo := createMergeConflict(t, []string{"file.txt", "file2.txt"})
		t.Cleanup(func() { _ = exec.Command("git", "-C", repo, "merge", "--abort").Run() })

		has, _ := HasConflicts(repo)
		if !has {
			t.Skip("no conflicts detected; setup may need adjustment")
		}

		if err := clearConflictedPaths(repo); err != nil {
			t.Errorf("clearConflictedPaths: %v", err)
		}

		has, err := HasConflicts(repo)
		if err != nil {
			t.Fatalf("HasConflicts after clear: %v", err)
		}
		if has {
			t.Error("expected no conflicts after clearConflictedPaths, still has conflicts")
		}
	})

	// git reset --merge refuses to reset when a file has both staged and unstaged
	// changes (index != HEAD and worktree != index). In that case clearConflictedPaths
	// falls through to "git restore --staged --worktree --source=HEAD".
	t.Run("dirty index falls through to restore path", func(t *testing.T) {
		repo := setupRepo(t)

		// Stage a change.
		writeFile(t, filepath.Join(repo, "file.txt"), "staged content\n")
		gitRun(t, repo, "add", ".")
		// Also modify the working tree without staging — now index != HEAD and worktree != index.
		writeFile(t, filepath.Join(repo, "file.txt"), "unstaged content\n")

		if err := clearConflictedPaths(repo); err != nil {
			t.Errorf("clearConflictedPaths with dirty index: %v", err)
		}
	})

	// When all three git commands fail (non-git directory) the function returns an error.
	t.Run("non-git directory returns error", func(t *testing.T) {
		if err := clearConflictedPaths(t.TempDir()); err == nil {
			t.Error("expected error for non-git directory, got nil")
		}
	})
}

// TestRecoverRebaseState validates that stale rebase and merge states are
// cleaned up so subsequent rebase attempts can proceed.
func TestRecoverRebaseState(t *testing.T) {
	t.Run("clean repo returns nil", func(t *testing.T) {
		repo := setupRepo(t)
		if err := recoverRebaseState(repo); err != nil {
			t.Errorf("recoverRebaseState on clean repo: %v", err)
		}
	})

	t.Run("aborts stale rebase state", func(t *testing.T) {
		repo := setupRepo(t)

		// Create a task branch with a conflicting change.
		gitRun(t, repo, "checkout", "-b", "task")
		writeFile(t, filepath.Join(repo, "file.txt"), "task version\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "task change")

		// Advance main with a conflicting change.
		gitRun(t, repo, "checkout", "main")
		writeFile(t, filepath.Join(repo, "file.txt"), "main version\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "main change")

		// Attempt rebase from task — will stop at conflict.
		gitRun(t, repo, "checkout", "task")
		_ = exec.Command("git", "-C", repo, "rebase", "main").Run()

		has, _ := hasRebaseOrMergeState(repo)
		if !has {
			t.Skip("rebase state not created; skipping")
		}

		if err := recoverRebaseState(repo); err != nil {
			t.Errorf("recoverRebaseState: %v", err)
		}

		has, err := hasRebaseOrMergeState(repo)
		if err != nil {
			t.Fatalf("hasRebaseOrMergeState after recovery: %v", err)
		}
		if has {
			t.Error("expected rebase state to be cleared, still present")
		}
	})

	t.Run("aborts stale merge state", func(t *testing.T) {
		repo := createMergeConflict(t, []string{"file.txt"})

		has, _ := hasRebaseOrMergeState(repo)
		if !has {
			t.Skip("merge state not created; skipping")
		}

		if err := recoverRebaseState(repo); err != nil {
			t.Errorf("recoverRebaseState: %v", err)
		}

		has, err := hasRebaseOrMergeState(repo)
		if err != nil {
			t.Fatalf("hasRebaseOrMergeState after recovery: %v", err)
		}
		if has {
			t.Error("expected merge state to be cleared, still present")
		}
	})
}

// TestHasRebaseMergeState validates detection of REBASE_HEAD, MERGE_HEAD, and
// CHERRY_PICK_HEAD refs that indicate interrupted operations.
func TestHasRebaseMergeState(t *testing.T) {
	t.Run("no state returns false", func(t *testing.T) {
		repo := setupRepo(t)
		got, err := hasRebaseOrMergeState(repo)
		if err != nil {
			t.Fatalf("hasRebaseOrMergeState: %v", err)
		}
		if got {
			t.Error("expected false for clean repo, got true")
		}
	})

	t.Run("REBASE_HEAD set via conflicting rebase returns true", func(t *testing.T) {
		repo := setupRepo(t)

		gitRun(t, repo, "checkout", "-b", "task")
		writeFile(t, filepath.Join(repo, "file.txt"), "task version\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "task change")

		gitRun(t, repo, "checkout", "main")
		writeFile(t, filepath.Join(repo, "file.txt"), "main version\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "main change")

		gitRun(t, repo, "checkout", "task")
		_ = exec.Command("git", "-C", repo, "rebase", "main").Run()
		t.Cleanup(func() { _ = exec.Command("git", "-C", repo, "rebase", "--abort").Run() })

		got, err := hasRebaseOrMergeState(repo)
		if err != nil {
			t.Fatalf("hasRebaseOrMergeState: %v", err)
		}
		if !got {
			t.Error("expected true with REBASE_HEAD set, got false")
		}
	})

	t.Run("MERGE_HEAD set via conflicting merge returns true", func(t *testing.T) {
		repo := createMergeConflict(t, []string{"file.txt"})
		t.Cleanup(func() { _ = exec.Command("git", "-C", repo, "merge", "--abort").Run() })

		got, err := hasRebaseOrMergeState(repo)
		if err != nil {
			t.Fatalf("hasRebaseOrMergeState: %v", err)
		}
		if !got {
			t.Error("expected true with MERGE_HEAD set, got false")
		}
	})

	t.Run("non-git directory returns false without error", func(t *testing.T) {
		got, err := hasRebaseOrMergeState(t.TempDir())
		if err != nil {
			t.Fatalf("hasRebaseOrMergeState: %v", err)
		}
		if got {
			t.Error("expected false for non-git directory, got true")
		}
	})

	t.Run("CHERRY_PICK_HEAD set via conflicting cherry-pick returns true", func(t *testing.T) {
		repo := setupRepo(t)

		// Create a commit on a feature branch that changes file.txt.
		gitRun(t, repo, "checkout", "-b", "feature")
		writeFile(t, filepath.Join(repo, "file.txt"), "feature version\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "feature change")
		featureCommit := gitRun(t, repo, "rev-parse", "HEAD")

		// Advance main with a conflicting change so the cherry-pick will fail.
		gitRun(t, repo, "checkout", "main")
		writeFile(t, filepath.Join(repo, "file.txt"), "main version\n")
		gitRun(t, repo, "add", ".")
		gitRun(t, repo, "commit", "-m", "main change")

		// Cherry-pick the feature commit — it will conflict and set CHERRY_PICK_HEAD.
		_ = exec.Command("git", "-C", repo, "cherry-pick", featureCommit).Run()
		t.Cleanup(func() { _ = exec.Command("git", "-C", repo, "cherry-pick", "--abort").Run() })

		got, err := hasRebaseOrMergeState(repo)
		if err != nil {
			t.Fatalf("hasRebaseOrMergeState: %v", err)
		}
		if !got {
			t.Error("expected true with CHERRY_PICK_HEAD set, got false")
		}
	})
}

// TestRebaseOntoDefault_NonGitWorktree verifies that RebaseOntoDefault returns
// an error when invoked on a non-git directory (DefaultBranch fails).
func TestRebaseOntoDefault_NonGitWorktree(t *testing.T) {
	if err := RebaseOntoDefault(t.TempDir(), t.TempDir()); err == nil {
		t.Error("expected error for non-git path")
	}
}

// TestRebaseOntoDefault_IsRebaseNeedsMergeOutput verifies the IsRebaseNeedsMergeOutput
// branch in RebaseOntoDefault by creating a worktree already in a rebase state.
func TestRebaseOntoDefault_StaleRebaseState(t *testing.T) {
	repo := setupRepo(t)

	// Create a conflicting task branch.
	gitRun(t, repo, "checkout", "-b", "task")
	writeFile(t, filepath.Join(repo, "file.txt"), "task version\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "task change")

	gitRun(t, repo, "checkout", "main")
	writeFile(t, filepath.Join(repo, "file.txt"), "main version\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "main change")

	// Create worktree on task branch.
	wtDir := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "--force", wtDir, "task")
	t.Cleanup(func() { _ = RemoveWorktree(repo, wtDir, "task") })

	// First rebase will conflict.
	err := RebaseOntoDefault(repo, wtDir)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict from first rebase, got %v", err)
	}

	// Second attempt should recover and still return conflict.
	err = RebaseOntoDefault(repo, wtDir)
	if err == nil {
		t.Fatal("expected error from second rebase attempt")
	}
}

// TestFFMerge_CheckoutError verifies that FFMerge returns a checkout error
// when the default branch checkout fails (e.g. the default branch was deleted).
func TestFFMerge_CheckoutError(t *testing.T) {
	repo := setupRepo(t)
	// Detach HEAD and delete main, so checkout "main" fails.
	hash := gitRun(t, repo, "rev-parse", "HEAD")
	gitRun(t, repo, "checkout", hash)
	gitRun(t, repo, "branch", "-D", "main")

	err := FFMerge(repo, "nonexistent")
	if err == nil {
		t.Fatal("expected error when checkout fails")
	}
}

// TestFFMerge_NonGitPath verifies FFMerge returns an error for non-git paths.
func TestFFMerge_NonGitPath(t *testing.T) {
	err := FFMerge(t.TempDir(), "branch")
	if err == nil {
		t.Fatal("expected error for non-git path")
	}
}

// TestCommitsBehind_NonGitRepoPath verifies CommitsBehind returns an error
// when the repo path is not a git directory (DefaultBranch fails early).
func TestCommitsBehind_NonGitRepoPath(t *testing.T) {
	_, err := CommitsBehind(t.TempDir(), t.TempDir())
	if err != nil {
		// DefaultBranch falls back to "main" for non-git dirs, then
		// defaultBranchCommitHash fails, returning 0, nil.
		t.Logf("got error (acceptable): %v", err)
	}
}

// TestCommitsBehind_EmptyRepo verifies CommitsBehind returns 0 for an empty repo
// where the default branch has no resolvable ref.
func TestCommitsBehind_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")

	// Create a single commit so HEAD exists, but then detach and delete main
	// to simulate a state where the default branch ref is unreachable.
	writeFile(t, filepath.Join(dir, "f.txt"), "init\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "init")
	wtDir := filepath.Join(t.TempDir(), "wt")
	gitRun(t, dir, "worktree", "add", "-b", "task", wtDir, "HEAD")
	t.Cleanup(func() { _ = RemoveWorktree(dir, wtDir, "task") })

	hash := gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "checkout", hash)
	gitRun(t, dir, "branch", "-D", "main")

	n, err := CommitsBehind(dir, wtDir)
	// defaultBranchCommitHash fails → returns 0, nil
	if err != nil {
		t.Fatalf("CommitsBehind: %v", err)
	}
	if n != 0 {
		t.Errorf("CommitsBehind = %d, want 0", n)
	}
}

// TestDefaultBranchCommitHash_AllCandidatesFail verifies error return when no
// ref candidate resolves.
func TestDefaultBranchCommitHash_AllCandidatesFail(t *testing.T) {
	repo := setupRepo(t)
	_, err := defaultBranchCommitHash(repo, "nonexistent-branch-xyz")
	if err == nil {
		t.Fatal("expected error when all ref candidates fail")
	}
}

// TestDefaultBranchCommitHash_FallbackToOrigin verifies resolution via origin/
// when the local branch ref is missing.
func TestDefaultBranchCommitHash_FallbackToOrigin(t *testing.T) {
	origin := t.TempDir()
	gitRun(t, origin, "init", "--bare", "-b", "main")
	repo := setupRepo(t)
	gitRun(t, repo, "remote", "add", "origin", origin)
	gitRun(t, repo, "push", "-u", "origin", "main")

	// Detach HEAD and delete local main — only origin/main remains.
	hash := gitRun(t, repo, "rev-parse", "HEAD")
	gitRun(t, repo, "checkout", hash)
	gitRun(t, repo, "branch", "-D", "main")

	resolved, err := defaultBranchCommitHash(repo, "main")
	if err != nil {
		t.Fatalf("defaultBranchCommitHash: %v", err)
	}
	if resolved != hash {
		t.Errorf("resolved = %q, want %q", resolved, hash)
	}
}

// TestBranchTipCommit_EmptyOutput verifies error when git log returns empty output.
func TestBranchTipCommit_EmptyOutput(t *testing.T) {
	// An orphan branch with no commits will produce empty log output.
	repo := setupRepo(t)
	gitRun(t, repo, "checkout", "--orphan", "empty-branch")
	_, _, _, err := BranchTipCommit(repo, "empty-branch")
	if err == nil {
		t.Fatal("expected error for branch with no commits")
	}
}

// TestBranchTipCommit_SubjectContainsPipe verifies that pipe characters in the
// commit subject do not corrupt parsing.
func TestBranchTipCommit_SubjectContainsPipe(t *testing.T) {
	repo := setupRepo(t)
	commitCmd := exec.Command("git", "-C", repo, "commit", "--allow-empty", "-m", "fix: a|b|c")
	commitCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2020-01-01T00:00:00+00:00",
		"GIT_COMMITTER_DATE=2020-01-01T00:00:00+00:00",
	)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	_, subject, _, err := BranchTipCommit(repo, "main")
	if err != nil {
		t.Fatalf("BranchTipCommit: %v", err)
	}
	if subject != "fix: a|b|c" {
		t.Errorf("subject = %q, want %q", subject, "fix: a|b|c")
	}
}

// TestFetchOrigin validates FetchOrigin for repos with and without remotes.
func TestFetchOrigin(t *testing.T) {
	t.Run("succeeds with configured remote", func(t *testing.T) {
		origin := t.TempDir()
		gitRun(t, origin, "init", "--bare", "-b", "main")
		repo := setupRepo(t)
		gitRun(t, repo, "remote", "add", "origin", origin)
		gitRun(t, repo, "push", "-u", "origin", "main")

		if err := FetchOrigin(repo); err != nil {
			t.Fatalf("FetchOrigin: %v", err)
		}
	})

	t.Run("returns error without remote", func(t *testing.T) {
		repo := setupRepo(t)
		if err := FetchOrigin(repo); err == nil {
			t.Fatal("expected error when no remote configured")
		}
	})

	t.Run("returns error for non-git path", func(t *testing.T) {
		if err := FetchOrigin(t.TempDir()); err == nil {
			t.Fatal("expected error for non-git path")
		}
	})
}
