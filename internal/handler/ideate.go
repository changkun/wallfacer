package handler

import (
	"context"
	"net/http"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
)

// ideaAgentDefaultTimeout is the default timeout (minutes) for idea-agent task cards.
const ideaAgentDefaultTimeout = 60

// StartIdeationWatcher subscribes to store change notifications and, whenever
// an idea-agent task transitions to done, creates a new idea-agent task card
// in the backlog so that autopilot can keep the brainstorm cycle running.
func (h *Handler) StartIdeationWatcher(ctx context.Context) {
	subID, ch := h.store.Subscribe()
	go func() {
		defer h.store.Unsubscribe(subID)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				h.maybeScheduleNextIdeation(ctx)
			}
		}
	}()
}

// maybeScheduleNextIdeation checks whether any idea-agent task just completed.
// If ideation is enabled and no idea-agent task is backlogged or in progress,
// it enqueues a new idea-agent card so the brainstorm cycle continues.
func (h *Handler) maybeScheduleNextIdeation(ctx context.Context) {
	if !h.IdeationEnabled() {
		return
	}

	tasks, err := h.store.ListTasks(ctx, false)
	if err != nil {
		return
	}

	for _, t := range tasks {
		if t.Kind == store.TaskKindIdeaAgent {
			switch t.Status {
			case store.TaskStatusBacklog, store.TaskStatusInProgress:
				// Already queued or running — nothing to do.
				return
			}
		}
	}

	// No active idea-agent task: create one to keep the cycle going.
	h.createIdeaAgentTask(ctx)
}

// createIdeaAgentTask creates a new idea-agent task card in the backlog.
func (h *Handler) createIdeaAgentTask(ctx context.Context) {
	task, err := h.store.CreateTask(ctx, "", ideaAgentDefaultTimeout, false, "", store.TaskKindIdeaAgent)
	if err != nil {
		logger.Handler.Warn("ideation: create idea-agent task", "error", err)
		return
	}
	h.store.InsertEvent(ctx, task.ID, store.EventTypeStateChange, map[string]string{
		"to": string(store.TaskStatusBacklog),
	})
	logger.Handler.Info("ideation: queued new idea-agent task", "task", task.ID)
}

// TriggerIdeation handles POST /api/ideate.
// Creates an idea-agent task card in the backlog immediately.
func (h *Handler) TriggerIdeation(w http.ResponseWriter, r *http.Request) {
	h.createIdeaAgentTask(r.Context())
	writeJSON(w, http.StatusAccepted, map[string]any{
		"queued": true,
	})
}

// CancelIdeation handles DELETE /api/ideate.
// Cancels the currently running or backlogged idea-agent task (if any).
func (h *Handler) CancelIdeation(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.store.ListTasks(r.Context(), false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cancelled := false
	for _, t := range tasks {
		if t.Kind != store.TaskKindIdeaAgent {
			continue
		}
		switch t.Status {
		case store.TaskStatusInProgress:
			h.runner.KillContainer(t.ID)
			// Status will be set to cancelled by the cancel handler's
			// UpdateTaskStatus call; just kill the container here.
			h.store.UpdateTaskStatus(r.Context(), t.ID, store.TaskStatusCancelled)
			h.store.InsertEvent(r.Context(), t.ID, store.EventTypeStateChange, map[string]string{
				"from": string(store.TaskStatusInProgress),
				"to":   string(store.TaskStatusCancelled),
			})
			cancelled = true
		case store.TaskStatusBacklog:
			h.store.UpdateTaskStatus(r.Context(), t.ID, store.TaskStatusCancelled)
			h.store.InsertEvent(r.Context(), t.ID, store.EventTypeStateChange, map[string]string{
				"from": string(store.TaskStatusBacklog),
				"to":   string(store.TaskStatusCancelled),
			})
			cancelled = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": cancelled})
}

// GetIdeationStatus handles GET /api/ideate.
// Returns the current brainstorm enabled/running state derived from the task list.
func (h *Handler) GetIdeationStatus(w http.ResponseWriter, r *http.Request) {
	tasks, _ := h.store.ListTasks(r.Context(), false)
	running := false
	for _, t := range tasks {
		if t.Kind == store.TaskKindIdeaAgent && t.Status == store.TaskStatusInProgress {
			running = true
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": h.IdeationEnabled(),
		"running": running,
	})
}
