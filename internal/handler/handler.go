package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/instructions"
	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/metrics"
	"changkun.de/wallfacer/internal/runner"
	"changkun.de/wallfacer/internal/sandbox"
	"changkun.de/wallfacer/internal/store"
	"changkun.de/wallfacer/internal/workspace"
	"github.com/google/uuid"
)

// Handler holds dependencies for all HTTP API handlers.
type Handler struct {
	store      *store.Store
	workspace  *workspace.Manager
	runner     *runner.Runner
	configDir  string
	workspaces []string
	envFile    string
	startTime  time.Time
	reg        *metrics.Registry

	autopilotMu sync.RWMutex
	autopilot   bool

	autotestMu sync.RWMutex
	autotest   bool

	autosubmitMu sync.RWMutex
	autosubmit   bool

	diffCache *diffCache
	fileIndex *fileIndex
	spanCache spanStatsCache

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
	sandboxTestPassed map[sandbox.Type]bool
	webhookNotifier   func(envconfig.Config) *runner.WebhookNotifier

	// testPhase1Done is called by tryAutoPromote after Phase 1 completes and
	// before Phase 2 begins. It is nil in production; tests set it to
	// coordinate goroutine timing and verify Phase 1 runs concurrently.
	testPhase1Done func()
}

// NewHandler constructs a Handler with the given dependencies.
func NewHandler(s *store.Store, r *runner.Runner, configDir string, workspaces []string, reg *metrics.Registry) *Handler {
	wsMgr := (*workspace.Manager)(nil)
	if r != nil {
		wsMgr = r.WorkspaceManager()
	}
	if wsMgr == nil {
		wsMgr = workspace.NewStatic(s, workspaces, instructions.FilePath(configDir, workspaces))
	}
	h := &Handler{
		store:            s,
		workspace:        wsMgr,
		runner:           r,
		configDir:        configDir,
		workspaces:       workspaces,
		envFile:          r.EnvFile(),
		diffCache:        newDiffCache(),
		fileIndex:        newFileIndex(),
		startTime:        time.Now(),
		ideationEnabled:  true,
		ideationInterval: 60 * time.Minute,
		reg:              reg,
		sandboxTestPassed: map[sandbox.Type]bool{
			sandbox.Claude: false,
			sandbox.Codex:  false,
		},
	}
	h.webhookNotifier = func(cfg envconfig.Config) *runner.WebhookNotifier {
		return runner.NewWorkspaceWebhookNotifier(h.workspace, cfg)
	}
	if snap := wsMgr.Snapshot(); true {
		h.store = snap.Store
		h.workspaces = snap.Workspaces
	}
	if wsMgr != nil {
		_, ch := wsMgr.Subscribe()
		go func() {
			for snap := range ch {
				h.store = snap.Store
				h.workspaces = snap.Workspaces
			}
		}()
	}
	h.refreshCodexBootstrapAuthState()
	return h
}

func (h *Handler) currentStore() (*store.Store, bool) {
	if h.workspace != nil {
		return h.workspace.Store()
	}
	return h.store, h.store != nil
}

func (h *Handler) requireStore(w http.ResponseWriter) (*store.Store, bool) {
	s, ok := h.currentStore()
	if !ok || s == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no workspaces configured"})
		return nil, false
	}
	return s, true
}

func (h *Handler) currentWorkspaces() []string {
	if h.workspace != nil {
		return h.workspace.Workspaces()
	}
	if len(h.workspaces) == 0 {
		return nil
	}
	out := make([]string, len(h.workspaces))
	copy(out, h.workspaces)
	return out
}

func (h *Handler) currentInstructionsPath() string {
	if h.workspace != nil {
		return h.workspace.InstructionsPath()
	}
	return ""
}

func (h *Handler) hasStore() bool {
	_, ok := h.currentStore()
	return ok
}

func (h *Handler) RequireStoreMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.hasStore() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no workspaces configured"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// incAutopilotAction increments the autopilot action counter for the given
// watcher and outcome. It is a no-op when no registry is configured.
func (h *Handler) incAutopilotAction(watcher, outcome string) {
	if h.reg == nil {
		return
	}
	h.reg.Counter("wallfacer_autopilot_actions_total", "").Inc(map[string]string{
		"watcher": watcher,
		"outcome": outcome,
	})
}

func (h *Handler) setSandboxTestPassed(sb sandbox.Type, passed bool) {
	s := normalizeSandbox(string(sb))
	h.sandboxTestMu.Lock()
	h.sandboxTestPassed[s] = passed
	h.sandboxTestMu.Unlock()
}

func (h *Handler) sandboxTestPassedState(sb sandbox.Type) bool {
	s := normalizeSandbox(string(sb))
	h.sandboxTestMu.RLock()
	defer h.sandboxTestMu.RUnlock()
	return h.sandboxTestPassed[s]
}

func (h *Handler) refreshCodexBootstrapAuthState() {
	if h.runner == nil {
		return
	}
	ok, _ := h.runner.HostCodexAuthStatus(time.Now())
	if ok {
		h.setSandboxTestPassed(sandbox.Codex, true)
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

// pauseAllAutomation disables the board-level automation toggles after an
// automated watcher/action fails so the server stops making further automatic
// changes until the user explicitly re-enables automation.
func (h *Handler) pauseAllAutomation(taskID *uuid.UUID, watcher, reason string) bool {
	wasEnabled := h.AutopilotEnabled() || h.AutotestEnabled() || h.AutosubmitEnabled()
	if !wasEnabled {
		return false
	}

	h.SetAutopilot(false)
	h.SetAutotest(false)
	h.SetAutosubmit(false)

	taskValue := ""
	if taskID != nil {
		taskValue = taskID.String()
		h.store.InsertEvent(context.Background(), *taskID, store.EventTypeSystem, map[string]string{
			"result": fmt.Sprintf(
				"Automation paused after %s failed: %s. Autopilot, auto-test, and auto-submit were disabled.",
				watcher, reason,
			),
		})
	}

	logger.Handler.Error("automation paused after failure",
		"watcher", watcher,
		"task", taskValue,
		"reason", reason)
	return true
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

// decodeJSONBody decodes the JSON request body into v. It rejects unknown
// fields and trailing tokens after the first JSON object, writing a 400
// response on any error.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return false
		}
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	if dec.More() {
		http.Error(w, "invalid JSON: unexpected trailing content", http.StatusBadRequest)
		return false
	}
	return true
}

// decodeOptionalJSONBody decodes the JSON request body into v when a body is
// present. An absent or empty body is silently accepted and leaves v
// unchanged. When a body is present the same strict rules apply as
// decodeJSONBody: unknown fields and trailing tokens are rejected with a 400.
func decodeOptionalJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if r == nil || r.Body == nil {
		return true
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return true // empty body — treat as no body provided
		}
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return false
		}
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	if dec.More() {
		http.Error(w, "invalid JSON: unexpected trailing content", http.StatusBadRequest)
		return false
	}
	return true
}

// writeJSON serialises v as JSON and writes it with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Handler.Error("write json", "error", err)
	}
}
