package handler

import (
	"context"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	"changkun.de/wallfacer/internal/gitutil"
	"changkun.de/wallfacer/internal/store"
	"changkun.de/wallfacer/prompts"
	"github.com/google/uuid"
)

// SubmitFeedback resumes a waiting task with user-provided feedback.
func (h *Handler) SubmitFeedback(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var req struct {
		Message string `json:"message"`
	}
	if !decodeJSONBody(w, r, &req) {
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
	if task.Status != store.TaskStatusWaiting {
		http.Error(w, "task is not in waiting status", http.StatusBadRequest)
		return
	}

	// Any further implementation work invalidates prior test verification.
	if err := h.store.UpdateTaskTestRun(r.Context(), id, false, ""); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	promoteMu.Lock()
	regularInProgress, err := h.countRegularInProgress(r.Context())
	if err != nil {
		promoteMu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if regularInProgress >= h.maxConcurrentTasks() {
		promoteMu.Unlock()
		http.Error(w, "max concurrent tasks reached", http.StatusConflict)
		return
	}

	if err := h.store.UpdateTaskStatus(r.Context(), id, store.TaskStatusInProgress); err != nil {
		promoteMu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	promoteMu.Unlock()

	h.store.InsertEvent(r.Context(), id, store.EventTypeFeedback, map[string]string{
		"message": req.Message,
	})
	h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
		"from":    string(store.TaskStatusWaiting),
		"to":      string(store.TaskStatusInProgress),
		"trigger": store.TriggerFeedback,
	})

	sessionID := ""
	if task.SessionID != nil {
		sessionID = *task.SessionID
	}
	h.runner.RunBackground(id, req.Message, sessionID, true)

	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

// CompleteTask marks a waiting task as done and triggers the commit pipeline.
func (h *Handler) CompleteTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != store.TaskStatusWaiting {
		http.Error(w, "only waiting tasks can be completed", http.StatusBadRequest)
		return
	}

	if task.SessionID != nil && *task.SessionID != "" {
		// Transition to "committing" while auto-commit runs in the background.
		// Use ForceUpdateTaskStatus since waiting → committing is a legitimate
		// user-initiated flow not in the automated state machine.
		if err := h.store.ForceUpdateTaskStatus(r.Context(), id, store.TaskStatusCommitting); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
			"from":    string(store.TaskStatusWaiting),
			"to":      string(store.TaskStatusCommitting),
			"trigger": store.TriggerUser,
		})
		sessionID := *task.SessionID
		go func() {
			bgCtx := context.Background()
			if err := h.runner.Commit(id, sessionID); err != nil {
				h.store.UpdateTaskStatus(bgCtx, id, store.TaskStatusFailed)
				h.store.InsertEvent(bgCtx, id, store.EventTypeError, map[string]string{
					"error": "commit failed: " + err.Error(),
				})
				h.store.InsertEvent(bgCtx, id, store.EventTypeStateChange, map[string]string{
					"from":    string(store.TaskStatusCommitting),
					"to":      string(store.TaskStatusFailed),
					"trigger": store.TriggerUser,
				})
				return
			}
			h.store.UpdateTaskStatus(bgCtx, id, store.TaskStatusDone)
			h.store.InsertEvent(bgCtx, id, store.EventTypeStateChange, map[string]string{
				"from":    string(store.TaskStatusCommitting),
				"to":      string(store.TaskStatusDone),
				"trigger": store.TriggerUser,
			})
		}()
	} else {
		// No session to commit — go directly to done (bypasses state machine
		// since waiting→done is deliberately blocked to protect the commit pipeline).
		if err := h.store.ForceUpdateTaskStatus(r.Context(), id, store.TaskStatusDone); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
			"from":    string(store.TaskStatusWaiting),
			"to":      string(store.TaskStatusDone),
			"trigger": store.TriggerUser,
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

	cancellable := map[store.TaskStatus]bool{
		store.TaskStatusBacklog:    true,
		store.TaskStatusInProgress: true,
		store.TaskStatusWaiting:    true,
		store.TaskStatusFailed:     true,
	}
	if !cancellable[task.Status] {
		http.Error(w, "task cannot be cancelled in its current status", http.StatusBadRequest)
		return
	}

	oldStatus := task.Status

	// For in_progress tasks: kill the running container first.
	if oldStatus == store.TaskStatusInProgress {
		h.runner.KillContainer(id)
	}

	// Persist the cancelled status BEFORE cleaning up worktrees.
	// Use ForceUpdateTaskStatus to handle transitions not in the normal state
	// machine (e.g. backlog → cancelled for tasks that never started).
	if err := h.store.ForceUpdateTaskStatus(r.Context(), id, store.TaskStatusCancelled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
		"from":    string(oldStatus),
		"to":      string(store.TaskStatusCancelled),
		"trigger": store.TriggerUser,
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
	// Body is optional — empty body is accepted; present body is decoded strictly.
	if !decodeOptionalJSONBody(w, r, &req) {
		return
	}

	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != store.TaskStatusFailed {
		http.Error(w, "only failed tasks can be resumed", http.StatusBadRequest)
		return
	}
	if task.SessionID == nil || *task.SessionID == "" {
		http.Error(w, "task has no session to resume", http.StatusBadRequest)
		return
	}

	promoteMu.Lock()
	regularInProgress, err := h.countRegularInProgress(r.Context())
	if err != nil {
		promoteMu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if regularInProgress >= h.maxConcurrentTasks() {
		promoteMu.Unlock()
		http.Error(w, "max concurrent tasks reached", http.StatusConflict)
		return
	}

	if err := h.store.ResumeTask(r.Context(), id, req.Timeout); err != nil {
		promoteMu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	promoteMu.Unlock()

	h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
		"from":    string(store.TaskStatusFailed),
		"to":      string(store.TaskStatusInProgress),
		"trigger": store.TriggerUser,
	})

	h.runner.RunBackground(id, "continue", *task.SessionID, false)

	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

// ArchiveAllDone archives all done and cancelled tasks in one operation.
func (h *Handler) ArchiveAllDone(w http.ResponseWriter, r *http.Request) {
	archived, err := h.store.ArchiveAllDone(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, id := range archived {
		h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
			"to":      "archived",
			"trigger": store.TriggerUser,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"archived": len(archived)})
}

// ArchiveTask archives a done task.
func (h *Handler) ArchiveTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != store.TaskStatusDone && task.Status != store.TaskStatusCancelled {
		http.Error(w, "only done or cancelled tasks can be archived", http.StatusBadRequest)
		return
	}
	if err := h.store.SetTaskArchived(r.Context(), id, true); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
		"to":      "archived",
		"trigger": store.TriggerUser,
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
		"to":      "unarchived",
		"trigger": store.TriggerUser,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "unarchived"})
}

// TestTask runs a verification agent on the same task to check its acceptance criteria.
// The task transitions from "waiting" back to "in_progress" with IsTestRun=true so the UI
// can distinguish a test run from normal work. On end_turn the runner moves it back to
// "waiting" (instead of "done") and records a pass/fail verdict.
func (h *Handler) TestTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var req struct {
		Criteria string `json:"criteria"`
	}
	// Body is optional — empty body is accepted; present body is decoded strictly.
	if !decodeOptionalJSONBody(w, r, &req) {
		return
	}

	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != store.TaskStatusWaiting {
		http.Error(w, "only waiting tasks can be tested", http.StatusBadRequest)
		return
	}

	// Include the implementation agent's result as context so the test agent
	// knows what was reported as done without re-reading the whole codebase.
	implResult := ""
	if task.Result != nil {
		implResult = *task.Result
	}

	// Generate a git diff from each worktree so the test agent can focus
	// directly on the changed files instead of exploring from scratch.
	diff := generateWorktreeDiff(task.WorktreePaths)

	testPrompt := buildTestPrompt(task.Prompt, req.Criteria, implResult, diff)

	// Mark task as a test run and clear any previous verdict.
	if err := h.store.UpdateTaskTestRun(r.Context(), id, true, ""); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Transition waiting → in_progress.
	if err := h.store.UpdateTaskStatus(r.Context(), id, store.TaskStatusInProgress); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
		"from":    string(store.TaskStatusWaiting),
		"to":      string(store.TaskStatusInProgress),
		"trigger": store.TriggerUser,
	})
	h.store.InsertEvent(r.Context(), id, store.EventTypeSystem, map[string]string{
		"result":      "Test verification started",
		"test_prompt": testPrompt,
	})

	// Run the test agent in a fresh session so it doesn't continue the implementation session.
	h.runner.RunBackground(id, testPrompt, "", false)

	writeJSON(w, http.StatusOK, map[string]string{"status": "testing"})
}

// buildTestPrompt constructs a prompt for the test verification agent.
// implResult is the implementation agent's self-reported summary (may be empty).
// diff is a git diff of the changes made (may be empty).
func buildTestPrompt(originalPrompt, criteria, implResult, diff string) string {
	return prompts.TestVerification(prompts.TestData{
		OriginalPrompt: originalPrompt,
		Criteria:       strings.TrimSpace(criteria),
		ImplResult:     strings.TrimSpace(implResult),
		Diff:           strings.TrimSpace(diff),
	})
}

// maxDiffBytes is the maximum number of bytes to include from the git diff in
// the test prompt. Diffs beyond this limit are truncated to keep the prompt
// focused and avoid hitting context limits.
const maxDiffBytes = 16000

// generateWorktreeDiff produces a unified git diff for each worktree showing
// all changes on the task branch relative to the default branch. Returns an
// empty string if no worktrees are provided or no diffs are found.
func generateWorktreeDiff(worktreePaths map[string]string) string {
	if len(worktreePaths) == 0 {
		return ""
	}
	var parts []string
	for repoPath, worktreePath := range worktreePaths {
		if !gitutil.IsGitRepo(repoPath) {
			continue
		}
		defBranch, err := gitutil.DefaultBranch(repoPath)
		if err != nil {
			continue
		}
		out, err := exec.Command("git", "-C", worktreePath, "diff", defBranch+"..HEAD").Output()
		if err != nil || len(strings.TrimSpace(string(out))) == 0 {
			continue
		}
		diff := string(out)
		if len(worktreePaths) > 1 {
			diff = "# " + filepath.Base(repoPath) + "\n" + diff
		}
		parts = append(parts, diff)
	}
	combined := strings.Join(parts, "\n")
	if len(combined) > maxDiffBytes {
		combined = combined[:maxDiffBytes] + "\n... (diff truncated)"
	}
	return combined
}

// SyncTask rebases task worktrees onto the latest default branch without merging.
func (h *Handler) SyncTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != store.TaskStatusWaiting && task.Status != store.TaskStatusFailed {
		http.Error(w, "only waiting or failed tasks with worktrees can be synced", http.StatusBadRequest)
		return
	}
	if len(task.WorktreePaths) == 0 {
		http.Error(w, "task has no worktrees to sync", http.StatusBadRequest)
		return
	}

	oldStatus := task.Status
	// Use ForceUpdateTaskStatus to handle failed → in_progress which is a
	// valid operational flow not in the automated state machine.
	promoteMu.Lock()
	regularInProgress, err := h.countRegularInProgress(r.Context())
	if err != nil {
		promoteMu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if regularInProgress >= h.maxConcurrentTasks() {
		promoteMu.Unlock()
		http.Error(w, "max concurrent tasks reached", http.StatusConflict)
		return
	}

	if err := h.store.ForceUpdateTaskStatus(r.Context(), id, store.TaskStatusInProgress); err != nil {
		promoteMu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	promoteMu.Unlock()
	h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
		"from":    string(oldStatus),
		"to":      string(store.TaskStatusInProgress),
		"trigger": store.TriggerUser,
	})

	sessionID := ""
	if task.SessionID != nil {
		sessionID = *task.SessionID
	}
	h.diffCache.invalidate(id)
	h.runner.SyncWorktreesBackground(id, sessionID, oldStatus, func() {
		h.diffCache.invalidate(id)
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "syncing"})
}
