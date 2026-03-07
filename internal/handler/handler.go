package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/runner"
	"changkun.de/wallfacer/internal/store"
)

// Handler holds dependencies for all HTTP API handlers.
type Handler struct {
	store      *store.Store
	runner     *runner.Runner
	configDir  string
	workspaces []string
	envFile    string

	autopilotMu sync.RWMutex
	autopilot   bool

	// Brainstorm / ideation state.
	ideationMu      sync.Mutex
	ideationEnabled bool
	ideationRunning bool
	ideationCancel  context.CancelFunc
	ideationTrigger chan struct{} // buffered(1): send to trigger an immediate run
}

// NewHandler constructs a Handler with the given dependencies.
func NewHandler(s *store.Store, r *runner.Runner, configDir string, workspaces []string) *Handler {
	return &Handler{
		store:           s,
		runner:          r,
		configDir:       configDir,
		workspaces:      workspaces,
		envFile:         r.EnvFile(),
		autopilot:       false,
		ideationTrigger: make(chan struct{}, 1),
	}
}

// AutopilotEnabled returns whether autopilot mode is active.
func (h *Handler) AutopilotEnabled() bool {
	h.autopilotMu.RLock()
	defer h.autopilotMu.RUnlock()
	return h.autopilot
}

// SetAutopilot enables or disables autopilot mode.
func (h *Handler) SetAutopilot(enabled bool) {
	h.autopilotMu.Lock()
	h.autopilot = enabled
	h.autopilotMu.Unlock()
}

// IdeationEnabled returns whether periodic brainstorm ideation is active.
func (h *Handler) IdeationEnabled() bool {
	h.ideationMu.Lock()
	defer h.ideationMu.Unlock()
	return h.ideationEnabled
}

// SetIdeation enables or disables periodic brainstorm ideation.
func (h *Handler) SetIdeation(enabled bool) {
	h.ideationMu.Lock()
	h.ideationEnabled = enabled
	h.ideationMu.Unlock()
}

// IdeationRunning reports whether a brainstorm run is currently in progress.
func (h *Handler) IdeationRunning() bool {
	h.ideationMu.Lock()
	defer h.ideationMu.Unlock()
	return h.ideationRunning
}

// writeJSON serialises v as JSON and writes it with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Handler.Error("write json", "error", err)
	}
}
