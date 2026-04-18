package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
)

// --- IdeationInterval state ---
//
// Timer-plumbing tests (SetIdeationInterval cancels pending timer,
// scheduleIdeation behaviour, maybeScheduleNextIdeation behaviour) lived
// here before the ideation migration. They tested internal state that no
// longer exists on the Handler — the schedule now lives on the
// system:ideation routine card, and timer semantics are verified by the
// scheduler engine's own unit tests in internal/routine/engine_test.go.

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

func TestIdeationNextRun_ZeroBeforeBootstrap(t *testing.T) {
	h := newTestHandler(t)
	if !h.IdeationNextRun().IsZero() {
		t.Error("expected zero next-run time before the engine reconciles")
	}
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
		t.Error("expected ideation_next_run to be absent before engine bootstrap")
	}
}

// --- Ideation bootstrap ---

// TestEnsureSystemIdeationRoutine_CreatesOnce verifies the bootstrap is
// idempotent: repeated reconciles find the existing system:ideation
// routine and leave it alone.
func TestEnsureSystemIdeationRoutine_CreatesOnce(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	h.ensureSystemIdeationRoutine(ctx, h.store)
	h.ensureSystemIdeationRoutine(ctx, h.store)

	tasks, _ := h.store.ListTasks(ctx, true)
	count := 0
	for _, t := range tasks {
		if t.IsRoutine() {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one system:ideation routine, got %d", count)
	}
}

// TestEnsureSystemIdeationRoutine_SeedsFromLegacyConfig confirms the seed
// interval is honored when the routine is first materialized.
func TestEnsureSystemIdeationRoutine_SeedsFromLegacyConfig(t *testing.T) {
	h := newTestHandler(t)
	h.legacyIdeationSeed.enabled = true
	h.legacyIdeationSeed.interval = 15 * time.Minute

	h.ensureSystemIdeationRoutine(context.Background(), h.store)

	routine := h.findSystemIdeationRoutine(context.Background())
	if routine == nil {
		t.Fatalf("expected system:ideation routine materialized")
	}
	if routine.RoutineIntervalSeconds != 15*60 {
		t.Fatalf("routine interval = %ds, want %ds", routine.RoutineIntervalSeconds, 15*60)
	}
	if !routine.RoutineEnabled {
		t.Fatal("expected enabled=true from seed")
	}
}

// TestSetIdeation_UpdatesRoutine exercises the post-bootstrap path:
// with a routine already materialized, SetIdeation must flip the
// routine's enabled flag, not the legacy seed.
func TestSetIdeation_UpdatesRoutine(t *testing.T) {
	h := newTestHandler(t)
	h.ensureSystemIdeationRoutine(context.Background(), h.store)

	h.SetIdeation(true)
	routine := h.findSystemIdeationRoutine(context.Background())
	if routine == nil || !routine.RoutineEnabled {
		t.Fatalf("expected routine enabled=true after SetIdeation(true)")
	}

	h.SetIdeation(false)
	routine = h.findSystemIdeationRoutine(context.Background())
	if routine == nil || routine.RoutineEnabled {
		t.Fatalf("expected routine enabled=false after SetIdeation(false)")
	}
}

// TestIdeationFiresExactlyOnce guards against a regression where both
// the legacy timer and the new engine fire on the same cadence. Enable
// the routine, trigger it through the shim endpoint, and assert that
// exactly one idea-agent instance appears.
func TestIdeationFiresExactlyOnce(t *testing.T) {
	mock := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, mock)
	installRoutineEngine(h, nil, h.fireRoutine)

	h.legacyIdeationSeed.enabled = true
	h.legacyIdeationSeed.interval = time.Hour
	h.ensureSystemIdeationRoutine(context.Background(), s)

	req := httptest.NewRequest(http.MethodPost, "/api/ideate", nil)
	rec := httptest.NewRecorder()
	h.TriggerIdeation(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}

	// Wait for the spawned idea-agent instance to appear without
	// polluting the test timing with fixed sleeps.
	deadline := time.Now().Add(2 * time.Second)
	var ideaCount int
	for time.Now().Before(deadline) {
		ideaCount = 0
		tasks, _ := s.ListTasks(context.Background(), false)
		for _, task := range tasks {
			if task.IsIdeaAgent() {
				ideaCount++
			}
		}
		if ideaCount > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if ideaCount != 1 {
		t.Fatalf("expected exactly 1 idea-agent instance, got %d", ideaCount)
	}
}

// --- TriggerIdeation ---

func TestTriggerIdeation_ReturnsAcceptedAfterBootstrap(t *testing.T) {
	mock := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, mock)
	installRoutineEngine(h, nil, h.fireRoutine)
	h.ensureSystemIdeationRoutine(context.Background(), s)

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
	if routineID, _ := resp["routine_id"].(string); routineID == "" {
		t.Errorf("expected non-empty routine_id in response, got %v", resp)
	}
}

func TestTriggerIdeation_AutoBootstrapsWhenMissing(t *testing.T) {
	mock := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, mock)
	// No engine installed, no pre-seeded routine — TriggerIdeation must
	// materialize the system:ideation routine on demand and still return
	// 202 so the legacy UI shim never regresses under fresh-boot timing.
	req := httptest.NewRequest(http.MethodPost, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.TriggerIdeation(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if h.findSystemIdeationRoutine(context.Background()) == nil {
		t.Fatalf("expected system:ideation routine materialized on demand")
	}
	// Wait briefly for the inline fireRoutine goroutine.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tasks, _ := s.ListTasks(context.Background(), false)
		for _, task := range tasks {
			if task.IsIdeaAgent() {
				return // success
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected idea-agent instance spawned by auto-bootstrap trigger")
}

// --- CancelIdeation ---

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

func TestCancelIdeation_CancelsBacklogIdeaAgentTask(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "brainstorm prompt", Timeout: 15, Kind: store.TaskKindIdeaAgent,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
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
	if !resp["cancelled"] {
		t.Error("expected cancelled=true for backlogged idea-agent task")
	}
}

func TestCancelIdeation_IgnoresNonIdeaAgentTasks(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	_, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "regular task", Timeout: 15,
	})
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

func TestGetIdeationStatus_ReturnsEnabledAndRunning(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.GetIdeationStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
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

func TestGetIdeationStatus_RunningWhenIdeaAgentInProgress(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "brainstorm", Timeout: 15, Kind: store.TaskKindIdeaAgent,
	})
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
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	running, _ := resp["running"].(bool)
	if !running {
		t.Error("expected running=true when an idea-agent task is in_progress")
	}
}

func TestGetIdeationStatus_NotRunningWhenNoIdeaAgentInProgress(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/ideate", nil)
	w := httptest.NewRecorder()
	h.GetIdeationStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	running, _ := resp["running"].(bool)
	if running {
		t.Error("expected running=false when no idea-agent task is in_progress")
	}
}
