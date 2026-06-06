package handler

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
	"changkun.de/x/wallfacer/internal/workspace"
)

// TestRunCommitTransition_UsesCapturedStoreAcrossSnapshotSwap verifies that the
// commit-transition goroutine targets the store captured by the caller, even
// when the workspace-switch subscription reassigns h.store concurrently. Before
// the fix the goroutine read the lock-guarded h.store field directly: run with
// -race it reported a data race, and a mid-flight workspace switch could point
// the transition at the wrong group's store. Now the store is passed in.
func TestRunCommitTransition_UsesCapturedStoreAcrossSnapshotSwap(t *testing.T) {
	m := &runner.MockRunner{}
	h, sA := newTestHandlerWithMockRunner(t, m)
	ctx := context.Background()

	task, _ := sA.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 30})
	if err := sA.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusCommitting); err != nil {
		t.Fatal(err)
	}
	worktreeDir := t.TempDir()
	_ = exec.Command("git", "init", worktreeDir).Run()
	_ = exec.Command("git", "-C", worktreeDir, "commit", "--allow-empty", "-m", "init").Run()
	_ = sA.UpdateTaskWorktrees(ctx, task.ID, map[string]string{worktreeDir: worktreeDir}, "task/branch")

	// A second store to swap h.store to mid-transition.
	otherDir, err := os.MkdirTemp("", "wallfacer-commit-other-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(otherDir) })
	sB, err := store.NewFileStore(otherDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sB.Close)

	// Launch the transition against sA, then immediately reassign h.store to sB,
	// racing the goroutine just like the workspace subscription would.
	h.runCommitTransition(sA, task.ID, "session-1", store.TriggerUser, "commit error: ")
	h.applySnapshot(workspace.Snapshot{Store: sB, Key: "b"})

	var updated *store.Task
	for range 100 {
		updated, _ = sA.GetTask(ctx, task.ID)
		if updated != nil && updated.Status == store.TaskStatusDone {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if updated == nil || updated.Status != store.TaskStatusDone {
		t.Fatalf("task in captured store should reach done, got %v", updated)
	}
}
