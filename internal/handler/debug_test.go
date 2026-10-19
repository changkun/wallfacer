package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/store"
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

// TestHealth_TasksByStatusCounts verifies counts are accurate after creating tasks.
func TestHealth_TasksByStatusCounts(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	if _, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "backlog task one", Timeout: 15, Kind: store.TaskKindTask}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "backlog task two", Timeout: 15, Kind: store.TaskKindTask}); err != nil {
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

	t1, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "first task", Timeout: 15, Kind: store.TaskKindTask})
	t2, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "second task", Timeout: 15, Kind: store.TaskKindTask})

	req := httptest.NewRequest(http.MethodGet, "/api/debug/board", nil)
	w := httptest.NewRecorder()
	h.BoardManifest(w, req)

	var resp boardManifestResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

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

	t1, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "self task", Timeout: 15, Kind: store.TaskKindTask})
	t2, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "sibling task", Timeout: 15, Kind: store.TaskKindTask})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+t1.ID.String()+"/board", nil)
	w := httptest.NewRecorder()
	h.TaskBoardManifest(w, req, t1.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp boardManifestResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

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
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp.SizeBytes <= 0 {
		t.Errorf("expected SizeBytes > 0, got %d", resp.SizeBytes)
	}
	if resp.SizeWarn {
		t.Errorf("expected SizeWarn=false for empty store")
	}
}
