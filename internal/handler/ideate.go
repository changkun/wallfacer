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

// createIdeaAgentTask creates a new idea-agent task card and immediately
// promotes it to in_progress, starting the runner. Returns nil if creation
// or promotion fails.
func (h *Handler) createIdeaAgentTask(ctx context.Context) *store.Task {
	tasks, err := h.store.ListTasks(ctx, false)
	if err != nil {
		logger.Handler.Warn("ideation: list tasks for execution prompt", "error", err)
		return nil
	}

	activeTasks := make([]store.Task, 0, len(tasks))
	for _, t := range tasks {
		if t.Kind == store.TaskKindIdeaAgent {
			continue
		}
		switch t.Status {
		case store.TaskStatusBacklog, store.TaskStatusInProgress, store.TaskStatusWaiting:
			activeTasks = append(activeTasks, t)
		}
	}

	ideaPrompt := h.runner.BuildIdeationPrompt(activeTasks)
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:  ideaPrompt,
		Timeout: ideaAgentDefaultTimeout,
		Kind:    store.TaskKindIdeaAgent,
	})
	if err != nil {
		logger.Handler.Warn("ideation: create idea-agent task", "error", err)
		return nil
	}
	if err := h.store.UpdateTaskExecutionPrompt(ctx, task.ID, ideaPrompt); err != nil {
		logger.Handler.Warn("ideation: persist idea-agent execution prompt", "task", task.ID, "error", err)
	}

	// Set the title immediately so the card always shows the date/time.
	title := "Brainstorm " + time.Now().Format("Jan 2, 2006 15:04")
	h.store.UpdateTaskTitle(ctx, task.ID, title)
	// Brainstorm tasks skip the backlog queue and go straight to in_progress.
	if err := h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		logger.Handler.Error("ideation: promote idea-agent task", "task", task.ID, "error", err)
		h.insertEventOrLog(ctx, task.ID, store.EventTypeStateChange,
			store.NewStateChangeData("", store.TaskStatusBacklog, "", nil))
		return task
	}
	h.insertEventOrLog(ctx, task.ID, store.EventTypeStateChange,
		store.NewStateChangeData("", store.TaskStatusInProgress, "", nil))
	h.runner.RunBackground(task.ID, task.Prompt, "", false)
	logger.Handler.Info("ideation: started idea-agent task", "task", task.ID)
	return task
}

// TriggerIdeation handles POST /api/ideate.
// Creates an idea-agent task card and immediately starts it regardless of
// whether autopilot is enabled.
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
			h.insertEventOrLog(r.Context(), t.ID, store.EventTypeStateChange,
				store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusCancelled, "", nil))
			cancelled = true
		case store.TaskStatusBacklog:
			h.store.UpdateTaskStatus(r.Context(), t.ID, store.TaskStatusCancelled)
			h.insertEventOrLog(r.Context(), t.ID, store.EventTypeStateChange,
				store.NewStateChangeData(store.TaskStatusBacklog, store.TaskStatusCancelled, "", nil))
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
