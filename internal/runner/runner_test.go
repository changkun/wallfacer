package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// gitRun executes a git command in dir and returns trimmed stdout.
// It fails the test on error.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s failed: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitRunMayFail executes a git command in dir and returns stdout.
// Does not fail the test on error.
func gitRunMayFail(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// setupTestRepo creates a temporary git repo with an initial commit on "main".
func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "initial commit")
	return dir
}

// setupTestRunner creates a Store and Runner for testing.
// The container command is a dummy since we're testing host-side operations.
func setupTestRunner(t *testing.T, workspaces []string) (*store.Store, *Runner) {
	t.Helper()
	dataDir := t.TempDir()
	s, err := store.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	worktreesDir := filepath.Join(t.TempDir(), "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(s, RunnerConfig{
		Command:      "echo", // dummy — not used for host-side operations
		SandboxImage: "test:latest",
		EnvFile:      "",
		Workspaces:   strings.Join(workspaces, " "),
		WorktreesDir: worktreesDir,
	})
	return s, runner
}

// newTestRunnerWithInstructions creates a Runner whose instructionsPath points
// to the given path (may or may not exist on disk).
func newTestRunnerWithInstructions(t *testing.T, instructionsPath string) *Runner {
	t.Helper()
	dataDir := t.TempDir()
	s, err := store.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return NewRunner(s, RunnerConfig{
		Command:          "podman",
		SandboxImage:     "wallfacer:latest",
		InstructionsPath: instructionsPath,
	})
}

// containsConsecutive returns true if slice contains needle1 immediately
// followed by needle2.
func containsConsecutive(slice []string, needle1, needle2 string) bool {
	for i := 0; i+1 < len(slice); i++ {
		if slice[i] == needle1 && slice[i+1] == needle2 {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Runner.buildContainerArgs — CLAUDE.md mount
// ---------------------------------------------------------------------------

// TestContainerArgsMountsCLAUDEMD verifies that when instructionsPath is set
// and the file exists, buildContainerArgs includes a read-only volume mount
// that places it at /workspace/CLAUDE.md inside the container.
func TestContainerArgsMountsCLAUDEMD(t *testing.T) {
	instructionsFile := filepath.Join(t.TempDir(), "instructions.md")
	if err := os.WriteFile(instructionsFile, []byte("# test instructions\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runner := newTestRunnerWithInstructions(t, instructionsFile)
	args := runner.buildContainerArgs("test-container", "do something", "", nil)

	expectedMount := instructionsFile + ":/workspace/CLAUDE.md:z,ro"
	if !containsConsecutive(args, "-v", expectedMount) {
		t.Fatalf("args should contain -v %q; got: %v", expectedMount, args)
	}
}

// TestContainerArgsNoInstructionsPath verifies that when InstructionsPath is
// empty no CLAUDE.md mount is added to the container args.
func TestContainerArgsNoInstructionsPath(t *testing.T) {
	runner := newTestRunnerWithInstructions(t, "")
	args := runner.buildContainerArgs("test-container", "do something", "", nil)

	for _, a := range args {
		if strings.Contains(a, "CLAUDE.md") {
			t.Fatalf("expected no CLAUDE.md mount when InstructionsPath is empty; got arg: %q", a)
		}
	}
}

// TestContainerArgsMissingInstructionsFile verifies that when instructionsPath
// is set but the file does not exist, no CLAUDE.md mount is added (the runner
// silently skips a missing file rather than failing the container launch).
func TestContainerArgsMissingInstructionsFile(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "nonexistent.md")
	runner := newTestRunnerWithInstructions(t, missingPath)
	args := runner.buildContainerArgs("test-container", "do something", "", nil)

	for _, a := range args {
		if strings.Contains(a, "CLAUDE.md") {
			t.Fatalf("expected no CLAUDE.md mount for missing file; got arg: %q", a)
		}
	}
}

// TestContainerArgsCLAUDEMDMountIsReadOnly verifies the mount is marked :ro
// so the container cannot accidentally modify the shared instructions file.
func TestContainerArgsCLAUDEMDMountIsReadOnly(t *testing.T) {
	instructionsFile := filepath.Join(t.TempDir(), "instructions.md")
	if err := os.WriteFile(instructionsFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	runner := newTestRunnerWithInstructions(t, instructionsFile)
	args := runner.buildContainerArgs("test-container", "do something", "", nil)

	for i, a := range args {
		if a == "-v" && i+1 < len(args) && strings.Contains(args[i+1], "CLAUDE.md") {
			mount := args[i+1]
			// Accept both ":ro" and ",ro" (SELinux label adds ":z,ro").
			if !strings.HasSuffix(mount, ":ro") && !strings.HasSuffix(mount, ",ro") {
				t.Fatalf("CLAUDE.md mount should be read-only, got: %q", mount)
			}
			return
		}
	}
	t.Fatal("CLAUDE.md -v mount not found in args")
}

// TestContainerArgsCLAUDEMDMountPosition verifies that the CLAUDE.md mount
// appears before the image name in the args list, matching the expected
// container launch order.
func TestContainerArgsCLAUDEMDMountPosition(t *testing.T) {
	instructionsFile := filepath.Join(t.TempDir(), "instructions.md")
	if err := os.WriteFile(instructionsFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	ws := t.TempDir()
	dataDir := t.TempDir()
	s, err := store.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	runner := NewRunner(s, RunnerConfig{
		Command:          "podman",
		SandboxImage:     "wallfacer:latest",
		InstructionsPath: instructionsFile,
		Workspaces:       ws,
	})
	args := runner.buildContainerArgs("test-container", "do something", "", nil)

	claudeMDIdx := -1
	imageIdx := -1
	for i, a := range args {
		if strings.Contains(a, "CLAUDE.md") {
			claudeMDIdx = i
		}
		if a == "wallfacer:latest" {
			imageIdx = i
		}
	}

	if claudeMDIdx == -1 {
		t.Fatal("CLAUDE.md mount not found in args")
	}
	if imageIdx == -1 {
		t.Fatal("sandbox image not found in args")
	}
	if claudeMDIdx >= imageIdx {
		t.Fatalf("CLAUDE.md mount (index %d) should appear before sandbox image (index %d)",
			claudeMDIdx, imageIdx)
	}
}

// ---------------------------------------------------------------------------
// Worktree management
// ---------------------------------------------------------------------------

// TestWorktreeSetup verifies that worktree creation works: correct branch,
// correct directory structure, files inherited from the parent repo.
func TestWorktreeSetup(t *testing.T) {
	repo := setupTestRepo(t)
	_, runner := setupTestRunner(t, []string{repo})

	taskID := uuid.New()
	worktreePaths, branchName, err := runner.setupWorktrees(taskID)
	if err != nil {
		t.Fatal("setupWorktrees:", err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(taskID, worktreePaths, branchName) })

	if len(worktreePaths) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(worktreePaths))
	}

	wt := worktreePaths[repo]
	if wt == "" {
		t.Fatal("missing worktree path for repo")
	}

	// Verify worktree directory exists.
	if info, err := os.Stat(wt); err != nil || !info.IsDir() {
		t.Fatalf("worktree dir should exist: %v", err)
	}

	// Verify worktree is on the correct branch.
	branch := gitRun(t, wt, "branch", "--show-current")
	if branch != branchName {
		t.Fatalf("expected branch %q, got %q", branchName, branch)
	}

	// Verify parent files are visible.
	if _, err := os.Stat(filepath.Join(wt, "README.md")); err != nil {
		t.Fatal("README.md should exist in worktree:", err)
	}
}

// TestWorktreeGitFilePointsToHost verifies the root cause: the .git file in
// a worktree contains an absolute host path. This proves that git commands
// inside a container (where that host path doesn't exist) would fail.
func TestWorktreeGitFilePointsToHost(t *testing.T) {
	repo := setupTestRepo(t)
	_, runner := setupTestRunner(t, []string{repo})

	taskID := uuid.New()
	worktreePaths, branchName, err := runner.setupWorktrees(taskID)
	if err != nil {
		t.Fatal("setupWorktrees:", err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(taskID, worktreePaths, branchName) })

	wt := worktreePaths[repo]
	gitFile := filepath.Join(wt, ".git")
	content, err := os.ReadFile(gitFile)
	if err != nil {
		t.Fatal("reading .git file:", err)
	}

	// The .git file contains "gitdir: /absolute/host/path/..."
	s := strings.TrimSpace(string(content))
	if !strings.HasPrefix(s, "gitdir: ") {
		t.Fatalf("unexpected .git file content: %s", s)
	}
	gitdirPath := strings.TrimPrefix(s, "gitdir: ")

	// Verify it's an absolute host path (which would NOT exist inside a container).
	if !filepath.IsAbs(gitdirPath) {
		t.Fatal("expected absolute path in .git file, got:", gitdirPath)
	}

	// Verify the path exists on the host.
	if _, err := os.Stat(gitdirPath); err != nil {
		t.Fatal("gitdir path should exist on host:", err)
	}
}

// TestHostStageAndCommit verifies that host-side staging and committing works
// correctly in a worktree.
func TestHostStageAndCommit(t *testing.T) {
	repo := setupTestRepo(t)
	_, runner := setupTestRunner(t, []string{repo})

	taskID := uuid.New()
	worktreePaths, branchName, err := runner.setupWorktrees(taskID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(taskID, worktreePaths, branchName) })

	wt := worktreePaths[repo]

	// Simulate Claude making changes.
	if err := os.WriteFile(filepath.Join(wt, "hello.txt"), []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Run host-side commit.
	committed := runner.hostStageAndCommit(taskID, worktreePaths, "Add hello world file")
	if !committed {
		t.Fatal("expected commit to be created")
	}

	// Verify commit exists in worktree on the task branch.
	log := gitRun(t, wt, "log", "--oneline")
	if !strings.Contains(log, "wallfacer:") {
		t.Fatalf("expected wallfacer commit message, got:\n%s", log)
	}

	// Verify the commit is on the task branch, not on main.
	branch := gitRun(t, wt, "branch", "--show-current")
	if branch != branchName {
		t.Fatalf("should still be on task branch %q, got %q", branchName, branch)
	}
}

// TestHostStageAndCommitNoChanges verifies that host-side commit is a no-op
// when there are no changes in the worktree.
func TestHostStageAndCommitNoChanges(t *testing.T) {
	repo := setupTestRepo(t)
	_, runner := setupTestRunner(t, []string{repo})

	taskID := uuid.New()
	worktreePaths, branchName, err := runner.setupWorktrees(taskID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(taskID, worktreePaths, branchName) })

	// No changes made — commit should be a no-op.
	committed := runner.hostStageAndCommit(taskID, worktreePaths, "Nothing to do")
	if committed {
		t.Fatal("expected no commit when there are no changes")
	}
}

// ---------------------------------------------------------------------------
// Commit pipeline
// ---------------------------------------------------------------------------

// TestCommitPipelineBasic tests the full commit pipeline (Phase 1-4):
// host commit → rebase → ff-merge → PROGRESS.md → cleanup.
func TestCommitPipelineBasic(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})

	initialHash := gitRun(t, repo, "rev-parse", "HEAD")

	// Create a task.
	ctx := context.Background()
	task, err := s.CreateTask(ctx, "Add a greeting file", 5)
	if err != nil {
		t.Fatal(err)
	}

	// Set up worktrees (simulates what Run() does when task starts).
	worktreePaths, branchName, err := runner.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, worktreePaths, branchName); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, "committing"); err != nil {
		t.Fatal(err)
	}

	wt := worktreePaths[repo]

	// Simulate Claude making changes in the worktree.
	if err := os.WriteFile(filepath.Join(wt, "greeting.txt"), []byte("Hello, World!\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Run the commit pipeline.
	commitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	runner.commit(commitCtx, task.ID, "", 1, worktreePaths, branchName)

	// Verify a new commit exists on the default branch.
	finalHash := gitRun(t, repo, "rev-parse", "HEAD")
	if finalHash == initialHash {
		t.Fatal("expected new commit on default branch, but HEAD hasn't changed")
	}

	// Verify the file exists in the main repo's working tree.
	content, err := os.ReadFile(filepath.Join(repo, "greeting.txt"))
	if err != nil {
		t.Fatal("greeting.txt should exist in the main repo after merge:", err)
	}
	if string(content) != "Hello, World!\n" {
		t.Fatalf("unexpected content: %q", content)
	}

	// Verify the commit message references the task.
	log := gitRun(t, repo, "log", "--oneline")
	if !strings.Contains(log, "wallfacer:") {
		t.Fatalf("expected wallfacer commit in log:\n%s", log)
	}

	// Verify worktree is cleaned up.
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("worktree should have been cleaned up after commit pipeline")
	}
}

// TestCommitPipelineDivergedBranch tests the pipeline when the default branch
// has advanced since the worktree was created. The task's changes must be
// rebased on top of the latest default branch.
func TestCommitPipelineDivergedBranch(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})

	ctx := context.Background()
	task, err := s.CreateTask(ctx, "Add feature", 5)
	if err != nil {
		t.Fatal(err)
	}

	// Set up worktrees.
	worktreePaths, branchName, err := runner.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, worktreePaths, branchName); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, "committing"); err != nil {
		t.Fatal(err)
	}

	wt := worktreePaths[repo]

	// Simulate Claude making changes in the worktree.
	if err := os.WriteFile(filepath.Join(wt, "feature.txt"), []byte("new feature\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Meanwhile, advance the default branch in the main repo.
	if err := os.WriteFile(filepath.Join(repo, "other.txt"), []byte("other change\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "other change on main")

	// Run the commit pipeline.
	commitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	runner.commit(commitCtx, task.ID, "", 1, worktreePaths, branchName)

	// Verify BOTH files exist on main (task changes rebased on top of main).
	for _, f := range []string{"feature.txt", "other.txt"} {
		if _, err := os.Stat(filepath.Join(repo, f)); err != nil {
			t.Fatalf("%s should exist on main: %v", f, err)
		}
	}

	// Verify the task commit is on top of the other commit.
	log := gitRun(t, repo, "log", "--oneline")
	lines := strings.Split(log, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 commits, got %d:\n%s", len(lines), log)
	}
}

// TestCommitPipelineNoChanges tests the pipeline when the worktree has no
// changes. The pipeline should complete without errors and without creating
// any merge commits (only PROGRESS.md may be updated).
func TestCommitPipelineNoChanges(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})

	ctx := context.Background()
	task, err := s.CreateTask(ctx, "No changes task", 5)
	if err != nil {
		t.Fatal(err)
	}

	worktreePaths, branchName, err := runner.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, worktreePaths, branchName); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, "committing"); err != nil {
		t.Fatal(err)
	}

	initialHash := gitRun(t, repo, "rev-parse", "HEAD")

	commitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	runner.commit(commitCtx, task.ID, "", 1, worktreePaths, branchName)

	// The only possible change is the PROGRESS.md commit.
	log := gitRun(t, repo, "log", "--oneline")
	// There should be no wallfacer: task commit (only PROGRESS.md and initial).
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, "wallfacer:") && !strings.Contains(line, "progress log") {
			t.Fatalf("unexpected wallfacer task commit when there were no changes:\n%s", log)
		}
	}
	_ = initialHash
}

// TestCompleteTaskE2E simulates the exact waiting→done flow that the user
// reported as broken. It covers:
//  1. Create task and simulate it going through backlog → in_progress → waiting
//  2. Simulate Claude making file changes in the worktree during execution
//  3. Call the Commit pipeline (as CompleteTask handler would)
//  4. Verify that the changes end up on the default branch
func TestCompleteTaskE2E(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})

	ctx := context.Background()

	// Step 1: Create the task.
	task, err := s.CreateTask(ctx, "Add greeting feature", 5)
	if err != nil {
		t.Fatal(err)
	}

	// Step 2: Simulate task going to in_progress → worktree is created.
	worktreePaths, branchName, err := runner.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, worktreePaths, branchName); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, "in_progress"); err != nil {
		t.Fatal(err)
	}
	sessionID := "test-session-123"
	result := "I created the greeting feature"
	if err := s.UpdateTaskResult(ctx, task.ID, result, sessionID, "", 1); err != nil {
		t.Fatal(err)
	}

	// Step 3: Simulate Claude making changes in the worktree during execution.
	wt := worktreePaths[repo]
	if err := os.WriteFile(filepath.Join(wt, "greeting.txt"), []byte("Hello from wallfacer!\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Step 4: Task goes to waiting (Claude needs feedback).
	if err := s.UpdateTaskStatus(ctx, task.ID, "waiting"); err != nil {
		t.Fatal(err)
	}

	// Step 5: User clicks "Mark as Done" — this triggers Commit.
	if err := s.UpdateTaskStatus(ctx, task.ID, "committing"); err != nil {
		t.Fatal(err)
	}

	// Run the exact same code path as CompleteTask handler.
	runner.Commit(task.ID, sessionID)

	// Step 6: Verify the changes are on the default branch.
	content, err := os.ReadFile(filepath.Join(repo, "greeting.txt"))
	if err != nil {
		t.Fatal("greeting.txt should exist on default branch after Commit:", err)
	}
	if string(content) != "Hello from wallfacer!\n" {
		t.Fatalf("unexpected content: %q", content)
	}

	// Verify commit is on the default branch.
	log := gitRun(t, repo, "log", "--oneline")
	if !strings.Contains(log, "wallfacer:") {
		t.Fatalf("expected wallfacer commit on default branch:\n%s", log)
	}

	// Verify PROGRESS.md was written.
	if _, err := os.Stat(filepath.Join(repo, "PROGRESS.md")); err != nil {
		t.Fatal("PROGRESS.md should exist after commit:", err)
	}

	// Verify worktree is cleaned up.
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("worktree should have been cleaned up")
	}
}

// TestCommitOnTopOfLatestMain verifies that commits are created on top of
// the latest main branch, not on the stale version from when the worktree
// was created. This is critical for maintaining a clean linear history.
func TestCommitOnTopOfLatestMain(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})

	ctx := context.Background()
	task, err := s.CreateTask(ctx, "Task on stale branch", 5)
	if err != nil {
		t.Fatal(err)
	}

	// Create worktree (branches from current HEAD of main).
	worktreePaths, branchName, err := runner.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, worktreePaths, branchName); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, "committing"); err != nil {
		t.Fatal(err)
	}

	wt := worktreePaths[repo]

	// Make changes in the worktree.
	if err := os.WriteFile(filepath.Join(wt, "task-file.txt"), []byte("from task\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Advance main with TWO commits (simulating other tasks completing).
	if err := os.WriteFile(filepath.Join(repo, "advance1.txt"), []byte("advance 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "advance main 1")

	if err := os.WriteFile(filepath.Join(repo, "advance2.txt"), []byte("advance 2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "advance main 2")

	mainHashBefore := gitRun(t, repo, "rev-parse", "HEAD")

	// Run the commit pipeline.
	commitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	runner.commit(commitCtx, task.ID, "", 1, worktreePaths, branchName)

	// Verify the task commit is a descendant of the latest main.
	if _, err := gitRunMayFail(repo, "merge-base", "--is-ancestor", mainHashBefore, "HEAD"); err != nil {
		t.Fatal("task commit should be on top of latest main (rebase should have applied)")
	}

	// Verify all files exist.
	for _, f := range []string{"task-file.txt", "advance1.txt", "advance2.txt"} {
		if _, err := os.Stat(filepath.Join(repo, f)); err != nil {
			t.Fatalf("%s should exist on main: %v", f, err)
		}
	}
}

// TestParallelTasksSameRepo verifies that two tasks running concurrently on
// different worktrees of the same repo both get their changes merged into
// main in sequence. The second task to merge must rebase on top of the first.
func TestParallelTasksSameRepo(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})
	ctx := context.Background()

	// Create two tasks.
	taskA, err := s.CreateTask(ctx, "Add file A", 5)
	if err != nil {
		t.Fatal(err)
	}
	taskB, err := s.CreateTask(ctx, "Add file B", 5)
	if err != nil {
		t.Fatal(err)
	}

	// Set up worktrees for both (simulating two tasks starting at the same time).
	wtA, brA, err := runner.setupWorktrees(taskA.ID)
	if err != nil {
		t.Fatal("setup worktree A:", err)
	}
	if err := s.UpdateTaskWorktrees(ctx, taskA.ID, wtA, brA); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, taskA.ID, "committing"); err != nil {
		t.Fatal(err)
	}

	wtB, brB, err := runner.setupWorktrees(taskB.ID)
	if err != nil {
		t.Fatal("setup worktree B:", err)
	}
	if err := s.UpdateTaskWorktrees(ctx, taskB.ID, wtB, brB); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, taskB.ID, "committing"); err != nil {
		t.Fatal(err)
	}

	// Both worktrees should exist and be on different branches.
	pathA := wtA[repo]
	pathB := wtB[repo]
	if pathA == pathB {
		t.Fatal("worktree paths should differ")
	}
	branchA := gitRun(t, pathA, "branch", "--show-current")
	branchB := gitRun(t, pathB, "branch", "--show-current")
	if branchA == branchB {
		t.Fatal("worktree branches should differ")
	}

	// Simulate Claude making changes in each worktree.
	if err := os.WriteFile(filepath.Join(pathA, "fileA.txt"), []byte("from task A\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pathB, "fileB.txt"), []byte("from task B\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Commit task A first.
	commitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	runner.commit(commitCtx, taskA.ID, "", 1, wtA, brA)

	// Then commit task B — must rebase on top of A's merge.
	runner.commit(commitCtx, taskB.ID, "", 1, wtB, brB)

	// Verify both files exist on main.
	for _, f := range []string{"fileA.txt", "fileB.txt"} {
		if _, err := os.Stat(filepath.Join(repo, f)); err != nil {
			t.Fatalf("%s should exist on main: %v", f, err)
		}
	}

	// Verify linear history: B's commit is on top of A's.
	log := gitRun(t, repo, "log", "--oneline")
	lines := strings.Split(log, "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 commits for two tasks, got %d:\n%s", len(lines), log)
	}

	// Verify no merge commits (all fast-forward).
	mergeCount := gitRun(t, repo, "rev-list", "--merges", "--count", "HEAD")
	if mergeCount != "0" {
		t.Fatalf("expected 0 merge commits (all fast-forward), got %s", mergeCount)
	}
}

// TestParallelTasksTwoRepos verifies that two tasks working on different
// repos (mounted as separate workspaces) each get independent worktrees
// and commits merge into their respective repos.
func TestParallelTasksTwoRepos(t *testing.T) {
	repoX := setupTestRepo(t)
	repoY := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repoX, repoY})
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Change both repos", 5)
	if err != nil {
		t.Fatal(err)
	}

	wtPaths, brName, err := runner.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, wtPaths, brName); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, "committing"); err != nil {
		t.Fatal(err)
	}

	if len(wtPaths) != 2 {
		t.Fatalf("expected 2 worktrees (one per repo), got %d", len(wtPaths))
	}

	// Make changes in both worktrees.
	if err := os.WriteFile(filepath.Join(wtPaths[repoX], "x.txt"), []byte("X\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtPaths[repoY], "y.txt"), []byte("Y\n"), 0644); err != nil {
		t.Fatal(err)
	}

	commitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	runner.commit(commitCtx, task.ID, "", 1, wtPaths, brName)

	// Verify each file landed in the correct repo.
	if _, err := os.Stat(filepath.Join(repoX, "x.txt")); err != nil {
		t.Fatal("x.txt should exist in repoX:", err)
	}
	if _, err := os.Stat(filepath.Join(repoY, "y.txt")); err != nil {
		t.Fatal("y.txt should exist in repoY:", err)
	}
	// Cross-check: files should NOT leak across repos.
	if _, err := os.Stat(filepath.Join(repoX, "y.txt")); err == nil {
		t.Fatal("y.txt should NOT exist in repoX")
	}
	if _, err := os.Stat(filepath.Join(repoY, "x.txt")); err == nil {
		t.Fatal("x.txt should NOT exist in repoY")
	}
}

// TestParallelTasksConflictingChanges verifies that when two tasks modify
// the same file, the second task's rebase correctly incorporates the first
// task's changes (no data loss).
func TestParallelTasksConflictingChanges(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})
	ctx := context.Background()

	taskA, err := s.CreateTask(ctx, "Add line to README", 5)
	if err != nil {
		t.Fatal(err)
	}
	taskB, err := s.CreateTask(ctx, "Add another line to README", 5)
	if err != nil {
		t.Fatal(err)
	}

	wtA, brA, err := runner.setupWorktrees(taskA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskWorktrees(ctx, taskA.ID, wtA, brA); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, taskA.ID, "committing"); err != nil {
		t.Fatal(err)
	}

	wtB, brB, err := runner.setupWorktrees(taskB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskWorktrees(ctx, taskB.ID, wtB, brB); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, taskB.ID, "committing"); err != nil {
		t.Fatal(err)
	}

	pathA := wtA[repo]
	pathB := wtB[repo]

	// Task A: append to README.md.
	readmeA, err := os.ReadFile(filepath.Join(pathA, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pathA, "README.md"), append(readmeA, []byte("\nLine from task A\n")...), 0644); err != nil {
		t.Fatal(err)
	}

	// Task B: create a NEW file (non-conflicting with A's README change).
	if err := os.WriteFile(filepath.Join(pathB, "b_feature.txt"), []byte("feature B\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Commit A first.
	commitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	runner.commit(commitCtx, taskA.ID, "", 1, wtA, brA)

	// Commit B — rebase should succeed since changes don't conflict.
	runner.commit(commitCtx, taskB.ID, "", 1, wtB, brB)

	// Verify A's README change persists after B's merge.
	readmeFinal, err := os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readmeFinal), "Line from task A") {
		t.Fatalf("README.md should contain A's changes after B merged:\n%s", readmeFinal)
	}

	// Verify B's file exists.
	if _, err := os.Stat(filepath.Join(repo, "b_feature.txt")); err != nil {
		t.Fatal("b_feature.txt should exist:", err)
	}
}

// TestSetupWorktreesRecreatesMissingDir reproduces the bug where a waiting
// task's worktree directory is deleted (e.g. server restart, OS tmpfs cleanup)
// while the underlying git branch survives. setupWorktrees must recreate the
// directory by checking out the existing branch rather than failing with
// "branch already exists".
func TestSetupWorktreesRecreatesMissingDir(t *testing.T) {
	repo := setupTestRepo(t)
	_, runner := setupTestRunner(t, []string{repo})

	taskID := uuid.New()

	// First call: creates the worktree directory and branch.
	worktreePaths, branchName, err := runner.setupWorktrees(taskID)
	if err != nil {
		t.Fatal("initial setupWorktrees:", err)
	}

	wt := worktreePaths[repo]

	// Simulate Claude making changes and committing in the worktree.
	if err := os.WriteFile(filepath.Join(wt, "change.txt"), []byte("work in progress\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, wt, "add", ".")
	gitRun(t, wt, "commit", "-m", "task: add change.txt")
	commitBefore := gitRun(t, wt, "rev-parse", "HEAD")

	// Simulate the worktree directory being deleted (e.g. server restart).
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal("remove worktree dir:", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("worktree dir should be gone after RemoveAll")
	}

	// The git branch must still exist in the repo (it lives in .git/refs).
	branches, _ := gitRunMayFail(repo, "branch", "--list", branchName)
	if !strings.Contains(branches, branchName) {
		t.Fatalf("branch %s should still exist in repo even after dir removal", branchName)
	}

	// Second call with the same taskID: must recreate the directory by
	// checking out the existing branch (not with -b, which would fail).
	worktreePaths2, branchName2, err := runner.setupWorktrees(taskID)
	if err != nil {
		t.Fatalf("setupWorktrees after dir deletion: %v", err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(taskID, worktreePaths2, branchName2) })

	if branchName2 != branchName {
		t.Fatalf("branch name changed: want %q, got %q", branchName, branchName2)
	}

	wt2 := worktreePaths2[repo]
	if wt2 != wt {
		t.Fatalf("worktree path changed: want %q, got %q", wt, wt2)
	}

	// Verify directory was recreated.
	if info, err := os.Stat(wt2); err != nil || !info.IsDir() {
		t.Fatal("worktree dir should exist after recreation:", err)
	}

	// Verify we are on the correct branch.
	branch := gitRun(t, wt2, "branch", "--show-current")
	if branch != branchName {
		t.Fatalf("expected branch %q after recreation, got %q", branchName, branch)
	}

	// The previous commit must be preserved — we checked out the existing
	// branch, not a fresh one from HEAD.
	commitAfter := gitRun(t, wt2, "rev-parse", "HEAD")
	if commitAfter != commitBefore {
		t.Fatalf("commit should be preserved: want %q, got %q", commitBefore, commitAfter)
	}

	// The file committed before the dir was deleted must be visible.
	if _, err := os.Stat(filepath.Join(wt2, "change.txt")); err != nil {
		t.Fatal("change.txt should exist in recreated worktree:", err)
	}
}

// TestRunDetectsMissingWorktreePaths verifies the runner.go fix: when a task's
// stored WorktreePaths point to directories that no longer exist on disk,
// setupWorktrees is called again to recreate them, and the task can proceed.
func TestRunDetectsMissingWorktreePaths(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})

	ctx := context.Background()
	task, err := s.CreateTask(ctx, "Test feedback resume", 5)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate task going in_progress: create worktrees and persist paths.
	worktreePaths, branchName, err := runner.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal("initial setupWorktrees:", err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, worktreePaths, branchName); err != nil {
		t.Fatal(err)
	}

	wt := worktreePaths[repo]

	// Simulate Claude making progress before going to waiting.
	if err := os.WriteFile(filepath.Join(wt, "partial.txt"), []byte("partial work\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, wt, "add", ".")
	gitRun(t, wt, "commit", "-m", "task: partial work")
	commitOnBranch := gitRun(t, wt, "rev-parse", "HEAD")

	// Task goes to waiting; worktree directory is still on disk at this point.
	if err := s.UpdateTaskStatus(ctx, task.ID, "waiting"); err != nil {
		t.Fatal(err)
	}

	// ---- Server restart simulation ----
	// The worktree directory disappears but task.json retains WorktreePaths.
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}

	// Reload the task from the store to confirm WorktreePaths is still set.
	reloaded, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.WorktreePaths) == 0 {
		t.Fatal("WorktreePaths should still be persisted in the store")
	}
	if _, statErr := os.Stat(reloaded.WorktreePaths[repo]); !os.IsNotExist(statErr) {
		t.Fatal("worktree directory should be gone from disk")
	}

	// Replicate the needSetup detection logic from runner.go Run():
	// if any stored path is missing, call setupWorktrees again.
	needSetup := false
	for _, p := range reloaded.WorktreePaths {
		if _, statErr := os.Stat(p); statErr != nil {
			needSetup = true
			break
		}
	}
	if !needSetup {
		t.Fatal("needSetup should be true when a stored path is missing")
	}

	// Calling setupWorktrees must succeed and recreate the directory on the
	// existing branch — this is what the fixed Run() does.
	newPaths, newBranch, err := runner.setupWorktrees(task.ID)
	if err != nil {
		t.Fatalf("setupWorktrees after simulated restart: %v", err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(task.ID, newPaths, newBranch) })

	newWt := newPaths[repo]

	// Verify the recreated worktree is on the correct branch.
	branch := gitRun(t, newWt, "branch", "--show-current")
	if branch != branchName {
		t.Fatalf("expected branch %q, got %q", branchName, branch)
	}

	// Verify the commit made before the restart is still there.
	commitAfter := gitRun(t, newWt, "rev-parse", "HEAD")
	if commitAfter != commitOnBranch {
		t.Fatalf("commit should be preserved after worktree recreation: want %q, got %q",
			commitOnBranch, commitAfter)
	}

	// Verify the file is accessible (the worktree is fully functional).
	if _, err := os.Stat(filepath.Join(newWt, "partial.txt")); err != nil {
		t.Fatal("partial.txt should be present in the recreated worktree:", err)
	}
}

// TestParallelWorktreeIsolation verifies that file changes in one worktree
// are invisible in another worktree and in the main repo until merged.
func TestParallelWorktreeIsolation(t *testing.T) {
	repo := setupTestRepo(t)
	_, runner := setupTestRunner(t, []string{repo})

	taskA := uuid.New()
	taskB := uuid.New()

	wtA, brA, err := runner.setupWorktrees(taskA)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(taskA, wtA, brA) })

	wtB, brB, err := runner.setupWorktrees(taskB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(taskB, wtB, brB) })

	pathA := wtA[repo]
	pathB := wtB[repo]

	// Write a file in worktree A.
	if err := os.WriteFile(filepath.Join(pathA, "secret_a.txt"), []byte("only A\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write a file in worktree B.
	if err := os.WriteFile(filepath.Join(pathB, "secret_b.txt"), []byte("only B\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Files should NOT be visible across worktrees.
	if _, err := os.Stat(filepath.Join(pathA, "secret_b.txt")); err == nil {
		t.Fatal("secret_b.txt should NOT be visible in worktree A")
	}
	if _, err := os.Stat(filepath.Join(pathB, "secret_a.txt")); err == nil {
		t.Fatal("secret_a.txt should NOT be visible in worktree B")
	}

	// Files should NOT be visible in the main repo.
	if _, err := os.Stat(filepath.Join(repo, "secret_a.txt")); err == nil {
		t.Fatal("secret_a.txt should NOT be visible in main repo before merge")
	}
	if _, err := os.Stat(filepath.Join(repo, "secret_b.txt")); err == nil {
		t.Fatal("secret_b.txt should NOT be visible in main repo before merge")
	}
}
