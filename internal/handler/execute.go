package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"latere.ai/x/pkg/cmdexec"
	"latere.ai/x/pkg/gitutil"
	"latere.ai/x/pkg/httpjson"

	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/prompts"
	runnerpkg "latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
)

// closeFeedbackWaitingSpan emits a span_end event to close the feedback_waiting
// span that was opened when the task entered the waiting state. This must be
// called on every path that transitions a task out of waiting (complete, cancel,
// test run, feedback) so the timeline doesn't show an ever-growing
// "Waiting for Feedback" bar.
func (h *Handler) closeFeedbackWaitingSpan(ctx context.Context, taskID uuid.UUID) {
	s, ok := h.currentStore()
	if !ok {
		return
	}
	_ = s.InsertEvent(ctx, taskID, store.EventTypeSpanEnd,
		store.SpanData{Phase: "feedback_waiting", Label: "feedback_waiting"})
}

// validateTaskWorktreesForCommit verifies that the task has worktrees and
// that all worktree directories exist on disk. Returns an HTTP-friendly error
// if any are missing.
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

// missingTaskWorktrees returns the repo paths whose worktree directories are
// absent, empty, or not valid git repositories.
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

// restoreTaskWorktreesForCommit re-creates any missing worktree directories
// for a task that is about to enter the commit pipeline. Returns the updated
// task with refreshed worktree paths, or the original task if no restoration
// was needed.
func (h *Handler) restoreTaskWorktreesForCommit(ctx context.Context, s *store.Store, task *store.Task) (*store.Task, error) {
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
	if err := s.UpdateTaskWorktrees(ctx, task.ID, worktreePaths, branchName); err != nil {
		return task, err
	}
	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		return task, err
	}
	return updated, nil
}

// statusError is an error that carries an HTTP status code. Used by
// validateTaskWorktreesForCommit and similar helpers to propagate
// structured errors that map directly to HTTP responses.
type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }

// httpErrorf creates a statusError with the given HTTP status code and
// formatted message.
func httpErrorf(code int, format string, args ...any) error {
	return &statusError{
		code: code,
		msg:  fmt.Sprintf(format, args...),
	}
}

// SubmitFeedback resumes a waiting task with user-provided feedback.
func (h *Handler) SubmitFeedback(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	req, ok := httpjson.DecodeBody[struct {
		Message string `json:"message"`
	}](w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	s, ok := h.requireStore(w)
	if !ok {
		return
	}

	// Acquire transitionMu BEFORE reading/modifying the task so a concurrent
	// Done cannot move the task to committing between our status check and
	// the UpdateTaskStatus call.
	transitionMu.Lock()

	task, err := s.GetTask(r.Context(), id)
	if err != nil {
		transitionMu.Unlock()
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != store.TaskStatusWaiting {
		transitionMu.Unlock()
		http.Error(w, "task is not in waiting status", http.StatusBadRequest)
		return
	}

	// Any further implementation work invalidates prior test verification.
	// This must happen AFTER the status check to avoid clearing test state
	// when the transition will fail (e.g. task already moved to committing).
	if err := s.UpdateTaskTestRun(r.Context(), id, false, ""); err != nil {
		transitionMu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Submitting feedback to a waiting task is always allowed even when max
	// concurrent tasks is reached: the task was previously in_progress and
	// paused for user input.
	if err := h.resumeWaitingTaskWithFeedbackLocked(r.Context(), s, task, req.Message); err != nil {
		transitionMu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	transitionMu.Unlock()

	httpjson.Write(w, http.StatusOK, map[string]string{"status": "resumed"})
}

// resumeWaitingTaskWithFeedbackLocked transitions a waiting task back to
// in_progress with the given feedback message and launches the runner in
// the background. Must be called with transitionMu held.
func (h *Handler) resumeWaitingTaskWithFeedbackLocked(ctx context.Context, s *store.Store, task *store.Task, message string) error {
	if err := s.UpdateTaskTestRun(ctx, task.ID, false, ""); err != nil {
		return err
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		return err
	}

	h.insertEventOrLogTo(ctx, s, task.ID, store.EventTypeFeedback, map[string]string{
		"message": message,
	})
	h.insertEventOrLogTo(ctx, s, task.ID, store.EventTypeStateChange,
		store.NewStateChangeData(store.TaskStatusWaiting, store.TaskStatusInProgress, store.TriggerFeedback, nil))

	sessionID := ""
	if task.SessionID != nil {
		sessionID = *task.SessionID
	}
	h.runner.RunBackground(task.ID, message, sessionID, true)
	return nil
}

// runCommitTransition launches the commit pipeline in a background goroutine.
// On success it transitions the task to done; on failure it transitions to
// failed (or back to waiting for recoverable commit-message errors). The
// trigger is recorded on the state-change events.
func (h *Handler) runCommitTransition(s *store.Store, taskID uuid.UUID, sessionID string, trigger store.Trigger, failurePrefix string) {
	go func() {
		if s == nil {
			logger.Handler.Error("commit transition skipped: no store", "task", taskID)
			return
		}
		bgCtx := h.runner.ShutdownCtx()
		task, err := s.GetTask(bgCtx, taskID)
		if err == nil && task != nil {
			task, err = h.restoreTaskWorktreesForCommit(bgCtx, s, task)
			if err != nil {
				logger.Handler.Error("restore task worktrees for commit", "task", taskID, "error", err)
			}
			if err := validateTaskWorktreesForCommit(task); err != nil {
				if waitErr := s.ForceUpdateTaskStatus(bgCtx, taskID, store.TaskStatusWaiting); waitErr != nil {
					// Validation already established the worktrees are missing.
					// If the recovery transition to waiting also failed, do not
					// fall through to Commit with known-missing worktrees; log
					// and leave the task in committing for the next reconcile.
					logger.Handler.Error("commit: worktree validation failed and recovery to waiting failed",
						"task", taskID, "validate_error", err, "wait_error", waitErr)
					return
				}
				h.insertEventOrLogTo(bgCtx, s, taskID, store.EventTypeStateChange,
					store.NewStateChangeData(store.TaskStatusCommitting, store.TaskStatusWaiting, trigger, nil))
				h.insertEventOrLogTo(bgCtx, s, taskID, store.EventTypeError, map[string]string{
					"error": err.Error(),
				})
				return
			}
		}
		if err := h.runner.Commit(taskID, sessionID); err != nil {
			if runnerpkg.IsCommitMessageGenerationError(err) {
				if waitErr := s.ForceUpdateTaskStatus(bgCtx, taskID, store.TaskStatusWaiting); waitErr == nil {
					h.insertEventOrLogTo(bgCtx, s, taskID, store.EventTypeStateChange,
						store.NewStateChangeData(store.TaskStatusCommitting, store.TaskStatusWaiting, trigger, nil))
					h.insertEventOrLogTo(bgCtx, s, taskID, store.EventTypeSystem, map[string]string{
						"result": "Commit aborted: commit message generation failed. Task returned to waiting for review.",
					})
					return
				}
			}
			if statusErr := s.UpdateTaskStatus(bgCtx, taskID, store.TaskStatusFailed); statusErr != nil {
				logger.Handler.Error("update task status to failed after commit error", "task", taskID, "error", statusErr)
			}
			h.insertEventOrLogTo(bgCtx, s, taskID, store.EventTypeError, map[string]string{
				"error": failurePrefix + err.Error(),
			})
			h.insertEventOrLogTo(bgCtx, s, taskID, store.EventTypeStateChange,
				store.NewStateChangeData(store.TaskStatusCommitting, store.TaskStatusFailed, trigger, nil))
			return
		}
		if statusErr := s.UpdateTaskStatus(bgCtx, taskID, store.TaskStatusDone); statusErr != nil {
			logger.Handler.Error("update task status to done after commit", "task", taskID, "error", statusErr)
		}
		h.insertEventOrLogTo(bgCtx, s, taskID, store.EventTypeStateChange,
			store.NewStateChangeData(store.TaskStatusCommitting, store.TaskStatusDone, trigger, nil))
	}()
}

// CompleteTask marks a waiting task as done and triggers the commit pipeline.
func (h *Handler) CompleteTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}

	// Hold transitionMu across the read-check-write so a second Done (or a
	// feedback, resume or sync) cannot act on the task between our status check
	// and the status write; two commit pipelines would double-commit and tear
	// down the worktree under each other. The background runCommitTransition
	// goroutine does not take transitionMu, so holding it until this handler
	// returns cannot deadlock.
	transitionMu.Lock()
	defer transitionMu.Unlock()

	task, err := s.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != store.TaskStatusWaiting {
		http.Error(w, "only waiting tasks can be completed", http.StatusBadRequest)
		return
	}

	h.closeFeedbackWaitingSpan(r.Context(), id)

	if task.SessionID != nil && *task.SessionID != "" {
		task, err = h.restoreTaskWorktreesForCommit(r.Context(), s, task)
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
		if err := s.ForceUpdateTaskStatus(r.Context(), id, store.TaskStatusCommitting); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.insertEventOrLogTo(r.Context(), s, id, store.EventTypeStateChange,
			store.NewStateChangeData(store.TaskStatusWaiting, store.TaskStatusCommitting, store.TriggerUser, nil))
		h.runCommitTransition(s, id, *task.SessionID, store.TriggerUser, "commit failed: ")
	} else {
		// No session to commit — go directly to done (bypasses state machine
		// since waiting→done is deliberately blocked to protect the commit pipeline).
		if err := s.ForceUpdateTaskStatus(r.Context(), id, store.TaskStatusDone); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.insertEventOrLogTo(r.Context(), s, id, store.EventTypeStateChange,
			store.NewStateChangeData(store.TaskStatusWaiting, store.TaskStatusDone, store.TriggerUser, nil))
	}

	httpjson.Write(w, http.StatusOK, map[string]string{"status": "ok"})
}

// cancellableStatuses lists the statuses a task may be cancelled from.
var cancellableStatuses = map[store.TaskStatus]bool{
	store.TaskStatusBacklog:    true,
	store.TaskStatusInProgress: true,
	store.TaskStatusWaiting:    true,
	store.TaskStatusFailed:     true,
}

// applyCancel runs the full cancellation cascade for a task: kill its
// container, clean worktrees, and archive task-mode threads. The caller must
// verify the task is in a cancellableStatuses state. Invoked from the PATCH
// status=cancelled path (UpdateTask).
func (h *Handler) applyCancel(ctx context.Context, task store.Task) error {
	if err := h.cancelTaskInternal(ctx, task); err != nil {
		return err
	}
	if task.Status == store.TaskStatusBacklog || task.Status == store.TaskStatusWaiting {
		h.cascadeArchiveThreadsForTask(task.ID.String())
	}
	return nil
}

// cancelTaskInternal runs the full cancel pipeline (container kill, store
// update, event emission, worktree cleanup) without writing an HTTP response.
// applyCancel (the PATCH status=cancelled path) calls it after validating
// that task.Status is cancellable.
func (h *Handler) cancelTaskInternal(ctx context.Context, task store.Task) error {
	oldStatus := task.Status
	if oldStatus == store.TaskStatusWaiting {
		h.closeFeedbackWaitingSpan(ctx, task.ID)
	}

	// Kill any active container for this task before persisting the new status.
	if oldStatus == store.TaskStatusInProgress {
		h.runner.KillContainer(task.ID)
	}

	s, ok := h.currentStore()
	if !ok {
		return nil
	}
	if err := s.CancelTask(ctx, task.ID); err != nil {
		return err
	}

	h.insertEventOrLog(ctx, task.ID, store.EventTypeStateChange,
		store.NewStateChangeData(oldStatus, store.TaskStatusCancelled, store.TriggerUser, nil))

	if len(task.WorktreePaths) > 0 {
		h.runner.CleanupWorktrees(task.ID, task.WorktreePaths, task.BranchName)
	}
	return nil
}

// ResumeTask resumes a failed task using its existing session.
func (h *Handler) ResumeTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	// Body is optional — empty body is accepted; present body is decoded strictly.
	req, ok := httpjson.DecodeOptionalBody[struct {
		Timeout *int `json:"timeout"`
	}](w, r)
	if !ok {
		return
	}

	s, ok := h.requireStore(w)
	if !ok {
		return
	}

	task, err := s.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != store.TaskStatusFailed && task.Status != store.TaskStatusWaiting {
		http.Error(w, "only failed or waiting tasks can be resumed", http.StatusBadRequest)
		return
	}
	if task.SessionID == nil || *task.SessionID == "" {
		http.Error(w, "task has no session to resume", http.StatusBadRequest)
		return
	}
	prevStatus := task.Status

	// Resuming a failed task is always allowed even when max concurrent tasks
	// is reached: it is follow-up work on an existing task.
	transitionMu.Lock()
	if err := s.ResumeTask(r.Context(), id, req.Timeout); err != nil {
		transitionMu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	transitionMu.Unlock()

	h.insertEventOrLog(r.Context(), id, store.EventTypeStateChange,
		store.NewStateChangeData(prevStatus, store.TaskStatusInProgress, store.TriggerUser, nil))

	h.runner.RunBackground(id, "continue", *task.SessionID, false)

	httpjson.Write(w, http.StatusOK, map[string]string{"status": "resumed"})
}

// ArchiveAllDone archives all done and cancelled tasks in one operation.
func (h *Handler) ArchiveAllDone(w http.ResponseWriter, r *http.Request) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	archived, err := s.ArchiveAllDone(r.Context())
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
	httpjson.Write(w, http.StatusOK, map[string]any{"archived": len(archived)})
}

// applyArchive flips a task's archived flag and runs the matching thread
// cascade. Archiving (archived=true) also stops any leaked worker. The
// caller verifies the task exists and (for archived=true) is in a
// done/cancelled status.
// Invoked from the PATCH archived path (UpdateTask).
func (h *Handler) applyArchive(ctx context.Context, task store.Task, archived bool) error {
	s, ok := h.currentStore()
	if !ok {
		return nil
	}
	if err := s.SetTaskArchived(ctx, task.ID, archived); err != nil {
		return err
	}
	to := "unarchived"
	if archived {
		to = "archived"
	}
	h.insertEventOrLog(ctx, task.ID, store.EventTypeStateChange, map[string]string{
		"to":      to,
		"trigger": string(store.TriggerUser),
	})
	if archived {
		h.cascadeArchiveThreadsForTask(task.ID.String())
	} else {
		h.cascadeUnarchiveThreadsForTask(task.ID.String())
	}
	return nil
}

// TestTask runs a verification agent on the same task to check its acceptance criteria.
// The task transitions from "waiting" back to "in_progress" with IsTestRun=true so the UI
// can distinguish a test run from normal work. On end_turn the runner moves it back to
// "waiting" (instead of "done") and records a pass/fail verdict.
func (h *Handler) TestTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	// Body is optional — empty body is accepted; present body is decoded strictly.
	req, ok := httpjson.DecodeOptionalBody[struct {
		Criteria string `json:"criteria"`
	}](w, r)
	if !ok {
		return
	}

	s, ok := h.requireStore(w)
	if !ok {
		return
	}

	// Hold transitionMu across the read-check-write so a concurrent Done cannot
	// move the task to committing between our status check and the
	// status/test-run writes (which would otherwise leave IsTestRun set on a task
	// now in the commit pipeline). Mirrors SubmitFeedback; RunBackground below
	// runs under the lock as it does there.
	transitionMu.Lock()
	defer transitionMu.Unlock()

	task, err := s.GetTask(r.Context(), id)
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

	// Resolve criteria: an explicit per-run override wins; otherwise fall back
	// to the criteria persisted on the task (the unattended path's source).
	criteria := strings.TrimSpace(req.Criteria)
	if criteria == "" {
		criteria = task.Criteria
	}
	testPrompt := buildTestPrompt(task.Prompt, criteria, implResult, diff)

	h.closeFeedbackWaitingSpan(r.Context(), id)

	// Mark task as a test run and clear any previous verdict.
	if err := s.UpdateTaskTestRun(r.Context(), id, true, ""); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Transition waiting → in_progress.
	if err := s.UpdateTaskStatus(r.Context(), id, store.TaskStatusInProgress); err != nil {
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

	httpjson.Write(w, http.StatusOK, map[string]string{"status": "testing"})
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
		out, err := cmdexec.Git(worktreePath, "diff", defBranch+"..HEAD", "--", ".").Output()
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
	if len(combined) > constants.MaxDiffBytes {
		combined = strings.ToValidUTF8(combined[:constants.MaxDiffBytes], "") + "\n... (diff truncated)"
	}
	return combined
}

// SyncTask rebases task worktrees onto the latest default branch without merging.
func (h *Handler) SyncTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	task, err := s.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status == store.TaskStatusInProgress {
		httpjson.Write(w, http.StatusOK, map[string]string{"status": "already_syncing"})
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
	// on an existing task.
	transitionMu.Lock()
	if err := s.ForceUpdateTaskStatus(r.Context(), id, store.TaskStatusInProgress); err != nil {
		transitionMu.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	transitionMu.Unlock()
	h.insertEventOrLog(r.Context(), id, store.EventTypeStateChange,
		store.NewStateChangeData(oldStatus, store.TaskStatusInProgress, store.TriggerUser, nil))

	sessionID := ""
	if task.SessionID != nil {
		sessionID = *task.SessionID
	}
	h.diffCache.invalidate(id)
	h.runner.SyncWorktreesBackground(id, sessionID, oldStatus, func() {
		h.diffCache.invalidate(id)
	})
	httpjson.Write(w, http.StatusOK, map[string]string{"status": "syncing"})
}
