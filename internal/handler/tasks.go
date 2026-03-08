package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// ListTasks returns all tasks, optionally including archived ones.
func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	tasks, err := h.store.ListTasks(r.Context(), includeArchived)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tasks == nil {
		tasks = []store.Task{}
	}
	writeJSON(w, http.StatusOK, tasks)
}

// CreateTask creates a new task in backlog status.
func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt         string           `json:"prompt"`
		Timeout        int              `json:"timeout"`
		MountWorktrees bool             `json:"mount_worktrees"`
		Model          string           `json:"model"`
		Kind           store.TaskKind   `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Prompt) == "" && req.Kind != store.TaskKindIdeaAgent {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}

	task, err := h.store.CreateTask(r.Context(), req.Prompt, req.Timeout, req.MountWorktrees, req.Model, req.Kind)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.store.InsertEvent(r.Context(), task.ID, store.EventTypeStateChange, map[string]string{
		"to": string(store.TaskStatusBacklog),
	})

	if task.Kind != store.TaskKindIdeaAgent {
		h.runner.GenerateTitleBackground(task.ID, task.Prompt)
	}

	writeJSON(w, http.StatusCreated, task)
}

// UpdateTask handles PATCH requests: status transitions, position, prompt, etc.
func (h *Handler) UpdateTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var req struct {
		Status         *store.TaskStatus `json:"status"`
		Position       *int              `json:"position"`
		Prompt         *string           `json:"prompt"`
		Timeout        *int              `json:"timeout"`
		FreshStart     *bool             `json:"fresh_start"`
		MountWorktrees *bool             `json:"mount_worktrees"`
		Model          *string           `json:"model"`
		DependsOn      *[]string         `json:"depends_on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	// Allow editing prompt, timeout, fresh_start, mount_worktrees, and model for backlog tasks.
	if task.Status == store.TaskStatusBacklog && (req.Prompt != nil || req.Timeout != nil || req.FreshStart != nil || req.MountWorktrees != nil || req.Model != nil) {
		if err := h.store.UpdateTaskBacklog(r.Context(), id, req.Prompt, req.Timeout, req.FreshStart, req.MountWorktrees, req.Model); err != nil {
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
				"from": string(oldStatus),
				"to":   string(store.TaskStatusBacklog),
			})
			h.diffCache.invalidate(id)
		} else {
			if err := h.store.UpdateTaskStatus(r.Context(), id, newStatus); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, map[string]string{
				"from": string(oldStatus),
				"to":   string(newStatus),
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
	}

	updated, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// DeleteTask removes a task and its data.
func (h *Handler) DeleteTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if task, err := h.store.GetTask(r.Context(), id); err == nil && len(task.WorktreePaths) > 0 {
		h.runner.CleanupWorktrees(id, task.WorktreePaths, task.BranchName)
	}
	if err := h.store.DeleteTask(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetEvents returns the event timeline for a task.
func (h *Handler) GetEvents(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	events, err := h.store.GetEvents(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if events == nil {
		events = []store.TaskEvent{}
	}
	writeJSON(w, http.StatusOK, events)
}

// ServeOutput serves a raw turn output file for a task.
func (h *Handler) ServeOutput(w http.ResponseWriter, r *http.Request, id uuid.UUID, filename string) {
	// Validate filename to prevent path traversal.
	if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	path := filepath.Join(h.store.OutputsDir(id), filename)
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	if strings.HasSuffix(filename, ".json") {
		w.Header().Set("Content-Type", "application/json")
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	http.ServeFile(w, r, path)
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

// maxConcurrentTasks reads the configured parallel task limit from the env file,
// falling back to defaultMaxConcurrentTasks.
func (h *Handler) maxConcurrentTasks() int {
	cfg, err := envconfig.Parse(h.envFile)
	if err != nil || cfg.MaxParallelTasks <= 0 {
		return defaultMaxConcurrentTasks
	}
	return cfg.MaxParallelTasks
}

// promoteMu serialises auto-promotion so two simultaneous state changes
// cannot both promote a task, exceeding the concurrency limit.
var promoteMu sync.Mutex

// StartAutoPromoter subscribes to store change notifications and automatically
// promotes backlog tasks to in_progress when there are fewer than
// maxConcurrentTasks running.
func (h *Handler) StartAutoPromoter(ctx context.Context) {
	subID, ch := h.store.Subscribe()
	go func() {
		defer h.store.Unsubscribe(subID)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
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
func (h *Handler) tryAutoPromote(ctx context.Context) {
	if !h.AutopilotEnabled() {
		return
	}

	promoteMu.Lock()
	defer promoteMu.Unlock()

	tasks, err := h.store.ListTasks(ctx, false)
	if err != nil {
		return
	}

	inProgressCount := 0
	var bestBacklog *store.Task
	for i := range tasks {
		t := &tasks[i]
		if t.Status == store.TaskStatusInProgress {
			inProgressCount++
		}
		if t.Status == store.TaskStatusBacklog && t.Kind != store.TaskKindIdeaAgent {
			satisfied, err := h.store.AreDependenciesSatisfied(ctx, t.ID)
			if err != nil || !satisfied {
				continue // skip: dependencies not yet done
			}
			if bestBacklog == nil || t.Position < bestBacklog.Position {
				cp := *t
				bestBacklog = &cp
			}
		}
	}

	maxTasks := h.maxConcurrentTasks()
	if inProgressCount >= maxTasks || bestBacklog == nil {
		return
	}

	logger.Handler.Info("auto-promoting backlog task",
		"task", bestBacklog.ID, "position", bestBacklog.Position,
		"in_progress", inProgressCount)

	if err := h.store.UpdateTaskStatus(ctx, bestBacklog.ID, store.TaskStatusInProgress); err != nil {
		logger.Handler.Error("auto-promote status update", "task", bestBacklog.ID, "error", err)
		return
	}
	h.store.InsertEvent(ctx, bestBacklog.ID, store.EventTypeStateChange, map[string]string{
		"from": string(store.TaskStatusBacklog),
		"to":   string(store.TaskStatusInProgress),
	})

	sessionID := ""
	if !bestBacklog.FreshStart && bestBacklog.SessionID != nil {
		sessionID = *bestBacklog.SessionID
	}
	h.runner.RunBackground(bestBacklog.ID, bestBacklog.Prompt, sessionID, false)
}
