package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// newTestHandlerForOversight creates a Handler and registers a cleanup that
// waits briefly for untracked oversight goroutines (launched via
// go h.runner.GenerateOversight) to finish writing files before TempDir
// cleanup removes the store directory.
func newTestHandlerForOversight(t *testing.T) *Handler {
	t.Helper()
	h := newTestHandler(t)
	// This cleanup is registered AFTER the TempDir and WaitBackground cleanups
	// so it runs FIRST (LIFO), giving goroutines time to finish before removal.
	t.Cleanup(func() { time.Sleep(200 * time.Millisecond) })
	return h
}

// --- GetOversight ---

func TestGetOversight_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+id.String()+"/oversight", nil)
	w := httptest.NewRecorder()
	h.GetOversight(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestGetOversight_PendingWhenNoFile(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/oversight", nil)
	w := httptest.NewRecorder()
	h.GetOversight(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var oversight store.TaskOversight
	if err := json.NewDecoder(w.Body).Decode(&oversight); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if oversight.Status != store.OversightStatusPending {
		t.Errorf("expected pending oversight status, got %s", oversight.Status)
	}
}

func TestGetOversight_ReturnsStoredOversight(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	saved := store.TaskOversight{
		Status: store.OversightStatusReady,
		Phases: []store.OversightPhase{{Title: "Phase 1", Summary: "All good"}},
	}
	if err := h.store.SaveOversight(task.ID, saved); err != nil {
		t.Fatalf("save oversight: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/oversight", nil)
	w := httptest.NewRecorder()
	h.GetOversight(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var oversight store.TaskOversight
	_ = json.NewDecoder(w.Body).Decode(&oversight)

	if oversight.Status != store.OversightStatusReady {
		t.Errorf("expected ready, got %s", oversight.Status)
	}
	if len(oversight.Phases) == 0 || oversight.Phases[0].Title != "Phase 1" {
		t.Errorf("expected phase 'Phase 1', got %+v", oversight.Phases)
	}
}

// --- GetOversight ?phase=test ---

func TestGetOversight_TestPhase_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+id.String()+"/oversight?phase=test", nil)
	w := httptest.NewRecorder()
	h.GetOversight(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestGetOversight_TestPhase_PendingWhenNoFile(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/oversight?phase=test", nil)
	w := httptest.NewRecorder()
	h.GetOversight(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var oversight store.TaskOversight
	_ = json.NewDecoder(w.Body).Decode(&oversight)

	if oversight.Status != store.OversightStatusPending {
		t.Errorf("expected pending, got %s", oversight.Status)
	}
}

func TestGetOversight_TestPhase_ReturnsStoredOversight(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	saved := store.TaskOversight{
		Status: store.OversightStatusReady,
		Phases: []store.OversightPhase{{Title: "Test Phase", Summary: "Test passed"}},
	}
	if err := h.store.SaveTestOversight(task.ID, saved); err != nil {
		t.Fatalf("save test oversight: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/oversight?phase=test", nil)
	w := httptest.NewRecorder()
	h.GetOversight(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp oversightResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp.Status != store.OversightStatusReady {
		t.Errorf("expected ready, got %s", resp.Status)
	}
	if len(resp.Phases) == 0 || resp.Phases[0].Summary != "Test passed" {
		t.Errorf("expected phase summary 'Test passed', got %+v", resp.Phases)
	}
	if resp.PhaseCount != 1 {
		t.Errorf("expected phase_count=1, got %d", resp.PhaseCount)
	}
}

// TestGetOversight_TestPhase_ServedByBaseRoute confirms the test-phase summary
// is reachable through the unified base route + ?phase=test, replacing the
// removed /oversight/test endpoint. Regression guard for the route collapse.
func TestGetOversight_TestPhase_ServedByBaseRoute(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	implOversight := store.TaskOversight{
		Status: store.OversightStatusReady,
		Phases: []store.OversightPhase{{Title: "Impl", Summary: "impl summary"}},
	}
	testOversight := store.TaskOversight{
		Status: store.OversightStatusReady,
		Phases: []store.OversightPhase{{Title: "Test", Summary: "test summary"}},
	}
	if err := h.store.SaveOversight(task.ID, implOversight); err != nil {
		t.Fatalf("save impl oversight: %v", err)
	}
	if err := h.store.SaveTestOversight(task.ID, testOversight); err != nil {
		t.Fatalf("save test oversight: %v", err)
	}

	// phase=test must return the test summary, not the impl summary.
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/oversight?phase=test", nil)
	w := httptest.NewRecorder()
	h.GetOversight(w, req, task.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp oversightResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if got := resp.Phases[0].Summary; got != "test summary" {
		t.Errorf("phase=test summary: want %q, got %q", "test summary", got)
	}

	// No phase param defaults to impl.
	req = httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/oversight", nil)
	w = httptest.NewRecorder()
	h.GetOversight(w, req, task.ID)
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if got := resp.Phases[0].Summary; got != "impl summary" {
		t.Errorf("default phase summary: want %q, got %q", "impl summary", got)
	}
}

// --- GenerateMissingOversight ---

func TestGenerateMissingOversight_NoEligible(t *testing.T) {
	h := newTestHandlerForOversight(t)
	ctx := context.Background()
	// Backlog task with 0 turns — not eligible.
	_, _ = h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "backlog task", Timeout: 15})

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/generate-oversight", nil)
	w := httptest.NewRecorder()
	h.GenerateMissingOversight(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if queued, ok := resp["queued"].(float64); !ok || queued != 0 {
		t.Errorf("expected queued=0, got %v", resp["queued"])
	}
}

func TestGenerateMissingOversight_SkipsAlreadyReady(t *testing.T) {
	h := newTestHandlerForOversight(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "done task", Timeout: 15})
	_ = h.store.UpdateTaskResult(ctx, task.ID, "done", "sess", "end_turn", 1)

	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

	// Set oversight to ready.
	_ = h.store.SaveOversight(task.ID, store.TaskOversight{

		Status: store.OversightStatusReady,
		Phases: []store.OversightPhase{{Title: "Done", Summary: "Already generated"}},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/generate-oversight", nil)
	w := httptest.NewRecorder()
	h.GenerateMissingOversight(w, req)

	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if queued, ok := resp["queued"].(float64); !ok || queued != 0 {
		t.Errorf("expected queued=0 (already ready), got %v", resp["queued"])
	}
}

func TestGenerateMissingOversight_QueuesEligibleTasks(t *testing.T) {
	h := newTestHandlerForOversight(t)
	ctx := context.Background()

	// Task done with turns — oversight is pending (no file).
	task1, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task 1", Timeout: 15})
	_ = h.store.UpdateTaskResult(ctx, task1.ID, "done", "sess1", "end_turn", 2)

	_ = h.store.ForceUpdateTaskStatus(ctx, task1.ID, store.TaskStatusDone)

	task2, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task 2", Timeout: 15})
	_ = h.store.UpdateTaskResult(ctx, task2.ID, "done", "sess2", "end_turn", 1)

	_ = h.store.ForceUpdateTaskStatus(ctx, task2.ID, store.TaskStatusWaiting)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/generate-oversight", nil)
	w := httptest.NewRecorder()
	h.GenerateMissingOversight(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if total, ok := resp["total_without_oversight"].(float64); !ok || total != 2 {
		t.Errorf("expected total_without_oversight=2, got %v", resp["total_without_oversight"])
	}
}

func TestGenerateMissingOversight_LimitParam(t *testing.T) {
	h := newTestHandlerForOversight(t)
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task", Timeout: 15})
		_ = h.store.UpdateTaskResult(ctx, task.ID, "done", "sess", "end_turn", 1)

		_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

	}

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/generate-oversight?limit=2", nil)
	w := httptest.NewRecorder()
	h.GenerateMissingOversight(w, req)

	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if queued, ok := resp["queued"].(float64); !ok || queued != 2 {
		t.Errorf("expected queued=2, got %v", resp["queued"])
	}
	if total, ok := resp["total_without_oversight"].(float64); !ok || total != 4 {
		t.Errorf("expected total_without_oversight=4, got %v", resp["total_without_oversight"])
	}
}

func TestGenerateMissingOversight_SkipsZeroTurns(t *testing.T) {
	h := newTestHandlerForOversight(t)
	ctx := context.Background()

	// Done task but 0 turns.
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task with no turns", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

	// Turns remain 0 (not updated via UpdateTaskResult).

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/generate-oversight", nil)
	w := httptest.NewRecorder()
	h.GenerateMissingOversight(w, req)

	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if queued, ok := resp["queued"].(float64); !ok || queued != 0 {
		t.Errorf("expected queued=0 for zero-turn task, got %v", resp["queued"])
	}
}
