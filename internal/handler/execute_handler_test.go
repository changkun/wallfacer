package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)


	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/feedback",
		strings.NewReader(`{"message": "please continue"}`))
	w := httptest.NewRecorder()
	h.SubmitFeedback(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

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
	t.Cleanup(func() { waitForBackground(2000) })
	ctx := context.Background()
	repo := setupRepo(t)
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: repo}, "main")

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

func TestCompleteTask_WithSessionRejectsMissingWorktrees(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	setTaskSessionID(t, h, task.ID, "sess-123")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/done", nil)
	w := httptest.NewRecorder()
	h.CompleteTask(w, req, task.ID)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}

	updated, _ := h.store.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusWaiting {
		t.Fatalf("expected waiting after rejected completion, got %s", updated.Status)
	}
}

func TestCompleteTask_WithSessionRestoresMissingWorktreeDir(t *testing.T) {
	h := newTestHandler(t)
	t.Cleanup(func() { waitForBackground(2000) })
	ctx := context.Background()
	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt-missing")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch")

	setTaskSessionID(t, h, task.ID, "sess-123")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/done", nil)
	w := httptest.NewRecorder()
	h.CompleteTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated *store.Task
	for range 100 {
		updated, _ = h.store.GetTask(ctx, task.ID)
		if updated != nil && updated.Status == store.TaskStatusDone {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if updated == nil || updated.Status != store.TaskStatusDone {
		t.Fatalf("expected done after restoring missing worktree, got %v", updated.Status)
	}
}

// --- WaitingToDone must go through commit pipeline ---

// TestWaitingToDone_PATCHBlocked verifies that the PATCH handler rejects a
// direct waiting→done transition, forcing callers through the POST /done
// endpoint (CompleteTask) which runs the commit pipeline.
func TestWaitingToDone_PATCHBlocked(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)


	body := `{"status":"done"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+task.ID.String(),
		strings.NewReader(body))
	w := httptest.NewRecorder()
	h.UpdateTask(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for waiting→done via PATCH, got %d: %s", w.Code, w.Body.String())
	}

	// Task must still be waiting.
	updated, _ := h.store.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusWaiting {
		t.Errorf("task status changed to %s, want waiting", updated.Status)
	}
}

// TestWaitingToDone_StateMachineBlocked verifies the underlying state machine
// rejects waiting→done via UpdateTaskStatus (not ForceUpdateTaskStatus).
func TestWaitingToDone_StateMachineBlocked(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)


	err := h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)
	if err == nil {
		t.Fatal("expected error for waiting→done via UpdateTaskStatus, got nil")
	}

	updated, _ := h.store.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusWaiting {
		t.Errorf("task status changed to %s, want waiting", updated.Status)
	}
}

// TestWaitingToDone_CompleteTaskCommits verifies that POST /done (CompleteTask)
// triggers the commit pipeline (waiting→committing) when a session exists,
// rather than skipping directly to done.
func TestWaitingToDone_CompleteTaskCommits(t *testing.T) {
	h := newTestHandler(t)
	t.Cleanup(func() { waitForBackground(2000) })
	ctx := context.Background()
	repo := setupRepo(t)
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: repo}, "main")

	setTaskSessionID(t, h, task.ID, "sess-abc")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/done", nil)
	w := httptest.NewRecorder()
	h.CompleteTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Immediately after the handler returns, the task should be in committing
	// (the commit goroutine runs in the background). It might also be done/failed
	// if the goroutine completed very quickly.
	updated, _ := h.store.GetTask(ctx, task.ID)
	switch updated.Status {
	case store.TaskStatusCommitting, store.TaskStatusDone, store.TaskStatusFailed:
		// OK — commit pipeline was triggered.
	default:
		t.Errorf("expected committing/done/failed, got %s — commit pipeline was not triggered", updated.Status)
	}
}

func TestCompleteTask_CommitMessageFailureFallsBackAndCompletes(t *testing.T) {
	h := newTestHandler(t)
	h.SetAutopilot(true)
	h.SetAutotest(true)
	h.SetAutosubmit(true)
	t.Cleanup(func() { waitForBackground(2000) })
	ctx := context.Background()

	repo := setupRepo(t)
	wt := filepath.Join(t.TempDir(), "wt-commit-fail")
	gitRun(t, repo, "worktree", "add", "-b", "task-commit-fail", wt, "HEAD")
	if err := os.WriteFile(filepath.Join(wt, "feature.txt"), []byte("new work\n"), 0644); err != nil {
		t.Fatal(err)
	}

	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-commit-fail")

	setTaskSessionID(t, h, task.ID, "sess-fail")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/done", nil)
	w := httptest.NewRecorder()
	h.CompleteTask(w, req, task.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated *store.Task
	for range 100 {
		updated, _ = h.store.GetTask(ctx, task.ID)
		if updated != nil && updated.Status == store.TaskStatusDone {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if updated == nil || updated.Status != store.TaskStatusDone {
		t.Fatalf("expected task to reach done via fallback commit message, got %v", updated.Status)
	}

	if got := gitRun(t, repo, "rev-list", "--count", "HEAD"); got != "2" {
		t.Fatalf("expected fallback commit to land on repo, got %s commits", got)
	}
	if !h.AutopilotEnabled() || !h.AutotestEnabled() || !h.AutosubmitEnabled() {
		t.Fatal("expected fallback commit path to leave automation toggles unchanged")
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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

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

func TestCancelTask_KillsRefineContainer(t *testing.T) {
	m := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, m)
	ctx := context.Background()

	task, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "backlog with refinement", Timeout: 15})
	// Task is in backlog (default) — no need to change status.

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/cancel", nil)
	w := httptest.NewRecorder()
	h.CancelTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// KillRefineContainer must have been called regardless of task status.
	calls := m.KillRefineCalls()
	if len(calls) != 1 || calls[0] != task.ID {
		t.Errorf("expected KillRefineContainer called with %v, got %v", task.ID, calls)
	}
}

func TestCancelTask_MarksRunningRefinementFailed(t *testing.T) {
	m := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, m)
	ctx := context.Background()

	task, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "backlog with running refinement", Timeout: 15})

	// Attach a running refinement job.
	job := &store.RefinementJob{
		ID:     uuid.New().String(),
		Status: store.RefinementJobStatusRunning,
	}
	if err := s.UpdateRefinementJob(ctx, task.ID, job); err != nil {
		t.Fatalf("UpdateRefinementJob: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/cancel", nil)
	w := httptest.NewRecorder()
	h.CancelTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The refinement job must be marked failed in the store.
	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CurrentRefinement == nil {
		t.Fatal("expected CurrentRefinement to be present after cancel")
	}
	if updated.CurrentRefinement.Status != store.RefinementJobStatusFailed {
		t.Errorf("expected refinement status %q, got %q", store.RefinementJobStatusFailed, updated.CurrentRefinement.Status)
	}
	if updated.CurrentRefinement.Error != "task cancelled" {
		t.Errorf("expected error 'task cancelled', got %q", updated.CurrentRefinement.Error)
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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)

	setTaskSessionID(t, h, task.ID, "session-xyz")
	// ResumeTask requires status to be "failed" after session is set.
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)


	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/resume", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.ResumeTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)

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

// --- Resume/Feedback bypass capacity ---

// createInProgressTask creates a task and moves it to in_progress status.
func createInProgressTask(t *testing.T, h *Handler, prompt string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: prompt, Timeout: 15})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("set in_progress: %v", err)
	}
	return task.ID
}

// createFailedTaskWithSession creates a failed task that has a session ID,
// ready to be resumed.
func createFailedTaskWithSession(t *testing.T, h *Handler, prompt, sessionID string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: prompt, Timeout: 15})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	setTaskSessionID(t, h, task.ID, sessionID)
	// UpdateTaskResult may change status; force it back to failed.
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed); err != nil {
		t.Fatalf("re-set failed: %v", err)
	}
	return task.ID
}

func TestResumeTask_SucceedsAtFullCapacity(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)

	// Set max parallel tasks to 1 so a single in-progress task fills capacity.
	if err := os.WriteFile(envPath, []byte("WALLFACER_MAX_PARALLEL=1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Fill the single slot with an in-progress task.
	createInProgressTask(t, h, "occupying slot")

	// Create a failed task with a session — this is the one we want to resume.
	failedID := createFailedTaskWithSession(t, h, "need resume", "sess-resume")

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+failedID.String()+"/resume", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.ResumeTask(w, req, failedID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (resume should bypass capacity), got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["status"] != "resumed" {
		t.Errorf("expected status=resumed, got %q", resp["status"])
	}
}

func TestSubmitFeedback_SucceedsAtFullCapacity(t *testing.T) {
	h, envPath := newTestHandlerWithEnv(t)

	// Set max parallel tasks to 1.
	if err := os.WriteFile(envPath, []byte("WALLFACER_MAX_PARALLEL=1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Fill the single slot.
	createInProgressTask(t, h, "occupying slot")

	// Create a waiting task that needs feedback.
	task, err := h.store.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "needs feedback", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.ForceUpdateTaskStatus(context.Background(), task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/feedback",
		strings.NewReader(`{"message": "please continue"}`))
	w := httptest.NewRecorder()
	h.SubmitFeedback(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (feedback should bypass capacity), got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["status"] != "resumed" {
		t.Errorf("expected status=resumed, got %q", resp["status"])
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
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if archived, ok := resp["archived"].(float64); !ok || archived != 0 {
		t.Errorf("expected archived=0, got %v", resp["archived"])
	}
}

func TestArchiveAllDone_ArchivesDoneTasks(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task1, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "done task 1", Timeout: 15})
	task2, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "done task 2", Timeout: 15})
	backlogTask, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "backlog task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task1.ID, store.TaskStatusDone)

	_ = h.store.ForceUpdateTaskStatus(ctx, task2.ID, store.TaskStatusDone)

	_ = backlogTask

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/archive-all-done", nil)
	w := httptest.NewRecorder()
	h.ArchiveAllDone(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "cancelled task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusCancelled)


	req := httptest.NewRequest(http.MethodPost, "/api/tasks/archive-all-done", nil)
	w := httptest.NewRecorder()
	h.ArchiveAllDone(w, req)

	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "done task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "cancelled", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusCancelled)


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "done task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

	_ = h.store.SetTaskArchived(ctx, task.ID, true)


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "done task", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusDone)

	_ = h.store.SetTaskArchived(ctx, task.ID, true)


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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)


	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/sync", nil)
	w := httptest.NewRecorder()
	h.SyncTask(w, req, task.ID)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for task without worktrees, got %d", w.Code)
	}
}

func TestSyncTask_InProgressReturnsAlreadySyncing(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/sync", nil)
	w := httptest.NewRecorder()
	h.SyncTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for in_progress task, got %d", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "already_syncing") {
		t.Errorf("expected already_syncing in response body, got %s", body)
	}
}

func TestSyncTask_WaitingWithWorktrees(t *testing.T) {
	repo := setupRepo(t)
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)

	// Provide a worktree path (repo itself, as a stand-in).
	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: repo}, "main")


	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/sync", nil)
	w := httptest.NewRecorder()
	h.SyncTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["status"] != "syncing" {
		t.Errorf("expected status=syncing, got %q", resp["status"])
	}
}

func TestSyncTask_FailedWithWorktrees(t *testing.T) {
	repo := setupRepo(t)
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})
	_ = h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)

	_ = h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: repo}, "main")


	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/sync", nil)
	w := httptest.NewRecorder()
	h.SyncTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["status"] != "syncing" {
		t.Errorf("expected status=syncing, got %q", resp["status"])
	}
}

func TestSyncTask_SucceedsAtFullCapacity(t *testing.T) {
	repo := setupRepo(t)
	h, envPath := newTestHandlerWithEnv(t)
	ctx := context.Background()

	if err := os.WriteFile(envPath, []byte("WALLFACER_MAX_PARALLEL=1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	createInProgressTask(t, h, "occupying slot")

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "sync me", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatal(err)
	}
	if err := h.store.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: repo}, "main"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+task.ID.String()+"/sync", nil)
	w := httptest.NewRecorder()
	h.SyncTask(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (sync should bypass capacity), got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["status"] != "syncing" {
		t.Errorf("expected status=syncing, got %q", resp["status"])
	}
}

// --- runCommitTransition ---

// TestRunCommitTransition_SuccessWithMock verifies that runCommitTransition
// transitions a committing task to done when the mock runner's Commit succeeds.
func TestRunCommitTransition_SuccessWithMock(t *testing.T) {
	m := &runner.MockRunner{}
	h, s := newTestHandlerWithMockRunner(t, m)
	ctx := context.Background()

	task, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 30})
	// Manually drive the task to committing status (bypassing the state machine).
	s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusCommitting) //nolint:errcheck

	// Set up a real worktree path (must be a git repo) so
	// validateTaskWorktreesForCommit does not reject the task.
	worktreeDir := t.TempDir()
	_ = exec.Command("git", "init", worktreeDir).Run()
	_ = exec.Command("git", "-C", worktreeDir, "commit", "--allow-empty", "-m", "init").Run()
	_ = s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{worktreeDir: worktreeDir}, "task/branch")

	h.runCommitTransition(task.ID, "session-1", store.TriggerUser, "commit error: ")

	// Poll until the background goroutine completes the commit transition.
	var updated *store.Task
	for range 100 {
		updated, _ = s.GetTask(ctx, task.ID)
		if updated != nil && updated.Status == store.TaskStatusDone {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// MockRunner.Commit returns nil, so the task should reach done status.
	if updated == nil || updated.Status != store.TaskStatusDone {
		t.Errorf("expected done status, got %q", updated.Status)
	}
}
