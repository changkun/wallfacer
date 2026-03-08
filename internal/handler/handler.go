package handler

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

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
	startTime  time.Time

	autopilotMu sync.RWMutex
	autopilot   bool

	autotestMu sync.RWMutex
	autotest   bool

	autosubmitMu sync.RWMutex
	autosubmit   bool

	diffCache *diffCache

	// ideationEnabled controls whether brainstorm auto-repeat is active.
	// ideationInterval is the delay between consecutive brainstorm runs (0 = run immediately on completion).
	// ideationNextRun is when the pending timer will fire (zero if not scheduled).
	// ideationTimer is a non-nil pending AfterFunc timer while a delayed run is waiting.
	// All fields are serialised by ideationMu.
	ideationMu       sync.Mutex
	ideationEnabled  bool
	ideationInterval time.Duration
	ideationNextRun  time.Time
	ideationTimer    *time.Timer

	sandboxTestMu     sync.RWMutex
	sandboxTestPassed map[string]bool
}

// NewHandler constructs a Handler with the given dependencies.
func NewHandler(s *store.Store, r *runner.Runner, configDir string, workspaces []string) *Handler {
	return &Handler{
		store:            s,
		runner:           r,
		configDir:        configDir,
		workspaces:       workspaces,
		envFile:          r.EnvFile(),
		diffCache:        newDiffCache(),
		startTime:        time.Now(),
		ideationEnabled:  true,
		ideationInterval: 60 * time.Minute,
		sandboxTestPassed: map[string]bool{
			"claude": false,
			"codex":  false,
		},
	}
}

func (h *Handler) setSandboxTestPassed(sandbox string, passed bool) {
	s := normalizeSandbox(sandbox)
	h.sandboxTestMu.Lock()
	h.sandboxTestPassed[s] = passed
	h.sandboxTestMu.Unlock()
}

func (h *Handler) sandboxTestPassedState(sandbox string) bool {
	s := normalizeSandbox(sandbox)
	h.sandboxTestMu.RLock()
	defer h.sandboxTestMu.RUnlock()
	return h.sandboxTestPassed[s]
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

// AutotestEnabled returns whether auto-test mode is active.
func (h *Handler) AutotestEnabled() bool {
	h.autotestMu.RLock()
	defer h.autotestMu.RUnlock()
	return h.autotest
}

// SetAutotest enables or disables auto-test mode.
func (h *Handler) SetAutotest(enabled bool) {
	h.autotestMu.Lock()
	h.autotest = enabled
	h.autotestMu.Unlock()
}

// AutosubmitEnabled returns whether auto-submit mode is active.
func (h *Handler) AutosubmitEnabled() bool {
	h.autosubmitMu.RLock()
	defer h.autosubmitMu.RUnlock()
	return h.autosubmit
}

// SetAutosubmit enables or disables auto-submit mode.
func (h *Handler) SetAutosubmit(enabled bool) {
	h.autosubmitMu.Lock()
	h.autosubmit = enabled
	h.autosubmitMu.Unlock()
}

// IdeationEnabled returns whether brainstorm auto-repeat is active.
func (h *Handler) IdeationEnabled() bool {
	h.ideationMu.Lock()
	defer h.ideationMu.Unlock()
	return h.ideationEnabled
}

// SetIdeation enables or disables brainstorm auto-repeat.
// Disabling cancels any pending scheduled run.
func (h *Handler) SetIdeation(enabled bool) {
	h.ideationMu.Lock()
	h.ideationEnabled = enabled
	if !enabled {
		h.cancelIdeationTimerLocked()
	}
	h.ideationMu.Unlock()
}

// IdeationInterval returns the delay between consecutive brainstorm runs.
func (h *Handler) IdeationInterval() time.Duration {
	h.ideationMu.Lock()
	defer h.ideationMu.Unlock()
	return h.ideationInterval
}

// SetIdeationInterval updates the delay between brainstorm runs.
// Any pending timer is cancelled; the caller is responsible for rescheduling.
func (h *Handler) SetIdeationInterval(d time.Duration) {
	h.ideationMu.Lock()
	h.ideationInterval = d
	h.cancelIdeationTimerLocked()
	h.ideationMu.Unlock()
}

// IdeationNextRun returns the scheduled time of the next brainstorm run,
// or a zero time if no run is pending.
func (h *Handler) IdeationNextRun() time.Time {
	h.ideationMu.Lock()
	defer h.ideationMu.Unlock()
	return h.ideationNextRun
}

// cancelIdeationTimerLocked stops and clears the pending ideation timer.
// Must be called with ideationMu held.
func (h *Handler) cancelIdeationTimerLocked() {
	if h.ideationTimer != nil {
		h.ideationTimer.Stop()
		h.ideationTimer = nil
		h.ideationNextRun = time.Time{}
	}
}

// writeJSON serialises v as JSON and writes it with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Handler.Error("write json", "error", err)
	}
}
