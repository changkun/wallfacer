package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// waitForBackground sleeps for ms milliseconds to allow untracked background
// goroutines (e.g. commit, oversight generation) to complete their disk writes
// before TempDir cleanup removes the store directory.
func waitForBackground(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// setTaskSessionID is a helper that sets a session ID on a task via UpdateTaskResult.
func setTaskSessionID(t *testing.T, h *Handler, id uuid.UUID, sessionID string) {
	t.Helper()
	ctx := context.Background()
	if err := h.store.UpdateTaskResult(ctx, id, "done", sessionID, "end_turn", 1); err != nil {
		t.Fatalf("set session ID: %v", err)
	}
}

// --- SubmitFeedback ---

func TestSubmitFeedback_RejectsInvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/feedback", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	h.SubmitFeedback(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestSubmitFeedback_RejectsEmptyMessage(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/feedback",
		strings.NewReader(`{"message": "   "}`))
	w := httptest.NewRecorder()
	h.SubmitFeedback(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty message, got %d", w.Code)
	}
}

func TestSubmitFeedback_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+id.String()+"/feedback",
		strings.NewReader(`{"message": "hello"}`))
	w := httptest.NewRecorder()
	h.SubmitFeedback(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestSubmitFeedback_RejectsNonWaiting(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	// Task is in "backlog", not "waiting".

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/feedback",
		strings.NewReader(`{"message": "hello"}`))
	w := httptest.NewRecorder()
	h.SubmitFeedback(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-waiting task, got %d", w.Code)
	}
}

func TestSubmitFeedback_Success(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/feedback",
		strings.NewReader(`{"message": "please continue"}`))
	w := httptest.NewRecorder()
	h.SubmitFeedback(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "resumed" {
		t.Errorf("expected status=resumed, got %q", resp["status"])
	}

	// Task should now be in_progress.
	updated, _ := h.store.GetTask(ctx, task.ID)
	// SubmitFeedback transitions waiting -> in_progress synchronously, but the
	// real runner starts in background and may fail quickly in tests (no runtime
	// command configured), moving the task to failed.
	if updated.Status != store.TaskStatusInProgress && updated.Status != store.TaskStatusFailed {
		t.Errorf("expected in_progress or failed, got %s", updated.Status)
	}

	// A feedback event should exist.
	events, _ := h.store.GetEvents(ctx, task.ID)
	found := false
	for _, ev := range events {
		if ev.EventType == store.EventTypeFeedback {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a feedback event")
	}
}

// --- CompleteTask ---

func TestCompleteTask_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+id.String()+"/done", nil)
	w := httptest.NewRecorder()
	h.CompleteTask(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestCompleteTask_RejectsNonWaiting(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	// In backlog — not waiting.

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/done", nil)
	w := httptest.NewRecorder()
	h.CompleteTask(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCompleteTask_NoSession_GoesToDone(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	// No session ID set, so CompleteTask should go directly to done.

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/done", nil)
	w := httptest.NewRecorder()
	h.CompleteTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated, _ := h.store.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusDone {
		t.Errorf("expected done, got %s", updated.Status)
	}
}

func TestCompleteTask_WithSession_GoesToCommitting(t *testing.T) {
	h := newTestHandler(t)
	// The background commit goroutine writes events to disk; wait for it to finish
	// before TempDir cleanup removes the store directory (LIFO: sleep runs first).
	t.Cleanup(func() { waitForBackground(200) })
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	setTaskSessionID(t, h, task.ID, "sess-123")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/done", nil)
	w := httptest.NewRecorder()
	h.CompleteTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// After the request the task should be in committing (or possibly done/failed
	// if the background goroutine ran fast — but committing is the initial state).
	updated, _ := h.store.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusCommitting && updated.Status != store.TaskStatusDone && updated.Status != store.TaskStatusFailed {
		t.Errorf("unexpected status %s", updated.Status)
	}
}

// --- CancelTask ---

func TestCancelTask_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+id.String()+"/cancel", nil)
	w := httptest.NewRecorder()
	h.CancelTask(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestCancelTask_RejectsDone(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/cancel", nil)
	w := httptest.NewRecorder()
	h.CancelTask(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for done task, got %d", w.Code)
	}
}

func TestCancelTask_BacklogTask(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/cancel", nil)
	w := httptest.NewRecorder()
	h.CancelTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated, _ := h.store.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusCancelled {
		t.Errorf("expected cancelled, got %s", updated.Status)
	}
}

func TestCancelTask_WaitingTask(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/cancel", nil)
	w := httptest.NewRecorder()
	h.CancelTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	updated, _ := h.store.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusCancelled {
		t.Errorf("expected cancelled, got %s", updated.Status)
	}
}

func TestCancelTask_FailedTask(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/cancel", nil)
	w := httptest.NewRecorder()
	h.CancelTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	updated, _ := h.store.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusCancelled {
		t.Errorf("expected cancelled, got %s", updated.Status)
	}
}

func TestCancelTask_InsertsCancelledEvent(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/cancel", nil)
	w := httptest.NewRecorder()
	h.CancelTask(w, req, task.ID)

	events, _ := h.store.GetEvents(ctx, task.ID)
	found := false
	for _, ev := range events {
		if ev.EventType == store.EventTypeStateChange {
			var data map[string]string
			if err := json.Unmarshal(ev.Data, &data); err == nil {
				if data["to"] == string(store.TaskStatusCancelled) {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Error("expected state_change event with to=cancelled")
	}
}

// --- ResumeTask ---

func TestResumeTask_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+id.String()+"/resume", nil)
	w := httptest.NewRecorder()
	h.ResumeTask(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestResumeTask_RejectsNonFailed(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	// Task is in backlog, not failed.

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/resume", nil)
	w := httptest.NewRecorder()
	h.ResumeTask(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-failed task, got %d", w.Code)
	}
}

func TestResumeTask_RejectsNoSession(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)
	// No session ID set.

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/resume", nil)
	w := httptest.NewRecorder()
	h.ResumeTask(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for task with no session, got %d", w.Code)
	}
}

func TestResumeTask_Success(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)
	setTaskSessionID(t, h, task.ID, "session-xyz")
	// ResumeTask requires status to be "failed" after session is set.
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/resume", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.ResumeTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "resumed" {
		t.Errorf("expected status=resumed, got %q", resp["status"])
	}

	// Task should be in_progress.
	updated, _ := h.store.GetTask(ctx, task.ID)
	// ResumeTask transitions failed -> in_progress synchronously, but the
	// real runner starts in background and may fail quickly in tests (no runtime
	// command configured), moving the task back to failed.
	if updated.Status != store.TaskStatusInProgress && updated.Status != store.TaskStatusFailed {
		t.Errorf("expected in_progress or failed, got %s", updated.Status)
	}
}

// --- ArchiveAllDone ---

func TestArchiveAllDone_NoTasks(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/archive-all-done", nil)
	w := httptest.NewRecorder()
	h.ArchiveAllDone(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if archived, ok := resp["archived"].(float64); !ok || archived != 0 {
		t.Errorf("expected archived=0, got %v", resp["archived"])
	}
}

func TestArchiveAllDone_ArchivesDoneTasks(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task1, _ := h.store.CreateTask(ctx, "done task 1", 15, false, "", "")
	task2, _ := h.store.CreateTask(ctx, "done task 2", 15, false, "", "")
	backlogTask, _ := h.store.CreateTask(ctx, "backlog task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task1.ID, store.TaskStatusDone)
	h.store.ForceUpdateTaskStatus(ctx, task2.ID, store.TaskStatusDone)
	_ = backlogTask

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/archive-all-done", nil)
	w := httptest.NewRecorder()
	h.ArchiveAllDone(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if archived, ok := resp["archived"].(float64); !ok || archived != 2 {
		t.Errorf("expected archived=2, got %v", resp["archived"])
	}

	// Verify the backlog task was not archived.
	tasks, _ := h.store.ListTasks(ctx, false)
	if len(tasks) != 1 || tasks[0].ID != backlogTask.ID {
		t.Errorf("expected only backlog task remaining, got %d tasks", len(tasks))
	}
}

func TestArchiveAllDone_ArchivesCancelledTasks(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "cancelled task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusCancelled)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/archive-all-done", nil)
	w := httptest.NewRecorder()
	h.ArchiveAllDone(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if archived, ok := resp["archived"].(float64); !ok || archived != 1 {
		t.Errorf("expected archived=1, got %v", resp["archived"])
	}
}

// --- ArchiveTask ---

func TestArchiveTask_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+id.String()+"/archive", nil)
	w := httptest.NewRecorder()
	h.ArchiveTask(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestArchiveTask_RejectsNonDone(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	// Task is in backlog.

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/archive", nil)
	w := httptest.NewRecorder()
	h.ArchiveTask(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for backlog task, got %d", w.Code)
	}
}

func TestArchiveTask_ArchivesDoneTask(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "done task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/archive", nil)
	w := httptest.NewRecorder()
	h.ArchiveTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Archived tasks should not appear in the default list.
	tasks, _ := h.store.ListTasks(ctx, false)
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks after archive, got %d", len(tasks))
	}
}

func TestArchiveTask_ArchivesCancelledTask(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "cancelled", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusCancelled)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/archive", nil)
	w := httptest.NewRecorder()
	h.ArchiveTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// --- UnarchiveTask ---

func TestUnarchiveTask_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+id.String()+"/unarchive", nil)
	w := httptest.NewRecorder()
	h.UnarchiveTask(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestUnarchiveTask_Success(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "done task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)
	h.store.SetTaskArchived(ctx, task.ID, true)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/unarchive", nil)
	w := httptest.NewRecorder()
	h.UnarchiveTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Task should be visible in non-archived list.
	tasks, _ := h.store.ListTasks(ctx, false)
	if len(tasks) != 1 {
		t.Errorf("expected 1 task after unarchive, got %d", len(tasks))
	}
}

func TestUnarchiveTask_InsertsEvent(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "done task", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)
	h.store.SetTaskArchived(ctx, task.ID, true)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/unarchive", nil)
	w := httptest.NewRecorder()
	h.UnarchiveTask(w, req, task.ID)

	events, _ := h.store.GetEvents(ctx, task.ID)
	found := false
	for _, ev := range events {
		if ev.EventType == store.EventTypeStateChange {
			var data map[string]string
			if err := json.Unmarshal(ev.Data, &data); err == nil {
				if data["to"] == "unarchived" {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Error("expected state_change event with to=unarchived")
	}
}

// --- SyncTask ---

func TestSyncTask_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+id.String()+"/sync", nil)
	w := httptest.NewRecorder()
	h.SyncTask(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestSyncTask_RejectsBacklog(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/sync", nil)
	w := httptest.NewRecorder()
	h.SyncTask(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for backlog task, got %d", w.Code)
	}
}

func TestSyncTask_RejectsNoWorktrees(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/sync", nil)
	w := httptest.NewRecorder()
	h.SyncTask(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for task without worktrees, got %d", w.Code)
	}
}

func TestSyncTask_WaitingWithWorktrees(t *testing.T) {
	repo := setupRepo(t)
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	// Provide a worktree path (repo itself, as a stand-in).
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: repo}, "main")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/sync", nil)
	w := httptest.NewRecorder()
	h.SyncTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "syncing" {
		t.Errorf("expected status=syncing, got %q", resp["status"])
	}
}

func TestSyncTask_FailedWithWorktrees(t *testing.T) {
	repo := setupRepo(t)
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")
	h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)
	h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: repo}, "main")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/sync", nil)
	w := httptest.NewRecorder()
	h.SyncTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "syncing" {
		t.Errorf("expected status=syncing, got %q", resp["status"])
	}
}
