package handler

import (
	"context"
	"net/http"
	"time"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
)

const ideationInterval = 6 * time.Hour

// StartIdeationLoop starts a background goroutine that runs the brainstorm
// ideation agent periodically (every ideationInterval) whenever ideation is
// enabled, and immediately whenever a trigger signal is sent.
func (h *Handler) StartIdeationLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(ideationInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-h.ideationTrigger:
				h.doIdeation(ctx)
			case <-ticker.C:
				if h.IdeationEnabled() {
					h.doIdeation(ctx)
				}
			}
		}
	}()
}

// doIdeation runs one brainstorm cycle: launches the ideation container, parses
// the proposed ideas, and creates backlog tasks for each.
// It is a no-op if another run is already in progress.
func (h *Handler) doIdeation(ctx context.Context) {
	h.ideationMu.Lock()
	if h.ideationRunning {
		h.ideationMu.Unlock()
		return
	}
	ideationCtx, cancel := context.WithCancel(ctx)
	h.ideationRunning = true
	h.ideationCancel = cancel
	h.ideationMu.Unlock()

	defer func() {
		cancel()
		h.ideationMu.Lock()
		h.ideationRunning = false
		h.ideationCancel = nil
		h.ideationMu.Unlock()
	}()

	logger.Handler.Info("ideation: starting brainstorm run")

	ideas, err := h.runner.RunIdeation(ideationCtx)
	if err != nil {
		if ideationCtx.Err() != nil {
			logger.Handler.Info("ideation: cancelled")
		} else {
			logger.Handler.Warn("ideation: failed", "error", err)
		}
		return
	}

	for _, idea := range ideas {
		task, createErr := h.store.CreateTask(ideationCtx, idea.Prompt, 60, false, "", "idea-agent")
		if createErr != nil {
			logger.Handler.Warn("ideation: create task failed", "error", createErr)
			continue
		}
		h.store.InsertEvent(ideationCtx, task.ID, store.EventTypeStateChange, map[string]string{
			"to": string(store.TaskStatusBacklog),
		})
		if idea.Title != "" {
			h.store.UpdateTaskTitle(ideationCtx, task.ID, idea.Title)
		}
		logger.Handler.Info("ideation: created task", "task", task.ID, "title", idea.Title)
	}

	logger.Handler.Info("ideation: run complete", "ideas", len(ideas))
}

// TriggerIdeation handles POST /api/ideate.
// Enqueues an immediate ideation run regardless of the enabled toggle.
func (h *Handler) TriggerIdeation(w http.ResponseWriter, r *http.Request) {
	if h.IdeationRunning() {
		http.Error(w, "ideation already running", http.StatusConflict)
		return
	}
	// Non-blocking send: if a trigger is already queued, skip.
	select {
	case h.ideationTrigger <- struct{}{}:
	default:
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"running": true,
	})
}

// CancelIdeation handles DELETE /api/ideate.
// Cancels any running ideation container and marks the run as stopped.
func (h *Handler) CancelIdeation(w http.ResponseWriter, r *http.Request) {
	h.ideationMu.Lock()
	running := h.ideationRunning
	cancel := h.ideationCancel
	h.ideationMu.Unlock()

	if !running {
		writeJSON(w, http.StatusOK, map[string]any{"running": false})
		return
	}
	// Kill the container first so the goroutine unblocks quickly.
	h.runner.KillIdeateContainer()
	if cancel != nil {
		cancel()
	}
	writeJSON(w, http.StatusOK, map[string]any{"running": false})
}

// GetIdeationStatus handles GET /api/ideate.
// Returns the current brainstorm enabled/running state.
func (h *Handler) GetIdeationStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": h.IdeationEnabled(),
		"running": h.IdeationRunning(),
	})
}
