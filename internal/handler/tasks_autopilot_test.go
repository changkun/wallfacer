package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/store"
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
			wb := newWatcherBreaker()

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
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "retry-reset integration", Timeout: 15, Kind: store.TaskKindTask})
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
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod cannot enforce read-only directories on Windows")
	}
	h := newTestHandler(t)
	h.SetAutopilot(true)
	h.SetAutotest(true)

	ctx := context.Background()

	// Create a real git repo so Phase1's CommitsBehind check returns 0 (not behind).
	repo := setupRepo(t)

	// Create and advance the task to waiting status with a valid worktree.
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "rollback test", Timeout: 15})
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
	t.Cleanup(func() { _ = os.Chmod(taskDir, 0755) })

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
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod cannot enforce read-only directories on Windows")
	}
	h := newTestHandler(t)
	h.SetAutopilot(true)
	h.SetAutotest(true)

	ctx := context.Background()

	// Create a real git repo so Phase1's CommitsBehind check returns 0 (not behind).
	repo := setupRepo(t)

	// Create and advance the task to waiting status.
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "auto-test candidate", Timeout: 15})
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
	t.Cleanup(func() { _ = os.Chmod(taskDir, 0755) })

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

// TestAutoTester_SettleDelayDefersTrigger verifies that the auto-tester pauses
// for constants.WatcherSettleDelay after receiving a wake signal before acting. This
// ensures the SSE event for the intermediate "waiting" state reaches the
// browser and is rendered before the watcher transitions the task back.
func TestAutoTester_SettleDelayDefersTrigger(t *testing.T) {
	// Lower the settle delay for the test so it runs quickly.
	orig := constants.WatcherSettleDelay
	constants.WatcherSettleDelay = 300 * time.Millisecond
	t.Cleanup(func() { constants.WatcherSettleDelay = orig })

	h := newTestHandler(t)
	h.SetAutotest(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a waiting task with a worktree so it would be eligible for
	// auto-test once the watcher runs.
	repo, err := os.MkdirTemp("", "wallfacer-test-repo-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repo) })
	gitRun(t, repo, "init", "-b", "main")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	gitRun(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "initial commit")

	wtParent, err := os.MkdirTemp("", "wallfacer-test-wt-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(wtParent) })
	wt := filepath.Join(wtParent, "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	// Start the watcher. The wake subscriber was created by StartAutoTester,
	// so any subsequent notify will be delivered.
	h.StartAutoTester(ctx)

	// Trigger a wake signal by mutating the store.
	_ = h.store.UpdateTaskTitle(ctx, task.ID, "trigger wake")

	// Immediately after the wake, the task should still be waiting because
	// the settle delay has not elapsed.
	time.Sleep(50 * time.Millisecond)
	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Fatalf("task should still be waiting during settle delay, got %s", got.Status)
	}

	// After the settle delay, the watcher should have transitioned the task
	// out of waiting. In the test environment the container runner is not
	// available, so the task may end up in "failed" rather than staying in
	// "in_progress" — the important thing is that it left "waiting".
	time.Sleep(constants.WatcherSettleDelay + 500*time.Millisecond)
	got, _ = h.store.GetTask(ctx, task.ID)
	if got.Status == store.TaskStatusWaiting {
		t.Error("task should have left waiting after settle delay, still waiting")
	}
}

// TestAutoSubmitter_SettleDelayDefersTrigger verifies that the auto-submitter
// waits constants.WatcherSettleDelay after a wake signal before committing, giving the UI
// time to render the "waiting" state.
func TestAutoSubmitter_SettleDelayDefersTrigger(t *testing.T) {
	orig := constants.WatcherSettleDelay
	constants.WatcherSettleDelay = 300 * time.Millisecond
	t.Cleanup(func() { constants.WatcherSettleDelay = orig })

	h := newTestHandler(t)
	h.SetAutosubmit(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a waiting task that qualifies for auto-submit: has worktrees,
	// passing test result, up to date, no conflicts.
	repo, err := os.MkdirTemp("", "wallfacer-test-repo-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repo) })
	gitRun(t, repo, "init", "-b", "main")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	gitRun(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "initial commit")

	wtParent, err := os.MkdirTemp("", "wallfacer-test-wt-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(wtParent) })
	wt := filepath.Join(wtParent, "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")
	_ = h.store.UpdateTaskTestRun(ctx, task.ID, false, "pass")

	h.StartAutoSubmitter(ctx)

	// Trigger a wake signal.
	_ = h.store.UpdateTaskTitle(ctx, task.ID, "trigger wake")

	// During the settle delay the task must remain in waiting.
	time.Sleep(50 * time.Millisecond)
	got, _ := h.store.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusWaiting {
		t.Fatalf("task should still be waiting during settle delay, got %s", got.Status)
	}

	// After the settle delay, auto-submit should act. Use a generous timeout
	// because git operations in the auto-submit pipeline can be slow on CI.
	time.Sleep(constants.WatcherSettleDelay + 2*time.Second)
	got, _ = h.store.GetTask(ctx, task.ID)
	if got.Status == store.TaskStatusWaiting {
		t.Error("task should have been submitted after settle delay, still waiting")
	}
}

// --- checkConcurrencyAndUpdateStatus ---

// TestCheckConcurrencyAndUpdateStatus_Success verifies that a valid backlog →
// in_progress transition returns true and leaves the task in_progress.
func TestCheckConcurrencyAndUpdateStatus_Success(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	w := httptest.NewRecorder()
	ok := h.checkConcurrencyAndUpdateStatus(ctx, w, task.ID, store.TaskStatusInProgress)
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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	w := httptest.NewRecorder()
	ok := h.checkConcurrencyAndUpdateStatus(ctx, w, task.ID, store.TaskStatusDone)
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
	maxConc := h.maxConcurrentTasks()
	// Saturate the concurrency limit with in-progress regular tasks.
	for i := 0; i < maxConc; i++ {
		task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "running", Timeout: 15})
		h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress) //nolint:errcheck
	}
	// Try to promote one more backlog task — must be rejected.
	backlog, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "backlog", Timeout: 15})
	w := httptest.NewRecorder()
	ok := h.checkConcurrencyAndUpdateStatus(ctx, w, backlog.ID, store.TaskStatusInProgress)
	if ok {
		t.Error("expected concurrency limit rejection")
	}
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d: %s", w.Code, w.Body.String())
	}
}

// TestMaxConcurrentTasks_Caching verifies that maxConcurrentTasks reads from
// the env file only on the first call and caches the result for subsequent
// calls. It also verifies that resetting the cache (Store(0)) causes the next
// call to re-read the updated file value.
func TestMaxConcurrentTasks_Caching(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)

	// Write an initial MaxParallelTasks value to the env file.
	initialLimit := "7"
	if err := envconfig.Update(envPath, envconfig.Updates{MaxParallel: &initialLimit}); err != nil {
		t.Fatalf("envconfig.Update initial: %v", err)
	}

	// First call should read from the file and cache the value.
	got := h.maxConcurrentTasks()
	if got != 7 {
		t.Fatalf("first call: want 7, got %d", got)
	}

	// Overwrite the env file with a different value WITHOUT invalidating the cache.
	updatedLimit := "12"
	if err := envconfig.Update(envPath, envconfig.Updates{MaxParallel: &updatedLimit}); err != nil {
		t.Fatalf("envconfig.Update updated: %v", err)
	}

	// Second call should still return the cached value (7), not the new value (12).
	got = h.maxConcurrentTasks()
	if got != 7 {
		t.Fatalf("second call (cached): want 7, got %d", got)
	}

	// Manually invalidate the cache (simulating what UpdateEnvConfig does).
	h.cachedMaxParallel.Invalidate()

	// Third call should re-read from the file and return the updated value.
	got = h.maxConcurrentTasks()
	if got != 12 {
		t.Fatalf("third call (after invalidation): want 12, got %d", got)
	}
}

// TestMaxTestConcurrentTasks_Caching mirrors TestMaxConcurrentTasks_Caching
// for the test-parallel limit.
func TestMaxTestConcurrentTasks_Caching(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)

	initialLimit := "3"
	if err := envconfig.Update(envPath, envconfig.Updates{MaxTestParallel: &initialLimit}); err != nil {
		t.Fatalf("envconfig.Update initial: %v", err)
	}

	got := h.maxTestConcurrentTasks()
	if got != 3 {
		t.Fatalf("first call: want 3, got %d", got)
	}

	updatedLimit := "8"
	if err := envconfig.Update(envPath, envconfig.Updates{MaxTestParallel: &updatedLimit}); err != nil {
		t.Fatalf("envconfig.Update updated: %v", err)
	}

	// Cache still holds 3.
	got = h.maxTestConcurrentTasks()
	if got != 3 {
		t.Fatalf("second call (cached): want 3, got %d", got)
	}

	h.cachedMaxTestParallel.Invalidate()

	got = h.maxTestConcurrentTasks()
	if got != 8 {
		t.Fatalf("third call (after invalidation): want 8, got %d", got)
	}
}

// TestUpdateEnvConfig_InvalidatesParallelLimitCache verifies that calling
// UpdateEnvConfig with a new MaxParallelTasks value invalidates the in-process
// cache so that the next call to maxConcurrentTasks reflects the new limit.
func TestUpdateEnvConfig_InvalidatesParallelLimitCache(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	// Prime the cache with the default (no env file entry → default 5).
	initial := h.maxConcurrentTasks()
	if initial != constants.DefaultMaxConcurrentTasks {
		t.Fatalf("initial: want %d, got %d", constants.DefaultMaxConcurrentTasks, initial)
	}

	// Call UpdateEnvConfig to change max_parallel_tasks to 9.
	newLimit := 9
	body, _ := json.Marshal(map[string]any{"max_parallel_tasks": newLimit})
	req := httptest.NewRequest(http.MethodPut, "/api/env", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("UpdateEnvConfig: want 204, got %d: %s", w.Code, w.Body.String())
	}

	// The cache should have been invalidated; the next call must return 9.
	got := h.maxConcurrentTasks()
	if got != newLimit {
		t.Fatalf("after UpdateEnvConfig: want %d, got %d", newLimit, got)
	}
}

// TestUpdateEnvConfig_InvalidatesTestParallelLimitCache mirrors the above for
// the test-parallel limit.
func TestUpdateEnvConfig_InvalidatesTestParallelLimitCache(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	initial := h.maxTestConcurrentTasks()
	if initial != constants.DefaultMaxTestConcurrentTasks {
		t.Fatalf("initial: want %d, got %d", constants.DefaultMaxTestConcurrentTasks, initial)
	}

	newLimit := 6
	body, _ := json.Marshal(map[string]any{"max_test_parallel_tasks": newLimit})
	req := httptest.NewRequest(http.MethodPut, "/api/env", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.UpdateEnvConfig(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("UpdateEnvConfig: want 204, got %d: %s", w.Code, w.Body.String())
	}

	got := h.maxTestConcurrentTasks()
	if got != newLimit {
		t.Fatalf("after UpdateEnvConfig: want %d, got %d", newLimit, got)
	}
}

// TestTryAutoRetry_HandlerPath covers the four core guard branches of the
// handler's tryAutoRetry method.
func TestTryAutoRetry_HandlerPath(t *testing.T) {
	// ── Test 1: regression guard ─────────────────────────────────────────────
	// The exact regressed scenario fixed in a4e6326: the handler was using
	// len(RetryHistory) (=5) instead of AutoRetryCount (=1) as the gate.
	// With count=1 < constants.MaxAutoRetries(3) and budget=2 > 0, the task MUST
	// be retried despite having 5 RetryHistory entries.
	t.Run("regression_uses_auto_retry_count_not_retry_history_length", func(t *testing.T) {
		h := newTestHandler(t)
		ctx := context.Background()

		created, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "regression test prompt", Timeout: 5})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.store.ForceUpdateTaskStatus(ctx, created.ID, store.TaskStatusFailed); err != nil {
			t.Fatal(err)
		}

		// Build 5 RetryRecord entries to simulate history from earlier runs.
		retryHistory := make([]store.RetryRecord, 5)
		for i := range retryHistory {
			retryHistory[i] = store.RetryRecord{
				RetiredAt: time.Now(),
				Prompt:    "previous prompt",
				Status:    store.TaskStatusFailed,
			}
		}

		task := store.Task{
			ID:              created.ID,
			Status:          store.TaskStatusFailed,
			Prompt:          created.Prompt,
			AutoRetryCount:  1, // low count — but history length is 5
			RetryHistory:    retryHistory,
			FailureCategory: store.FailureCategoryContainerCrash,
			AutoRetryBudget: map[store.FailureCategory]int{
				store.FailureCategoryContainerCrash: 2,
			},
		}

		h.tryAutoRetry(ctx, task)

		got, err := h.store.GetTask(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != store.TaskStatusBacklog {
			t.Errorf("status = %q, want backlog — regression: handler must use AutoRetryCount not len(RetryHistory)", got.Status)
		}
	})

	// ── Test 2: total cap enforcement ────────────────────────────────────────
	// AutoRetryCount == constants.MaxAutoRetries(3) must block retry even when
	// the per-category budget is plentiful.
	t.Run("total_cap_prevents_retry", func(t *testing.T) {
		h := newTestHandler(t)
		ctx := context.Background()

		created, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "cap test prompt", Timeout: 5})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.store.ForceUpdateTaskStatus(ctx, created.ID, store.TaskStatusFailed); err != nil {
			t.Fatal(err)
		}

		task := store.Task{
			ID:              created.ID,
			Status:          store.TaskStatusFailed,
			Prompt:          created.Prompt,
			AutoRetryCount:  constants.MaxAutoRetries, // == 3, at the cap
			FailureCategory: store.FailureCategoryContainerCrash,
			AutoRetryBudget: map[store.FailureCategory]int{
				store.FailureCategoryContainerCrash: 5, // budget is irrelevant when count is at cap
			},
		}

		h.tryAutoRetry(ctx, task)

		got, err := h.store.GetTask(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != store.TaskStatusFailed {
			t.Errorf("status = %q, want failed — total cap should suppress retry", got.Status)
		}
	})

	// ── Test 3: per-category budget exhaustion ────────────────────────────────
	// AutoRetryBudget[container_crash]=0 must block retry even when count < max.
	t.Run("category_budget_exhausted_prevents_retry", func(t *testing.T) {
		h := newTestHandler(t)
		ctx := context.Background()

		created, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "budget test prompt", Timeout: 5})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.store.ForceUpdateTaskStatus(ctx, created.ID, store.TaskStatusFailed); err != nil {
			t.Fatal(err)
		}

		task := store.Task{
			ID:              created.ID,
			Status:          store.TaskStatusFailed,
			Prompt:          created.Prompt,
			AutoRetryCount:  1,
			FailureCategory: store.FailureCategoryContainerCrash,
			AutoRetryBudget: map[store.FailureCategory]int{
				store.FailureCategoryContainerCrash: 0, // budget exhausted
			},
		}

		h.tryAutoRetry(ctx, task)

		got, err := h.store.GetTask(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != store.TaskStatusFailed {
			t.Errorf("status = %q, want failed — exhausted budget should suppress retry", got.Status)
		}
	})

	// ── Test 4: non-retryable category ────────────────────────────────────────
	// agent_error is not in retryableCategories so the handler must not retry.
	t.Run("non_retryable_category_agent_error", func(t *testing.T) {
		h := newTestHandler(t)
		ctx := context.Background()

		created, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "agent error prompt", Timeout: 5})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.store.ForceUpdateTaskStatus(ctx, created.ID, store.TaskStatusFailed); err != nil {
			t.Fatal(err)
		}

		task := store.Task{
			ID:              created.ID,
			Status:          store.TaskStatusFailed,
			Prompt:          created.Prompt,
			AutoRetryCount:  0,
			FailureCategory: store.FailureCategoryAgentError,
			AutoRetryBudget: map[store.FailureCategory]int{
				store.FailureCategoryAgentError: 5, // budget exists but category is not retryable
			},
		}

		h.tryAutoRetry(ctx, task)

		got, err := h.store.GetTask(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != store.TaskStatusFailed {
			t.Errorf("status = %q, want failed — agent_error is not in retryableCategories", got.Status)
		}
	})
}

// Note (test 5 — runner path): The runner's tryAutoRetry is covered in
// internal/runner/auto_retry_test.go which tests the unexported method directly
// in the runner package.

// Note (test 6 — classifyFailure): All 5 branches of classifyFailure are
// already exhaustively covered in internal/runner/classify_failure_test.go.

// TestStartAutoRetrier_StartupScan verifies the recovery scan that
// StartAutoRetrier runs on startup.  Three tasks are pre-populated:
//
//	(a) failed + container_crash + default budget (>0) + count=0 → MUST be retried
//	(b) failed + agent_error (non-retryable category) → must NOT be retried
//	(c) in_progress → not in the failed list, must remain unchanged
func TestStartAutoRetrier_StartupScan(t *testing.T) {
	h := newTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// (a) Eligible failed task.
	taskA, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "crash task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, taskA.ID, store.TaskStatusFailed); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetTaskFailureCategory(ctx, taskA.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatal(err)
	}

	// (b) Non-retryable failed task.
	taskB, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "agent error task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, taskB.ID, store.TaskStatusFailed); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetTaskFailureCategory(ctx, taskB.ID, store.FailureCategoryAgentError); err != nil {
		t.Fatal(err)
	}

	// (c) In-progress task (not in the failed list; included to confirm no side-effects).
	taskC, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "in progress task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, taskC.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}

	h.StartAutoRetrier(ctx)

	// Poll until the startup goroutine completes the recovery scan.
	var gotA *store.Task
	for range 100 {
		gotA, _ = h.store.GetTask(ctx, taskA.ID)
		if gotA != nil && gotA.Status == store.TaskStatusBacklog {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// (a) should be reset to backlog.
	if gotA == nil || gotA.Status != store.TaskStatusBacklog {
		t.Errorf("task (a): status = %q, want backlog (eligible retry should fire)", gotA.Status)
	}

	// (b) should remain failed — agent_error is non-retryable.
	gotB, err := h.store.GetTask(ctx, taskB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotB.Status != store.TaskStatusFailed {
		t.Errorf("task (b): status = %q, want failed (agent_error must not be retried)", gotB.Status)
	}

	// (c) should remain in_progress — not touched by the failed-task scan.
	gotC, err := h.store.GetTask(ctx, taskC.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotC.Status != store.TaskStatusInProgress {
		t.Errorf("task (c): status = %q, want in_progress (untouched by startup scan)", gotC.Status)
	}
}

// TestStartAutoRetrier_ServerRestartDoubleRetryGuard verifies that the startup
// scan does not cascade into infinite retries after a server restart.
//
// Two tasks are pre-populated to simulate state left on disk after a crash:
//
//   - task1: AutoRetryCount=2, FailureCategory=container_crash, budget=1.
//     The scan should retry this once more (reset to backlog).
//
//   - task2: AutoRetryCount=3 (== constants.MaxAutoRetries), FailureCategory=container_crash.
//     The scan must NOT retry this — the total cap is already hit.
func TestStartAutoRetrier_ServerRestartDoubleRetryGuard(t *testing.T) {
	h := newTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── task1: count=2, container_crash budget=1 ─────────────────────────────
	// Build count=2 with container_crash budget=1 by incrementing two different
	// categories so that the container_crash budget is only spent once:
	//   Inc(sync_error):      count=1, sync_error budget=1, container_crash budget=2
	//   Inc(container_crash): count=2, container_crash budget=1
	task1, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "restart task one", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.IncrementAutoRetryCount(ctx, task1.ID, store.FailureCategorySyncError); err != nil {
		t.Fatal(err)
	}
	if err := h.store.IncrementAutoRetryCount(ctx, task1.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatal(err)
	}

	// Verify the setup is correct before running the scan.
	t1pre, _ := h.store.GetTask(ctx, task1.ID)
	if t1pre.AutoRetryCount != 2 {
		t.Fatalf("setup: task1 AutoRetryCount=%d, want 2", t1pre.AutoRetryCount)
	}
	if t1pre.AutoRetryBudget[store.FailureCategoryContainerCrash] != 1 {
		t.Fatalf("setup: task1 container_crash budget=%d, want 1",
			t1pre.AutoRetryBudget[store.FailureCategoryContainerCrash])
	}

	if err := h.store.ForceUpdateTaskStatus(ctx, task1.ID, store.TaskStatusFailed); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetTaskFailureCategory(ctx, task1.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatal(err)
	}

	// ── task2: count=3 (cap already hit), container_crash budget=2 ──────────
	// Build count=3 by incrementing sync_error three times, leaving the
	// container_crash budget at its default of 2 to isolate the count guard.
	task2, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "restart task two", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	for range constants.MaxAutoRetries {
		if err := h.store.IncrementAutoRetryCount(ctx, task2.ID, store.FailureCategorySyncError); err != nil {
			t.Fatal(err)
		}
	}

	t2pre, _ := h.store.GetTask(ctx, task2.ID)
	if t2pre.AutoRetryCount != constants.MaxAutoRetries {
		t.Fatalf("setup: task2 AutoRetryCount=%d, want %d", t2pre.AutoRetryCount, constants.MaxAutoRetries)
	}

	if err := h.store.ForceUpdateTaskStatus(ctx, task2.ID, store.TaskStatusFailed); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetTaskFailureCategory(ctx, task2.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatal(err)
	}

	h.StartAutoRetrier(ctx)

	// Poll until the startup goroutine completes the recovery scan.
	var got1 *store.Task
	for range 100 {
		got1, _ = h.store.GetTask(ctx, task1.ID)
		if got1 != nil && got1.Status == store.TaskStatusBacklog {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// task1 should be reset to backlog (count=2 < 3, budget=1 > 0).
	if got1 == nil || got1.Status != store.TaskStatusBacklog {
		t.Errorf("task1: status = %q, want backlog (one retry remaining at count=2)", got1.Status)
	}

	// task2 must remain failed — count=3 hits constants.MaxAutoRetries.
	got2, err := h.store.GetTask(ctx, task2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Status != store.TaskStatusFailed {
		t.Errorf("task2: status = %q, want failed (max count=%d already hit)", got2.Status, constants.MaxAutoRetries)
	}
	if got2.AutoRetryCount != constants.MaxAutoRetries {
		t.Errorf("task2: AutoRetryCount=%d, want %d (unchanged)", got2.AutoRetryCount, constants.MaxAutoRetries)
	}
}

// TestTryAutoSubmit_SkipsTaskWithRecentFetchError verifies that a waiting task
// whose LastFetchErrorAt is within the 5-minute window is NOT selected for
// auto-submission, even when it has passed testing and its worktrees are up to date.
func TestTryAutoSubmit_SkipsTaskWithRecentFetchError(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)
	h.SetAutosubmit(true)

	ctx := context.Background()

	// Use a real git repo WITH a remote so the stale-fetch guard applies.
	repo := setupRepo(t)
	origin := t.TempDir()
	gitRun(t, origin, "init", "--bare")
	gitRun(t, repo, "remote", "add", "origin", origin)
	gitRun(t, repo, "push", "-u", "origin", "main")

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "fetch-error guard test", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus waiting: %v", err)
	}
	// Mark as passing verification.
	if err := h.store.UpdateTaskTestRun(ctx, task.ID, false, "pass"); err != nil {
		t.Fatalf("UpdateTaskTestRun pass: %v", err)
	}
	// Set worktree paths so the task passes the worktree eligibility checks.
	if err := h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: repo}, "task-branch"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}

	// Simulate a recent git fetch failure on this task.
	if err := h.store.RecordFetchFailure(ctx, task.ID, "dial tcp: connection refused"); err != nil {
		t.Fatalf("RecordFetchFailure: %v", err)
	}

	h.tryAutoSubmit(ctx)

	got, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	// The stale-fetch guard must keep the task in waiting.
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("expected task still waiting after stale fetch guard, got %s", got.Status)
	}
}

// TestTryAutoSubmit_LocalRepoIgnoresStaleFetchError verifies that a task on
// a local-only git repo (no origin remote) is auto-submitted even when
// LastFetchErrorAt is set. Before the fix, the stale-fetch guard blocked
// auto-submit regardless of whether the task had repos that actually needed
// remote checks.
func TestTryAutoSubmit_LocalRepoIgnoresStaleFetchError(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)
	h.SetAutosubmit(true)

	ctx := context.Background()

	// Local-only git repo: no origin remote configured.
	repo := setupRepo(t)

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "local repo stale fetch test", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus waiting: %v", err)
	}
	if err := h.store.UpdateTaskTestRun(ctx, task.ID, false, "pass"); err != nil {
		t.Fatalf("UpdateTaskTestRun pass: %v", err)
	}
	if err := h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: repo}, "task-branch"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}

	// Simulate a stale fetch failure (from before the HasOriginRemote fix).
	if err := h.store.RecordFetchFailure(ctx, task.ID, "fatal: 'origin' does not appear to be a git repository"); err != nil {
		t.Fatalf("RecordFetchFailure: %v", err)
	}

	h.tryAutoSubmit(ctx)

	got, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	// Local-only repos have no remote, so the stale-fetch guard must not apply.
	// No session → task should go directly to done.
	if got.Status != store.TaskStatusDone {
		t.Errorf("expected local-repo task to reach done despite stale fetch error, got %s", got.Status)
	}
}

// TestTryAutoPromote_PromotesMultipleTasks verifies that a single call to
// tryAutoPromote promotes all eligible backlog tasks up to the concurrency
// limit, rather than only promoting one task per watcher cycle.
//
// This is a regression test for the bug where tryAutoPromote's Phase1 returned
// only a single best candidate, causing the promoter to advance one task per
// 60-second tick even when multiple slots were available.
func TestTryAutoPromote_PromotesMultipleTasks(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)

	ctx := context.Background()

	// Create 3 backlog tasks.
	var taskIDs []uuid.UUID
	for i := 0; i < 3; i++ {
		task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
			Prompt:  fmt.Sprintf("task %d", i),
			Timeout: 15,
		})
		if err != nil {
			t.Fatalf("CreateTask[%d]: %v", i, err)
		}
		taskIDs = append(taskIDs, task.ID)
	}

	// Default max concurrent is 5, so all 3 should be promoted in one call.
	h.tryAutoPromote(ctx)

	for i, id := range taskIDs {
		got, err := h.store.GetTask(ctx, id)
		if err != nil {
			t.Fatalf("GetTask[%d]: %v", i, err)
		}
		if got.Status != store.TaskStatusInProgress {
			t.Errorf("task[%d] status = %q, want in_progress — tryAutoPromote must promote all eligible tasks in one pass", i, got.Status)
		}
	}
}

// TestTryAutoPromote_RespectsCapacityLimit verifies that tryAutoPromote does
// not exceed the concurrency limit when more backlog tasks exist than available
// slots.
func TestTryAutoPromote_RespectsCapacityLimit(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)
	h.SetAutopilot(true)

	ctx := context.Background()

	// Set max parallel to 2.
	limit := "2"
	if err := envconfig.Update(envPath, envconfig.Updates{MaxParallel: &limit}); err != nil {
		t.Fatalf("envconfig.Update: %v", err)
	}
	h.cachedMaxParallel.Invalidate()

	// Create 5 backlog tasks.
	var taskIDs []uuid.UUID
	for i := 0; i < 5; i++ {
		task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
			Prompt:  fmt.Sprintf("task %d", i),
			Timeout: 15,
		})
		if err != nil {
			t.Fatalf("CreateTask[%d]: %v", i, err)
		}
		taskIDs = append(taskIDs, task.ID)
	}

	h.tryAutoPromote(ctx)

	promoted := 0
	backlog := 0
	for i, id := range taskIDs {
		got, err := h.store.GetTask(ctx, id)
		if err != nil {
			t.Fatalf("GetTask[%d]: %v", i, err)
		}
		switch got.Status {
		case store.TaskStatusInProgress:
			promoted++
		case store.TaskStatusBacklog:
			backlog++
		default:
			// Runner may transition tasks to failed quickly in test env.
			promoted++
		}
	}

	if promoted != 2 {
		t.Errorf("promoted = %d, want 2 (max_parallel=2)", promoted)
	}
	if backlog != 3 {
		t.Errorf("backlog = %d, want 3 remaining", backlog)
	}
}

// TestCheckAndSyncWaitingTasks_SkipsAlreadyPromotedTask verifies that the sync
// watcher does not trip its circuit breaker when another watcher (e.g. auto-test)
// has already promoted a waiting task to in_progress between the snapshot read
// and the status transition.
func TestCheckAndSyncWaitingTasks_SkipsAlreadyPromotedTask(t *testing.T) {
	h := newStaticWorkspaceHandler(t, nil)
	h.SetAutopilot(true)
	ctx := context.Background()

	// Create a git repo with a remote so the sync watcher's fetch/behind checks apply.
	origin := t.TempDir()
	gitRun(t, origin, "init", "--bare")
	repo := setupRepo(t)
	gitRun(t, repo, "remote", "add", "origin", origin)
	gitRun(t, repo, "push", "-u", "origin", "main")

	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "sync race test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	// Add a commit to main so the worktree is behind (sync watcher would want to sync).
	_ = os.WriteFile(filepath.Join(repo, "upstream.txt"), []byte("upstream\n"), 0644)
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "upstream commit")

	// Simulate another watcher promoting the task to in_progress before the
	// sync watcher's Phase 2 runs.
	_ = h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)

	// Run the sync watcher — it should see the task as in_progress under the
	// lock and skip it, NOT trip the circuit breaker.
	h.checkAndSyncWaitingTasks(ctx)

	// The auto-sync breaker must remain closed (healthy).
	if h.breakers["auto-sync"].isOpen() {
		t.Error("auto-sync circuit breaker should not have tripped for an already-promoted task")
	}
}

// TestTryAutoSubmit_AllowsTaskAfterFetchErrorCleared verifies that once
// ClearFetchFailure is called (simulating a successful subsequent fetch), the
// previously blocked task becomes eligible and is auto-submitted.
func TestTryAutoSubmit_AllowsTaskAfterFetchErrorCleared(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)
	h.SetAutosubmit(true)

	ctx := context.Background()

	repo := setupRepo(t)

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "fetch-error cleared test", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus waiting: %v", err)
	}
	if err := h.store.UpdateTaskTestRun(ctx, task.ID, false, "pass"); err != nil {
		t.Fatalf("UpdateTaskTestRun pass: %v", err)
	}
	if err := h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: repo}, "task-branch"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}

	// Record then clear the fetch failure to simulate a successful subsequent fetch.
	if err := h.store.RecordFetchFailure(ctx, task.ID, "network unreachable"); err != nil {
		t.Fatalf("RecordFetchFailure: %v", err)
	}
	if err := h.store.ClearFetchFailure(ctx, task.ID); err != nil {
		t.Fatalf("ClearFetchFailure: %v", err)
	}

	// Confirm the failure fields are nil after clearing.
	snapshot, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask after clear: %v", err)
	}
	if snapshot.LastFetchError != "" || snapshot.LastFetchErrorAt != nil {
		t.Errorf("expected fetch error fields cleared, got error=%q errorAt=%v",
			snapshot.LastFetchError, snapshot.LastFetchErrorAt)
	}

	h.tryAutoSubmit(ctx)

	got, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask after tryAutoSubmit: %v", err)
	}
	// No session → task should go directly to done.
	if got.Status != store.TaskStatusDone {
		t.Errorf("expected task done after fetch error cleared, got %s", got.Status)
	}
}
