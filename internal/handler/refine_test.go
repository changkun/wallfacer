package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// --- StartRefinement ---

// TestStartRefinement_NotFound verifies 404 when the task does not exist.
func TestStartRefinement_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+id.String()+"/refine", nil)
	w := httptest.NewRecorder()
	h.StartRefinement(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestStartRefinement_NotBacklog verifies 400 when the task is not in backlog.
func TestStartRefinement_NotBacklog(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test prompt", 15, false, "", "")
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine", nil)
	w := httptest.NewRecorder()
	h.StartRefinement(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-backlog task, got %d", w.Code)
	}
}

// TestStartRefinement_AlreadyRunning verifies 409 when a refinement is already running.
func TestStartRefinement_AlreadyRunning(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test prompt", 15, false, "", "")

	job := &store.RefinementJob{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
		Status:    "running",
	}
	h.store.UpdateRefinementJob(ctx, task.ID, job)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine", nil)
	w := httptest.NewRecorder()
	h.StartRefinement(w, req, task.ID)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 when refinement already running, got %d", w.Code)
	}
}

// TestStartRefinement_Success verifies that a new refinement job is created and
// the handler returns 202 with the updated task.
func TestStartRefinement_Success(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "implement feature X", 15, false, "", "")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine", nil)
	w := httptest.NewRecorder()
	h.StartRefinement(w, req, task.ID)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var updated store.Task
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.CurrentRefinement == nil {
		t.Fatal("expected CurrentRefinement to be set")
	}
	if updated.CurrentRefinement.Status != "running" {
		t.Errorf("expected refinement status 'running', got %q", updated.CurrentRefinement.Status)
	}
	if updated.CurrentRefinement.ID == "" {
		t.Error("expected refinement job to have a non-empty ID")
	}

	// Confirm the store reflects the new job.
	stored, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CurrentRefinement == nil || stored.CurrentRefinement.Status != "running" {
		t.Error("expected store to have a running refinement job")
	}
}

// TestStartRefinement_PreviousNonRunningAllowed verifies that a previously
// failed (non-running) refinement does not block a new one.
func TestStartRefinement_PreviousNonRunningAllowed(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test prompt", 15, false, "", "")

	// Set a previously failed job.
	job := &store.RefinementJob{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
		Status:    "failed",
		Error:     "previous error",
	}
	h.store.UpdateRefinementJob(ctx, task.ID, job)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine", nil)
	w := httptest.NewRecorder()
	h.StartRefinement(w, req, task.ID)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202 when prior refinement was not running, got %d: %s", w.Code, w.Body.String())
	}
}

// --- CancelRefinement ---

// TestCancelRefinement_NotFound verifies 404 when the task does not exist.
func TestCancelRefinement_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/tasks/"+id.String()+"/refine", nil)
	w := httptest.NewRecorder()
	h.CancelRefinement(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestCancelRefinement_NoRefinementRunning verifies 400 when no refinement is active.
func TestCancelRefinement_NoRefinementRunning(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test prompt", 15, false, "", "")

	req := httptest.NewRequest(http.MethodDelete, "/api/tasks/"+task.ID.String()+"/refine", nil)
	w := httptest.NewRecorder()
	h.CancelRefinement(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when no refinement running, got %d", w.Code)
	}
}

// TestCancelRefinement_NonRunningJobRejected verifies 400 when the job is done, not running.
func TestCancelRefinement_NonRunningJobRejected(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test prompt", 15, false, "", "")

	// A completed (done) job should not be cancellable.
	job := &store.RefinementJob{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
		Status:    "done",
	}
	h.store.UpdateRefinementJob(ctx, task.ID, job)

	req := httptest.NewRequest(http.MethodDelete, "/api/tasks/"+task.ID.String()+"/refine", nil)
	w := httptest.NewRecorder()
	h.CancelRefinement(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-running job, got %d", w.Code)
	}
}

// TestCancelRefinement_Success verifies that cancellation marks the job as failed
// with a "cancelled by user" message.
func TestCancelRefinement_Success(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test prompt", 15, false, "", "")

	job := &store.RefinementJob{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
		Status:    "running",
	}
	h.store.UpdateRefinementJob(ctx, task.ID, job)

	req := httptest.NewRequest(http.MethodDelete, "/api/tasks/"+task.ID.String()+"/refine", nil)
	w := httptest.NewRecorder()
	h.CancelRefinement(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated store.Task
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.CurrentRefinement == nil {
		t.Fatal("expected CurrentRefinement to be present after cancel")
	}
	if updated.CurrentRefinement.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", updated.CurrentRefinement.Status)
	}
	if updated.CurrentRefinement.Error != "cancelled by user" {
		t.Errorf("expected error 'cancelled by user', got %q", updated.CurrentRefinement.Error)
	}

	// Confirm the store reflects the cancelled state.
	stored, _ := h.store.GetTask(ctx, task.ID)
	if stored.CurrentRefinement == nil || stored.CurrentRefinement.Status != "failed" {
		t.Error("expected store to have failed refinement after cancel")
	}
}

// --- RefineApply ---

// TestRefineApply_NotFound verifies 404 for a non-existent task.
func TestRefineApply_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	body := `{"prompt": "new detailed prompt"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+id.String()+"/refine/apply", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.RefineApply(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestRefineApply_NotBacklog verifies 400 when the task is not in backlog.
func TestRefineApply_NotBacklog(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test prompt", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

	body := `{"prompt": "new prompt"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine/apply", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.RefineApply(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-backlog task, got %d", w.Code)
	}
}

// TestRefineApply_InvalidJSON verifies 400 for a malformed request body.
func TestRefineApply_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test prompt", 15, false, "", "")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine/apply", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	h.RefineApply(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

// TestRefineApply_EmptyPrompt verifies 400 when the prompt is blank.
func TestRefineApply_EmptyPrompt(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test prompt", 15, false, "", "")

	body := `{"prompt": "   "}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine/apply", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.RefineApply(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty prompt, got %d", w.Code)
	}
}

// TestRefineApply_Success verifies that the task prompt is updated, the old
// prompt is moved to history, and a refinement session is recorded.
func TestRefineApply_Success(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "original prompt", 15, false, "", "")

	// Attach a completed refinement job so its result is captured in the session.
	job := &store.RefinementJob{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
		Status:    "done",
		Result:    "detailed implementation spec from sandbox",
	}
	h.store.UpdateRefinementJob(ctx, task.ID, job)

	body := `{"prompt": "detailed refined prompt"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine/apply", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.RefineApply(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated store.Task
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// New prompt must be applied.
	if updated.Prompt != "detailed refined prompt" {
		t.Errorf("expected prompt 'detailed refined prompt', got %q", updated.Prompt)
	}
	// Old prompt must be in history.
	if len(updated.PromptHistory) == 0 {
		t.Fatal("expected PromptHistory to be non-empty")
	}
	if updated.PromptHistory[0] != "original prompt" {
		t.Errorf("expected PromptHistory[0]='original prompt', got %q", updated.PromptHistory[0])
	}
	// A refinement session must be recorded with the start prompt.
	if len(updated.RefineSessions) == 0 {
		t.Fatal("expected at least one RefinementSession")
	}
	if updated.RefineSessions[0].StartPrompt != "original prompt" {
		t.Errorf("expected session StartPrompt='original prompt', got %q", updated.RefineSessions[0].StartPrompt)
	}
}

// TestRefineApply_NoCurrentRefinement verifies that applying without a prior
// refinement job still succeeds (manual prompt edit path).
func TestRefineApply_NoCurrentRefinement(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "original prompt", 15, false, "", "")

	body := `{"prompt": "manually written detailed prompt"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine/apply", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.RefineApply(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated store.Task
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Prompt != "manually written detailed prompt" {
		t.Errorf("expected 'manually written detailed prompt', got %q", updated.Prompt)
	}
}

// TestRefineApply_UpdatesStorePrompt verifies the store is updated, not just the response.
func TestRefineApply_UpdatesStorePrompt(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "original prompt", 15, false, "", "")

	body := `{"prompt": "store-verified prompt"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine/apply", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.RefineApply(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Read back from store to confirm persistence.
	stored, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Prompt != "store-verified prompt" {
		t.Errorf("store prompt not updated: got %q", stored.Prompt)
	}
	if len(stored.PromptHistory) == 0 || stored.PromptHistory[0] != "original prompt" {
		t.Errorf("expected 'original prompt' in store history, got %v", stored.PromptHistory)
	}
}

// --- Concurrency tests ---

// TestStartRefinement_ConcurrentRequestsOnlyOneSucceeds fires two concurrent
// POST /api/tasks/{id}/refine requests and asserts exactly one returns 202 and
// the other returns 409.
func TestStartRefinement_ConcurrentRequestsOnlyOneSucceeds(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "concurrent test prompt", 15, false, "", "")

	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine", nil)
			w := httptest.NewRecorder()
			h.StartRefinement(w, req, task.ID)
			codes[idx] = w.Code
		}(i)
	}
	wg.Wait()

	accepted := 0
	conflict := 0
	for _, code := range codes {
		switch code {
		case http.StatusAccepted:
			accepted++
		case http.StatusConflict:
			conflict++
		default:
			t.Errorf("unexpected status code: %d", code)
		}
	}
	if accepted != 1 {
		t.Errorf("expected exactly 1 accepted (202), got %d", accepted)
	}
	if conflict != 1 {
		t.Errorf("expected exactly 1 conflict (409), got %d", conflict)
	}
}

// TestStartRefinementJobIfIdle_AtomicGuard calls StartRefinementJobIfIdle twice
// from two goroutines and asserts exactly one succeeds and the other returns
// ErrRefinementAlreadyRunning.
func TestStartRefinementJobIfIdle_AtomicGuard(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "atomic guard prompt", 15, false, "", "")

	makeJob := func() *store.RefinementJob {
		return &store.RefinementJob{
			ID:        uuid.New().String(),
			CreatedAt: time.Now(),
			Status:    "running",
		}
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = h.store.StartRefinementJobIfIdle(ctx, task.ID, makeJob())
		}(i)
	}
	wg.Wait()

	nilCount := 0
	alreadyRunningCount := 0
	for _, err := range errs {
		if err == nil {
			nilCount++
		} else if errors.Is(err, store.ErrRefinementAlreadyRunning) {
			alreadyRunningCount++
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if nilCount != 1 {
		t.Errorf("expected exactly 1 success (nil), got %d", nilCount)
	}
	if alreadyRunningCount != 1 {
		t.Errorf("expected exactly 1 ErrRefinementAlreadyRunning, got %d", alreadyRunningCount)
	}
}
