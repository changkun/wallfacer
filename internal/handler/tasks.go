package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/sandbox"
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
		category, ok := store.ParseFailureCategory(q)
		if !ok {
			http.Error(w, "invalid failure_category", http.StatusBadRequest)
			return
		}
		tasks = filterByFailureCategory(tasks, category)
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
		Prompt             string                  `json:"prompt"`
		Timeout            int                     `json:"timeout"`
		MountWorktrees     bool                    `json:"mount_worktrees"`
		Sandbox            sandbox.Type            `json:"sandbox"`
		SandboxByActivity  map[string]sandbox.Type `json:"sandbox_by_activity"`
		Kind               store.TaskKind          `json:"kind"`
		Tags               []string                `json:"tags"`
		MaxCostUSD         float64                 `json:"max_cost_usd"`
		MaxInputTokens     int                     `json:"max_input_tokens"`
		Model              string                  `json:"model"`
		ScheduledAt        *time.Time              `json:"scheduled_at,omitempty"`
		CustomPassPatterns []string                `json:"custom_pass_patterns,omitempty"`
		CustomFailPatterns []string                `json:"custom_fail_patterns,omitempty"`
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
	if err := validateCustomPatterns(req.CustomPassPatterns, req.CustomFailPatterns); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	task, err := h.store.CreateTaskWithOptions(r.Context(), store.TaskCreateOptions{
		Prompt:             req.Prompt,
		Timeout:            req.Timeout,
		Tags:               req.Tags,
		MountWorktrees:     req.MountWorktrees,
		Kind:               req.Kind,
		Sandbox:            req.Sandbox,
		SandboxByActivity:  req.SandboxByActivity,
		MaxCostUSD:         req.MaxCostUSD,
		MaxInputTokens:     req.MaxInputTokens,
		ModelOverride:      req.Model,
		ScheduledAt:        req.ScheduledAt,
		CustomPassPatterns: req.CustomPassPatterns,
		CustomFailPatterns: req.CustomFailPatterns,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.store.InsertEvent(r.Context(), task.ID, store.EventTypeStateChange,
		store.NewStateChangeData("", store.TaskStatusBacklog, store.TriggerUser, nil))

	if task.Kind != store.TaskKindIdeaAgent {
		h.runner.GenerateTitleBackground(task.ID, task.Prompt)
	}

	writeJSON(w, http.StatusCreated, task)
}

// batchTaskInput describes a single task in a BatchCreateTasks request.
type batchTaskInput struct {
	Ref               string                  `json:"ref"`
	Prompt            string                  `json:"prompt"`
	Timeout           int                     `json:"timeout"`
	Tags              []string                `json:"tags"`
	Sandbox           sandbox.Type            `json:"sandbox"`
	SandboxByActivity map[string]sandbox.Type `json:"sandbox_by_activity"`
	Kind              store.TaskKind          `json:"kind"`
	MountWorktrees    bool                    `json:"mount_worktrees"`
	DependsOnRefs     []string                `json:"depends_on_refs"`
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

		h.store.InsertEvent(r.Context(), task.ID, store.EventTypeStateChange,
			store.NewStateChangeData("", store.TaskStatusBacklog, store.TriggerUser, nil))

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
		Status            *store.TaskStatus        `json:"status"`
		Position          *int                     `json:"position"`
		Prompt            *string                  `json:"prompt"`
		Timeout           *int                     `json:"timeout"`
		FreshStart        *bool                    `json:"fresh_start"`
		MountWorktrees    *bool                    `json:"mount_worktrees"`
		Sandbox           *sandbox.Type            `json:"sandbox"`
		SandboxByActivity *map[string]sandbox.Type `json:"sandbox_by_activity"`
		DependsOn         *[]string                `json:"depends_on"`
		Tags              *[]string                `json:"tags"`
		MaxCostUSD        *float64                 `json:"max_cost_usd"`
		MaxInputTokens    *int                     `json:"max_input_tokens"`
		// Model sets the per-task model override; empty string clears it.
		Model *string `json:"model"`
		// ScheduledAt uses json.RawMessage so we can distinguish "absent" (nil)
		// from explicitly-sent "null" (clear the schedule) or a valid time (set it).
		ScheduledAt        json.RawMessage `json:"scheduled_at"`
		CustomPassPatterns []string        `json:"custom_pass_patterns,omitempty"`
		CustomFailPatterns []string        `json:"custom_fail_patterns,omitempty"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	// Allow editing prompt, timeout, fresh_start, mount_worktrees, sandbox, model, budget, and custom patterns for backlog tasks.
	if task.Status == store.TaskStatusBacklog && (req.Prompt != nil || req.Timeout != nil || req.FreshStart != nil || req.MountWorktrees != nil || req.Sandbox != nil || req.SandboxByActivity != nil || req.MaxCostUSD != nil || req.MaxInputTokens != nil || req.Model != nil || req.CustomPassPatterns != nil || req.CustomFailPatterns != nil) {
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
		if req.CustomPassPatterns != nil || req.CustomFailPatterns != nil {
			passP := req.CustomPassPatterns
			failP := req.CustomFailPatterns
			if passP == nil {
				passP = task.CustomPassPatterns
			}
			if failP == nil {
				failP = task.CustomFailPatterns
			}
			if err := h.store.UpdateTaskCustomPatterns(r.Context(), id, passP, failP); err != nil {
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

	if req.Tags != nil {
		if err := h.store.UpdateTaskTags(r.Context(), id, *req.Tags); err != nil {
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
			h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange,
				store.NewStateChangeData(oldStatus, store.TaskStatusBacklog, store.TriggerUser, nil))
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
				h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange,
					store.NewStateChangeData(oldStatus, newStatus, store.TriggerUser, nil))
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
				h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange,
					store.NewStateChangeData(oldStatus, newStatus, store.TriggerUser, nil))
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
		h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange,
			store.NewStateChangeData(oldStatus, newStatus, store.TriggerUser, nil))
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
