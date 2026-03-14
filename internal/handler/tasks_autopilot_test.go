package handler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
)

type autopilotPhase1Store struct {
	waitingTasks []store.Task
	waitingErr   error
	backlogTasks []store.Task
	backlogErr   error
	calls        []store.TaskStatus
}

func (m *autopilotPhase1Store) ListTasksByStatus(_ context.Context, status store.TaskStatus) ([]store.Task, error) {
	m.calls = append(m.calls, status)
	switch status {
	case store.TaskStatusWaiting:
		return append([]store.Task(nil), m.waitingTasks...), m.waitingErr
	case store.TaskStatusBacklog:
		return append([]store.Task(nil), m.backlogTasks...), m.backlogErr
	default:
		return nil, nil
	}
}

func TestTryAutoPromote_Phase1StoreErrorsLogAndOpenBreaker(t *testing.T) {
	waitingErr := errors.New("waiting list failed")
	backlogErr := errors.New("backlog list failed")

	tests := []struct {
		name         string
		store        autopilotPhase1Store
		wantErr      error
		wantCalls    []store.TaskStatus
		wantLogError string
	}{
		{
			name: "waiting_list_error",
			store: autopilotPhase1Store{
				waitingErr: waitingErr,
			},
			wantErr:      waitingErr,
			wantCalls:    []store.TaskStatus{store.TaskStatusWaiting},
			wantLogError: "waiting list failed",
		},
		{
			name: "backlog_list_error",
			store: autopilotPhase1Store{
				backlogErr: backlogErr,
			},
			wantErr:      backlogErr,
			wantCalls:    []store.TaskStatus{store.TaskStatusWaiting, store.TaskStatusBacklog},
			wantLogError: "backlog list failed",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			mockStore := tc.store
			wb := &watcherBreaker{}

			phase1 := func(ctx context.Context) (*store.Task, error) {
				waitingTasks, err := mockStore.ListTasksByStatus(ctx, store.TaskStatusWaiting)
				if err != nil {
					return nil, err
				}
				_ = waitingTasks

				backlogTasks, err := mockStore.ListTasksByStatus(ctx, store.TaskStatusBacklog)
				if err != nil {
					return nil, err
				}
				if len(backlogTasks) == 0 {
					return nil, nil
				}
				return &backlogTasks[0], nil
			}

			candidate, err := phase1(ctx)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Phase1 error = %v, want %v", err, tc.wantErr)
			}
			if candidate != nil {
				t.Fatalf("Phase1 candidate = %+v, want nil", candidate)
			}
			if len(mockStore.calls) != len(tc.wantCalls) {
				t.Fatalf("ListTasksByStatus calls = %v, want %v", mockStore.calls, tc.wantCalls)
			}
			for i, status := range tc.wantCalls {
				if mockStore.calls[i] != status {
					t.Fatalf("ListTasksByStatus call[%d] = %q, want %q", i, mockStore.calls[i], status)
				}
			}

			mockStore.calls = nil

			var buf bytes.Buffer
			prev := logger.Handler
			logger.Handler = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})).With("component", "handler")
			defer func() {
				logger.Handler = prev
			}()

			phase2Called := false
			runTwoPhase(ctx, nil, TwoPhaseWatcherConfig{
				Name: "auto-promote",
				OnPhase1Error: func(err error) {
					wb.recordFailure(nil, err.Error())
				},
				Phase1: phase1,
				Phase2: func(_ context.Context, _ *store.Task) (bool, error) {
					phase2Called = true
					return true, nil
				},
			})

			if phase2Called {
				t.Fatal("Phase2 must not run when Phase1 returns an error")
			}
			if !wb.isOpen() {
				t.Fatal("expected circuit breaker to be open after Phase1 store error")
			}

			logOutput := buf.String()
			if !strings.Contains(logOutput, `"msg":"two-phase watcher: phase1 error"`) {
				t.Fatalf("expected phase1 error log, got %q", logOutput)
			}
			if !strings.Contains(logOutput, `"watcher":"auto-promote"`) {
				t.Fatalf("expected watcher name in log, got %q", logOutput)
			}
			if !strings.Contains(logOutput, `"error":"`+tc.wantLogError+`"`) {
				t.Fatalf("expected store error in log, got %q", logOutput)
			}
		})
	}
}

// TestTryAutoRetry_EligibleAfterManualRetryReset verifies that a task whose
// auto-retry budget was exhausted (AutoRetryBudget ≤ 0 or AutoRetryCount ≥ 3)
// is still picked up by tryAutoRetry after the user triggers a manual retry
// via ResetTaskForRetry, which restores both counters to their initial values.
//
// This is a regression test for the bug where ResetTaskForRetry did not reset
// AutoRetryCount / AutoRetryBudget, causing the auto-retrier to silently skip
// the task on the next failure.
func TestTryAutoRetry_EligibleAfterManualRetryReset(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)

	ctx := context.Background()

	// Create a task and bring it to failed state with an exhausted budget.
	task, err := h.store.CreateTask(ctx, "retry-reset integration", 15, false, "", store.TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Simulate IncrementAutoRetryCount until the budget and count are exhausted
	// (ContainerCrash budget = 2, maxHandlerAutoRetries = 3).
	for i := 0; i < 3; i++ {
		if err := h.store.IncrementAutoRetryCount(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
			t.Fatalf("IncrementAutoRetryCount[%d]: %v", i, err)
		}
	}

	// Transition to in_progress then failed with a retryable category.
	if err := h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus in_progress: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed); err != nil {
		t.Fatalf("ForceUpdateTaskStatus failed: %v", err)
	}
	if err := h.store.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatalf("SetTaskFailureCategory: %v", err)
	}

	// Confirm that tryAutoRetry suppresses the task (budget exhausted).
	failed, _ := h.store.ListTasksByStatus(ctx, store.TaskStatusFailed)
	for _, ft := range failed {
		h.tryAutoRetry(ctx, ft)
	}
	// After suppression the task should still be in failed state.
	snapshot, err := h.store.GetTask(ctx, task.ID)
	if err != nil || snapshot == nil {
		t.Fatalf("GetTask after suppression: %v", err)
	}
	if snapshot.Status != store.TaskStatusFailed {
		t.Fatalf("expected task still failed after suppression, got %s", snapshot.Status)
	}

	// --- Manual retry reset ---
	if err := h.store.ResetTaskForRetry(ctx, task.ID, task.Prompt, true); err != nil {
		t.Fatalf("ResetTaskForRetry: %v", err)
	}

	// Transition back to in_progress → failed to simulate the next failure.
	if err := h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus in_progress after reset: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed); err != nil {
		t.Fatalf("ForceUpdateTaskStatus failed after reset: %v", err)
	}
	if err := h.store.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatalf("SetTaskFailureCategory after reset: %v", err)
	}

	// tryAutoRetry must now reset the task to backlog (budget restored).
	failed, _ = h.store.ListTasksByStatus(ctx, store.TaskStatusFailed)
	for _, ft := range failed {
		h.tryAutoRetry(ctx, ft)
	}

	after, err := h.store.GetTask(ctx, task.ID)
	if err != nil || after == nil {
		t.Fatalf("GetTask after retry reset: %v", err)
	}
	if after.Status != store.TaskStatusBacklog {
		t.Errorf("expected task in backlog after auto-retry post-reset, got %s", after.Status)
	}
}

// TestTryAutoTest_Phase2StoreError_OpensOnlyAutoTestBreaker verifies that a
// store write failure inside tryAutoTest's Phase2 loop (e.g. UpdateTaskTestRun
// or UpdateTaskStatus) uses recordFailure on the "auto-test" breaker rather
// than pauseAllAutomation, so global autopilot remains enabled and no other
// watcher breakers are affected.
func TestTryAutoTest_Phase2StoreError_OpensOnlyAutoTestBreaker(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)
	h.SetAutotest(true)

	ctx := context.Background()

	// Create a real git repo so Phase1's CommitsBehind check returns 0 (not behind).
	repo := setupRepo(t)

	// Create and advance the task to waiting status.
	task, err := h.store.CreateTask(ctx, "auto-test candidate", 15, false, "", "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus in_progress: %v", err)
	}
	if err := h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("UpdateTaskStatus waiting: %v", err)
	}
	// Set worktree paths so Phase1 finds the task as an eligible candidate.
	if err := h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: repo}, "task-branch"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}

	// Make the task directory read-only so Phase2 store writes fail.
	taskDir := filepath.Join(h.store.DataDir(), task.ID.String())
	if err := os.Chmod(taskDir, 0555); err != nil {
		t.Fatalf("chmod read-only: %v", err)
	}
	// Restore write permission before cleanup (os.RemoveAll) runs; t.Cleanup
	// callbacks execute in LIFO order, so this runs before newTestHandler's cleanup.
	t.Cleanup(func() { os.Chmod(taskDir, 0755) })

	h.tryAutoTest(ctx)

	// Global automation must NOT be paused — a transient store error must only
	// open the per-watcher breaker, not the global autopilot toggle.
	if !h.AutopilotEnabled() {
		t.Error("AutopilotEnabled() = false after store error in tryAutoTest; expected true")
	}
	if !h.breakers["auto-test"].isOpen() {
		t.Error("expected auto-test breaker to be open after store write failure in Phase2")
	}
	if h.breakers["auto-promote"].isOpen() {
		t.Error("auto-promote breaker must remain closed after auto-test store error")
	}
	if h.breakers["auto-submit"].isOpen() {
		t.Error("auto-submit breaker must remain closed after auto-test store error")
	}
}
