package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
		Prompt  string `json:"prompt"`
		Timeout int    `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}

	task, err := h.store.CreateTask(r.Context(), req.Prompt, req.Timeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.store.InsertEvent(r.Context(), task.ID, "state_change", map[string]string{
		"to": "backlog",
	})

	go h.runner.GenerateTitle(task.ID, task.Prompt)

	writeJSON(w, http.StatusCreated, task)
}

// UpdateTask handles PATCH requests: status transitions, position, prompt, etc.
func (h *Handler) UpdateTask(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var req struct {
		Status     *string `json:"status"`
		Position   *int    `json:"position"`
		Prompt     *string `json:"prompt"`
		Timeout    *int    `json:"timeout"`
		FreshStart *bool   `json:"fresh_start"`
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

	// Allow editing prompt, timeout, and fresh_start for backlog tasks.
	if task.Status == "backlog" && (req.Prompt != nil || req.Timeout != nil || req.FreshStart != nil) {
		if err := h.store.UpdateTaskBacklog(r.Context(), id, req.Prompt, req.Timeout, req.FreshStart); err != nil {
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

	if req.Status != nil {
		oldStatus := task.Status
		newStatus := *req.Status

		// Handle retry: done/failed/waiting/cancelled → backlog
		if newStatus == "backlog" && (oldStatus == "done" || oldStatus == "failed" || oldStatus == "cancelled" || oldStatus == "waiting") {
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
			h.store.InsertEvent(r.Context(), id, "state_change", map[string]string{
				"from": oldStatus,
				"to":   "backlog",
			})
		} else {
			if err := h.store.UpdateTaskStatus(r.Context(), id, newStatus); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			h.store.InsertEvent(r.Context(), id, "state_change", map[string]string{
				"from": oldStatus,
				"to":   newStatus,
			})

			if newStatus == "in_progress" && oldStatus == "backlog" {
				sessionID := ""
				if !task.FreshStart && task.SessionID != nil {
					sessionID = *task.SessionID
				}
				go h.runner.Run(id, task.Prompt, sessionID, false)
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
		go h.runner.GenerateTitle(t.ID, t.Prompt)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"queued":              len(untitled),
		"total_without_title": total,
		"task_ids":            taskIDs,
	})
}
