package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/store"
)

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

// Tests for Start* autoimplement goroutine launchers.
// Each function spawns a goroutine that exits when ctx is cancelled.
// Pre-cancelling the context causes the goroutine to exit almost immediately,
// allowing tests to verify the function does not panic or block indefinitely.

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

// Note (test 5 — runner path): The runner's tryAutoRetry is covered in
// internal/runner/auto_retry_test.go which tests the unexported method directly
// in the runner package.

// Note (test 6 — classifyFailure): All 5 branches of classifyFailure are
// already exhaustively covered in internal/runner/classify_failure_test.go.
