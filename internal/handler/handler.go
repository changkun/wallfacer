package handler

import (
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
}

// NewHandler constructs a Handler with the given dependencies.
func NewHandler(s *store.Store, r *runner.Runner, configDir string, workspaces []string) *Handler {
	return &Handler{
		store:      s,
		runner:     r,
		configDir:  configDir,
		workspaces: workspaces,
		envFile:    r.EnvFile(),
		autopilot:  false,
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

// writeJSON serialises v as JSON and writes it with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Handler.Error("write json", "error", err)
	}
}
