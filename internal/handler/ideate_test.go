package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/store"
)

// --- IdeationInterval state ---

func TestIdeationInterval_DefaultIs60Minutes(t *testing.T) {
	h := newTestHandler(t)
	if h.IdeationInterval() != 60*time.Minute {
		t.Errorf("expected default interval=60m, got %v", h.IdeationInterval())
	}
}

func TestSetIdeationInterval_StoresValue(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeationInterval(30 * time.Minute)
	if h.IdeationInterval() != 30*time.Minute {
		t.Errorf("expected 30m, got %v", h.IdeationInterval())
	}
}

func TestSetIdeationInterval_CancelsPendingTimer(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeation(true)

	// Arm a 10-second timer by calling scheduleIdeation with a non-zero interval.
	h.SetIdeationInterval(10 * time.Second)
	h.scheduleIdeation(context.Background())

	// Timer should be pending.
	h.ideationMu.Lock()
	timerBefore := h.ideationTimer
	h.ideationMu.Unlock()
	if timerBefore == nil {
		t.Fatal("expected a pending timer after scheduleIdeation")
	}

	// Changing the interval should cancel the timer.
	h.SetIdeationInterval(20 * time.Second)

	h.ideationMu.Lock()
	timerAfter := h.ideationTimer
	h.ideationMu.Unlock()
	if timerAfter != nil {
		t.Error("expected pending timer to be cancelled after SetIdeationInterval")
	}
}

func TestSetIdeation_DisablingCancelsPendingTimer(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeation(true)
	h.SetIdeationInterval(10 * time.Second)
	h.scheduleIdeation(context.Background())

	// Disable ideation — timer should be cancelled.
	h.SetIdeation(false)

	h.ideationMu.Lock()
	timer := h.ideationTimer
	h.ideationMu.Unlock()
	if timer != nil {
		t.Error("expected pending timer to be cancelled when ideation is disabled")
	}
}

func TestIdeationNextRun_ZeroWhenNoTimerPending(t *testing.T) {
	h := newTestHandler(t)
	if !h.IdeationNextRun().IsZero() {
		t.Error("expected zero next-run time when no timer is pending")
	}
}

func TestScheduleIdeation_ImmediateWhenIntervalZero(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeation(true)
	h.SetIdeationInterval(0) // explicitly test the zero-interval path
	// interval = 0: scheduleIdeation should create the task directly, no timer.
	h.scheduleIdeation(context.Background())

	h.ideationMu.Lock()
	timer := h.ideationTimer
	h.ideationMu.Unlock()
	if timer != nil {
		t.Error("expected no timer when interval is zero (immediate scheduling)")
	}
}

func TestScheduleIdeation_SetsTimerWhenIntervalNonZero(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeation(true)
	h.SetIdeationInterval(5 * time.Minute)
	h.scheduleIdeation(context.Background())

	h.ideationMu.Lock()
	timer := h.ideationTimer
	nextRun := h.ideationNextRun
	h.ideationMu.Unlock()

	if timer == nil {
		t.Fatal("expected a pending timer when interval > 0")
	}
	if nextRun.IsZero() {
		t.Error("expected ideationNextRun to be set")
	}
	if nextRun.Before(time.Now()) {
		t.Error("expected ideationNextRun to be in the future")
	}

	// Stop timer so it doesn't fire during test cleanup.
	h.ideationMu.Lock()
	h.cancelIdeationTimerLocked()
	h.ideationMu.Unlock()
}

func TestScheduleIdeation_NoDuplicateTimer(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeation(true)
	h.SetIdeationInterval(5 * time.Minute)

	// Call scheduleIdeation twice — should not create a second timer.
	h.scheduleIdeation(context.Background())

	h.ideationMu.Lock()
	first := h.ideationTimer
	h.ideationMu.Unlock()

	h.scheduleIdeation(context.Background())

	h.ideationMu.Lock()
	second := h.ideationTimer
	h.ideationMu.Unlock()

	if first != second {
		t.Error("expected the same timer pointer (no double-scheduling)")
	}

	h.ideationMu.Lock()
	h.cancelIdeationTimerLocked()
	h.ideationMu.Unlock()
}

// --- UpdateConfig ideation_interval ---

func TestUpdateConfig_SetsIdeationInterval(t *testing.T) {
	h := newTestHandler(t)

	body := `{"ideation_interval": 60}`
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)


	got, ok := resp["ideation_interval"].(float64)
	if !ok {
		t.Fatalf("expected ideation_interval in response, got %v", resp["ideation_interval"])
	}
	if int(got) != 60 {
		t.Errorf("expected ideation_interval=60 in response, got %v", got)
	}

	if h.IdeationInterval() != 60*time.Minute {
		t.Errorf("expected handler interval=60m, got %v", h.IdeationInterval())
	}
}

func TestUpdateConfig_IdeationIntervalClampedToZero(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeation(false)

	body := `{"ideation_interval": -5}`
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if h.IdeationInterval() != 0 {
		t.Errorf("expected negative interval to be clamped to 0, got %v", h.IdeationInterval())
	}
}

func TestUpdateConfig_ReturnsIdeationIntervalByDefault(t *testing.T) {
	h := newTestHandler(t)

	// Empty body — should still return ideation_interval (0).
	body := `{}`
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateConfig(w, req)

	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if _, ok := resp["ideation_interval"]; !ok {
		t.Error("expected ideation_interval in UpdateConfig response")
	}
}

// --- GetConfig ideation_interval ---

func TestGetConfig_ReturnsIdeationInterval(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)
	h.SetIdeationInterval(120 * time.Minute)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)


	got, ok := resp["ideation_interval"].(float64)
	if !ok {
		t.Fatalf("expected ideation_interval in GetConfig response, got %v", resp["ideation_interval"])
	}
	if int(got) != 120 {
		t.Errorf("expected ideation_interval=120, got %v", got)
	}
}

func TestGetConfig_IdeationNextRunAbsentWhenNotPending(t *testing.T) {
	h, _ := newTestHandlerWithWorkspaces(t)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.GetConfig(w, req)

	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)


	if _, ok := resp["ideation_next_run"]; ok {
		t.Error("expected ideation_next_run to be absent when no timer is pending")
	}
}

func TestCreateIdeaAgentTask_PopulatesExecutionPrompt(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	_, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Refactor auth handler", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	existingDone, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Write tests for sync", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.UpdateTaskStatus(ctx, existingDone.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}

	task := h.createIdeaAgentTask(ctx)
	if task == nil {
		t.Fatal("expected idea-agent task to be created")
	}
	created, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if created.ExecutionPrompt == "" {
		t.Fatal("expected execution prompt to be set immediately")
	}
	if !strings.Contains(created.ExecutionPrompt, "Refactor auth handler") {
		t.Errorf("expected execution prompt to include active task context, got %q", created.ExecutionPrompt)
	}
	if !strings.Contains(created.ExecutionPrompt, "Existing active tasks") {
		t.Errorf("expected execution prompt to include active tasks section, got %q", created.ExecutionPrompt)
	}
}

// --- StartIdeationWatcher ---

// TestStartIdeationWatcher_ExitsOnCancel verifies that the watcher goroutine
// returns without blocking when the context is already cancelled.
func TestStartIdeationWatcher_ExitsOnCancel(t *testing.T) {
	h := newTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so the goroutine exits at once
	h.StartIdeationWatcher(ctx)
	// Give the goroutine a moment to finish; no hang means the test passes.
	time.Sleep(20 * time.Millisecond)
}

// --- TriggerIdeation ---

// TestTriggerIdeation_ReturnsAccepted verifies that POST /api/ideate returns
// 202 with {"queued": true} and includes the created task ID.
func TestTriggerIdeation_ReturnsAccepted(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.TriggerIdeation(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if queued, _ := resp["queued"].(bool); !queued {
		t.Errorf("expected queued=true, got %v", resp)
	}
	taskID, ok := resp["task_id"].(string)
	if !ok || taskID == "" {
		t.Errorf("expected non-empty task_id in response, got %v", resp)
	}
}

// --- CancelIdeation ---

// TestCancelIdeation_NoTasks verifies that DELETE /api/ideate returns 200
// with cancelled=false when no idea-agent tasks exist.
func TestCancelIdeation_NoTasks(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.CancelIdeation(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["cancelled"] {
		t.Error("expected cancelled=false when no tasks exist")
	}
}

// TestCancelIdeation_CancelsBacklogIdeaAgentTask verifies that DELETE /api/ideate
// returns cancelled=true for a backlogged idea-agent task. The handler attempts
// the backlog→cancelled transition (which the state machine rejects because that
// transition is not defined); the response still reports cancelled=true because
// the handler sets the flag before checking the error.
func TestCancelIdeation_CancelsBacklogIdeaAgentTask(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm prompt", Timeout: 15, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// The task starts in backlog by default.
	if task.Status != store.TaskStatusBacklog {
		t.Fatalf("expected backlog status, got %s", task.Status)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.CancelIdeation(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Handler reports cancelled=true for matching backlog idea-agent tasks.
	if !resp["cancelled"] {
		t.Error("expected cancelled=true for backlogged idea-agent task")
	}
}

// TestCancelIdeation_IgnoresNonIdeaAgentTasks verifies that regular tasks are
// not cancelled by DELETE /api/ideate.
func TestCancelIdeation_IgnoresNonIdeaAgentTasks(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	_, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "regular task", Timeout: 15, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.CancelIdeation(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["cancelled"] {
		t.Error("expected cancelled=false: non-idea-agent tasks must not be cancelled")
	}
}

// --- GetIdeationStatus ---

// TestGetIdeationStatus_ReturnsEnabledAndRunning verifies that GET /api/ideate
// returns a JSON object with at least "enabled" and "running" fields.
func TestGetIdeationStatus_ReturnsEnabledAndRunning(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.GetIdeationStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["enabled"]; !ok {
		t.Error("response missing 'enabled' field")
	}
	if _, ok := resp["running"]; !ok {
		t.Error("response missing 'running' field")
	}
}

// TestGetIdeationStatus_RunningWhenIdeaAgentInProgress verifies that
// "running" is true when an idea-agent task is in_progress.
func TestGetIdeationStatus_RunningWhenIdeaAgentInProgress(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm", Timeout: 15, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.GetIdeationStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	running, _ := resp["running"].(bool)
	if !running {
		t.Error("expected running=true when an idea-agent task is in_progress")
	}
}

// TestGetIdeationStatus_NotRunningWhenNoIdeaAgentInProgress verifies that
// "running" is false when no idea-agent task is in_progress.
func TestGetIdeationStatus_NotRunningWhenNoIdeaAgentInProgress(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.GetIdeationStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	running, _ := resp["running"].(bool)
	if running {
		t.Error("expected running=false when no idea-agent task is in_progress")
	}
}

// --- maybeScheduleNextIdeation ---

// TestMaybeScheduleNextIdeation_DisabledNoOp verifies that when ideation is
// disabled, maybeScheduleNextIdeation is a no-op and does not set a timer.
func TestMaybeScheduleNextIdeation_DisabledNoOp(t *testing.T) {
	h := newTestHandler(t)
	// Ideation is disabled by default in the test handler.
	if h.IdeationEnabled() {
		h.SetIdeation(false)
	}

	h.maybeScheduleNextIdeation(context.Background())

	h.ideationMu.Lock()
	timer := h.ideationTimer
	h.ideationMu.Unlock()
	if timer != nil {
		t.Error("expected no timer when ideation is disabled")
	}
}

// TestMaybeScheduleNextIdeation_EnabledNoActiveTask verifies that when ideation
// is enabled and no idea-agent task is running or backlogged, scheduleIdeation
// is called, setting an ideation timer (when interval > 0).
func TestMaybeScheduleNextIdeation_EnabledNoActiveTask(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeation(true)
	h.SetIdeationInterval(10 * time.Hour)

	h.maybeScheduleNextIdeation(context.Background())

	h.ideationMu.Lock()
	hasTimer := h.ideationTimer != nil
	if hasTimer {
		// Clean up so the timer does not fire during test cleanup.
		h.cancelIdeationTimerLocked()
	}
	h.ideationMu.Unlock()

	if !hasTimer {
		t.Error("expected ideation timer to be set when ideation is enabled and no active idea-agent task exists")
	}
}

// TestMaybeScheduleNextIdeation_ActiveBacklogTask verifies that when an
// idea-agent task is in backlog, maybeScheduleNextIdeation does not schedule a
// new run (the existing backlog task already represents a pending brainstorm).
func TestMaybeScheduleNextIdeation_ActiveBacklogTask(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeation(true)
	h.SetIdeationInterval(10 * time.Hour)
	ctx := context.Background()

	// Create a backlogged idea-agent task.
	_, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm prompt", Timeout: 60, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	h.maybeScheduleNextIdeation(ctx)

	h.ideationMu.Lock()
	hasTimer := h.ideationTimer != nil
	h.ideationMu.Unlock()

	if hasTimer {
		t.Error("should not set a timer when an idea-agent task is already in backlog")
		h.ideationMu.Lock()
		h.cancelIdeationTimerLocked()
		h.ideationMu.Unlock()
	}
}

// TestMaybeScheduleNextIdeation_ActiveInProgressTask verifies that when an
// idea-agent task is in_progress, maybeScheduleNextIdeation does not schedule.
func TestMaybeScheduleNextIdeation_ActiveInProgressTask(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeation(true)
	h.SetIdeationInterval(10 * time.Hour)
	ctx := context.Background()

	// Create an idea-agent task and move it to in_progress.
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm running", Timeout: 60, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	h.maybeScheduleNextIdeation(ctx)

	h.ideationMu.Lock()
	hasTimer := h.ideationTimer != nil
	h.ideationMu.Unlock()

	if hasTimer {
		t.Error("should not set a timer when an idea-agent task is in_progress")
		h.ideationMu.Lock()
		h.cancelIdeationTimerLocked()
		h.ideationMu.Unlock()
	}
}

// TestMaybeScheduleNextIdeation_DoneTaskDoesNotBlock verifies that a completed
// idea-agent task does not prevent maybeScheduleNextIdeation from scheduling a
// new run; only active (backlog/in_progress) tasks should suppress scheduling.
func TestMaybeScheduleNextIdeation_DoneTaskDoesNotBlock(t *testing.T) {
	h := newTestHandler(t)
	h.SetIdeation(true)
	h.SetIdeationInterval(10 * time.Hour)
	ctx := context.Background()

	// Create an idea-agent task and advance it to done (terminal state).
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "old brainstorm", Timeout: 60, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}

	h.maybeScheduleNextIdeation(ctx)

	h.ideationMu.Lock()
	hasTimer := h.ideationTimer != nil
	if hasTimer {
		h.cancelIdeationTimerLocked()
	}
	h.ideationMu.Unlock()

	if !hasTimer {
		t.Error("expected scheduling to proceed when the only idea-agent task is done")
	}
}
