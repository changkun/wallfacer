package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// SubmitFeedback resumes a waiting task with user-provided feedback.
func (h *Handler) SubmitFeedback(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != "waiting" {
		http.Error(w, "task is not in waiting status", http.StatusBadRequest)
		return
	}

	if err := h.store.UpdateTaskStatus(r.Context(), id, "in_progress"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.store.InsertEvent(r.Context(), id, store.EventTypeFeedback, map[string]string{
		"message": req.Message,
	})
	h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
		"from": "waiting",
		"to":   "in_progress",
	})

	sessionID := ""
	if task.SessionID != nil {
		sessionID = *task.SessionID
	}
	go h.runner.Run(id, req.Message, sessionID, true)

	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

// CompleteTask marks a waiting task as done and triggers the commit pipeline.
func (h *Handler) CompleteTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != "waiting" {
		http.Error(w, "only waiting tasks can be completed", http.StatusBadRequest)
		return
	}

	if task.SessionID != nil && *task.SessionID != "" {
		// Transition to "committing" while auto-commit runs in the background.
		if err := h.store.UpdateTaskStatus(r.Context(), id, "committing"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
			"from": "waiting",
			"to":   "committing",
		})
		sessionID := *task.SessionID
		go func() {
			bgCtx := context.Background()
			if err := h.runner.Commit(id, sessionID); err != nil {
				h.store.UpdateTaskStatus(bgCtx, id, "failed")
				h.store.InsertEvent(bgCtx, id, store.EventTypeError, map[string]string{
					"error": "commit failed: " + err.Error(),
				})
				h.store.InsertEvent(bgCtx, id, store.EventTypeStateChange, map[string]string{
					"from": "committing",
					"to":   "failed",
				})
				return
			}
			h.store.UpdateTaskStatus(bgCtx, id, "done")
			h.store.InsertEvent(bgCtx, id, store.EventTypeStateChange, map[string]string{
				"from": "committing",
				"to":   "done",
			})
		}()
	} else {
		// No session to commit — go directly to done.
		if err := h.store.UpdateTaskStatus(r.Context(), id, "done"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
			"from": "waiting",
			"to":   "done",
		})
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// CancelTask cancels a task in backlog, in_progress, waiting, or failed state.
func (h *Handler) CancelTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	cancellable := map[string]bool{
		"backlog":     true,
		"in_progress": true,
		"waiting":     true,
		"failed":      true,
	}
	if !cancellable[task.Status] {
		http.Error(w, "task cannot be cancelled in its current status", http.StatusBadRequest)
		return
	}

	oldStatus := task.Status

	// For in_progress tasks: kill the running container first.
	if oldStatus == "in_progress" {
		h.runner.KillContainer(id)
	}

	// Persist the cancelled status BEFORE cleaning up worktrees.
	if err := h.store.UpdateTaskStatus(r.Context(), id, "cancelled"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
		"from": oldStatus,
		"to":   "cancelled",
	})

	if len(task.WorktreePaths) > 0 {
		h.runner.CleanupWorktrees(id, task.WorktreePaths, task.BranchName)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// ResumeTask resumes a failed task using its existing session.
func (h *Handler) ResumeTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var req struct {
		Timeout *int `json:"timeout"`
	}
	// Body is optional — ignore parse errors for backward compatibility.
	json.NewDecoder(r.Body).Decode(&req)

	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != "failed" {
		http.Error(w, "only failed tasks can be resumed", http.StatusBadRequest)
		return
	}
	if task.SessionID == nil || *task.SessionID == "" {
		http.Error(w, "task has no session to resume", http.StatusBadRequest)
		return
	}

	if err := h.store.ResumeTask(r.Context(), id, req.Timeout); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
		"from": "failed",
		"to":   "in_progress",
	})

	go h.runner.Run(id, "continue", *task.SessionID, false)

	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

// ArchiveTask archives a done task.
func (h *Handler) ArchiveTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != "done" && task.Status != "cancelled" {
		http.Error(w, "only done or cancelled tasks can be archived", http.StatusBadRequest)
		return
	}
	if err := h.store.SetTaskArchived(r.Context(), id, true); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
		"to": "archived",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "archived"})
}

// UnarchiveTask restores an archived task.
func (h *Handler) UnarchiveTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if _, err := h.store.GetTask(r.Context(), id); err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if err := h.store.SetTaskArchived(r.Context(), id, false); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
		"to": "unarchived",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "unarchived"})
}

// TestTask creates a standalone test agent task to verify a waiting task's acceptance criteria.
// The test task runs with MountWorktrees=true so it can access sibling worktrees via board.json.
// The parent task remains in "waiting" state while the test task runs independently.
func (h *Handler) TestTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var req struct {
		Criteria string `json:"criteria"`
	}
	// Body is optional — ignore decode errors.
	json.NewDecoder(r.Body).Decode(&req)

	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != "waiting" {
		http.Error(w, "only waiting tasks can be tested", http.StatusBadRequest)
		return
	}

	testPrompt := buildTestPrompt(task.Prompt, req.Criteria)

	// Create a new test task. MountWorktrees=true lets it read sibling worktrees
	// and board.json to locate the parent task's code changes.
	testTask, err := h.store.CreateTask(r.Context(), testPrompt, task.Timeout, true, task.Model)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.store.InsertEvent(r.Context(), testTask.ID, store.EventTypeStateChange, map[string]string{
		"to": "backlog",
	})

	// Record on the parent task that a test was launched.
	h.store.InsertEvent(r.Context(), id, store.EventTypeSystem, map[string]string{
		"result": fmt.Sprintf("Test agent launched (task %s)", testTask.ID),
	})

	go h.runner.GenerateTitle(testTask.ID, testTask.Prompt)

	writeJSON(w, http.StatusCreated, map[string]string{
		"test_task_id": testTask.ID.String(),
		"status":       "created",
	})
}

// buildTestPrompt constructs a prompt for the test agent from the original task
// prompt and optional user-specified acceptance criteria.
func buildTestPrompt(originalPrompt, criteria string) string {
	var b strings.Builder
	b.WriteString("You are a test agent. Your job is to verify that the implementation meets the specified requirements.\n\n")
	b.WriteString("## Original Task\n\n")
	b.WriteString(originalPrompt)
	b.WriteString("\n\n")
	if strings.TrimSpace(criteria) != "" {
		b.WriteString("## Acceptance Criteria\n\n")
		b.WriteString(criteria)
		b.WriteString("\n\n")
	}
	b.WriteString("## Instructions\n\n")
	b.WriteString("1. Find the sibling task's worktree paths by reading `/workspace/.tasks/board.json` — look for the task in `waiting` status to locate its worktree directories.\n")
	b.WriteString("2. Examine the code changes in those worktrees to understand what was implemented.\n")
	b.WriteString("3. Run any available tests (unit tests, integration tests, linters, etc.).\n")
	b.WriteString("4. Verify the implementation satisfies every requirement listed above.\n")
	b.WriteString("5. Conclude with a clear **PASS** or **FAIL** verdict and specific details about what was checked.\n\n")
	b.WriteString("Be thorough but focused. If tests fail or requirements are unmet, describe exactly what is missing or broken.")
	return b.String()
}

// SyncTask rebases task worktrees onto the latest default branch without merging.
func (h *Handler) SyncTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != "waiting" && task.Status != "failed" {
		http.Error(w, "only waiting or failed tasks with worktrees can be synced", http.StatusBadRequest)
		return
	}
	if len(task.WorktreePaths) == 0 {
		http.Error(w, "task has no worktrees to sync", http.StatusBadRequest)
		return
	}

	oldStatus := task.Status
	if err := h.store.UpdateTaskStatus(r.Context(), id, "in_progress"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
		"from": oldStatus,
		"to":   "in_progress",
	})

	sessionID := ""
	if task.SessionID != nil {
		sessionID = *task.SessionID
	}
	go h.runner.SyncWorktrees(id, sessionID, oldStatus)
	writeJSON(w, http.StatusOK, map[string]string{"status": "syncing"})
}
