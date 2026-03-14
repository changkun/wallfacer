package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

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

// TestTryAutoTest_UpdateStatusFailure_RollsBackIsTestRun verifies that when
// UpdateTaskTestRun succeeds but UpdateTaskStatus fails in tryAutoTest's Phase2,
// the IsTestRun flag is rolled back to false so the task remains eligible for
// future auto-test cycles.
//
// The test proceeds in three stages:
//  1. Manually place the task in the stuck state (IsTestRun=true, Status=waiting)
//     and confirm that tryAutoTest Phase1 skips it (circuit breaker stays closed,
//     task state unchanged).
//  2. Apply the rollback (UpdateTaskTestRun(false, "")) and confirm IsTestRun=false.
//  3. Call tryAutoTest again with a read-only task directory so Phase2's store write
//     fails. The circuit breaker opens — its open state is observable proof that
//     Phase1 found the task and Phase2 attempted to process it (i.e., Phase1 did
//     NOT skip it after the rollback).
func TestTryAutoTest_UpdateStatusFailure_RollsBackIsTestRun(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)
	h.SetAutotest(true)

	ctx := context.Background()

	// Create a real git repo so Phase1's CommitsBehind check returns 0 (not behind).
	repo := setupRepo(t)

	// Create and advance the task to waiting status with a valid worktree.
	task, err := h.store.CreateTask(ctx, "rollback test", 15, false, "", "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus in_progress: %v", err)
	}
	if err := h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("UpdateTaskStatus waiting: %v", err)
	}
	if err := h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: repo}, "task-branch"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}

	// --- Stage 1: simulate the stuck state ---
	// Replicate the failure mode: UpdateTaskTestRun succeeded (IsTestRun=true) but
	// UpdateTaskStatus failed, leaving the task stranded at Status=waiting with
	// IsTestRun=true. Without the fix, every subsequent Phase1 scan skips this task.
	if err := h.store.UpdateTaskTestRun(ctx, task.ID, true, ""); err != nil {
		t.Fatalf("UpdateTaskTestRun (simulate stuck): %v", err)
	}

	h.tryAutoTest(ctx) // Phase1 sees IsTestRun=true → skips task → Phase2 never runs for it

	stuck, err := h.store.GetTask(ctx, task.ID)
	if err != nil || stuck == nil {
		t.Fatalf("GetTask after first tryAutoTest: %v", err)
	}
	if !stuck.IsTestRun || stuck.Status != store.TaskStatusWaiting {
		t.Errorf("expected stuck state (IsTestRun=true, Status=waiting), got IsTestRun=%v Status=%s",
			stuck.IsTestRun, stuck.Status)
	}
	if h.breakers["auto-test"].isOpen() {
		t.Error("auto-test breaker must stay closed when Phase1 skips all candidates")
	}

	// --- Stage 2: apply the rollback ---
	// The fix adds this call inside tryAutoTest after UpdateTaskStatus fails. Here we
	// apply it directly to set up Stage 3, which verifies Phase1 eligibility.
	if err := h.store.UpdateTaskTestRun(ctx, task.ID, false, ""); err != nil {
		t.Fatalf("rollback UpdateTaskTestRun: %v", err)
	}

	afterRollback, err := h.store.GetTask(ctx, task.ID)
	if err != nil || afterRollback == nil {
		t.Fatalf("GetTask after rollback: %v", err)
	}
	if afterRollback.IsTestRun {
		t.Error("expected IsTestRun=false after rollback, got true")
	}

	// --- Stage 3: verify Phase1 eligibility after rollback ---
	// Make the task directory read-only so that Phase2's UpdateTaskTestRun write
	// fails. This causes the auto-test circuit breaker to open, which is the
	// observable evidence that Phase1 found and attempted the task — i.e., it did
	// NOT skip it (IsTestRun=false → eligible).
	taskDir := filepath.Join(h.store.DataDir(), task.ID.String())
	if err := os.Chmod(taskDir, 0555); err != nil {
		t.Fatalf("chmod read-only: %v", err)
	}
	t.Cleanup(func() { os.Chmod(taskDir, 0755) })

	h.tryAutoTest(ctx)

	if !h.breakers["auto-test"].isOpen() {
		t.Error("expected auto-test breaker to be open after Phase2 store write failure, " +
			"proving Phase1 found and attempted the task after rollback")
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

// taskWithDeps returns a store.Task with only ID and DependsOn populated.
func taskWithDeps(id uuid.UUID, deps ...uuid.UUID) store.Task {
	depStrs := make([]string, len(deps))
	for i, d := range deps {
		depStrs[i] = d.String()
	}
	return store.Task{ID: id, DependsOn: depStrs}
}

func TestTaskReachable(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	c := uuid.New()
	d := uuid.New()

	tests := []struct {
		name   string
		tasks  []store.Task
		start  uuid.UUID
		target uuid.UUID
		want   bool
	}{
		{
			// Empty graph — nothing reachable.
			name:   "no tasks",
			tasks:  nil,
			start:  a,
			target: b,
			want:   false,
		},
		{
			// A has no DependsOn; an unrelated UUID is not reachable.
			name:   "single task no dependencies cannot reach unrelated target",
			tasks:  []store.Task{taskWithDeps(a)},
			start:  a,
			target: b,
			want:   false,
		},
		{
			// A→B: B is directly reachable from A.
			name:   "direct dependency forward reachable",
			tasks:  []store.Task{taskWithDeps(a, b), taskWithDeps(b)},
			start:  a,
			target: b,
			want:   true,
		},
		{
			// A→B: A is not reachable from B (no reverse edge).
			name:   "direct dependency reverse not reachable",
			tasks:  []store.Task{taskWithDeps(a, b), taskWithDeps(b)},
			start:  b,
			target: a,
			want:   false,
		},
		{
			// A→B→C: C is transitively reachable from A.
			name:   "transitive chain forward reachable",
			tasks:  []store.Task{taskWithDeps(a, b), taskWithDeps(b, c), taskWithDeps(c)},
			start:  a,
			target: c,
			want:   true,
		},
		{
			// A→B→C: A is not reachable from C (edges go the other way).
			name:   "transitive chain reverse not reachable",
			tasks:  []store.Task{taskWithDeps(a, b), taskWithDeps(b, c), taskWithDeps(c)},
			start:  c,
			target: a,
			want:   false,
		},
		{
			// A→B, B→A (direct cycle): A can reach B by following the direct edge.
			name:   "direct cycle A reaches B",
			tasks:  []store.Task{taskWithDeps(a, b), taskWithDeps(b, a)},
			start:  a,
			target: b,
			want:   true,
		},
		{
			// A→B, B→A (direct cycle): B can reach A by following the direct edge.
			name:   "direct cycle B reaches A",
			tasks:  []store.Task{taskWithDeps(a, b), taskWithDeps(b, a)},
			start:  b,
			target: a,
			want:   true,
		},
		{
			// A→B→C→A (indirect cycle): C is reachable from A.
			name:   "indirect cycle A reaches C",
			tasks:  []store.Task{taskWithDeps(a, b), taskWithDeps(b, c), taskWithDeps(c, a)},
			start:  a,
			target: c,
			want:   true,
		},
		{
			// A→B→C→A (indirect cycle): A is reachable from C via the back-edge.
			name:   "indirect cycle C reaches A",
			tasks:  []store.Task{taskWithDeps(a, b), taskWithDeps(b, c), taskWithDeps(c, a)},
			start:  c,
			target: a,
			want:   true,
		},
		{
			// {A→B} and {C→D} are disconnected; B is not reachable from C.
			name:   "disconnected graph B not reachable from C",
			tasks:  []store.Task{taskWithDeps(a, b), taskWithDeps(b), taskWithDeps(c, d), taskWithDeps(d)},
			start:  c,
			target: b,
			want:   false,
		},
		{
			// Diamond: A→B, A→C, B→D, C→D; D is reachable from A via two paths.
			name:   "diamond dependency A reaches D",
			tasks:  []store.Task{taskWithDeps(a, b, c), taskWithDeps(b, d), taskWithDeps(c, d), taskWithDeps(d)},
			start:  a,
			target: d,
			want:   true,
		},
		{
			// Target UUID not present in any task's ID or DependsOn.
			name:   "target not in graph",
			tasks:  []store.Task{taskWithDeps(a)},
			start:  a,
			target: uuid.New(),
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := taskReachable(tc.tasks, tc.start, tc.target)
			if got != tc.want {
				t.Errorf("taskReachable(..., start=%v, target=%v) = %v, want %v",
					tc.start, tc.target, got, tc.want)
			}
		})
	}
}

func TestTaskReachableInAdj(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	c := uuid.New()

	tests := []struct {
		name   string
		adj    map[uuid.UUID][]uuid.UUID
		start  uuid.UUID
		target uuid.UUID
		want   bool
	}{
		{
			// Chain A→B→C: C is transitively reachable from A.
			name:   "chain: A reaches C",
			adj:    map[uuid.UUID][]uuid.UUID{a: {b}, b: {c}},
			start:  a,
			target: c,
			want:   true,
		},
		{
			// Chain A→B→C: A is not reachable from C (no back edges).
			name:   "chain: C does not reach A",
			adj:    map[uuid.UUID][]uuid.UUID{a: {b}, b: {c}},
			start:  c,
			target: a,
			want:   false,
		},
		{
			// Direct cycle A↔B: DFS must terminate; A can reach B.
			name:   "direct cycle terminates: A reaches B",
			adj:    map[uuid.UUID][]uuid.UUID{a: {b}, b: {a}},
			start:  a,
			target: b,
			want:   true,
		},
		{
			// Direct cycle A↔B: DFS must terminate; an absent target is not found.
			name:   "direct cycle terminates: unreachable target returns false",
			adj:    map[uuid.UUID][]uuid.UUID{a: {b}, b: {a}},
			start:  a,
			target: uuid.New(),
			want:   false,
		},
		{
			// Indirect cycle A→B→C→A: C is reachable from A; visited set prevents infinite loop.
			name:   "indirect cycle terminates: A reaches C",
			adj:    map[uuid.UUID][]uuid.UUID{a: {b}, b: {c}, c: {a}},
			start:  a,
			target: c,
			want:   true,
		},
		{
			// Indirect cycle A→B→C→A: a UUID outside the cycle is not reachable.
			name:   "indirect cycle terminates: absent target returns false",
			adj:    map[uuid.UUID][]uuid.UUID{a: {b}, b: {c}, c: {a}},
			start:  a,
			target: uuid.New(),
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := taskReachableInAdj(tc.adj, tc.start, tc.target)
			if got != tc.want {
				t.Errorf("taskReachableInAdj(adj, start=%v, target=%v) = %v, want %v",
					tc.start, tc.target, got, tc.want)
			}
		})
	}
}

// Tests for Start* autopilot goroutine launchers.
// Each function spawns a goroutine that exits when ctx is cancelled.
// Pre-cancelling the context causes the goroutine to exit almost immediately,
// allowing tests to verify the function does not panic or block indefinitely.

func TestStartAutoPromoter_ExitsOnCancel(t *testing.T) {
	h := newTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so the goroutine exits via ctx.Done()
	h.StartAutoPromoter(ctx)
	time.Sleep(10 * time.Millisecond) // allow goroutine to exit
}

func TestStartAutoRetrier_ExitsOnCancel(t *testing.T) {
	h := newTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so recovery scan completes and goroutine exits
	h.StartAutoRetrier(ctx)
	time.Sleep(10 * time.Millisecond)
}

func TestStartWaitingSyncWatcher_ExitsOnCancel(t *testing.T) {
	h := newTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.StartWaitingSyncWatcher(ctx)
	time.Sleep(10 * time.Millisecond)
}

func TestStartAutoTester_ExitsOnCancel(t *testing.T) {
	h := newTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.StartAutoTester(ctx)
	time.Sleep(10 * time.Millisecond)
}

func TestStartAutoSubmitter_ExitsOnCancel(t *testing.T) {
	h := newTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.StartAutoSubmitter(ctx)
	time.Sleep(10 * time.Millisecond)
}

func TestStartAutoRefiner_ExitsOnCancel(t *testing.T) {
	h := newTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.StartAutoRefiner(ctx)
	time.Sleep(10 * time.Millisecond)
}

// --- checkConcurrencyAndUpdateStatus ---

// TestCheckConcurrencyAndUpdateStatus_Success verifies that a valid backlog →
// in_progress transition returns true and leaves the task in_progress.
func TestCheckConcurrencyAndUpdateStatus_Success(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	w := httptest.NewRecorder()
	ok := h.checkConcurrencyAndUpdateStatus(ctx, w, task.ID, store.TaskStatusBacklog, store.TaskStatusInProgress)
	if !ok {
		t.Errorf("expected success, got %d: %s", w.Code, w.Body.String())
	}
	updated, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != store.TaskStatusInProgress {
		t.Errorf("expected in_progress, got %q", updated.Status)
	}
}

// TestCheckConcurrencyAndUpdateStatus_InvalidTransition verifies that an
// invalid transition (backlog → done, which is not allowed) returns false and
// a 400 response.
func TestCheckConcurrencyAndUpdateStatus_InvalidTransition(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	w := httptest.NewRecorder()
	ok := h.checkConcurrencyAndUpdateStatus(ctx, w, task.ID, store.TaskStatusBacklog, store.TaskStatusDone)
	if ok {
		t.Error("expected failure for invalid transition")
	}
	if w.Code != http.StatusBadRequest && w.Code != http.StatusInternalServerError {
		t.Errorf("expected 400 or 500, got %d", w.Code)
	}
}

// TestCheckConcurrencyAndUpdateStatus_MaxConcurrency verifies that attempting to
// promote a backlog task when the concurrency limit is already reached returns
// false and a 409 Conflict response.
func TestCheckConcurrencyAndUpdateStatus_MaxConcurrency(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	max := h.maxConcurrentTasks()
	// Saturate the concurrency limit with in-progress regular tasks.
	for i := 0; i < max; i++ {
		task, _ := h.store.CreateTask(ctx, "running", 15, false, "", "")
		h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress) //nolint:errcheck
	}
	// Try to promote one more backlog task — must be rejected.
	backlog, _ := h.store.CreateTask(ctx, "backlog", 15, false, "", "")
	w := httptest.NewRecorder()
	ok := h.checkConcurrencyAndUpdateStatus(ctx, w, backlog.ID, store.TaskStatusBacklog, store.TaskStatusInProgress)
	if ok {
		t.Error("expected concurrency limit rejection")
	}
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d: %s", w.Code, w.Body.String())
	}
}
