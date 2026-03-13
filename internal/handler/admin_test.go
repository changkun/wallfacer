package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"changkun.de/wallfacer/internal/store"
)

func TestRebuildIndex_EmptyStore_Returns200(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/rebuild-index", nil)
	w := httptest.NewRecorder()
	h.RebuildIndex(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp rebuildIndexResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Repaired < 0 {
		t.Errorf("repaired count should be >= 0, got %d", resp.Repaired)
	}
}

func TestRebuildIndex_WithTasks_ReturnsJSON(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	// Create some tasks so the rebuild has something to process.
	for i := 0; i < 3; i++ {
		if _, err := h.store.CreateTask(ctx, "task prompt", 30, false, "", store.TaskKindTask); err != nil {
			t.Fatalf("CreateTask %d: %v", i, err)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/rebuild-index", nil)
	w := httptest.NewRecorder()
	h.RebuildIndex(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp rebuildIndexResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Repaired should be >= 0 (tasks may or may not need repair).
	if resp.Repaired < 0 {
		t.Errorf("repaired count should be >= 0, got %d", resp.Repaired)
	}
}

func TestRebuildIndex_ResponseIsJSON(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/rebuild-index", nil)
	w := httptest.NewRecorder()
	h.RebuildIndex(w, req)

	ct := w.Header().Get("Content-Type")
	if ct == "" {
		t.Error("expected Content-Type header")
	}
}
