package handler

import (
	"context"
	"net/http"
	"time"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
)

// ideaAgentDefaultTimeout is the default timeout (minutes) for idea-agent task cards.
const ideaAgentDefaultTimeout = 60

// StartIdeationWatcher subscribes to store change notifications and, whenever
// an idea-agent task transitions out of active states, schedules the next
// brainstorm run according to the configured interval.
// It also kicks off an initial schedule immediately so that ideation begins
// as soon as the server starts (when enabled by default).
func (h *Handler) StartIdeationWatcher(ctx context.Context) {
	// Kick off the first brainstorm run (or timer) right away.
	go h.maybeScheduleNextIdeation(ctx)

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

// maybeScheduleNextIdeation checks whether ideation is enabled and no
// idea-agent task is already active (backlogged or in progress). If so, it
// schedules the next brainstorm run — either immediately (interval == 0) or
// via a delayed timer.
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

	// No active idea-agent task: schedule the next one.
	h.scheduleIdeation(ctx)
}

// scheduleIdeation enqueues the next brainstorm run. If the interval is zero
// it creates the task immediately; otherwise it arms a one-shot timer.
// If a timer is already pending it is left in place (avoid double-scheduling).
func (h *Handler) scheduleIdeation(ctx context.Context) {
	h.ideationMu.Lock()

	// If a timer is already waiting, do not create a second one.
	if h.ideationTimer != nil {
		h.ideationMu.Unlock()
		return
	}

	interval := h.ideationInterval

	if interval == 0 {
		h.ideationMu.Unlock()
		h.createIdeaAgentTask(ctx)
		return
	}

	nextRun := time.Now().Add(interval)
	h.ideationNextRun = nextRun
	h.ideationTimer = time.AfterFunc(interval, func() {
		h.ideationMu.Lock()
		enabled := h.ideationEnabled
		h.ideationTimer = nil
		h.ideationNextRun = time.Time{}
		h.ideationMu.Unlock()

		if enabled {
			h.createIdeaAgentTask(context.Background())
		}
	})
	h.ideationMu.Unlock()
	logger.Handler.Info("ideation: next run scheduled", "at", nextRun.Format(time.RFC3339))
}

// ideaAgentPrompt is the user-visible prompt shown on idea-agent task cards.
const ideaAgentPrompt = "Analyzes the workspace and proposes 3 actionable improvements."

// createIdeaAgentTask creates a new idea-agent task card in the backlog and
// returns it. Returns nil if creation fails.
func (h *Handler) createIdeaAgentTask(ctx context.Context) *store.Task {
	task, err := h.store.CreateTask(ctx, ideaAgentPrompt, ideaAgentDefaultTimeout, false, "", store.TaskKindIdeaAgent)
	if err != nil {
		logger.Handler.Warn("ideation: create idea-agent task", "error", err)
		return nil
	}
	// Set the title immediately so the card always shows the date/time,
	// even while the task is still in the backlog.
	title := "Brainstorm " + time.Now().Format("Jan 2, 2006 15:04")
	h.store.UpdateTaskTitle(ctx, task.ID, title)
	h.store.InsertEvent(ctx, task.ID, store.EventTypeStateChange, map[string]string{
		"to": string(store.TaskStatusBacklog),
	})
	logger.Handler.Info("ideation: queued new idea-agent task", "task", task.ID)
	return task
}

// TriggerIdeation handles POST /api/ideate.
// Creates an idea-agent task card and immediately starts it regardless of
// whether autopilot is enabled.
func (h *Handler) TriggerIdeation(w http.ResponseWriter, r *http.Request) {
	task := h.createIdeaAgentTask(r.Context())
	if task != nil {
		if err := h.store.UpdateTaskStatus(r.Context(), task.ID, store.TaskStatusInProgress); err != nil {
			logger.Handler.Error("ideation: promote idea-agent task", "task", task.ID, "error", err)
		} else {
			h.store.InsertEvent(r.Context(), task.ID, store.EventTypeStateChange, map[string]string{
				"from": string(store.TaskStatusBacklog),
				"to":   string(store.TaskStatusInProgress),
			})
			h.runner.RunBackground(task.ID, task.Prompt, "", false)
		}
	}
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

	resp := map[string]any{
		"enabled": h.IdeationEnabled(),
		"running": running,
	}
	if nextRun := h.IdeationNextRun(); !nextRun.IsZero() {
		resp["next_run_at"] = nextRun
	}
	writeJSON(w, http.StatusOK, resp)
}
