package handler

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/store/storetest"
	"latere.ai/x/wallfacer/internal/workspace"
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
	sB, err := storetest.NewFileStore(t, otherDir)
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

// failOnWaitingBackend wraps a StorageBackend and fails SaveTask only when the
// task is being persisted in the waiting state. Lets a test make the
// commit-transition recovery (ForceUpdateTaskStatus(waiting)) fail while the
// committing seed and any later done/failed save still succeed.
type failOnWaitingBackend struct {
	store.StorageBackend
}

func (b *failOnWaitingBackend) SaveTask(t *store.Task) error {
	if t.Status == store.TaskStatusWaiting {
		return errInjectedWaiting
	}
	return b.StorageBackend.SaveTask(t)
}

var errInjectedWaiting = errInjected("injected save failure for waiting")

type errInjected string

func (e errInjected) Error() string { return string(e) }

// TestRunCommitTransition_MissingWorktreesAndFailedRecoveryDoesNotCommit is a
// regression test for a control-flow gap: when validateTaskWorktreesForCommit
// fails AND the recovery ForceUpdateTaskStatus(waiting) also fails, the code
// fell through to runner.Commit with known-missing worktrees, which (in the
// mock) returns nil and drove the task to done. With the fix it returns early
// and leaves the task in committing for the next reconcile.
func TestRunCommitTransition_MissingWorktreesAndFailedRecoveryDoesNotCommit(t *testing.T) {
	m := &runner.MockRunner{}

	fsBackend, err := store.NewFilesystemBackend(t.TempDir())
	if err != nil {
		t.Fatalf("NewFilesystemBackend: %v", err)
	}
	s, err := storetest.NewStore(t, &failOnWaitingBackend{StorageBackend: fsBackend})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	h := &Handler{runner: m, store: s}
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 30})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusCommitting); err != nil {
		t.Fatal(err)
	}
	// Point the worktree at a nonexistent path so validation fails (the mock
	// runner's EnsureTaskWorktrees returns it unchanged, so it stays missing).
	if err := s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{"/repo": "/nonexistent-worktree-xyz"}, "task/branch"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}

	h.runCommitTransition(s, task.ID, "session-1", store.TriggerUser, "commit error: ")

	// Let the goroutine run to completion. The fix returns early on the
	// double-failure path, so runner.Commit must never be called with the
	// known-missing worktrees. Without the fix, control falls through and
	// Commit is invoked.
	time.Sleep(300 * time.Millisecond)
	if calls := m.CommitCallsSnapshot(); len(calls) != 0 {
		t.Fatalf("Commit was called %d time(s) despite missing worktrees and failed recovery", len(calls))
	}
}
