package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"latere.ai/x/pkg/httpjson"

	"latere.ai/x/wallfacer/internal/store"
)

// createWaitingTask creates a task in the store and moves it to waiting status.
func createWaitingTask(t *testing.T, h *Handler, prompt string) uuid.UUID {
	t.Helper()
	task, err := h.store.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: prompt, Timeout: 15})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(context.Background(), task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatalf("set waiting: %v", err)
	}
	return task.ID
}

// ─────────────────────────────────────────────────────────────────────────────
// ReviewTask
// ─────────────────────────────────────────────────────────────────────────────

func TestReviewTask_RejectsNonWaiting(t *testing.T) {
	h := newTestHandler(t)

	task, err := h.store.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "task", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/review", nil)
	req.SetPathValue("id", task.ID.String())
	w := httptest.NewRecorder()

	h.ReviewTask(w, req, task.ID)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 for non-waiting task, got %d", w.Code)
	}
}

func TestReviewTask_RejectsTaskWithoutWorktree(t *testing.T) {
	h := newTestHandler(t)
	// verifier returns immediately so the goroutine doesn't block
	h.verifier = &mockVerifier{result: finished(0, "")}

	id := createWaitingTask(t, h, "no-worktree task")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+id.String()+"/review", nil)
	req.SetPathValue("id", id.String())
	w := httptest.NewRecorder()

	h.ReviewTask(w, req, id)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for task without worktree, got %d", w.Code)
	}
	var env httpjson.ErrorEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if env.Error.Code != codeReviewNoWorktree || env.Error.Message != messageReviewNoWorktree {
		t.Errorf("error = %+v, want code %q with its fixed message", env.Error, codeReviewNoWorktree)
	}
}

// TestReviewTask_RejectsMissingWorktree: a worktree that is gone leaves no
// change to review, so the manual trigger refuses it like a task without one.
func TestReviewTask_RejectsMissingWorktree(t *testing.T) {
	h := newTestHandler(t)
	h.verifier = &mockVerifier{result: finished(0, "")}

	id := createWaitingTask(t, h, "gone")
	gone := filepath.Join(t.TempDir(), "removed")
	if err := h.store.UpdateTaskWorktrees(context.Background(), id, map[string]string{t.TempDir(): gone}, "branch"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+id.String()+"/review", nil)
	req.SetPathValue("id", id.String())
	w := httptest.NewRecorder()
	h.ReviewTask(w, req, id)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a missing worktree, got %d", w.Code)
	}
}

// TestReviewTask_Accepts202ForWaitingTaskWithWorktree: a review needs a
// worktree, not an agent session, so a task without a session is accepted.
func TestReviewTask_Accepts202ForWaitingTaskWithWorktree(t *testing.T) {
	h := newTestHandler(t)
	h.verifier = &mockVerifier{result: finished(0, "")}

	id := createWaitingTask(t, h, "verify-me")
	worktree := t.TempDir()
	gitRun(t, worktree, "init", "-b", "main")
	if err := h.store.UpdateTaskWorktrees(context.Background(), id, map[string]string{t.TempDir(): worktree}, "branch"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+id.String()+"/review", nil)
	req.SetPathValue("id", id.String())
	w := httptest.NewRecorder()

	h.ReviewTask(w, req, id)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202 for valid review trigger, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "running" {
		t.Errorf("expected status=running, got %q", resp["status"])
	}
}
