package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
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

// --- GetSpanStats tests ---

// TestGetSpanStats_EmptyStore verifies the response shape when no tasks exist.
func TestGetSpanStats_EmptyStore(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/spans", nil)
	w := httptest.NewRecorder()
	h.GetSpanStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp spanStatsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TasksScanned != 0 {
		t.Errorf("expected tasks_scanned=0, got %d", resp.TasksScanned)
	}
	if resp.SpansTotal != 0 {
		t.Errorf("expected spans_total=0, got %d", resp.SpansTotal)
	}
	if resp.Phases == nil {
		t.Error("expected phases to be a non-nil map")
	}
	if len(resp.Phases) != 0 {
		t.Errorf("expected empty phases map, got %d entries", len(resp.Phases))
	}
}

// TestGetSpanStats_KnownSpanPairs seeds a task with deterministic span events
// and verifies the computed statistics.
func TestGetSpanStats_KnownSpanPairs(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTask(ctx, "test", 15, false, "", store.TaskKindTask)
	if err != nil {
		t.Fatal(err)
	}

	// Insert three agent_turn spans with fixed durations by sleeping between events.
	// We sleep at least 10ms per span so DurationMs is reliably > 0.
	for i := 0; i < 3; i++ {
		h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{
			Phase: "agent_turn",
			Label: "agent_turn_" + string(rune('1'+i)),
		})
		time.Sleep(10 * time.Millisecond)
		h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanEnd, store.SpanData{
			Phase: "agent_turn",
			Label: "agent_turn_" + string(rune('1'+i)),
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/debug/spans", nil)
	w := httptest.NewRecorder()
	h.GetSpanStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp spanStatsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.TasksScanned != 1 {
		t.Errorf("expected tasks_scanned=1, got %d", resp.TasksScanned)
	}
	if resp.SpansTotal != 3 {
		t.Errorf("expected spans_total=3, got %d", resp.SpansTotal)
	}

	ps, ok := resp.Phases["agent_turn"]
	if !ok {
		t.Fatal("expected 'agent_turn' phase in response")
	}
	if ps.Count != 3 {
		t.Errorf("expected count=3, got %d", ps.Count)
	}
	if ps.MinMs < 0 {
		t.Errorf("expected min_ms >= 0, got %d", ps.MinMs)
	}
	if ps.MaxMs < ps.MinMs {
		t.Errorf("expected max_ms >= min_ms, got max=%d min=%d", ps.MaxMs, ps.MinMs)
	}
	if ps.P50Ms < ps.MinMs || ps.P50Ms > ps.MaxMs {
		t.Errorf("expected p50_ms in [min, max], got p50=%d min=%d max=%d", ps.P50Ms, ps.MinMs, ps.MaxMs)
	}
}

// TestGetSpanStats_IncludesArchived verifies that archived tasks are counted.
func TestGetSpanStats_IncludesArchived(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTask(ctx, "archived task", 15, false, "", store.TaskKindTask)
	if err != nil {
		t.Fatal(err)
	}

	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{
		Phase: "worktree_setup",
		Label: "worktree_setup",
	})
	time.Sleep(5 * time.Millisecond)
	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanEnd, store.SpanData{
		Phase: "worktree_setup",
		Label: "worktree_setup",
	})

	// Archive the task.
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetTaskArchived(ctx, task.ID, true); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/debug/spans", nil)
	w := httptest.NewRecorder()
	h.GetSpanStats(w, req)

	var resp spanStatsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.TasksScanned != 1 {
		t.Errorf("expected tasks_scanned=1 (archived task included), got %d", resp.TasksScanned)
	}
	if resp.SpansTotal != 1 {
		t.Errorf("expected spans_total=1, got %d", resp.SpansTotal)
	}
	if _, ok := resp.Phases["worktree_setup"]; !ok {
		t.Error("expected 'worktree_setup' phase from archived task")
	}
}

// TestGetSpanStats_PercentileIndexSingleElement verifies that with N=1
// all percentiles resolve to the single value.
func TestGetSpanStats_PercentileIndexSingleElement(t *testing.T) {
	cases := []int{50, 95, 99}
	for _, pct := range cases {
		idx := percentileIndex(1, pct)
		if idx != 0 {
			t.Errorf("percentileIndex(1, %d) = %d; want 0", pct, idx)
		}
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

// --- BoardManifest tests ---

// TestBoardManifest_Empty verifies that GET /api/debug/board returns 200 with
// an empty task list when the store has no tasks.
func TestBoardManifest_Empty(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/board", nil)
	w := httptest.NewRecorder()
	h.BoardManifest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp boardManifestResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Manifest.Tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(resp.Manifest.Tasks))
	}
}

// TestBoardManifest_ContainsBothTasks verifies that both created tasks appear in
// the manifest, and that none of them has IsSelf=true (debug endpoint has no self-task).
func TestBoardManifest_ContainsBothTasks(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	t1, _ := h.store.CreateTask(ctx, "first task", 15, false, "", store.TaskKindTask)
	t2, _ := h.store.CreateTask(ctx, "second task", 15, false, "", store.TaskKindTask)

	req := httptest.NewRequest(http.MethodGet, "/api/debug/board", nil)
	w := httptest.NewRecorder()
	h.BoardManifest(w, req)

	var resp boardManifestResponse
	json.NewDecoder(w.Body).Decode(&resp)

	ids := map[string]bool{}
	for _, bt := range resp.Manifest.Tasks {
		ids[bt.ID] = true
		if bt.IsSelf {
			t.Errorf("task %s: expected IsSelf=false from debug endpoint", bt.ID)
		}
	}
	if !ids[t1.ID.String()] {
		t.Errorf("task1 %s not found in manifest", t1.ID)
	}
	if !ids[t2.ID.String()] {
		t.Errorf("task2 %s not found in manifest", t2.ID)
	}
}

// TestTaskBoardManifest_NotFound verifies that a 404 is returned for an unknown UUID.
func TestTaskBoardManifest_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+id.String()+"/board", nil)
	w := httptest.NewRecorder()
	h.TaskBoardManifest(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestTaskBoardManifest_IsSelfTrue verifies that the queried task has IsSelf=true
// in the manifest while the sibling task has IsSelf=false.
func TestTaskBoardManifest_IsSelfTrue(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	t1, _ := h.store.CreateTask(ctx, "self task", 15, false, "", store.TaskKindTask)
	t2, _ := h.store.CreateTask(ctx, "sibling task", 15, false, "", store.TaskKindTask)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+t1.ID.String()+"/board", nil)
	w := httptest.NewRecorder()
	h.TaskBoardManifest(w, req, t1.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp boardManifestResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Manifest.SelfTaskID != t1.ID.String() {
		t.Errorf("SelfTaskID: got %s, want %s", resp.Manifest.SelfTaskID, t1.ID)
	}
	for _, bt := range resp.Manifest.Tasks {
		switch bt.ID {
		case t1.ID.String():
			if !bt.IsSelf {
				t.Errorf("task1: expected IsSelf=true")
			}
		case t2.ID.String():
			if bt.IsSelf {
				t.Errorf("task2: expected IsSelf=false")
			}
		}
	}
}

// TestBoardManifest_SizeMetadata verifies that SizeBytes > 0 and SizeWarn=false
// for a small (empty) store.
func TestBoardManifest_SizeMetadata(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/board", nil)
	w := httptest.NewRecorder()
	h.BoardManifest(w, req)

	var resp boardManifestResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.SizeBytes <= 0 {
		t.Errorf("expected SizeBytes > 0, got %d", resp.SizeBytes)
	}
	if resp.SizeWarn {
		t.Errorf("expected SizeWarn=false for empty store")
	}
}
