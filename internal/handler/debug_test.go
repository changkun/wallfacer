package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"changkun.de/wallfacer/internal/store"
)

// TestHealth_StatusOK verifies that GET /api/debug/health returns 200.
func TestHealth_StatusOK(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/health", nil)
	w := httptest.NewRecorder()
	h.Health(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct == "" {
		t.Error("expected Content-Type to be set")
	}
}

// TestHealth_GoroutinesPositive verifies the goroutine count is greater than zero.
func TestHealth_GoroutinesPositive(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/health", nil)
	w := httptest.NewRecorder()
	h.Health(w, req)

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	goroutines, ok := resp["goroutines"].(float64)
	if !ok {
		t.Fatalf("goroutines field missing or not a number, got %T: %v", resp["goroutines"], resp["goroutines"])
	}
	if goroutines <= 0 {
		t.Errorf("expected goroutines > 0, got %v", goroutines)
	}
}

// TestHealth_UptimeNonNegative verifies uptime_seconds is >= 0.
func TestHealth_UptimeNonNegative(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/health", nil)
	w := httptest.NewRecorder()
	h.Health(w, req)

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	uptime, ok := resp["uptime_seconds"].(float64)
	if !ok {
		t.Fatalf("uptime_seconds field missing or not a number, got %T: %v", resp["uptime_seconds"], resp["uptime_seconds"])
	}
	if uptime < 0 {
		t.Errorf("expected uptime_seconds >= 0, got %v", uptime)
	}
}

// TestHealth_TasksByStatusIsObject verifies tasks_by_status is a JSON object.
func TestHealth_TasksByStatusIsObject(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/health", nil)
	w := httptest.NewRecorder()
	h.Health(w, req)

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	_, ok := resp["tasks_by_status"].(map[string]any)
	if !ok {
		t.Errorf("expected tasks_by_status to be a JSON object, got %T: %v", resp["tasks_by_status"], resp["tasks_by_status"])
	}
}

// TestHealth_TasksByStatusCounts verifies counts are accurate after creating tasks.
func TestHealth_TasksByStatusCounts(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	if _, err := h.store.CreateTask(ctx, "backlog task one", 15, false, "", store.TaskKindTask); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.CreateTask(ctx, "backlog task two", 15, false, "", store.TaskKindTask); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/debug/health", nil)
	w := httptest.NewRecorder()
	h.Health(w, req)

	var resp healthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got := resp.TasksByStatus["backlog"]; got != 2 {
		t.Errorf("expected 2 backlog tasks, got %d", got)
	}
}

// TestHealth_RunningContainersEmpty verifies running_containers has count=0 and
// an empty items list when the runner has no container runtime configured (test env).
func TestHealth_RunningContainersEmpty(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/health", nil)
	w := httptest.NewRecorder()
	h.Health(w, req)

	var resp healthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.RunningContainers.Count != 0 {
		t.Errorf("expected 0 running containers, got %d", resp.RunningContainers.Count)
	}
	if resp.RunningContainers.Items == nil {
		t.Error("expected items to be an empty slice, not nil")
	}
}
