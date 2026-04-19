package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"changkun.de/x/wallfacer/internal/routine"
	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
)

// installRoutineEngine replaces h.routineEngine with a version that
// uses the supplied clock and a fire callback of the caller's choosing.
// Returns the engine so tests can Register directly for convenience.
func installRoutineEngine(h *Handler, clock routine.Clock, fire routine.FireFunc) *routine.Engine {
	eng := routine.NewEngine(context.Background(), clock, fire)
	h.routineMu.Lock()
	h.routineEngine = eng
	h.routineMu.Unlock()
	return eng
}

func TestReconcileRoutines_RegistersNewRoutines(t *testing.T) {
	mock := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, mock)
	installRoutineEngine(h, nil, h.fireRoutine)

	ctx := context.Background()
	routineTask, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:                 "daily",
		Timeout:                30,
		Kind:                   store.TaskKindRoutine,
		RoutineIntervalSeconds: 60,
		RoutineEnabled:         true,
	})
	if err != nil {
		t.Fatalf("create routine: %v", err)
	}

	h.reconcileRoutines(ctx)

	next, ok := h.routineEngine.NextRuns()[routineTask.ID]
	if !ok {
		t.Fatalf("expected routine registered in engine")
	}
	if next.IsZero() {
		t.Fatalf("expected non-zero NextRun for enabled routine")
	}

	// Reconcile must persist RoutineNextRun so the UI can render it.
	got, _ := s.GetTask(ctx, routineTask.ID)
	if got.RoutineNextRun == nil {
		t.Fatalf("expected RoutineNextRun persisted after reconcile")
	}
}

func TestReconcileRoutines_UnregistersDeletedRoutines(t *testing.T) {
	mock := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, mock)
	installRoutineEngine(h, nil, h.fireRoutine)

	ctx := context.Background()
	routineTask, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "r", Timeout: 10, Kind: store.TaskKindRoutine,
		RoutineIntervalSeconds: 60, RoutineEnabled: true,
	})
	h.reconcileRoutines(ctx)
	if _, ok := h.routineEngine.NextRuns()[routineTask.ID]; !ok {
		t.Fatalf("pre-condition: routine should be registered")
	}

	if err := s.DeleteTask(ctx, routineTask.ID, "test"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	h.reconcileRoutines(ctx)

	if _, ok := h.routineEngine.NextRuns()[routineTask.ID]; ok {
		t.Fatalf("expected routine unregistered after delete")
	}
}

func TestReconcileRoutines_DisabledHasZeroNextRun(t *testing.T) {
	mock := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, mock)
	installRoutineEngine(h, nil, h.fireRoutine)

	ctx := context.Background()
	routineTask, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "paused", Timeout: 10, Kind: store.TaskKindRoutine,
		RoutineIntervalSeconds: 60, RoutineEnabled: false,
	})
	h.reconcileRoutines(ctx)

	next := h.routineEngine.NextRuns()[routineTask.ID]
	if !next.IsZero() {
		t.Fatalf("disabled routine should have zero next run, got %v", next)
	}
	got, _ := s.GetTask(ctx, routineTask.ID)
	if got.RoutineNextRun != nil {
		t.Fatalf("disabled routine RoutineNextRun should be cleared, got %v", got.RoutineNextRun)
	}
}

func TestFireRoutine_CreatesAndRunsInstanceTask(t *testing.T) {
	mock := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, mock)
	installRoutineEngine(h, nil, h.fireRoutine)

	ctx := context.Background()
	routineTask, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "scan PRs", Timeout: 30, Kind: store.TaskKindRoutine,
		RoutineIntervalSeconds: 60, RoutineEnabled: true,
	})

	h.fireRoutine(ctx, routineTask.ID)

	tasks, _ := s.ListTasks(ctx, false)
	var instance *store.Task
	for i := range tasks {
		t := &tasks[i]
		if t.Kind == store.TaskKindTask {
			for _, tag := range t.Tags {
				if tag == "spawned-by:"+routineTask.ID.String() {
					instance = t
				}
			}
		}
	}
	if instance == nil {
		t.Fatalf("expected instance task tagged spawned-by:<routine-id>")
	}
	if instance.Prompt != "scan PRs" {
		t.Fatalf("instance prompt = %q, want %q", instance.Prompt, "scan PRs")
	}
	if instance.Status != store.TaskStatusInProgress {
		t.Fatalf("instance status = %q, want in_progress", instance.Status)
	}
	if instance.Timeout != 30 {
		t.Fatalf("instance timeout = %d, want 30 (inherited)", instance.Timeout)
	}

	// RunBackground was invoked on the mock runner for this instance.
	calls := mock.RunCalls()
	if len(calls) != 1 || calls[0] != instance.ID {
		t.Fatalf("expected 1 RunBackground call for instance, got %+v", calls)
	}

	// Routine card records the fire timestamp.
	routineReloaded, _ := s.GetTask(ctx, routineTask.ID)
	if routineReloaded.RoutineLastFiredAt == nil {
		t.Fatalf("expected RoutineLastFiredAt set after fire")
	}
}

func TestFireRoutine_IdeaAgentSpawnKind_SpawnsIdeaAgentInstance(t *testing.T) {
	mock := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, mock)
	installRoutineEngine(h, nil, h.fireRoutine)

	ctx := context.Background()
	routineTask, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:                 "Ideation routine",
		Timeout:                60,
		Kind:                   store.TaskKindRoutine,
		RoutineIntervalSeconds: 3600,
		RoutineEnabled:         true,
		RoutineSpawnKind:       store.TaskKindIdeaAgent,
	})

	h.fireRoutine(ctx, routineTask.ID)

	tasks, _ := s.ListTasks(ctx, false)
	var instance *store.Task
	for i := range tasks {
		if tasks[i].Kind == store.TaskKindIdeaAgent {
			instance = &tasks[i]
		}
	}
	if instance == nil {
		t.Fatalf("expected spawned idea-agent task")
	}
	if instance.Status != store.TaskStatusInProgress {
		t.Fatalf("idea-agent instance status = %q, want in_progress", instance.Status)
	}

	// RunBackground was invoked with the idea-agent instance.
	calls := mock.RunCalls()
	if len(calls) != 1 || calls[0] != instance.ID {
		t.Fatalf("expected 1 RunBackground call for idea-agent instance, got %+v", calls)
	}
}

func TestFireRoutine_UnknownRoutineIsNoop(t *testing.T) {
	mock := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, mock)
	installRoutineEngine(h, nil, h.fireRoutine)

	before, _ := s.ListTasks(context.Background(), false)
	h.fireRoutine(context.Background(), uuid.New())
	after, _ := s.ListTasks(context.Background(), false)

	if len(before) != len(after) {
		t.Fatalf("task count changed after firing unknown routine")
	}
	if calls := mock.RunCalls(); len(calls) != 0 {
		t.Fatalf("expected no RunBackground calls, got %d", len(calls))
	}
}

// TestReconcileRoutines_UnregistersCancelledRoutines guards the bug where
// a user cancels a routine card but the engine keeps firing it because the
// reconcile loop only checked Kind, not Status.
func TestReconcileRoutines_UnregistersCancelledRoutines(t *testing.T) {
	mock := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, mock)
	installRoutineEngine(h, nil, h.fireRoutine)

	ctx := context.Background()
	routineTask, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "r", Timeout: 10, Kind: store.TaskKindRoutine,
		RoutineIntervalSeconds: 60, RoutineEnabled: true,
	})
	h.reconcileRoutines(ctx)
	if _, ok := h.routineEngine.NextRuns()[routineTask.ID]; !ok {
		t.Fatalf("pre-condition: routine should be registered")
	}

	if err := s.CancelTask(ctx, routineTask.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	h.reconcileRoutines(ctx)

	if _, ok := h.routineEngine.NextRuns()[routineTask.ID]; ok {
		t.Fatalf("expected cancelled routine unregistered from engine")
	}
}

// TestFireRoutine_CancelledRoutineDoesNotSpawn guards against a race where
// an engine timer dispatches fireRoutine onto a goroutine while the user
// concurrently cancels the routine card. The fire must bail out without
// creating an instance task.
func TestFireRoutine_CancelledRoutineDoesNotSpawn(t *testing.T) {
	mock := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, mock)
	installRoutineEngine(h, nil, h.fireRoutine)

	ctx := context.Background()
	routineTask, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "r", Timeout: 10, Kind: store.TaskKindRoutine,
		RoutineIntervalSeconds: 60, RoutineEnabled: true,
	})
	if err := s.CancelTask(ctx, routineTask.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	before, _ := s.ListTasks(ctx, false)
	h.fireRoutine(ctx, routineTask.ID)
	after, _ := s.ListTasks(ctx, false)

	if len(after) != len(before) {
		t.Fatalf("cancelled routine spawned instance task: before=%d after=%d", len(before), len(after))
	}
	if calls := mock.RunCalls(); len(calls) != 0 {
		t.Fatalf("expected no RunBackground calls for cancelled routine, got %d", len(calls))
	}
}

// TestFireRoutine_ArchivedRoutineDoesNotSpawn guards the race where the
// engine timer dispatches fire on a goroutine and the user archives the
// routine card before the dispatched goroutine runs. Archive flips the card
// out of ListTasks(false) so reconcile eventually unregisters it, but a
// pre-archive in-flight fire could still slip through without this guard.
func TestFireRoutine_ArchivedRoutineDoesNotSpawn(t *testing.T) {
	mock := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, mock)
	installRoutineEngine(h, nil, h.fireRoutine)

	ctx := context.Background()
	routineTask, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "r", Timeout: 10, Kind: store.TaskKindRoutine,
		RoutineIntervalSeconds: 60, RoutineEnabled: true,
	})
	// Archive directly — a routine card can be archived after being
	// cancelled, or (in-flight) via SetTaskArchived from another code path.
	if err := s.SetTaskArchived(ctx, routineTask.ID, true); err != nil {
		t.Fatalf("archive: %v", err)
	}

	beforeAll, _ := s.ListTasks(ctx, true)
	h.fireRoutine(ctx, routineTask.ID)
	afterAll, _ := s.ListTasks(ctx, true)

	if len(afterAll) != len(beforeAll) {
		t.Fatalf("archived routine spawned instance task: before=%d after=%d", len(beforeAll), len(afterAll))
	}
	if calls := mock.RunCalls(); len(calls) != 0 {
		t.Fatalf("expected no RunBackground calls for archived routine, got %d", len(calls))
	}
}

// TestArchiveTask_UnregistersRoutineSynchronously pins that archiving a
// routine card drops the engine entry immediately, not via the 250 ms
// reconcile settle window. Regression guard for the bug where an archived
// routine could keep spawning instances because the engine timer was still
// armed until the next wake-driven reconcile.
func TestArchiveTask_UnregistersRoutineSynchronously(t *testing.T) {
	mock := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, mock)
	installRoutineEngine(h, nil, h.fireRoutine)

	ctx := context.Background()
	// Routine must be in a terminal state for ArchiveTask to accept it.
	routineTask, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "daily", Timeout: 10, Kind: store.TaskKindRoutine,
		RoutineIntervalSeconds: 60, RoutineEnabled: true,
	})
	if err != nil {
		t.Fatalf("create routine: %v", err)
	}
	h.reconcileRoutines(ctx)
	if _, ok := h.routineEngine.NextRuns()[routineTask.ID]; !ok {
		t.Fatalf("pre-condition: routine should be registered")
	}
	if err := s.CancelTask(ctx, routineTask.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+routineTask.ID.String()+"/archive", nil)
	w := httptest.NewRecorder()
	h.ArchiveTask(w, req, routineTask.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("ArchiveTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Engine entry must be gone BEFORE any reconcile has a chance to run.
	// If it lingers, an already-armed timer could still dispatch a fire
	// and spawn a child task during the settle window.
	if _, ok := h.routineEngine.NextRuns()[routineTask.ID]; ok {
		t.Fatalf("archived routine must be unregistered from engine synchronously")
	}
}

// TestDeleteTask_UnregistersRoutineSynchronously is the soft-delete analog
// of the archive test above.
func TestDeleteTask_UnregistersRoutineSynchronously(t *testing.T) {
	mock := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, mock)
	installRoutineEngine(h, nil, h.fireRoutine)

	ctx := context.Background()
	routineTask, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "daily", Timeout: 10, Kind: store.TaskKindRoutine,
		RoutineIntervalSeconds: 60, RoutineEnabled: true,
	})
	if err != nil {
		t.Fatalf("create routine: %v", err)
	}
	h.reconcileRoutines(ctx)
	if _, ok := h.routineEngine.NextRuns()[routineTask.ID]; !ok {
		t.Fatalf("pre-condition: routine should be registered")
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/tasks/"+routineTask.ID.String(), nil)
	w := httptest.NewRecorder()
	h.DeleteTask(w, req, routineTask.ID)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteTask: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if _, ok := h.routineEngine.NextRuns()[routineTask.ID]; ok {
		t.Fatalf("deleted routine must be unregistered from engine synchronously")
	}
}

func TestTriggerRoutine_WithEngine_SpawnsInstance(t *testing.T) {
	mock := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, mock)
	installRoutineEngine(h, nil, h.fireRoutine)

	ctx := context.Background()
	routineTask, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "run me now", Timeout: 30, Kind: store.TaskKindRoutine,
		RoutineIntervalSeconds: 3600, RoutineEnabled: true,
	})
	// Register in the engine so Trigger has an entry to act on.
	h.reconcileRoutines(ctx)

	// The engine Trigger dispatches fire on a goroutine. Wait for the
	// instance task to appear rather than sleeping a fixed duration.
	h.routineEngine.Trigger(routineTask.ID)

	wantTag := "spawned-by:" + routineTask.ID.String()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tasks, _ := s.ListTasks(ctx, false)
		for _, tk := range tasks {
			if slices.Contains(tk.Tags, wantTag) {
				return // success
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for Trigger to spawn instance")
}
