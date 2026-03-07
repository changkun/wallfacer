package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"changkun.de/wallfacer/internal/runner"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// gitRun runs a git command in dir and fails the test on error.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// setupRepo creates a temporary git repo with an initial commit on "main".
func setupRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "initial commit")
	return dir
}

// newTestHandler creates a Handler backed by a temp-dir store and minimal runner.
func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{})
	// Wait for background goroutines (oversight generation) before the store's
	// data directory is cleaned up. Registered last so it runs first (LIFO).
	t.Cleanup(r.WaitBackground)
	return NewHandler(s, r, t.TempDir(), nil)
}

// diffResponse is the JSON shape returned by TaskDiff.
type diffResponse struct {
	Diff         string         `json:"diff"`
	BehindCounts map[string]int `json:"behind_counts"`
}

func callTaskDiff(t *testing.T, h *Handler, taskID uuid.UUID) diffResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+taskID.String()+"/diff", nil)
	w := httptest.NewRecorder()
	h.TaskDiff(w, req, taskID)
	if w.Code != http.StatusOK {
		t.Fatalf("TaskDiff returned %d: %s", w.Code, w.Body.String())
	}
	var resp diffResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal diff response: %v", err)
	}
	return resp
}

func TestTaskDiffShowsOnlyTaskChanges(t *testing.T) {
	repo := setupRepo(t)
	h := newTestHandler(t)
	ctx := context.Background()

	// Create worktree for task A.
	wtA := filepath.Join(t.TempDir(), "wt-a")
	gitRun(t, repo, "worktree", "add", "-b", "task-a", wtA, "HEAD")

	// Create worktree for task B.
	wtB := filepath.Join(t.TempDir(), "wt-b")
	gitRun(t, repo, "worktree", "add", "-b", "task-b", wtB, "HEAD")

	// Task A makes a change and commits.
	os.WriteFile(filepath.Join(wtA, "a.txt"), []byte("from task A\n"), 0644)
	gitRun(t, wtA, "add", ".")
	gitRun(t, wtA, "commit", "-m", "task A commit")

	// Task B makes a different change and commits.
	os.WriteFile(filepath.Join(wtB, "b.txt"), []byte("from task B\n"), 0644)
	gitRun(t, wtB, "add", ".")
	gitRun(t, wtB, "commit", "-m", "task B commit")

	// Merge task A into main (simulating task A completing).
	gitRun(t, repo, "merge", "--ff-only", "task-a")

	// Create store tasks with worktree paths.
	taskB, _ := h.store.CreateTask(ctx, "task B", 5, false, "")
	h.store.UpdateTaskWorktrees(ctx, taskB.ID, map[string]string{repo: wtB}, "task-b")

	// Get diff for task B — should only show b.txt, NOT the inverse of a.txt.
	resp := callTaskDiff(t, h, taskB.ID)

	if !strings.Contains(resp.Diff, "b.txt") {
		t.Error("expected diff to contain b.txt (task B's change)")
	}
	if strings.Contains(resp.Diff, "a.txt") {
		t.Error("diff should NOT contain a.txt (task A's change merged to main)")
	}
}

func TestTaskDiffIncludesUncommittedChanges(t *testing.T) {
	repo := setupRepo(t)
	h := newTestHandler(t)
	ctx := context.Background()

	wtDir := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task", wtDir, "HEAD")

	// Make uncommitted change.
	os.WriteFile(filepath.Join(wtDir, "file.txt"), []byte("modified\n"), 0644)

	task, _ := h.store.CreateTask(ctx, "test", 5, false, "")
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wtDir}, "task")

	resp := callTaskDiff(t, h, task.ID)

	if !strings.Contains(resp.Diff, "modified") {
		t.Error("expected diff to include uncommitted changes")
	}
}

func TestTaskDiffIncludesUntrackedFiles(t *testing.T) {
	repo := setupRepo(t)
	h := newTestHandler(t)
	ctx := context.Background()

	wtDir := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task", wtDir, "HEAD")

	// Add an untracked file.
	os.WriteFile(filepath.Join(wtDir, "new-file.txt"), []byte("new content\n"), 0644)

	task, _ := h.store.CreateTask(ctx, "test", 5, false, "")
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wtDir}, "task")

	resp := callTaskDiff(t, h, task.ID)

	if !strings.Contains(resp.Diff, "new-file.txt") {
		t.Error("expected diff to include untracked file new-file.txt")
	}
}

func TestTaskDiffEmptyWhenNoChanges(t *testing.T) {
	repo := setupRepo(t)
	h := newTestHandler(t)
	ctx := context.Background()

	wtDir := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task", wtDir, "HEAD")

	task, _ := h.store.CreateTask(ctx, "test", 5, false, "")
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wtDir}, "task")

	resp := callTaskDiff(t, h, task.ID)

	if resp.Diff != "" {
		t.Errorf("expected empty diff, got: %s", resp.Diff)
	}
}

func TestTaskDiffFallbackToCommitHashes(t *testing.T) {
	repo := setupRepo(t)
	h := newTestHandler(t)
	ctx := context.Background()

	// Record current HEAD as base.
	baseHash := gitRun(t, repo, "rev-parse", "HEAD")

	// Make a commit to simulate task work.
	os.WriteFile(filepath.Join(repo, "task-work.txt"), []byte("task\n"), 0644)
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "task work")
	commitHash := gitRun(t, repo, "rev-parse", "HEAD")

	// Create task pointing to a non-existent worktree path, with commit hashes set.
	task, _ := h.store.CreateTask(ctx, "test", 5, false, "")
	nonexistent := filepath.Join(t.TempDir(), "gone")
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: nonexistent}, "task")
	h.store.UpdateTaskCommitHashes(ctx, task.ID, map[string]string{repo: commitHash})
	h.store.UpdateTaskBaseCommitHashes(ctx, task.ID, map[string]string{repo: baseHash})

	resp := callTaskDiff(t, h, task.ID)

	if !strings.Contains(resp.Diff, "task-work.txt") {
		t.Error("expected fallback diff to show task-work.txt")
	}
}

func TestTaskDiffFallbackBranchUseMergeBase(t *testing.T) {
	repo := setupRepo(t)
	h := newTestHandler(t)
	ctx := context.Background()

	// Create a task branch with commits, then advance main.
	gitRun(t, repo, "checkout", "-b", "task-x")
	os.WriteFile(filepath.Join(repo, "task-x.txt"), []byte("task X\n"), 0644)
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "task X commit")
	gitRun(t, repo, "checkout", "main")

	// Advance main with a different change.
	os.WriteFile(filepath.Join(repo, "main-advance.txt"), []byte("main\n"), 0644)
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "main advance")

	// Task with worktree gone, but branch exists with commits ahead.
	task, _ := h.store.CreateTask(ctx, "test", 5, false, "")
	nonexistent := filepath.Join(t.TempDir(), "gone")
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: nonexistent}, "task-x")

	resp := callTaskDiff(t, h, task.ID)

	// Should show task-x.txt (the task's change).
	if !strings.Contains(resp.Diff, "task-x.txt") {
		t.Error("expected fallback branch diff to show task-x.txt")
	}
	// Should NOT show main-advance.txt (main's change that the task doesn't have).
	if strings.Contains(resp.Diff, "main-advance.txt") {
		t.Error("fallback branch diff should NOT contain main-advance.txt")
	}
}

// TestTaskDiffAfterCommitPipeline verifies that TaskDiff returns the correct
// diff using stored commit hashes after the commit pipeline has run and cleaned
// up worktrees. Specifically tests that the diff works when the original repo
// is NOT on the default branch.
func TestTaskDiffAfterCommitPipeline(t *testing.T) {
	repo := setupRepo(t)
	h := newTestHandler(t)
	ctx := context.Background()

	// Record the initial main HEAD as base.
	baseHash := gitRun(t, repo, "rev-parse", "HEAD")

	// Create a feature branch and switch to it (simulates user not being on main).
	gitRun(t, repo, "checkout", "-b", "user-feature")
	os.WriteFile(filepath.Join(repo, "user-work.txt"), []byte("user work\n"), 0644)
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "user feature commit")

	// Go back to main and simulate a task commit.
	gitRun(t, repo, "checkout", "main")
	os.WriteFile(filepath.Join(repo, "task-work.txt"), []byte("task output\n"), 0644)
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "wallfacer: task work")
	commitHash := gitRun(t, repo, "rev-parse", "HEAD")

	// Switch back to the feature branch (repo HEAD is NOT on main).
	gitRun(t, repo, "checkout", "user-feature")

	// Create a task with worktree gone (cleaned up after commit pipeline),
	// but with correct commit hashes stored using the defBranch ref.
	task, _ := h.store.CreateTask(ctx, "test", 5, false, "")
	nonexistent := filepath.Join(t.TempDir(), "cleaned-up")
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: nonexistent}, "task-branch")
	h.store.UpdateTaskCommitHashes(ctx, task.ID, map[string]string{repo: commitHash})
	h.store.UpdateTaskBaseCommitHashes(ctx, task.ID, map[string]string{repo: baseHash})

	resp := callTaskDiff(t, h, task.ID)

	// Should show the task's work.
	if !strings.Contains(resp.Diff, "task-work.txt") {
		t.Error("expected diff to show task-work.txt")
	}
	// Should NOT show the user's feature branch work.
	if strings.Contains(resp.Diff, "user-work.txt") {
		t.Error("diff should NOT contain user-work.txt (user's feature branch)")
	}
}

func TestTaskDiffIsolationConcurrent(t *testing.T) {
	repo := setupRepo(t)
	h := newTestHandler(t)
	ctx := context.Background()

	// Create two worktrees from the same base.
	wtA := filepath.Join(t.TempDir(), "wt-a")
	wtB := filepath.Join(t.TempDir(), "wt-b")
	gitRun(t, repo, "worktree", "add", "-b", "task-a", wtA, "HEAD")
	gitRun(t, repo, "worktree", "add", "-b", "task-b", wtB, "HEAD")

	// Each task makes different changes.
	os.WriteFile(filepath.Join(wtA, "only-a.txt"), []byte("A\n"), 0644)
	gitRun(t, wtA, "add", ".")
	gitRun(t, wtA, "commit", "-m", "A")

	os.WriteFile(filepath.Join(wtB, "only-b.txt"), []byte("B\n"), 0644)
	gitRun(t, wtB, "add", ".")
	gitRun(t, wtB, "commit", "-m", "B")

	taskA, _ := h.store.CreateTask(ctx, "A", 5, false, "")
	h.store.UpdateTaskWorktrees(ctx, taskA.ID, map[string]string{repo: wtA}, "task-a")

	taskB, _ := h.store.CreateTask(ctx, "B", 5, false, "")
	h.store.UpdateTaskWorktrees(ctx, taskB.ID, map[string]string{repo: wtB}, "task-b")

	// Query diffs concurrently.
	var wg sync.WaitGroup
	var respA, respB diffResponse
	wg.Add(2)
	go func() {
		defer wg.Done()
		respA = callTaskDiff(t, h, taskA.ID)
	}()
	go func() {
		defer wg.Done()
		respB = callTaskDiff(t, h, taskB.ID)
	}()
	wg.Wait()

	// Task A's diff should only contain only-a.txt.
	if !strings.Contains(respA.Diff, "only-a.txt") {
		t.Error("task A diff missing only-a.txt")
	}
	if strings.Contains(respA.Diff, "only-b.txt") {
		t.Error("task A diff should not contain only-b.txt")
	}

	// Task B's diff should only contain only-b.txt.
	if !strings.Contains(respB.Diff, "only-b.txt") {
		t.Error("task B diff missing only-b.txt")
	}
	if strings.Contains(respB.Diff, "only-a.txt") {
		t.Error("task B diff should not contain only-a.txt")
	}
}
