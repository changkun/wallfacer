package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

func TestForkTask_RejectsNonEligibleStatus(t *testing.T) {
	h := newTestHandler(t)
	task, _ := h.store.CreateTask(context.Background(), "a task", 15, false, "", "")
	// task is backlog — should return 422.
	body := `{"prompt":"fork it"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/fork",
		bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.ForkTask(w, req, task.ID)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

func TestForkTask_RejectsEmptyPrompt(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "source", 15, false, "", "")
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

	body := `{"prompt":"  "}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/fork",
		bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.ForkTask(w, req, task.ID)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestForkTask_RejectsNoWorktrees(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "source", 15, false, "", "")
	h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	// No worktrees set — should return 422.
	body := `{"prompt":"fork it"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/fork",
		bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.ForkTask(w, req, task.ID)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

func TestForkTask_SourceNotFound(t *testing.T) {
	h := newTestHandler(t)
	body := `{"prompt":"fork it"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+uuid.New().String()+"/fork",
		bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.ForkTask(w, req, uuid.New())
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestForkTask_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+uuid.New().String()+"/fork",
		bytes.NewBufferString("{bad json"))
	w := httptest.NewRecorder()
	h.ForkTask(w, req, uuid.New())
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestForkTask_StoreLayerForkedFrom verifies CreateForkedTask sets ForkedFrom
// correctly (no runner needed since we test only the store part).
func TestForkTask_StoreLayerForkedFrom(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	source, _ := h.store.CreateTask(ctx, "source task", 30, false, "", "")
	forked, err := h.store.CreateForkedTask(ctx, source.ID, "forked prompt", 30)
	if err != nil {
		t.Fatalf("CreateForkedTask: %v", err)
	}
	if forked.ForkedFrom == nil || *forked.ForkedFrom != source.ID {
		t.Errorf("ForkedFrom not set correctly: %v", forked.ForkedFrom)
	}

	// Verify JSON serialization includes forked_from.
	data, _ := json.Marshal(forked)
	if !strings.Contains(string(data), `"forked_from"`) {
		t.Error("JSON missing forked_from field")
	}
}
