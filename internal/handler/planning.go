package handler

import (
	"net/http"

	"changkun.de/x/wallfacer/internal/pkg/httpjson"
)

// GetPlanningStatus reports whether the planning sandbox is running.
func (h *Handler) GetPlanningStatus(w http.ResponseWriter, _ *http.Request) {
	running := false
	if h.planner != nil {
		running = h.planner.IsRunning()
	}
	httpjson.Write(w, http.StatusOK, map[string]any{
		"running": running,
	})
}

// StartPlanning starts the planning sandbox container.
// If already running, returns 200 with running=true (idempotent).
func (h *Handler) StartPlanning(w http.ResponseWriter, r *http.Request) {
	if h.planner == nil {
		http.Error(w, "planning not configured", http.StatusServiceUnavailable)
		return
	}
	if h.planner.IsRunning() {
		httpjson.Write(w, http.StatusOK, map[string]any{"running": true})
		return
	}
	if err := h.planner.Start(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpjson.Write(w, http.StatusAccepted, map[string]any{"running": true})
}

// StopPlanning stops the planning sandbox container.
func (h *Handler) StopPlanning(w http.ResponseWriter, _ *http.Request) {
	if h.planner != nil {
		h.planner.Stop()
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"stopped": true})
}
