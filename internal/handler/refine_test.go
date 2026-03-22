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

	"changkun.de/x/wallfacer/internal/store"
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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test prompt", Timeout: 15})
	_ = h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test prompt", Timeout: 15})

	job := &store.RefinementJob{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
		Status:    "running",
	}
	_ = h.store.UpdateRefinementJob(ctx, task.ID, job)

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "implement feature X", Timeout: 15})

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

	// NOTE: We do not re-read from the store here because RunRefinementBackground
	// launches a goroutine that may race with the assertion and overwrite the
	// status (e.g. to "failed" when no real container runtime is configured in
	// tests). The HTTP response body above already proves the job was created
	// with "running" status before the goroutine had a chance to update it.
}

// TestStartRefinement_PreviousNonRunningAllowed verifies that a previously
// failed (non-running) refinement does not block a new one.
func TestStartRefinement_PreviousNonRunningAllowed(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test prompt", Timeout: 15})

	// Set a previously failed job.
	job := &store.RefinementJob{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
		Status:    "failed",
		Error:     "previous error",
	}
	_ = h.store.UpdateRefinementJob(ctx, task.ID, job)

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test prompt", Timeout: 15})

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test prompt", Timeout: 15})

	// A completed (done) job should not be cancellable.
	job := &store.RefinementJob{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
		Status:    "done",
	}
	_ = h.store.UpdateRefinementJob(ctx, task.ID, job)

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test prompt", Timeout: 15})

	job := &store.RefinementJob{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
		Status:    "running",
	}
	_ = h.store.UpdateRefinementJob(ctx, task.ID, job)

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test prompt", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test prompt", Timeout: 15})

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test prompt", Timeout: 15})

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "original prompt", Timeout: 15})

	// Attach a completed refinement job so its result is captured in the session.
	job := &store.RefinementJob{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
		Status:    "done",
		Result:    "detailed implementation spec from sandbox",
	}
	_ = h.store.UpdateRefinementJob(ctx, task.ID, job)

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "original prompt", Timeout: 15})

	body := `{"prompt": "manually written detailed prompt"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine/apply", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.RefineApply(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated store.Task
	_ = json.NewDecoder(w.Body).Decode(&updated)

	if updated.Prompt != "manually written detailed prompt" {
		t.Errorf("expected 'manually written detailed prompt', got %q", updated.Prompt)
	}
}

// TestRefineApply_UpdatesStorePrompt verifies the store is updated, not just the response.
func TestRefineApply_UpdatesStorePrompt(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "original prompt", Timeout: 15})

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "concurrent test prompt", Timeout: 15})

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "atomic guard prompt", Timeout: 15})

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
		switch {
		case err == nil:
			nilCount++
		case errors.Is(err, store.ErrRefinementAlreadyRunning):
			alreadyRunningCount++
		default:
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

// --- strict JSON decoding (optional body) ---

// TestStartRefinement_RejectsUnknownFieldsInBody verifies that an optional body
// with unknown JSON keys is rejected with 400.
func TestStartRefinement_RejectsUnknownFieldsInBody(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test prompt", Timeout: 15})

	body := `{"user_instructions": "be careful", "unknown_field": true}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.StartRefinement(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown fields in optional body, got %d: %s", w.Code, w.Body.String())
	}
}

// TestStartRefinement_AcceptsEmptyBody verifies that an absent body is still valid.
func TestStartRefinement_AcceptsEmptyBody(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test prompt", Timeout: 15})

	// No body — must not return 400 from the decode step.
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine", nil)
	w := httptest.NewRecorder()
	h.StartRefinement(w, req, task.ID)

	// The handler may return non-200 for other reasons (e.g. runner not running),
	// but it must not fail because the body was absent.
	if w.Code == http.StatusBadRequest {
		t.Errorf("unexpected 400 for empty body: %s", w.Body.String())
	}
}

// --- RefineDismiss ---

// TestRefineDismiss_NotFound verifies that a non-existent task ID returns 404.
func TestRefineDismiss_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+id.String()+"/refine/dismiss", nil)
	w := httptest.NewRecorder()
	h.RefineDismiss(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRefineDismiss_NotBacklog verifies that dismissing a refinement on a task
// that is not in backlog returns 400.
func TestRefineDismiss_NotBacklog(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine/dismiss", nil)
	w := httptest.NewRecorder()
	h.RefineDismiss(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-backlog task, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRefineDismiss_NoRefinement verifies that dismissing when there is no
// completed refinement returns 400.
func TestRefineDismiss_NoRefinement(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Task is in backlog with no CurrentRefinement.

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine/dismiss", nil)
	w := httptest.NewRecorder()
	h.RefineDismiss(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when no completed refinement, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRefineDismiss_RunningRefinementRejected verifies that a running (not
// yet completed) refinement job is not dismissable.
func TestRefineDismiss_RunningRefinementRejected(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Attach a running (not done) refinement job.
	job := &store.RefinementJob{
		ID:     uuid.New().String(),
		Status: store.RefinementJobStatusRunning,
	}
	if err := h.store.UpdateRefinementJob(ctx, task.ID, job); err != nil {
		t.Fatalf("UpdateRefinementJob: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine/dismiss", nil)
	w := httptest.NewRecorder()
	h.RefineDismiss(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for running (non-done) refinement, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRefineDismiss_Success verifies that dismissing a completed refinement
// clears it and returns 200 with the updated task.
func TestRefineDismiss_Success(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Attach a completed refinement job.
	job := &store.RefinementJob{
		ID:     uuid.New().String(),
		Status: store.RefinementJobStatusDone,
		Result: "refined spec",
	}
	if err := h.store.UpdateRefinementJob(ctx, task.ID, job); err != nil {
		t.Fatalf("UpdateRefinementJob: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/refine/dismiss", nil)
	w := httptest.NewRecorder()
	h.RefineDismiss(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated store.Task
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if updated.CurrentRefinement != nil {
		t.Errorf("expected CurrentRefinement to be cleared after dismiss, got %+v", updated.CurrentRefinement)
	}
}
