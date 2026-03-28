package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"changkun.de/x/wallfacer/internal/store"
)

// TestGetRuntimeStatus_StatusOK verifies that GET /api/debug/runtime returns 200.
func TestGetRuntimeStatus_StatusOK(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/runtime", nil)
	w := httptest.NewRecorder()
	h.GetRuntimeStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetRuntimeStatus_ValidJSON verifies the response is valid JSON with
// the required top-level keys.
func TestGetRuntimeStatus_ValidJSON(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/runtime", nil)
	w := httptest.NewRecorder()
	h.GetRuntimeStatus(w, req)

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	requiredKeys := []string{"goroutines", "go_goroutine_count", "go_heap_alloc_bytes", "task_states", "active_containers", "timestamp"}
	for _, key := range requiredKeys {
		if _, ok := resp[key]; !ok {
			t.Errorf("expected key %q in response, not found; keys: %v", key, resp)
		}
	}
}

// TestGetRuntimeStatus_GoroutinesIsSlice verifies goroutines is a JSON array.
func TestGetRuntimeStatus_GoroutinesIsSlice(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/runtime", nil)
	w := httptest.NewRecorder()
	h.GetRuntimeStatus(w, req)

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, ok := resp["goroutines"].([]any); !ok {
		t.Errorf("expected goroutines to be a JSON array, got %T: %v", resp["goroutines"], resp["goroutines"])
	}
}

// TestGetRuntimeStatus_TaskStatesIsObject verifies task_states is a JSON object.
func TestGetRuntimeStatus_TaskStatesIsObject(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/runtime", nil)
	w := httptest.NewRecorder()
	h.GetRuntimeStatus(w, req)

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, ok := resp["task_states"].(map[string]any); !ok {
		t.Errorf("expected task_states to be a JSON object, got %T: %v", resp["task_states"], resp["task_states"])
	}
}

// TestGetRuntimeStatus_TaskStatesBacklogCount verifies that task_states.backlog
// reflects the number of tasks in the store.
func TestGetRuntimeStatus_TaskStatesBacklogCount(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	if _, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task one", Timeout: 15, Kind: store.TaskKindTask}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task two", Timeout: 15, Kind: store.TaskKindTask}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/debug/runtime", nil)
	w := httptest.NewRecorder()
	h.GetRuntimeStatus(w, req)

	var resp runtimeStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got := resp.TaskStates["backlog"]; got != 2 {
		t.Errorf("expected task_states.backlog == 2, got %d", got)
	}
}

// TestGetRuntimeStatus_GoGoroutineCountPositive verifies the goroutine count > 0.
func TestGetRuntimeStatus_GoGoroutineCountPositive(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/runtime", nil)
	w := httptest.NewRecorder()
	h.GetRuntimeStatus(w, req)

	var resp runtimeStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.GoGoroutineCount <= 0 {
		t.Errorf("expected go_goroutine_count > 0, got %d", resp.GoGoroutineCount)
	}
}

// TestGetRuntimeStatus_TimestampPresent verifies the timestamp field is non-zero.
func TestGetRuntimeStatus_TimestampPresent(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/runtime", nil)
	w := httptest.NewRecorder()
	h.GetRuntimeStatus(w, req)

	var resp runtimeStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

// TestDebugRuntimeIncludesWorkerStats verifies the runtime endpoint includes
// the worker_stats field.
func TestDebugRuntimeIncludesWorkerStats(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/runtime", nil)
	w := httptest.NewRecorder()
	h.GetRuntimeStatus(w, req)

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ws, ok := raw["worker_stats"]
	if !ok {
		t.Fatal("expected worker_stats in runtime response")
	}
	var stats struct {
		Enabled       bool `json:"enabled"`
		ActiveWorkers int  `json:"active_workers"`
	}
	if err := json.Unmarshal(ws, &stats); err != nil {
		t.Fatalf("unmarshal worker_stats: %v", err)
	}
	// Default test handler has no worker manager, so stats should be zero.
	if stats.ActiveWorkers != 0 {
		t.Errorf("expected 0 active workers, got %d", stats.ActiveWorkers)
	}
}
