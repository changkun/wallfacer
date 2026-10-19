package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/pkg/httpjson"
	"latere.ai/x/pkg/statemachine"

	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/store"
)

// SearchTasks handles GET /api/tasks/search?q=<text>.
// Returns a JSON array of store.TaskSearchResult (at most 50).
// q must be at least 2 runes; returns 400 otherwise.
func (h *Handler) SearchTasks(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < 2 {
		http.Error(w, "q must be at least 2 characters", http.StatusBadRequest)
		return
	}
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	results, err := s.SearchTasks(r.Context(), q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []store.TaskSearchResult{}
	}
	httpjson.Write(w, http.StatusOK, results)
}

// ListSummaries returns all immutable task summaries (one per completed task).
// Unlike ListTasks, it reads summary.json files directly without loading the
// full task.json, making it efficient for cost dashboards and analytics.
// Tasks that completed before summary.json was introduced are omitted.
func (h *Handler) ListSummaries(w http.ResponseWriter, _ *http.Request) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	summaries, err := s.ListSummaries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if summaries == nil {
		summaries = []store.TaskSummary{}
	}
	httpjson.Write(w, http.StatusOK, summaries)
}

// ListTasks returns all tasks, optionally including archived ones.
func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	// Capture the store once so all operations below are scoped to the same
	// workspace group, regardless of concurrent workspace switches.
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	pageSizeRaw := strings.TrimSpace(r.URL.Query().Get("archived_page_size"))
	if pageSizeRaw != "" {
		if !includeArchived {
			http.Error(w, "include_archived=true is required with archived_page_size", http.StatusBadRequest)
			return
		}
		pageSize, err := strconv.Atoi(pageSizeRaw)
		if err != nil {
			http.Error(w, "invalid archived_page_size", http.StatusBadRequest)
			return
		}
		if pageSize < 1 {
			pageSize = 1
		}
		if pageSize > 200 {
			pageSize = 200
		}
		var beforeID *uuid.UUID
		beforeRaw := strings.TrimSpace(r.URL.Query().Get("archived_before"))
		if beforeRaw != "" {
			parsed, err := uuid.Parse(beforeRaw)
			if err != nil {
				http.Error(w, "invalid archived_before", http.StatusBadRequest)
				return
			}
			beforeID = &parsed
		}
		var afterID *uuid.UUID
		afterRaw := strings.TrimSpace(r.URL.Query().Get("archived_after"))
		if afterRaw != "" {
			parsed, err := uuid.Parse(afterRaw)
			if err != nil {
				http.Error(w, "invalid archived_after", http.StatusBadRequest)
				return
			}
			afterID = &parsed
		}
		page, total, hasMoreBefore, hasMoreAfter, err := s.ListArchivedTasksPage(r.Context(), pageSize, beforeID, afterID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := struct {
			Tasks         []store.Task `json:"tasks"`
			TotalArchived int          `json:"total_archived"`
			HasMoreBefore bool         `json:"has_more_before"`
			HasMoreAfter  bool         `json:"has_more_after"`
			BeforeCursor  string       `json:"before_cursor,omitempty"`
			AfterCursor   string       `json:"after_cursor,omitempty"`
		}{
			Tasks:         page,
			TotalArchived: total,
			HasMoreBefore: hasMoreBefore,
			HasMoreAfter:  hasMoreAfter,
		}
		if len(page) > 0 {
			resp.AfterCursor = page[0].ID.String()
			resp.BeforeCursor = page[len(page)-1].ID.String()
		}
		httpjson.Write(w, http.StatusOK, resp)
		return
	}
	tasks, err := s.ListTasks(r.Context(), includeArchived)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tasks == nil {
		tasks = []store.Task{}
	}
	if q := r.URL.Query().Get("failure_category"); q != "" {
		category, ok2 := store.ParseFailureCategory(q)
		if !ok2 {
			http.Error(w, "invalid failure_category", http.StatusBadRequest)
			return
		}
		tasks = filterByFailureCategory(tasks, category)
	}
	httpjson.Write(w, http.StatusOK, tasks)
}

// filterByFailureCategory returns only those tasks whose FailureCategory
// matches cat. The input slice is not modified; a new slice is returned.
func filterByFailureCategory(tasks []store.Task, cat store.FailureCategory) []store.Task {
	filtered := make([]store.Task, 0, len(tasks))
	for _, t := range tasks {
		if t.FailureCategory == cat {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// taskCreateFields are the fields of a new task that POST /api/tasks
// accepts, with their mapping onto the stored task in apply.
type taskCreateFields struct {
	Prompt             string         `json:"prompt"`
	Criteria           string         `json:"criteria"`
	Timeout            int            `json:"timeout"`
	MountWorktrees     bool           `json:"mount_worktrees"`
	Kind               store.TaskKind `json:"kind"`
	Tags               []string       `json:"tags"`
	ScheduledAt        *time.Time     `json:"scheduled_at,omitempty"`
	CustomPassPatterns []string       `json:"custom_pass_patterns,omitempty"`
	CustomFailPatterns []string       `json:"custom_fail_patterns,omitempty"`
	MaxCostUSD         float64        `json:"max_cost_usd"`
	MaxInputTokens     int            `json:"max_input_tokens"`
	Model              string         `json:"model"`
}

// apply copies the fields into opts. The store normalizes the model and the
// budget: a negative limit becomes unlimited and a blank model leaves the
// global default in effect. The caller validates the prompt and the custom
// patterns first.
func (f taskCreateFields) apply(opts *store.TaskCreateOptions) {
	opts.Prompt = f.Prompt
	opts.Criteria = f.Criteria
	opts.Timeout = f.Timeout
	opts.MountWorktrees = f.MountWorktrees
	opts.Kind = f.Kind
	opts.Tags = f.Tags
	opts.ScheduledAt = f.ScheduledAt
	opts.CustomPassPatterns = f.CustomPassPatterns
	opts.CustomFailPatterns = f.CustomFailPatterns
	opts.MaxCostUSD = f.MaxCostUSD
	opts.MaxInputTokens = f.MaxInputTokens
	opts.ModelOverride = f.Model
}

// CreateTask creates a new task in backlog status.
//
// The `sandbox` and `sandbox_by_activity` fields are not accepted on
// the POST body. A task's harness, and its per-activity harness map,
// are set via PATCH /api/tasks/{id} after creation; without one the
// task follows the env file's WALLFACER_SANDBOX_* settings. Requests
// that include either field get a 400 that names the PATCH path.
func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	req, ok := httpjson.DecodeBody[struct {
		taskCreateFields
		Sandbox           *harness.ID                          `json:"sandbox,omitempty"`
		SandboxByActivity map[store.SandboxActivity]harness.ID `json:"sandbox_by_activity,omitempty"`
	}](w, r)
	if !ok {
		return
	}
	if req.Sandbox != nil {
		http.Error(w,
			"the \"sandbox\" field is not accepted on POST /api/tasks; set the task's harness via PATCH /api/tasks/{id} after creation",
			http.StatusBadRequest)
		return
	}
	if len(req.SandboxByActivity) > 0 {
		http.Error(w,
			"the \"sandbox_by_activity\" field is not accepted on POST /api/tasks; set the task's per-activity harnesses via PATCH /api/tasks/{id} after creation",
			http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}
	if err := validateCustomPatterns(req.CustomPassPatterns, req.CustomFailPatterns); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s, ok2 := h.requireStore(w)
	if !ok2 {
		return
	}

	var opts store.TaskCreateOptions
	req.apply(&opts)
	task, err := s.CreateTaskWithOptions(r.Context(), opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.insertEventOrLog(r.Context(), task.ID, store.EventTypeStateChange,
		store.NewStateChangeData("", store.TaskStatusBacklog, store.TriggerUser, nil))

	h.runner.GenerateTitleBackground(task.ID, task.Prompt)

	httpjson.Write(w, http.StatusCreated, task)
}

// UpdateTask handles PATCH requests: status transitions, position, prompt, etc.
func (h *Handler) UpdateTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	req, ok := httpjson.DecodeBody[struct {
		Status            *store.TaskStatus                     `json:"status"`
		Position          *int                                  `json:"position"`
		Prompt            *string                               `json:"prompt"`
		Criteria          *string                               `json:"criteria"`
		Timeout           *int                                  `json:"timeout"`
		FreshStart        *bool                                 `json:"fresh_start"`
		MountWorktrees    *bool                                 `json:"mount_worktrees"`
		Sandbox           *harness.ID                           `json:"sandbox"`
		SandboxByActivity *map[store.SandboxActivity]harness.ID `json:"sandbox_by_activity"`
		DependsOn         *[]string                             `json:"depends_on"`
		Tags              *[]string                             `json:"tags"`
		// Archived flips the archived flag (replaces POST /archive,/unarchive).
		Archived *bool `json:"archived"`
		// Deleted=false restores a soft-deleted task (replaces POST /restore).
		// Soft-delete itself uses DELETE /api/tasks/{id}, not PATCH.
		Deleted        *bool    `json:"deleted"`
		MaxCostUSD     *float64 `json:"max_cost_usd"`
		MaxInputTokens *int     `json:"max_input_tokens"`
		// Model sets the per-task model override; empty string clears it.
		Model *string `json:"model"`
		// ScheduledAt uses json.RawMessage so we can distinguish "absent" (nil)
		// from explicitly-sent "null" (clear the schedule) or a valid time (set it).
		ScheduledAt        json.RawMessage `json:"scheduled_at"`
		CustomPassPatterns []string        `json:"custom_pass_patterns,omitempty"`
		CustomFailPatterns []string        `json:"custom_fail_patterns,omitempty"`
	}](w, r)
	if !ok {
		return
	}

	s, ok2 := h.requireStore(w)
	if !ok2 {
		return
	}

	// Restore (deleted=false) operates on a tombstoned task, which the
	// GetTask lookup below would not find. Handle it up front. Only
	// deleted=false is accepted; soft-delete uses DELETE /api/tasks/{id}.
	if req.Deleted != nil {
		if *req.Deleted {
			http.Error(w, "soft-delete uses DELETE /api/tasks/{id}, not PATCH", http.StatusBadRequest)
			return
		}
		if err := h.applyRestore(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		restored, err := s.GetTask(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		httpjson.Write(w, http.StatusOK, restored)
		return
	}

	task, err := s.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	// Archived flag flip (replaces POST /archive and /unarchive). It is a
	// standalone mutation, independent of status, so it short-circuits.
	if req.Archived != nil {
		if *req.Archived && task.Status != store.TaskStatusDone && task.Status != store.TaskStatusCancelled {
			http.Error(w, "only done or cancelled tasks can be archived", http.StatusBadRequest)
			return
		}
		if err := h.applyArchive(r.Context(), *task, *req.Archived); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		updated, err := s.GetTask(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		httpjson.Write(w, http.StatusOK, updated)
		return
	}

	// Allow editing prompt, criteria, timeout, fresh_start, mount_worktrees, sandbox, model, budget, and custom patterns for backlog tasks.
	if task.Status == store.TaskStatusBacklog && (req.Prompt != nil || req.Criteria != nil || req.Timeout != nil || req.FreshStart != nil || req.MountWorktrees != nil || req.Sandbox != nil || req.SandboxByActivity != nil || req.MaxCostUSD != nil || req.MaxInputTokens != nil || req.Model != nil || req.CustomPassPatterns != nil || req.CustomFailPatterns != nil) {
		sandbox := task.Sandbox
		if req.Sandbox != nil {
			sandbox = *req.Sandbox
		}
		activity := task.SandboxByActivity
		if req.SandboxByActivity != nil {
			activity = *req.SandboxByActivity
		}
		if err := h.validateRequestedSandboxes(sandbox, activity); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := validateCustomPatterns(req.CustomPassPatterns, req.CustomFailPatterns); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.UpdateTaskBacklog(r.Context(), id, req.Prompt, req.Timeout, req.FreshStart, req.MountWorktrees, req.SandboxByActivity, req.MaxCostUSD, req.MaxInputTokens); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if req.Criteria != nil {
			if err := s.UpdateTaskCriteria(r.Context(), id, *req.Criteria); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if req.Sandbox != nil {
			if err := s.UpdateTaskSandbox(r.Context(), id, *req.Sandbox); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if req.Model != nil {
			if err := s.UpdateTaskModelOverride(r.Context(), id, *req.Model); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if req.CustomPassPatterns != nil || req.CustomFailPatterns != nil {
			passP := req.CustomPassPatterns
			failP := req.CustomFailPatterns
			if passP == nil {
				passP = task.CustomPassPatterns
			}
			if failP == nil {
				failP = task.CustomFailPatterns
			}
			if err := s.UpdateTaskCustomPatterns(r.Context(), id, passP, failP); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	// Allow setting/clearing scheduled_at for backlog tasks.
	// req.ScheduledAt is nil when the field was absent from the JSON body (no-op).
	// When present it is either "null" (clear) or an ISO 8601 timestamp (set).
	if task.Status == store.TaskStatusBacklog && len(req.ScheduledAt) > 0 {
		var scheduledAt *time.Time
		// "null" clears the schedule; any other value is parsed as a time.
		if string(req.ScheduledAt) != "null" {
			var t time.Time
			if err := json.Unmarshal(req.ScheduledAt, &t); err != nil {
				http.Error(w, "invalid scheduled_at: "+err.Error(), http.StatusBadRequest)
				return
			}
			if !t.IsZero() {
				scheduledAt = &t
			}
		}
		if err := s.UpdateTaskScheduledAt(r.Context(), id, scheduledAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Allow raising budget limits for waiting tasks (so users can continue a paused task).
	if task.Status == store.TaskStatusWaiting && (req.MaxCostUSD != nil || req.MaxInputTokens != nil) {
		if err := s.UpdateTaskBudget(r.Context(), id, req.MaxCostUSD, req.MaxInputTokens); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if req.Position != nil {
		if err := s.UpdateTaskPosition(r.Context(), id, *req.Position); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if req.DependsOn != nil {
		parsedDeps := make([]uuid.UUID, 0, len(*req.DependsOn))
		for _, depStr := range *req.DependsOn {
			depID, err := uuid.Parse(depStr)
			if err != nil {
				http.Error(w, fmt.Sprintf("invalid dependency UUID %q: %v", depStr, err), http.StatusBadRequest)
				return
			}
			if depID == id {
				http.Error(w, "task cannot depend on itself", http.StatusBadRequest)
				return
			}
			if _, err := s.GetTask(r.Context(), depID); err != nil {
				http.Error(w, fmt.Sprintf("dependency task not found: %s", depStr), http.StatusBadRequest)
				return
			}
			parsedDeps = append(parsedDeps, depID)
		}
		// Cycle detection using full graph including archived tasks.
		allTasks, _ := s.ListTasks(r.Context(), true)
		for _, depID := range parsedDeps {
			if taskReachable(allTasks, depID, id) {
				http.Error(w, fmt.Sprintf("dependency on %s would create a cycle", depID), http.StatusBadRequest)
				return
			}
		}
		strs := make([]string, len(parsedDeps))
		for i, d := range parsedDeps {
			strs[i] = d.String()
		}
		if err := s.UpdateTaskDependsOn(r.Context(), id, strs); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if req.Tags != nil {
		if err := s.UpdateTaskTags(r.Context(), id, *req.Tags); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if req.Status != nil {
		oldStatus := task.Status
		newStatus := *req.Status

		// Cancel is a side-effecting transition (kill container, clean
		// worktrees) that bypasses the state machine. Intercept it before the
		// agent-session-lock check below, matching the old
		// POST /api/tasks/{id}/cancel behavior.
		if newStatus == store.TaskStatusCancelled {
			if !cancellableStatuses[oldStatus] {
				http.Error(w, "task cannot be cancelled in its current status", http.StatusBadRequest)
				return
			}
			if err := h.applyCancel(r.Context(), *task); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			updated, err := s.GetTask(r.Context(), id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			httpjson.Write(w, http.StatusOK, updated)
			return
		}

		// Reject status changes on tasks that have an in-flight plan turn.
		if oldStatus != newStatus {
			if locked, threadID := h.isTaskLockedByAgent(id.String()); locked {
				httpjson.Write(w, http.StatusConflict, map[string]string{
					"error":     "task is locked by an in-flight plan turn",
					"thread_id": threadID,
				})
				return
			}
		}

		// Handle retry: done/failed/waiting/cancelled → backlog
		if newStatus == store.TaskStatusBacklog && (oldStatus == store.TaskStatusDone || oldStatus == store.TaskStatusFailed || oldStatus == store.TaskStatusCancelled || oldStatus == store.TaskStatusWaiting) {
			newPrompt := task.Prompt
			if req.Prompt != nil {
				newPrompt = *req.Prompt
			}
			// Default to resuming the previous session; the client can opt out by sending fresh_start=true.
			freshStart := false
			if req.FreshStart != nil {
				freshStart = *req.FreshStart
			}
			// Only delete the worktree directory and branch on fresh_start.
			// For normal retries the branch holds Claude's committed work;
			// ensureTaskWorktrees will reattach it on the next run.
			if freshStart && len(task.WorktreePaths) > 0 {
				h.runner.CleanupWorktrees(id, task.WorktreePaths, task.BranchName)
			}
			if err := s.ResetTaskForRetry(r.Context(), id, newPrompt, freshStart); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			h.insertEventOrLog(r.Context(), id, store.EventTypeStateChange,
				store.NewStateChangeData(oldStatus, store.TaskStatusBacklog, store.TriggerUser, nil))
			h.diffCache.invalidate(id)

			updated, err := s.GetTask(r.Context(), id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			httpjson.Write(w, http.StatusOK, updated)
			return
		}
		// Enforce concurrency limit for manual backlog → in_progress transitions.
		if newStatus == store.TaskStatusInProgress && oldStatus == store.TaskStatusBacklog && !task.IsTestRun {
			if !h.checkConcurrencyAndUpdateStatus(r.Context(), w, id, newStatus) {
				return
			}
			h.insertEventOrLog(r.Context(), id, store.EventTypeStateChange,
				store.NewStateChangeData(oldStatus, newStatus, store.TriggerUser, nil))
			h.diffCache.invalidate(id)
			h.cascadeArchiveThreadsForTask(id.String())
			sessionID := ""
			if !task.FreshStart && task.SessionID != nil {
				sessionID = *task.SessionID
			}
			h.runner.RunBackground(id, task.Prompt, sessionID, false)
			updated, err := s.GetTask(r.Context(), id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			httpjson.Write(w, http.StatusOK, updated)
			return
		}

		// Reject any other direct PATCH to in_progress (i.e. from waiting or
		// failed; backlog is handled above). These transitions only flip the
		// status and consume a concurrency slot — they do NOT launch an agent
		// worker, leaving the task stuck in_progress with no worker. Resuming a
		// waiting/failed task must go through ResumeTask/TestTask/SubmitFeedback,
		// which pair the status flip with a RunBackground launch.
		if newStatus == store.TaskStatusInProgress && !task.IsTestRun {
			http.Error(w, "cannot move a task to in_progress via PATCH; use resume, test, or feedback to start a worker", http.StatusBadRequest)
			return
		}

		if err := s.UpdateTaskStatus(r.Context(), id, newStatus); err != nil {
			if errors.Is(err, statemachine.ErrInvalidTransition) {
				http.Error(w, err.Error(), http.StatusBadRequest)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		h.insertEventOrLog(r.Context(), id, store.EventTypeStateChange,
			store.NewStateChangeData(oldStatus, newStatus, store.TriggerUser, nil))
		h.diffCache.invalidate(id)
		if oldStatus == store.TaskStatusBacklog || oldStatus == store.TaskStatusWaiting {
			h.cascadeArchiveThreadsForTask(id.String())
		}

		if newStatus == store.TaskStatusInProgress && oldStatus == store.TaskStatusBacklog {
			sessionID := ""
			if !task.FreshStart && task.SessionID != nil {
				sessionID = *task.SessionID
			}
			h.runner.RunBackground(id, task.Prompt, sessionID, false)
		}

	}

	updated, err := s.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpjson.Write(w, http.StatusOK, updated)
}

// DeleteTask soft-deletes a task by writing a tombstone. The task data is
// retained on disk for the configured retention period so it can be restored.
func (h *Handler) DeleteTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	// reason is optional; an empty or absent body is fine.
	req, ok := httpjson.DecodeOptionalBody[struct {
		Reason string `json:"reason"`
	}](w, r)
	if !ok {
		return
	}
	s, ok2 := h.requireStore(w)
	if !ok2 {
		return
	}
	if task, err := s.GetTask(r.Context(), id); err == nil {
		if len(task.WorktreePaths) > 0 {
			h.runner.CleanupWorktrees(id, task.WorktreePaths, task.BranchName)
		}
	}
	if err := s.DeleteTask(r.Context(), id, req.Reason); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.cascadeArchiveThreadsForTask(id.String())
	w.WriteHeader(http.StatusNoContent)
}

// ListDeletedTasks returns all soft-deleted (tombstoned) tasks.
func (h *Handler) ListDeletedTasks(w http.ResponseWriter, r *http.Request) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	tasks, err := s.ListDeletedTasks(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tasks == nil {
		tasks = []store.Task{}
	}
	httpjson.Write(w, http.StatusOK, tasks)
}

// applyRestore removes the tombstone from a soft-deleted task, making it
// active again, and reverses the lifecycle-cascade thread archiving.
// Invoked from the PATCH deleted=false path (UpdateTask).
func (h *Handler) applyRestore(ctx context.Context, id uuid.UUID) error {
	s, ok := h.currentStore()
	if !ok {
		return errors.New("no workspaces configured")
	}
	if err := s.RestoreTask(ctx, id); err != nil {
		return err
	}
	h.cascadeUnarchiveThreadsForTask(id.String())
	return nil
}

// validateCustomPatterns compiles each pattern to ensure it is valid RE2 syntax.
// Returns a 400-friendly error message that includes the offending pattern.
func validateCustomPatterns(passPatterns, failPatterns []string) error {
	for _, p := range passPatterns {
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("invalid custom_pass_pattern %q: %v", p, err)
		}
	}
	for _, p := range failPatterns {
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("invalid custom_fail_pattern %q: %v", p, err)
		}
	}
	return nil
}
