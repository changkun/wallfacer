package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/gitutil"
	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
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
	results, err := h.store.SearchTasks(r.Context(), q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []store.TaskSearchResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

// ListSummaries returns all immutable task summaries (one per completed task).
// Unlike ListTasks, it reads summary.json files directly without loading the
// full task.json, making it efficient for cost dashboards and analytics.
// Tasks that completed before summary.json was introduced are omitted.
func (h *Handler) ListSummaries(w http.ResponseWriter, r *http.Request) {
	summaries, err := h.store.ListSummaries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if summaries == nil {
		summaries = []store.TaskSummary{}
	}
	writeJSON(w, http.StatusOK, summaries)
}

// ListTasks returns all tasks, optionally including archived ones.
func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
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
		page, total, hasMoreBefore, hasMoreAfter, err := h.store.ListArchivedTasksPage(r.Context(), pageSize, beforeID, afterID)
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
		writeJSON(w, http.StatusOK, resp)
		return
	}
	tasks, err := h.store.ListTasks(r.Context(), includeArchived)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tasks == nil {
		tasks = []store.Task{}
	}
	if q := r.URL.Query().Get("failure_category"); q != "" {
		tasks = filterByFailureCategory(tasks, store.FailureCategory(q))
	}
	writeJSON(w, http.StatusOK, tasks)
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

// CreateTask creates a new task in backlog status.
func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt            string            `json:"prompt"`
		Timeout           int               `json:"timeout"`
		MountWorktrees    bool              `json:"mount_worktrees"`
		Sandbox           string            `json:"sandbox"`
		SandboxByActivity map[string]string `json:"sandbox_by_activity"`
		Kind              store.TaskKind    `json:"kind"`
		MaxCostUSD        float64           `json:"max_cost_usd"`
		MaxInputTokens    int               `json:"max_input_tokens"`
		Model             string            `json:"model"`
		ScheduledAt       *time.Time        `json:"scheduled_at,omitempty"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Prompt) == "" && req.Kind != store.TaskKindIdeaAgent {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}
	if err := h.validateRequestedSandboxes(req.Sandbox, req.SandboxByActivity); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	task, err := h.store.CreateTaskWithOptions(r.Context(), store.TaskCreateOptions{
		Prompt:            req.Prompt,
		Timeout:           req.Timeout,
		MountWorktrees:    req.MountWorktrees,
		Kind:              req.Kind,
		Sandbox:           req.Sandbox,
		SandboxByActivity: req.SandboxByActivity,
		MaxCostUSD:        req.MaxCostUSD,
		MaxInputTokens:    req.MaxInputTokens,
		ModelOverride:     req.Model,
		ScheduledAt:       req.ScheduledAt,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.store.InsertEvent(r.Context(), task.ID, store.EventTypeStateChange, map[string]string{
		"to":      string(store.TaskStatusBacklog),
		"trigger": store.TriggerUser,
	})

	if task.Kind != store.TaskKindIdeaAgent {
		h.runner.GenerateTitleBackground(task.ID, task.Prompt)
	}

	writeJSON(w, http.StatusCreated, task)
}

// batchTaskInput describes a single task in a BatchCreateTasks request.
type batchTaskInput struct {
	Ref               string            `json:"ref"`
	Prompt            string            `json:"prompt"`
	Timeout           int               `json:"timeout"`
	Tags              []string          `json:"tags"`
	Sandbox           string            `json:"sandbox"`
	SandboxByActivity map[string]string `json:"sandbox_by_activity"`
	Kind              store.TaskKind    `json:"kind"`
	MountWorktrees    bool              `json:"mount_worktrees"`
	DependsOnRefs     []string          `json:"depends_on_refs"`
}

type batchCreateRequest struct {
	Tasks []batchTaskInput `json:"tasks"`
}

// BatchCreateTasks creates multiple tasks atomically with dependency wiring via
// symbolic ref names declared within the batch. The handler runs a full preflight
// validation phase before any persistence: duplicate refs, empty prompts, sandbox
// validity, existence of external dependency UUIDs, self-dependencies, and cycle
// detection against the combined graph of existing tasks plus proposed edges.
// If validation fails the store is untouched and the handler returns 400 or 422.
// On success it returns 201 Created with tasks (in input order) and ref_to_id.
func (h *Handler) BatchCreateTasks(w http.ResponseWriter, r *http.Request) {
	var req batchCreateRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}

	if len(req.Tasks) == 0 {
		http.Error(w, "tasks must not be empty", http.StatusBadRequest)
		return
	}
	if len(req.Tasks) > 50 {
		http.Error(w, "tasks must not exceed 50 items", http.StatusBadRequest)
		return
	}

	n := len(req.Tasks)

	// =========================================================================
	// PREFLIGHT VALIDATION — zero side effects; all checks run before any write
	// =========================================================================

	// 1. Collect refs and validate uniqueness.
	refToIdx := make(map[string]int, n)
	for i, t := range req.Tasks {
		if t.Ref == "" {
			continue
		}
		if _, dup := refToIdx[t.Ref]; dup {
			http.Error(w, fmt.Sprintf("duplicate ref: %q", t.Ref), http.StatusBadRequest)
			return
		}
		refToIdx[t.Ref] = i
	}

	// 2. Validate prompts.
	for _, t := range req.Tasks {
		if strings.TrimSpace(t.Prompt) == "" && t.Kind != store.TaskKindIdeaAgent {
			ref := t.Ref
			if ref == "" {
				ref = "<unnamed>"
			}
			http.Error(w, fmt.Sprintf("ref %q: prompt is required", ref), http.StatusBadRequest)
			return
		}
	}

	// 3. Validate sandboxes.
	for i, t := range req.Tasks {
		if err := h.validateRequestedSandboxes(t.Sandbox, t.SandboxByActivity); err != nil {
			ref := t.Ref
			if ref == "" {
				ref = fmt.Sprintf("<index %d>", i)
			}
			http.Error(w, fmt.Sprintf("ref %q: %s", ref, err.Error()), http.StatusBadRequest)
			return
		}
	}

	// 4. Validate each depends_on_refs entry is a known batch ref or a valid UUID syntax.
	for i, t := range req.Tasks {
		for _, dep := range t.DependsOnRefs {
			if _, ok := refToIdx[dep]; !ok {
				if _, err := uuid.Parse(dep); err != nil {
					ref := t.Ref
					if ref == "" {
						ref = fmt.Sprintf("<index %d>", i)
					}
					http.Error(w, fmt.Sprintf("ref %q: unknown ref in depends_on_refs: %q", ref, dep), http.StatusBadRequest)
					return
				}
			}
		}
	}

	// 5. Topological sort on batch-internal edges (Kahn's algorithm) to detect
	//    internal cycles and compute creation order.
	inDegree := make([]int, n)
	// batchAdj[i] holds the indices of tasks that depend on task i.
	batchAdj := make([][]int, n)
	for i, t := range req.Tasks {
		for _, dep := range t.DependsOnRefs {
			if depIdx, ok := refToIdx[dep]; ok {
				batchAdj[depIdx] = append(batchAdj[depIdx], i)
				inDegree[i]++
			}
		}
	}
	queue := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if inDegree[i] == 0 {
			queue = append(queue, i)
		}
	}
	topoOrder := make([]int, 0, n)
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		topoOrder = append(topoOrder, curr)
		for _, next := range batchAdj[curr] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if len(topoOrder) != n {
		processed := make(map[int]bool, len(topoOrder))
		for _, idx := range topoOrder {
			processed[idx] = true
		}
		var cycleRefs []string
		for i, t := range req.Tasks {
			if !processed[i] {
				ref := t.Ref
				if ref == "" {
					ref = fmt.Sprintf("<index %d>", i)
				}
				cycleRefs = append(cycleRefs, ref)
			}
		}
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": "cycle detected",
			"cycle": cycleRefs,
		})
		return
	}

	// 6. Pre-assign UUIDs so external-dep existence checks and full-graph cycle
	//    detection can reason about the complete post-creation dependency graph.
	preAssignedIDs := make([]uuid.UUID, n)
	for i := range req.Tasks {
		preAssignedIDs[i] = uuid.New()
	}

	// 7. Verify every external UUID dep actually exists in the store.
	//    Self-deps via symbolic ref are already caught by Kahn's above (self-loop
	//    leaves inDegree > 0). We also explicitly guard against a task listing its
	//    own pre-assigned UUID, though callers cannot know it in practice.
	for i, t := range req.Tasks {
		for _, dep := range t.DependsOnRefs {
			if _, ok := refToIdx[dep]; ok {
				// Batch-internal ref — already validated by Kahn's.
				if refToIdx[dep] == i {
					// Self-dep through batch ref (redundant guard; Kahn's catches it).
					ref := t.Ref
					if ref == "" {
						ref = fmt.Sprintf("<index %d>", i)
					}
					http.Error(w, fmt.Sprintf("ref %q: task cannot depend on itself", ref), http.StatusBadRequest)
					return
				}
				continue
			}
			// External UUID — verify it exists.
			depID, _ := uuid.Parse(dep) // already confirmed parseable in step 4
			if _, err := h.store.GetTask(r.Context(), depID); err != nil {
				ref := t.Ref
				if ref == "" {
					ref = fmt.Sprintf("<index %d>", i)
				}
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
					"error": fmt.Sprintf("ref %q: dependency task not found: %s", ref, dep),
				})
				return
			}
		}
	}

	// 8. Full combined-graph cycle check: build an adjacency map that merges
	//    existing task edges with the proposed batch edges (using pre-assigned
	//    UUIDs) and confirm no new cycle is introduced.
	//    Note: existing tasks cannot reference the freshly pre-assigned UUIDs, so
	//    cycles through external deps are mathematically impossible here; this
	//    check is defence-in-depth and guards against any future store changes.
	allTasks, _ := h.store.ListTasks(r.Context(), true)
	combinedAdj := make(map[uuid.UUID][]uuid.UUID, len(allTasks)+n)
	for _, t := range allTasks {
		for _, depStr := range t.DependsOn {
			if depID, err := uuid.Parse(depStr); err == nil {
				combinedAdj[t.ID] = append(combinedAdj[t.ID], depID)
			}
		}
	}
	for i, t := range req.Tasks {
		myID := preAssignedIDs[i]
		for _, dep := range t.DependsOnRefs {
			var depID uuid.UUID
			if depIdx, ok := refToIdx[dep]; ok {
				depID = preAssignedIDs[depIdx]
			} else {
				depID, _ = uuid.Parse(dep)
			}
			combinedAdj[myID] = append(combinedAdj[myID], depID)
		}
	}
	for i, t := range req.Tasks {
		myID := preAssignedIDs[i]
		for _, depID := range combinedAdj[myID] {
			if taskReachableInAdj(combinedAdj, depID, myID) {
				ref := t.Ref
				if ref == "" {
					ref = fmt.Sprintf("<index %d>", i)
				}
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
					"error": fmt.Sprintf("ref %q: dependency would create a cycle", ref),
				})
				return
			}
		}
	}

	// =========================================================================
	// PERSISTENCE — all validation passed; create tasks in topological order
	// =========================================================================

	// refToID maps each batch ref to its created UUID for dependency resolution.
	refToID := make(map[string]uuid.UUID, n)

	for _, idx := range topoOrder {
		t := req.Tasks[idx]

		// Resolve depends_on_refs to UUID strings before the store call so the
		// task is persisted in final form (no post-create UpdateTaskDependsOn).
		depStrs := make([]string, 0, len(t.DependsOnRefs))
		for _, dep := range t.DependsOnRefs {
			if depID, ok := refToID[dep]; ok {
				depStrs = append(depStrs, depID.String())
			} else {
				depStrs = append(depStrs, dep)
			}
		}

		task, err := h.store.CreateTaskWithOptions(r.Context(), store.TaskCreateOptions{
			ID:                preAssignedIDs[idx],
			Prompt:            t.Prompt,
			Timeout:           t.Timeout,
			Tags:              t.Tags,
			MountWorktrees:    t.MountWorktrees,
			Kind:              t.Kind,
			Sandbox:           t.Sandbox,
			SandboxByActivity: t.SandboxByActivity,
			DependsOn:         depStrs,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		h.store.InsertEvent(r.Context(), task.ID, store.EventTypeStateChange, map[string]string{
			"to":      string(store.TaskStatusBacklog),
			"trigger": store.TriggerUser,
		})

		if t.Kind != store.TaskKindIdeaAgent {
			h.runner.GenerateTitleBackground(task.ID, task.Prompt)
		}

		if t.Ref != "" {
			refToID[t.Ref] = task.ID
		}
	}

	// Return tasks in input order.
	finalTasks := make([]store.Task, n)
	for i := range req.Tasks {
		updated, err := h.store.GetTask(r.Context(), preAssignedIDs[i])
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		finalTasks[i] = *updated
	}

	refToIDStr := make(map[string]string, len(refToID))
	for ref, id := range refToID {
		refToIDStr[ref] = id.String()
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"tasks":     finalTasks,
		"ref_to_id": refToIDStr,
	})
}

// UpdateTask handles PATCH requests: status transitions, position, prompt, etc.
func (h *Handler) UpdateTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var req struct {
		Status            *store.TaskStatus  `json:"status"`
		Position          *int               `json:"position"`
		Prompt            *string            `json:"prompt"`
		Timeout           *int               `json:"timeout"`
		FreshStart        *bool              `json:"fresh_start"`
		MountWorktrees    *bool              `json:"mount_worktrees"`
		Sandbox           *string            `json:"sandbox"`
		SandboxByActivity *map[string]string `json:"sandbox_by_activity"`
		DependsOn         *[]string          `json:"depends_on"`
		MaxCostUSD        *float64           `json:"max_cost_usd"`
		MaxInputTokens    *int               `json:"max_input_tokens"`
		// Model sets the per-task model override; empty string clears it.
		Model *string `json:"model"`
		// ScheduledAt uses json.RawMessage so we can distinguish "absent" (nil)
		// from explicitly-sent "null" (clear the schedule) or a valid time (set it).
		ScheduledAt json.RawMessage `json:"scheduled_at"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	// Allow editing prompt, timeout, fresh_start, mount_worktrees, sandbox, model, and budget for backlog tasks.
	if task.Status == store.TaskStatusBacklog && (req.Prompt != nil || req.Timeout != nil || req.FreshStart != nil || req.MountWorktrees != nil || req.Sandbox != nil || req.SandboxByActivity != nil || req.MaxCostUSD != nil || req.MaxInputTokens != nil || req.Model != nil) {
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
		if err := h.store.UpdateTaskBacklog(r.Context(), id, req.Prompt, req.Timeout, req.FreshStart, req.MountWorktrees, req.SandboxByActivity, req.MaxCostUSD, req.MaxInputTokens); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if req.Sandbox != nil {
			if err := h.store.UpdateTaskSandbox(r.Context(), id, *req.Sandbox); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if req.Model != nil {
			if err := h.store.UpdateTaskModelOverride(r.Context(), id, *req.Model); err != nil {
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
		if err := h.store.UpdateTaskScheduledAt(r.Context(), id, scheduledAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Allow raising budget limits for waiting tasks (so users can continue a paused task).
	if task.Status == store.TaskStatusWaiting && (req.MaxCostUSD != nil || req.MaxInputTokens != nil) {
		if err := h.store.UpdateTaskBudget(r.Context(), id, req.MaxCostUSD, req.MaxInputTokens); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if req.Position != nil {
		if err := h.store.UpdateTaskPosition(r.Context(), id, *req.Position); err != nil {
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
			if _, err := h.store.GetTask(r.Context(), depID); err != nil {
				http.Error(w, fmt.Sprintf("dependency task not found: %s", depStr), http.StatusBadRequest)
				return
			}
			parsedDeps = append(parsedDeps, depID)
		}
		// Cycle detection using full graph including archived tasks.
		allTasks, _ := h.store.ListTasks(r.Context(), true)
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
		if err := h.store.UpdateTaskDependsOn(r.Context(), id, strs); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if req.Status != nil {
		oldStatus := task.Status
		newStatus := *req.Status

		// Handle retry: done/failed/waiting/cancelled → backlog
		if newStatus == store.TaskStatusBacklog && (oldStatus == store.TaskStatusDone || oldStatus == store.TaskStatusFailed || oldStatus == store.TaskStatusCancelled || oldStatus == store.TaskStatusWaiting) {
			// Clean up any existing worktrees before resetting.
			if len(task.WorktreePaths) > 0 {
				h.runner.CleanupWorktrees(id, task.WorktreePaths, task.BranchName)
			}
			newPrompt := task.Prompt
			if req.Prompt != nil {
				newPrompt = *req.Prompt
			}
			// Default to resuming the previous session; the client can opt out by sending fresh_start=true.
			freshStart := false
			if req.FreshStart != nil {
				freshStart = *req.FreshStart
			}
			if err := h.store.ResetTaskForRetry(r.Context(), id, newPrompt, freshStart); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
				"from":    string(oldStatus),
				"to":      string(store.TaskStatusBacklog),
				"trigger": store.TriggerUser,
			})
			h.diffCache.invalidate(id)

			updated, err := h.store.GetTask(r.Context(), id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, updated)
			return
		} else {
			// Enforce concurrency limit for manual backlog → in_progress transitions.
			if newStatus == store.TaskStatusInProgress && oldStatus == store.TaskStatusBacklog && !task.IsTestRun {
				if !h.checkConcurrencyAndUpdateStatus(r.Context(), w, id, oldStatus, newStatus) {
					return
				}
				h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
					"from":    string(oldStatus),
					"to":      string(newStatus),
					"trigger": store.TriggerUser,
				})
				h.diffCache.invalidate(id)
				sessionID := ""
				if !task.FreshStart && task.SessionID != nil {
					sessionID = *task.SessionID
				}
				h.runner.RunBackground(id, task.Prompt, sessionID, false)
				updated, err := h.store.GetTask(r.Context(), id)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				writeJSON(w, http.StatusOK, updated)
				return
			}

			// Also block any direct in_progress transition that is not marked as
			// a test run. This protects API callers that PATCH waiting/failed →
			// in_progress from bypassing the concurrency limit.
			if newStatus == store.TaskStatusInProgress && !task.IsTestRun {
				if !h.checkConcurrencyAndUpdateStatus(r.Context(), w, id, oldStatus, newStatus) {
					return
				}
				h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
					"from":    string(oldStatus),
					"to":      string(newStatus),
					"trigger": store.TriggerUser,
				})
				h.diffCache.invalidate(id)
				updated, err := h.store.GetTask(r.Context(), id)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				writeJSON(w, http.StatusOK, updated)
				return
			}
		}

		if err := h.store.UpdateTaskStatus(r.Context(), id, newStatus); err != nil {
			if errors.Is(err, store.ErrInvalidTransition) {
				http.Error(w, err.Error(), http.StatusBadRequest)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
			"from":    string(oldStatus),
			"to":      string(newStatus),
			"trigger": store.TriggerUser,
		})
		h.diffCache.invalidate(id)

		if newStatus == store.TaskStatusInProgress && oldStatus == store.TaskStatusBacklog {
			sessionID := ""
			if !task.FreshStart && task.SessionID != nil {
				sessionID = *task.SessionID
			}
			h.runner.RunBackground(id, task.Prompt, sessionID, false)
		}
	}

	updated, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// DeleteTask soft-deletes a task by writing a tombstone. The task data is
// retained on disk for the configured retention period so it can be restored.
func (h *Handler) DeleteTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var req struct {
		Reason string `json:"reason"`
	}
	// reason is optional; an empty or absent body is fine.
	if !decodeOptionalJSONBody(w, r, &req) {
		return
	}
	if task, err := h.store.GetTask(r.Context(), id); err == nil && len(task.WorktreePaths) > 0 {
		h.runner.CleanupWorktrees(id, task.WorktreePaths, task.BranchName)
	}
	if err := h.store.DeleteTask(r.Context(), id, req.Reason); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListDeletedTasks returns all soft-deleted (tombstoned) tasks.
func (h *Handler) ListDeletedTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.store.ListDeletedTasks(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tasks == nil {
		tasks = []store.Task{}
	}
	writeJSON(w, http.StatusOK, tasks)
}

// RestoreTask removes the tombstone from a soft-deleted task, making it active again.
func (h *Handler) RestoreTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if err := h.store.RestoreTask(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetTurnUsage returns the per-turn usage log for a task as a JSON array.
func (h *Handler) GetTurnUsage(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid task id", http.StatusBadRequest)
		return
	}
	records, err := h.store.GetTurnUsages(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, records)
}

// eventsPageResponse is the JSON envelope returned when pagination params are present.
type eventsPageResponse struct {
	Events        []store.TaskEvent `json:"events"`
	NextAfter     int64             `json:"next_after"`
	HasMore       bool              `json:"has_more"`
	TotalFiltered int               `json:"total_filtered"`
}

// validEventTypes is the set of known event type strings for param validation.
var validEventTypes = map[string]store.EventType{
	string(store.EventTypeStateChange): store.EventTypeStateChange,
	string(store.EventTypeOutput):      store.EventTypeOutput,
	string(store.EventTypeFeedback):    store.EventTypeFeedback,
	string(store.EventTypeError):       store.EventTypeError,
	string(store.EventTypeSystem):      store.EventTypeSystem,
	string(store.EventTypeSpanStart):   store.EventTypeSpanStart,
	string(store.EventTypeSpanEnd):     store.EventTypeSpanEnd,
}

// GetEvents returns the event timeline for a task.
//
// Without query params, the full event list is returned as a JSON array
// (backward-compatible behaviour).
//
// With any of after, limit, or types present, a paginated envelope is returned:
//
//	{"events": [...], "next_after": <int64>, "has_more": <bool>, "total_filtered": <int>}
//
// Query params:
//   - after  – exclusive event ID cursor; only events with ID > after are returned (default 0)
//   - limit  – max events per page, 1–1000 (default 200)
//   - types  – comma-separated event types to include (default: all types)
func (h *Handler) GetEvents(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	q := r.URL.Query()
	isPaged := q.Has("after") || q.Has("limit") || q.Has("types")

	if !isPaged {
		// Backward-compatible: return the full list as a plain JSON array.
		events, err := h.store.GetEvents(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if events == nil {
			events = []store.TaskEvent{}
		}
		writeJSON(w, http.StatusOK, events)
		return
	}

	// Parse and validate pagination params.
	var afterID int64
	if v := q.Get("after"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			http.Error(w, "after must be a non-negative integer", http.StatusBadRequest)
			return
		}
		afterID = n
	}

	limit := 200 // default
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
			return
		}
		if n > 1000 {
			n = 1000
		}
		limit = n
	}

	var typeSet map[store.EventType]struct{}
	if v := q.Get("types"); v != "" {
		typeSet = make(map[store.EventType]struct{})
		for _, raw := range strings.Split(v, ",") {
			t := strings.TrimSpace(raw)
			if t == "" {
				continue
			}
			et, ok := validEventTypes[t]
			if !ok {
				http.Error(w, "unknown event type: "+t, http.StatusBadRequest)
				return
			}
			typeSet[et] = struct{}{}
		}
		if len(typeSet) == 0 {
			typeSet = nil
		}
	}

	page, err := h.store.GetEventsPage(r.Context(), id, afterID, limit, typeSet)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	events := page.Events
	if events == nil {
		events = []store.TaskEvent{}
	}
	writeJSON(w, http.StatusOK, eventsPageResponse{
		Events:        events,
		NextAfter:     page.NextAfter,
		HasMore:       page.HasMore,
		TotalFiltered: page.TotalFiltered,
	})
}

// ServeOutput serves a raw turn output file for a task.
func (h *Handler) ServeOutput(w http.ResponseWriter, r *http.Request, id uuid.UUID, filename string) {
	// Validate filename to prevent path traversal.
	if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	path := filepath.Join(h.store.OutputsDir(id), filename)
	fi, err := os.Stat(path)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Detect server-side truncation by reading the last 256 bytes of the file.
	// A truncation_notice sentinel is appended by SaveTurnOutput when the output
	// exceeds the WALLFACER_MAX_TURN_OUTPUT_BYTES budget.
	if fi.Size() > 0 {
		tailSize := int64(256)
		if fi.Size() < tailSize {
			tailSize = fi.Size()
		}
		if f, ferr := os.Open(path); ferr == nil {
			buf := make([]byte, tailSize)
			if _, rerr := f.ReadAt(buf, fi.Size()-tailSize); rerr == nil {
				if bytes.Contains(buf, []byte(`"truncation_notice"`)) {
					w.Header().Set("X-Wallfacer-Truncated", "true")
				}
			}
			f.Close()
		}
	}

	if strings.HasSuffix(filename, ".json") {
		w.Header().Set("Content-Type", "application/json")
	} else if strings.HasSuffix(filename, ".stderr.txt") {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	http.ServeFile(w, r, path)
}

// ForkTask creates a new backlog task branched from the source task's current
// worktree state. The source must be in done, waiting, or failed status and
// must have established worktrees. Responds 201 Created with the new task.
func (h *Handler) ForkTask(w http.ResponseWriter, r *http.Request, sourceID uuid.UUID) {
	var req struct {
		Prompt  string `json:"prompt"`
		Timeout int    `json:"timeout"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}

	source, err := h.store.GetTask(r.Context(), sourceID)
	if err != nil {
		http.Error(w, "source task not found", http.StatusNotFound)
		return
	}
	if source.Status != store.TaskStatusDone &&
		source.Status != store.TaskStatusWaiting &&
		source.Status != store.TaskStatusFailed {
		http.Error(w, "can only fork done, waiting, or failed tasks", http.StatusUnprocessableEntity)
		return
	}
	if len(source.WorktreePaths) == 0 {
		http.Error(w, "source task has no worktrees to fork from", http.StatusUnprocessableEntity)
		return
	}

	timeout := req.Timeout
	if timeout == 0 {
		timeout = source.Timeout
	}

	newTask, err := h.store.CreateForkedTask(r.Context(), sourceID, req.Prompt, timeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.runner.Fork(r.Context(), sourceID, newTask.ID); err != nil {
		// Clean up the orphaned task record on worktree setup failure.
		h.store.DeleteTask(r.Context(), newTask.ID, "fork setup failed")
		http.Error(w, fmt.Sprintf("fork setup failed: %v", err), http.StatusInternalServerError)
		return
	}

	h.store.InsertEvent(r.Context(), newTask.ID, store.EventTypeSystem, map[string]string{
		"message": fmt.Sprintf("forked from task %s", sourceID.String()[:8]),
	})
	h.runner.GenerateTitleBackground(newTask.ID, newTask.Prompt)

	// Re-fetch to include WorktreePaths and BranchName set by Fork.
	newTask, err = h.store.GetTask(r.Context(), newTask.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, newTask)
}

// GenerateMissingTitles triggers background title generation for untitled tasks.
func (h *Handler) GenerateMissingTitles(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			limit = n
		}
	}

	tasks, err := h.store.ListTasks(r.Context(), true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var untitled []store.Task
	for _, t := range tasks {
		if t.Title == "" {
			untitled = append(untitled, t)
		}
	}

	total := len(untitled)
	if limit > 0 && len(untitled) > limit {
		untitled = untitled[:limit]
	}

	taskIDs := make([]string, len(untitled))
	for i, t := range untitled {
		taskIDs[i] = t.ID.String()
		h.runner.GenerateTitleBackground(t.ID, t.Prompt)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"queued":              len(untitled),
		"total_without_title": total,
		"task_ids":            taskIDs,
	})
}

// defaultMaxConcurrentTasks is used when WALLFACER_MAX_PARALLEL is not set.
const defaultMaxConcurrentTasks = 5

// defaultMaxTestConcurrentTasks is used when WALLFACER_MAX_TEST_PARALLEL is not set.
const defaultMaxTestConcurrentTasks = 2

// maxConcurrentTasks reads the configured parallel task limit from the env file,
// falling back to defaultMaxConcurrentTasks.
func (h *Handler) maxConcurrentTasks() int {
	cfg, err := envconfig.Parse(h.envFile)
	if err != nil || cfg.MaxParallelTasks <= 0 {
		return defaultMaxConcurrentTasks
	}
	return cfg.MaxParallelTasks
}

// maxTestConcurrentTasks reads the configured parallel test-run limit from the
// env file, falling back to defaultMaxTestConcurrentTasks.
func (h *Handler) maxTestConcurrentTasks() int {
	cfg, err := envconfig.Parse(h.envFile)
	if err != nil || cfg.MaxTestParallelTasks <= 0 {
		return defaultMaxTestConcurrentTasks
	}
	return cfg.MaxTestParallelTasks
}

func (h *Handler) countRegularInProgress(_ context.Context) (int, error) {
	return h.store.CountRegularInProgress(), nil
}

func countRegularInProgress(tasks []store.Task) int {
	count := 0
	for i := range tasks {
		if tasks[i].Status == store.TaskStatusInProgress && !tasks[i].IsTestRun {
			count++
		}
	}
	return count
}

// checkConcurrencyAndUpdateStatus acquires promoteMu, enforces the regular
// in-progress concurrency limit, and calls store.UpdateTaskStatus. It writes
// the appropriate HTTP error response and returns false on any failure;
// on success it returns true with the mutex already released.
func (h *Handler) checkConcurrencyAndUpdateStatus(ctx context.Context, w http.ResponseWriter, id uuid.UUID, oldStatus, newStatus store.TaskStatus) bool {
	promoteMu.Lock()
	defer promoteMu.Unlock()

	regularInProgress, err := h.countRegularInProgress(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	if regularInProgress >= h.maxConcurrentTasks() {
		http.Error(w, fmt.Sprintf("max concurrent tasks (%d) reached", h.maxConcurrentTasks()), http.StatusConflict)
		return false
	}
	if err := h.store.UpdateTaskStatus(ctx, id, newStatus); err != nil {
		if errors.Is(err, store.ErrInvalidTransition) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return false
	}
	return true
}

// promoteMu serialises auto-promotion so two simultaneous state changes
// cannot both promote a task, exceeding the concurrency limit.
var promoteMu sync.Mutex

// StartAutoPromoter subscribes to store change notifications and automatically
// promotes backlog tasks to in_progress when there are fewer than
// maxConcurrentTasks running. A supplementary 60-second ticker fires
// periodically so that scheduled tasks are promoted even when no other
// state change occurs.
func (h *Handler) StartAutoPromoter(ctx context.Context) {
	subID, ch := h.store.Subscribe()
	ticker := time.NewTicker(60 * time.Second)
	go func() {
		defer h.store.Unsubscribe(subID)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				h.tryAutoPromote(ctx)
			case <-ticker.C:
				h.tryAutoPromote(ctx)
			}
		}
	}()
}

// taskReachable reports whether target is reachable from start by following
// DependsOn edges (i.e., target is a transitive dependency of start).
// Used to detect cycles before accepting a new dependency edge.
func taskReachable(taskList []store.Task, start, target uuid.UUID) bool {
	adj := make(map[uuid.UUID][]uuid.UUID, len(taskList))
	for _, t := range taskList {
		for _, s := range t.DependsOn {
			if id, err := uuid.Parse(s); err == nil {
				adj[t.ID] = append(adj[t.ID], id)
			}
		}
	}
	return taskReachableInAdj(adj, start, target)
}

// taskReachableInAdj reports whether target is reachable from start by following
// edges in the provided adjacency map (task → its dependencies).
// Used by BatchCreateTasks for full-graph cycle detection.
func taskReachableInAdj(adj map[uuid.UUID][]uuid.UUID, start, target uuid.UUID) bool {
	visited := make(map[uuid.UUID]bool)
	var dfs func(uuid.UUID) bool
	dfs = func(cur uuid.UUID) bool {
		if cur == target {
			return true
		}
		if visited[cur] {
			return false
		}
		visited[cur] = true
		for _, dep := range adj[cur] {
			if dfs(dep) {
				return true
			}
		}
		return false
	}
	return dfs(start)
}

// tryAutoPromote checks if there is capacity to run more tasks and promotes
// the highest-priority (lowest position) backlog task if so.
// When autopilot is disabled, no promotion happens.
//
// Concurrency design mirrors tryAutoTest's two-phase approach:
//
// Phase 1 (no lock): call store.ListTasks, compute the regular in-progress
// count, and find the best backlog candidate. AreDependenciesSatisfied may do
// disk I/O here; we must not hold promoteMu during these potentially slow
// operations so that a concurrent tryAutoPromote call (or tryAutoTest) can
// proceed in parallel.
//
// Phase 2 (under promoteMu): re-count to pick up any state changes that
// happened during Phase 1, re-check capacity, then promote.
func (h *Handler) tryAutoPromote(ctx context.Context) {
	if !h.AutopilotEnabled() {
		return
	}

	// Phase 1 (no lock): build candidate and count without holding promoteMu.
	regularInProgress := h.store.CountRegularInProgress()
	maxTasks := h.maxConcurrentTasks()

	if regularInProgress >= maxTasks {
		return
	}

	backlogTasks, err := h.store.ListTasksByStatus(ctx, store.TaskStatusBacklog)
	if err != nil {
		return
	}

	var bestBacklog *store.Task
	for i := range backlogTasks {
		t := &backlogTasks[i]
		if t.Kind == store.TaskKindIdeaAgent {
			continue
		}
		// Skip tasks that have a future scheduled start time.
		if t.ScheduledAt != nil && time.Now().Before(*t.ScheduledAt) {
			continue
		}
		satisfied, err := h.store.AreDependenciesSatisfied(ctx, t.ID)
		if err != nil || !satisfied {
			continue // skip: dependencies not yet done
		}
		if bestBacklog == nil || t.Position < bestBacklog.Position {
			cp := *t
			bestBacklog = &cp
		}
	}

	if bestBacklog == nil {
		return
	}

	if h.testPhase1Done != nil {
		h.testPhase1Done()
	}

	// Phase 2 (under promoteMu): re-verify capacity with a fresh count and promote.
	promoteMu.Lock()
	defer promoteMu.Unlock()

	// Re-read in-progress count; state may have changed during Phase 1 I/O.
	if h.store.CountRegularInProgress() >= maxTasks {
		return
	}

	// Abort promotion when the container runtime is known-unavailable.
	// Without this guard, slot openings caused by failures would trigger
	// back-to-back promotions that all immediately fail, cascading across
	// every backlog task.
	if !h.runner.ContainerCircuitAllow() {
		logger.Handler.Warn("auto-promote skipped: container circuit breaker open")
		return
	}

	logger.Handler.Info("auto-promoting backlog task",
		"task", bestBacklog.ID, "position", bestBacklog.Position,
		"in_progress", regularInProgress)

	if err := h.store.UpdateTaskStatus(ctx, bestBacklog.ID, store.TaskStatusInProgress); err != nil {
		logger.Handler.Error("auto-promote status update", "task", bestBacklog.ID, "error", err)
		return
	}
	h.store.InsertEvent(ctx, bestBacklog.ID, store.EventTypeStateChange, map[string]string{
		"from":    string(store.TaskStatusBacklog),
		"to":      string(store.TaskStatusInProgress),
		"trigger": store.TriggerAutoPromote,
	})

	sessionID := ""
	if !bestBacklog.FreshStart && bestBacklog.SessionID != nil {
		sessionID = *bestBacklog.SessionID
	}
	h.runner.RunBackground(bestBacklog.ID, bestBacklog.Prompt, sessionID, false)
}

// waitingSyncInterval is how often the watcher polls for waiting tasks that
// have fallen behind the default branch.
const waitingSyncInterval = 30 * time.Second

// StartWaitingSyncWatcher starts a background goroutine that periodically
// checks all waiting tasks and automatically syncs any whose worktrees have
// fallen behind the default branch.
func (h *Handler) StartWaitingSyncWatcher(ctx context.Context) {
	ticker := time.NewTicker(waitingSyncInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.checkAndSyncWaitingTasks(ctx)
			}
		}
	}()
}

// checkAndSyncWaitingTasks inspects every waiting task that has worktrees. If
// any worktree is behind the default branch it automatically transitions the
// task to in_progress and triggers SyncWorktrees, exactly as if the user had
// clicked the "Sync" button.
func (h *Handler) checkAndSyncWaitingTasks(ctx context.Context) {
	tasks, err := h.store.ListTasksByStatus(ctx, store.TaskStatusWaiting)
	if err != nil {
		return
	}
	maxTasks := h.maxConcurrentTasks()

	for i := range tasks {
		t := &tasks[i]
		if len(t.WorktreePaths) == 0 {
			continue
		}

		behind := false
		for repoPath, worktreePath := range t.WorktreePaths {
			n, err := gitutil.CommitsBehind(repoPath, worktreePath)
			if err != nil {
				logger.Handler.Warn("auto-sync: check commits behind",
					"task", t.ID, "repo", repoPath, "error", err)
				continue
			}
			if n > 0 {
				behind = true
				break
			}
		}

		if !behind {
			continue
		}

		logger.Handler.Info("auto-sync: waiting task behind default branch, syncing",
			"task", t.ID)

		promoteMu.Lock()
		regularInProgress := h.store.CountRegularInProgress()

		if regularInProgress >= maxTasks {
			promoteMu.Unlock()
			logger.Handler.Info("auto-sync: regular in-progress limit reached, deferring sync",
				"task", t.ID, "count", regularInProgress, "max", maxTasks)
			continue
		}

		if err := h.store.UpdateTaskStatus(ctx, t.ID, store.TaskStatusInProgress); err != nil {
			promoteMu.Unlock()
			logger.Handler.Error("auto-sync: update task status", "task", t.ID, "error", err)
			continue
		}
		regularInProgress++
		h.store.InsertEvent(ctx, t.ID, store.EventTypeStateChange, map[string]string{
			"from":    string(store.TaskStatusWaiting),
			"to":      string(store.TaskStatusInProgress),
			"trigger": store.TriggerSync,
		})
		h.store.InsertEvent(ctx, t.ID, store.EventTypeSystem, map[string]string{
			"result": "Auto-syncing: worktree is behind the default branch.",
		})

		sessionID := ""
		if t.SessionID != nil {
			sessionID = *t.SessionID
		}
		h.diffCache.invalidate(t.ID)
		taskID := t.ID
		promoteMu.Unlock()
		h.runner.SyncWorktreesBackground(taskID, sessionID, store.TaskStatusWaiting, func() {
			h.diffCache.invalidate(taskID)
		})
	}
}

// autoTestInterval is how often the auto-tester polls for eligible waiting tasks
// in addition to reacting to store change notifications.
const autoTestInterval = 30 * time.Second

// StartAutoTester subscribes to store change notifications and automatically
// triggers the test agent for waiting tasks that are untested and not behind
// the default branch tip.
func (h *Handler) StartAutoTester(ctx context.Context) {
	subID, ch := h.store.Subscribe()
	ticker := time.NewTicker(autoTestInterval)
	go func() {
		defer h.store.Unsubscribe(subID)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				h.tryAutoTest(ctx)
			case <-ticker.C:
				h.tryAutoTest(ctx)
			}
		}
	}()
}

// autoTestCandidate holds an eligible waiting task and its pre-built test prompt.
type autoTestCandidate struct {
	task       store.Task
	testPrompt string
}

// tryAutoTest scans all waiting tasks and triggers the test agent for any
// that are untested (LastTestResult == "") and whose worktrees are not behind
// the default branch. Does nothing when auto-test is disabled.
//
// Concurrency limit: test runs have their own independent limit controlled by
// maxTestConcurrentTasks (WALLFACER_MAX_TEST_PARALLEL). Only IsTestRun
// in-progress tasks count against this limit; regular tasks are unaffected.
// The promoteMu mutex is still shared with tryAutoPromote to prevent races on
// the same task.
func (h *Handler) tryAutoTest(ctx context.Context) {
	if !h.AutotestEnabled() {
		return
	}

	waitingTasks, err := h.store.ListTasksByStatus(ctx, store.TaskStatusWaiting)
	if err != nil {
		return
	}

	// Phase 1 (no lock): build the list of eligible candidates.
	// Git I/O (CommitsBehind) happens here so we don't hold promoteMu
	// during potentially slow filesystem operations.
	var candidates []autoTestCandidate
	for i := range waitingTasks {
		t := &waitingTasks[i]
		// Skip tasks that already have a test result or are currently being tested.
		if t.LastTestResult != "" || t.IsTestRun {
			continue
		}

		// Skip tasks with no worktrees (nothing to test yet).
		if len(t.WorktreePaths) == 0 {
			continue
		}

		// Only trigger if the worktree is up to date with the default branch.
		behind := false
		for repoPath, worktreePath := range t.WorktreePaths {
			n, err := gitutil.CommitsBehind(repoPath, worktreePath)
			if err != nil {
				logger.Handler.Warn("auto-test: check commits behind",
					"task", t.ID, "repo", repoPath, "error", err)
				behind = true // treat errors conservatively
				break
			}
			if n > 0 {
				behind = true
				break
			}
		}
		if behind {
			continue
		}

		implResult := ""
		if t.Result != nil {
			implResult = *t.Result
		}
		diff := generateWorktreeDiff(t.WorktreePaths)
		testPrompt := buildTestPrompt(t.Prompt, "", implResult, diff)
		candidates = append(candidates, autoTestCandidate{task: *t, testPrompt: testPrompt})
	}

	if len(candidates) == 0 {
		return
	}

	// Phase 2 (under promoteMu): enforce the concurrency limit and trigger.
	// Sharing promoteMu with tryAutoPromote prevents the two from racing to
	// exceed maxConcurrentTasks simultaneously.
	promoteMu.Lock()
	defer promoteMu.Unlock()

	// Re-read for a fresh snapshot; state may have changed during the git checks above.
	freshWaiting, err := h.store.ListTasksByStatus(ctx, store.TaskStatusWaiting)
	if err != nil {
		return
	}
	freshByID := make(map[uuid.UUID]store.Task, len(freshWaiting))
	for _, t := range freshWaiting {
		freshByID[t.ID] = t
	}
	freshInProgress, err := h.store.ListTasksByStatus(ctx, store.TaskStatusInProgress)
	if err != nil {
		return
	}
	testInProgress := 0
	for _, t := range freshInProgress {
		if t.IsTestRun {
			testInProgress++
		}
	}

	maxTestTasks := h.maxTestConcurrentTasks()

	for _, c := range candidates {
		if testInProgress >= maxTestTasks {
			logger.Handler.Info("auto-test: test concurrency limit reached, deferring remaining tests",
				"limit", maxTestTasks)
			break
		}

		// Re-verify eligibility using the fresh snapshot.
		ft, ok := freshByID[c.task.ID]
		if !ok || ft.Status != store.TaskStatusWaiting || ft.LastTestResult != "" || ft.IsTestRun {
			continue
		}

		logger.Handler.Info("auto-test: triggering test agent for waiting task", "task", c.task.ID)

		if err := h.store.UpdateTaskTestRun(ctx, c.task.ID, true, ""); err != nil {
			logger.Handler.Error("auto-test: update test run flag", "task", c.task.ID, "error", err)
			continue
		}
		if err := h.store.UpdateTaskStatus(ctx, c.task.ID, store.TaskStatusInProgress); err != nil {
			logger.Handler.Error("auto-test: update task status", "task", c.task.ID, "error", err)
			continue
		}
		h.store.InsertEvent(ctx, c.task.ID, store.EventTypeStateChange, map[string]string{
			"from":    string(store.TaskStatusWaiting),
			"to":      string(store.TaskStatusInProgress),
			"trigger": store.TriggerAutoTest,
		})
		h.store.InsertEvent(ctx, c.task.ID, store.EventTypeSystem, map[string]string{
			"result": "Auto-test: triggering test verification agent.",
		})

		h.runner.RunBackground(c.task.ID, c.testPrompt, "", false)
		testInProgress++
	}
}

// autoSubmitInterval is how often the auto-submitter polls for eligible waiting tasks
// in addition to reacting to store change notifications.
const autoSubmitInterval = 30 * time.Second

// StartAutoSubmitter subscribes to store change notifications and automatically
// moves waiting tasks to done when they are verified (LastTestResult == "pass"),
// not behind the default branch tip, and have no unresolved worktree conflicts.
func (h *Handler) StartAutoSubmitter(ctx context.Context) {
	subID, ch := h.store.Subscribe()
	ticker := time.NewTicker(autoSubmitInterval)
	go func() {
		defer h.store.Unsubscribe(subID)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				h.tryAutoSubmit(ctx)
			case <-ticker.C:
				h.tryAutoSubmit(ctx)
			}
		}
	}()
}

// tryAutoSubmit scans all waiting tasks and moves any that are verified
// (LastTestResult == "pass"), not behind the default branch, and free of
// worktree conflicts directly to done (via the commit pipeline if a session
// exists). Does nothing when auto-submit is disabled.
func (h *Handler) tryAutoSubmit(ctx context.Context) {
	if !h.AutosubmitEnabled() {
		return
	}

	tasks, err := h.store.ListTasks(ctx, false)
	if err != nil {
		return
	}

	for i := range tasks {
		t := &tasks[i]
		if t.Status != store.TaskStatusWaiting {
			continue
		}
		// Determine eligibility:
		// (a) Passed verification ("pass").
		// (b) Naturally completed (stop_reason="end_turn") and not yet tested,
		//     but only when auto-test is off — otherwise let auto-test run first.
		// Tasks that failed testing are never auto-submitted.
		tested := t.LastTestResult == "pass"
		naturallyComplete := t.StopReason != nil && *t.StopReason == "end_turn" && t.LastTestResult == "" && !h.AutotestEnabled()
		if !tested && !naturallyComplete {
			continue
		}
		// Skip while the test agent is still running.
		if t.IsTestRun {
			continue
		}

		// Check that all worktrees are up to date and conflict-free.
		skip := false
		for repoPath, worktreePath := range t.WorktreePaths {
			n, err := gitutil.CommitsBehind(repoPath, worktreePath)
			if err != nil {
				logger.Handler.Warn("auto-submit: check commits behind",
					"task", t.ID, "repo", repoPath, "error", err)
				skip = true
				break
			}
			if n > 0 {
				skip = true
				break
			}
			hasConflict, err := gitutil.HasConflicts(worktreePath)
			if err != nil {
				logger.Handler.Warn("auto-submit: check conflicts",
					"task", t.ID, "worktree", worktreePath, "error", err)
				skip = true
				break
			}
			if hasConflict {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		logger.Handler.Info("auto-submit: completing verified waiting task", "task", t.ID)
		autoSubmitMsg := "Auto-submit: task verified with passing tests, up to date, and no conflicts."
		if naturallyComplete {
			autoSubmitMsg = "Auto-submit: task naturally completed, up to date, and no conflicts."
		}
		h.store.InsertEvent(ctx, t.ID, store.EventTypeSystem, map[string]string{
			"result": autoSubmitMsg,
		})

		if t.SessionID != nil && *t.SessionID != "" {
			if err := h.store.UpdateTaskStatus(ctx, t.ID, store.TaskStatusCommitting); err != nil {
				logger.Handler.Error("auto-submit: update task status", "task", t.ID, "error", err)
				continue
			}
			h.store.InsertEvent(ctx, t.ID, store.EventTypeStateChange, map[string]string{
				"from":    string(store.TaskStatusWaiting),
				"to":      string(store.TaskStatusCommitting),
				"trigger": store.TriggerAutoSubmit,
			})
			sessionID := *t.SessionID
			taskID := t.ID
			go func() {
				bgCtx := context.Background()
				if err := h.runner.Commit(taskID, sessionID); err != nil {
					h.store.UpdateTaskStatus(bgCtx, taskID, store.TaskStatusFailed)
					h.store.InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]string{
						"error": "auto-submit: commit failed: " + err.Error(),
					})
					h.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
						"from":    string(store.TaskStatusCommitting),
						"to":      string(store.TaskStatusFailed),
						"trigger": store.TriggerAutoSubmit,
					})
					return
				}
				h.store.UpdateTaskStatus(bgCtx, taskID, store.TaskStatusDone)
				h.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
					"from":    string(store.TaskStatusCommitting),
					"to":      string(store.TaskStatusDone),
					"trigger": store.TriggerAutoSubmit,
				})
			}()
		} else {
			// No session — move directly to done (bypasses state machine
			// since waiting→done is deliberately blocked to protect the commit pipeline).
			if err := h.store.ForceUpdateTaskStatus(ctx, t.ID, store.TaskStatusDone); err != nil {
				logger.Handler.Error("auto-submit: update task status to done", "task", t.ID, "error", err)
				continue
			}
			h.store.InsertEvent(ctx, t.ID, store.EventTypeStateChange, map[string]string{
				"from":    string(store.TaskStatusWaiting),
				"to":      string(store.TaskStatusDone),
				"trigger": store.TriggerAutoSubmit,
			})
		}
	}
}
