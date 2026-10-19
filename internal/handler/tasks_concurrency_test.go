package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/store"
)

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
	for range maxConc {
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

// TestUpdateEnvConfig_InvalidatesParallelLimitCache verifies that calling
// UpdateEnvConfig with a new MaxParallelTasks value invalidates the in-process
// cache so that the next call to maxConcurrentTasks reflects the new limit.
func TestUpdateEnvConfig_InvalidatesParallelLimitCache(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	// Prime the cache. Host mode caps parallelism to 1 when no explicit
	// WALLFACER_MAX_PARALLEL is set — the value is what matters for cache
	// invalidation, not the specific number.
	initial := h.maxConcurrentTasks()
	if initial < 1 {
		t.Fatalf("initial parallel cap should be positive; got %d", initial)
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

// Note (test 6 — classifyFailure): All 5 branches of classifyFailure are
// already exhaustively covered in internal/runner/classify_failure_test.go.
