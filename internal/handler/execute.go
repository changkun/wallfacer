package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"changkun.de/x/wallfacer/internal/gitutil"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
	runnerpkg "changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
	"changkun.de/x/wallfacer/prompts"
	"github.com/google/uuid"
)

// closeFeedbackWaitingSpan emits a span_end event to close the feedback_waiting
// span that was opened when the task entered the waiting state. This must be
// called on every path that transitions a task out of waiting (complete, cancel,
// test run, auto-submit, etc.) so the timeline doesn't show an ever-growing
// "Waiting for Feedback" bar.
func (h *Handler) closeFeedbackWaitingSpan(ctx context.Context, taskID uuid.UUID) {
	_ = h.store.InsertEvent(ctx, taskID, store.EventTypeSpanEnd,
		store.SpanData{Phase: "feedback_waiting", Label: "feedback_waiting"})
}

func validateTaskWorktreesForCommit(task *store.Task) error {
	if len(task.WorktreePaths) == 0 {
		return httpErrorf(http.StatusConflict, "task has no worktrees to commit")
	}

	missing := missingTaskWorktrees(task)
	if len(missing) > 0 {
		return httpErrorf(http.StatusConflict, "task worktree missing for: %s", strings.Join(missing, ", "))
	}
	return nil
}

func missingTaskWorktrees(task *store.Task) []string {
	if task == nil {
		return nil
	}

	var missing []string
	for repoPath, worktreePath := range task.WorktreePaths {
		if worktreePath == "" {
			missing = append(missing, repoPath)
			continue
		}
		if _, err := os.Stat(worktreePath); err != nil {
			missing = append(missing, repoPath)
		} else if !gitutil.IsGitRepo(worktreePath) {
			missing = append(missing, repoPath)
		}
	}
	return missing
}

func (h *Handler) restoreTaskWorktreesForCommit(ctx context.Context, task *store.Task) (*store.Task, error) {
	if task == nil || len(task.WorktreePaths) == 0 || h.runner == nil {
		return task, nil
	}
	if len(missingTaskWorktrees(task)) == 0 {
		return task, nil
	}

	worktreePaths, branchName, err := h.runner.EnsureTaskWorktrees(task.ID, task.WorktreePaths, task.BranchName)
	if err != nil {
		return task, err
	}
	if err := h.store.UpdateTaskWorktrees(ctx, task.ID, worktreePaths, branchName); err != nil {
		return task, err
	}
	updated, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		return task, err
	}
	return updated, nil
}

type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }

func httpErrorf(code int, format string, args ...any) error {
	return &statusError{
		code: code,
		msg:  fmt.Sprintf(format, args...),
	}
}

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

	// Acquire promoteMu BEFORE reading/modifying the task to prevent races
	// with tryAutoSubmit (which also holds promoteMu in Phase 2). Without
	// this, auto-submit can transition the task to committing between our
	// status check and the UpdateTaskStatus call, causing the feedback to
	// fail while the commit pipeline cleans up the worktree.
	promoteMu.Lock()

	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		promoteMu.Unlock()
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != store.TaskStatusWaiting {
		promoteMu.Unlock()
		http.Error(w, "task is not in waiting status", http.StatusBadRequest)
		return
	}

	// Any further implementation work invalidates prior test verification.
	// This must happen AFTER the status check to avoid clearing test state
	// when the transition will fail (e.g. task already moved to committing).
	if err := h.store.UpdateTaskTestRun(r.Context(), id, false, ""); err != nil {
		promoteMu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Submitting feedback to a waiting task is always allowed even when max
	// concurrent tasks is reached. The task was previously in_progress and
	// paused for user input — blocking it would leave it stuck when autopilot
	// fills all slots.
	if err := h.resumeWaitingTaskWithFeedbackLocked(r.Context(), task, req.Message, store.TriggerFeedback, ""); err != nil {
		promoteMu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	promoteMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (h *Handler) resumeWaitingTaskWithFeedbackLocked(ctx context.Context, task *store.Task, message string, trigger store.Trigger, systemMessage string) error {
	if err := h.store.UpdateTaskTestRun(ctx, task.ID, false, ""); err != nil {
		return err
	}
	if err := h.store.UpdateTaskPendingTestFeedback(ctx, task.ID, ""); err != nil {
		return err
	}
	// Reset the consecutive test failure counter so the auto-resume cycle
	// can start fresh after manual intervention.
	if err := h.store.ResetTestFailCount(ctx, task.ID); err != nil {
		return err
	}
	if err := h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		return err
	}

	h.insertEventOrLog(ctx, task.ID, store.EventTypeFeedback, map[string]string{
		"message": message,
	})
	h.insertEventOrLog(ctx, task.ID, store.EventTypeStateChange,
		store.NewStateChangeData(store.TaskStatusWaiting, store.TaskStatusInProgress, trigger, nil))
	if systemMessage != "" {
		h.insertEventOrLog(ctx, task.ID, store.EventTypeSystem, map[string]string{
			"result": systemMessage,
		})
	}

	sessionID := ""
	if task.SessionID != nil {
		sessionID = *task.SessionID
	}
	h.runner.RunBackground(task.ID, message, sessionID, true)
	return nil
}

func (h *Handler) runCommitTransition(taskID uuid.UUID, sessionID string, trigger store.Trigger, failurePrefix string) {
	go func() {
		bgCtx := h.runner.ShutdownCtx()
		task, err := h.store.GetTask(bgCtx, taskID)
		if err == nil && task != nil {
			task, err = h.restoreTaskWorktreesForCommit(bgCtx, task)
			if err != nil {
				logger.Handler.Error("restore task worktrees for commit", "task", taskID, "error", err)
			}
			if err := validateTaskWorktreesForCommit(task); err != nil {
				if waitErr := h.store.ForceUpdateTaskStatus(bgCtx, taskID, store.TaskStatusWaiting); waitErr == nil {
					h.insertEventOrLog(bgCtx, taskID, store.EventTypeStateChange,
						store.NewStateChangeData(store.TaskStatusCommitting, store.TaskStatusWaiting, trigger, nil))
					h.insertEventOrLog(bgCtx, taskID, store.EventTypeError, map[string]string{
						"error": err.Error(),
					})
					return
				}
			}
		}
		if err := h.runner.Commit(taskID, sessionID); err != nil {
			if runnerpkg.IsCommitMessageGenerationError(err) {
				if waitErr := h.store.ForceUpdateTaskStatus(bgCtx, taskID, store.TaskStatusWaiting); waitErr == nil {
					h.insertEventOrLog(bgCtx, taskID, store.EventTypeStateChange,
						store.NewStateChangeData(store.TaskStatusCommitting, store.TaskStatusWaiting, trigger, nil))
					h.insertEventOrLog(bgCtx, taskID, store.EventTypeSystem, map[string]string{
						"result": "Commit aborted: commit message generation failed. Task returned to waiting for review.",
					})
					if trigger != store.TriggerUser {
						h.pauseAllAutomation(&taskID, "auto-submit", err.Error())
					}
					return
				}
			}
			if statusErr := h.store.UpdateTaskStatus(bgCtx, taskID, store.TaskStatusFailed); statusErr != nil {
				logger.Handler.Error("update task status to failed after commit error", "task", taskID, "error", statusErr)
			}
			h.insertEventOrLog(bgCtx, taskID, store.EventTypeError, map[string]string{
				"error": failurePrefix + err.Error(),
			})
			h.insertEventOrLog(bgCtx, taskID, store.EventTypeStateChange,
				store.NewStateChangeData(store.TaskStatusCommitting, store.TaskStatusFailed, trigger, nil))
			if trigger != store.TriggerUser {
				h.pauseAllAutomation(&taskID, "auto-submit", err.Error())
			}
			return
		}
		if statusErr := h.store.UpdateTaskStatus(bgCtx, taskID, store.TaskStatusDone); statusErr != nil {
			logger.Handler.Error("update task status to done after commit", "task", taskID, "error", statusErr)
		}
		h.insertEventOrLog(bgCtx, taskID, store.EventTypeStateChange,
			store.NewStateChangeData(store.TaskStatusCommitting, store.TaskStatusDone, trigger, nil))
	}()
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

	h.closeFeedbackWaitingSpan(r.Context(), id)

	// Idea-agent tasks held at waiting: create backlog tasks on approval.
	if task.Kind == store.TaskKindIdeaAgent {
		if createErr := h.runner.CreateIdeaBacklogTasks(r.Context(), id); createErr != nil {
			logger.Handler.Warn("complete idea-agent task: create backlog tasks", "task", id, "error", createErr)
		}
		if err := h.store.ForceUpdateTaskStatus(r.Context(), id, store.TaskStatusDone); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.insertEventOrLog(r.Context(), id, store.EventTypeStateChange,
			store.NewStateChangeData(store.TaskStatusWaiting, store.TaskStatusDone, store.TriggerUser, nil))
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if task.SessionID != nil && *task.SessionID != "" {
		task, err = h.restoreTaskWorktreesForCommit(r.Context(), task)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := validateTaskWorktreesForCommit(task); err != nil {
			if se, ok := err.(*statusError); ok {
				http.Error(w, se.msg, se.code)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Transition to "committing" while auto-commit runs in the background.
		// Use ForceUpdateTaskStatus since waiting → committing is a legitimate
		// user-initiated flow not in the automated state machine.
		if err := h.store.ForceUpdateTaskStatus(r.Context(), id, store.TaskStatusCommitting); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.insertEventOrLog(r.Context(), id, store.EventTypeStateChange,
			store.NewStateChangeData(store.TaskStatusWaiting, store.TaskStatusCommitting, store.TriggerUser, nil))
		h.runCommitTransition(id, *task.SessionID, store.TriggerUser, "commit failed: ")
	} else {
		// No session to commit — go directly to done (bypasses state machine
		// since waiting→done is deliberately blocked to protect the commit pipeline).
		if err := h.store.ForceUpdateTaskStatus(r.Context(), id, store.TaskStatusDone); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.insertEventOrLog(r.Context(), id, store.EventTypeStateChange,
			store.NewStateChangeData(store.TaskStatusWaiting, store.TaskStatusDone, store.TriggerUser, nil))
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
	if oldStatus == store.TaskStatusWaiting {
		h.closeFeedbackWaitingSpan(r.Context(), id)
	}

	// Kill any active containers for this task before persisting the new status.
	// KillRefineContainer is a no-op when no refinement container is registered.
	h.runner.KillRefineContainer(id)
	// For in_progress tasks: also kill the main execution container.
	if oldStatus == store.TaskStatusInProgress {
		h.runner.KillContainer(id)
	}

	// If a refinement is actively running, mark it as failed so the UI
	// reflects the correct state (same pattern as CancelRefinement).
	if task.CurrentRefinement != nil && task.CurrentRefinement.Status == store.RefinementJobStatusRunning {
		task.CurrentRefinement.Status = store.RefinementJobStatusFailed
		task.CurrentRefinement.Error = "task cancelled"
		if err := h.store.UpdateRefinementJob(r.Context(), id, task.CurrentRefinement); err != nil {
			logger.Handler.Error("cancel task: update refinement job", "task", id, "error", err)
		}
	}

	// Persist the cancelled status BEFORE cleaning up worktrees.
	// CancelTask uses ForceUpdateTaskStatus internally to handle transitions not
	// in the normal state machine (e.g. backlog → cancelled for tasks that never
	// started), and also cleans up orphaned DependsOn entries in dependent tasks.
	if err := h.store.CancelTask(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.insertEventOrLog(r.Context(), id, store.EventTypeStateChange,
		store.NewStateChangeData(oldStatus, store.TaskStatusCancelled, store.TriggerUser, nil))

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

	// Resuming a failed task is always allowed even when max concurrent tasks
	// is reached. When autopilot is on, all slots are filled by auto-promotion
	// and the user would otherwise be unable to resume any failed task. The
	// autopilot will naturally refrain from promoting another backlog task while
	// this resumed task is running, so the over-capacity is transient.
	promoteMu.Lock()
	if err := h.store.ResumeTask(r.Context(), id, req.Timeout); err != nil {
		promoteMu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	promoteMu.Unlock()

	h.insertEventOrLog(r.Context(), id, store.EventTypeStateChange,
		store.NewStateChangeData(store.TaskStatusFailed, store.TaskStatusInProgress, store.TriggerUser, nil))

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
		h.insertEventOrLog(r.Context(), id, store.EventTypeStateChange, map[string]string{
			"to":      "archived",
			"trigger": string(store.TriggerUser),
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
	h.insertEventOrLog(r.Context(), id, store.EventTypeStateChange, map[string]string{
		"to":      "archived",
		"trigger": string(store.TriggerUser),
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
	h.insertEventOrLog(r.Context(), id, store.EventTypeStateChange, map[string]string{
		"to":      "unarchived",
		"trigger": string(store.TriggerUser),
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

	h.closeFeedbackWaitingSpan(r.Context(), id)

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
	h.insertEventOrLog(r.Context(), id, store.EventTypeStateChange,
		store.NewStateChangeData(store.TaskStatusWaiting, store.TaskStatusInProgress, store.TriggerUser, nil))
	h.insertEventOrLog(r.Context(), id, store.EventTypeSystem, map[string]string{
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
		out, err := cmdexec.Git(worktreePath, "diff", defBranch+"..HEAD").Output()
		if err != nil || len(out) == 0 {
			continue
		}
		diff := out
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
	if task.Status == store.TaskStatusInProgress {
		writeJSON(w, http.StatusOK, map[string]string{"status": "already_syncing"})
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
	// Syncing a waiting/failed task must not be blocked by the regular
	// in-progress capacity limit. Like resume/feedback, this is follow-up work
	// on an existing task, and rejecting it when autopilot has filled all slots
	// leaves the user unable to recover or update the task.
	promoteMu.Lock()
	if err := h.store.ForceUpdateTaskStatus(r.Context(), id, store.TaskStatusInProgress); err != nil {
		promoteMu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	promoteMu.Unlock()
	h.insertEventOrLog(r.Context(), id, store.EventTypeStateChange,
		store.NewStateChangeData(oldStatus, store.TaskStatusInProgress, store.TriggerUser, nil))

	sessionID := ""
	if task.SessionID != nil {
		sessionID = *task.SessionID
	}
	h.diffCache.invalidate(id)
	for repoPath, worktreePath := range task.WorktreePaths {
		h.commitsBehindCache.invalidate(repoPath, worktreePath)
	}
	worktreePaths := task.WorktreePaths
	h.runner.SyncWorktreesBackground(id, sessionID, oldStatus, func() {
		h.diffCache.invalidate(id)
		for repoPath, worktreePath := range worktreePaths {
			h.commitsBehindCache.invalidate(repoPath, worktreePath)
		}
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "syncing"})
}
