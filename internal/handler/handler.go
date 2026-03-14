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

// watcherBreaker is a per-watcher circuit breaker that suppresses a specific
// watcher when it encounters repeated failures. Unlike pauseAllAutomation it
// leaves all other watchers unaffected and auto-heals after a backoff period.
type watcherBreaker struct {
	mu         sync.Mutex
	failures   int
	openUntil  time.Time // zero means closed (healthy)
	lastReason string
	lastTaskID *uuid.UUID
}

func (wb *watcherBreaker) isOpen() bool {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	return !wb.openUntil.IsZero() && time.Now().Before(wb.openUntil)
}

// recordFailure increments the failure counter and opens the breaker with
// exponential backoff (30s * 2^(n-1), capped at 5 minutes). Returns the
// updated failure count.
func (wb *watcherBreaker) recordFailure(taskID *uuid.UUID, reason string) int {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	wb.failures++
	wb.lastReason = reason
	if taskID != nil {
		cp := *taskID
		wb.lastTaskID = &cp
	} else {
		wb.lastTaskID = nil
	}
	// Exponential backoff: 30s * 2^(n-1), capped at 5 minutes.
	backoff := time.Duration(30<<uint(wb.failures-1)) * time.Second
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	wb.openUntil = time.Now().Add(backoff)
	return wb.failures
}

func (wb *watcherBreaker) recordSuccess() {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	wb.failures = 0
	wb.openUntil = time.Time{}
}

// watcherHealthEntry is the per-watcher health state returned by GET /api/config.
type watcherHealthEntry struct {
	Name       string     `json:"name"`
	Healthy    bool       `json:"healthy"`
	Failures   int        `json:"failures,omitempty"`
	RetryAt    *time.Time `json:"retry_at,omitempty"`
	LastReason string     `json:"last_reason,omitempty"`
}

func (wb *watcherBreaker) healthEntry(name string) watcherHealthEntry {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	open := !wb.openUntil.IsZero() && time.Now().Before(wb.openUntil)
	entry := watcherHealthEntry{
		Name:     name,
		Healthy:  !open,
		Failures: wb.failures,
	}
	if open {
		retryAt := wb.openUntil
		entry.RetryAt = &retryAt
		entry.LastReason = wb.lastReason
	}
	return entry
}

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

	autorefineMu sync.RWMutex
	autorefine   bool

	autosyncMu sync.RWMutex
	autosync   bool

	autopushMu sync.RWMutex
	autopush   bool

	// breakers holds per-watcher circuit breakers. Keyed by watcher name
	// (e.g. "auto-promote"). These are transient and auto-heal; they do not
	// affect the user-controlled toggle flags.
	breakers map[string]*watcherBreaker

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
		breakers: map[string]*watcherBreaker{
			"auto-promote": {},
			"auto-retry":   {},
			"auto-test":    {},
			"auto-submit":  {},
			"auto-sync":    {},
			"auto-refine":  {},
		},
	}
	// Initialize auto-push from env config so the header toggle reflects the persisted state.
	if envCfg, err := envconfig.Parse(r.EnvFile()); err == nil {
		h.autopush = envCfg.AutoPushEnabled
	}
	h.webhookNotifier = func(cfg envconfig.Config) *runner.WebhookNotifier {
		return runner.NewWorkspaceWebhookNotifier(h.workspace, cfg)
	}
	h.applySnapshot(wsMgr.Snapshot())
	if wsMgr != nil {
		_, ch := wsMgr.Subscribe()
		go func() {
			for snap := range ch {
				h.applySnapshot(snap)
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

// applySnapshot updates all handler fields that mirror the active workspace
// snapshot. It is the single place where snapshot-derived state is written
// into the handler, called both at construction time and from the workspace
// subscription goroutine so that every workspace switch is reflected
// consistently.
func (h *Handler) applySnapshot(snap workspace.Snapshot) {
	h.store = snap.Store
	h.workspaces = snap.Workspaces
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

// incAutopilotPhase2Miss increments the phase-2 miss counter for the named watcher.
func (h *Handler) incAutopilotPhase2Miss(watcher string) {
	if h.reg == nil {
		return
	}
	h.reg.Counter("wallfacer_autopilot_phase2_miss_total", "").Inc(map[string]string{
		"watcher": watcher,
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

// AutorefineEnabled returns whether auto-refinement mode is active.
func (h *Handler) AutorefineEnabled() bool {
	h.autorefineMu.RLock()
	defer h.autorefineMu.RUnlock()
	return h.autorefine
}

// SetAutorefine enables or disables auto-refinement mode.
func (h *Handler) SetAutorefine(enabled bool) {
	h.autorefineMu.Lock()
	h.autorefine = enabled
	h.autorefineMu.Unlock()
}

// AutosyncEnabled returns whether auto-sync (tip-sync) mode is active.
func (h *Handler) AutosyncEnabled() bool {
	h.autosyncMu.RLock()
	defer h.autosyncMu.RUnlock()
	return h.autosync
}

// SetAutosync enables or disables auto-sync (tip-sync) mode.
func (h *Handler) SetAutosync(enabled bool) {
	h.autosyncMu.Lock()
	h.autosync = enabled
	h.autosyncMu.Unlock()
}

// AutopushEnabled returns whether auto-push mode is active.
func (h *Handler) AutopushEnabled() bool {
	h.autopushMu.RLock()
	defer h.autopushMu.RUnlock()
	return h.autopush
}

// SetAutopush enables or disables auto-push mode.
func (h *Handler) SetAutopush(enabled bool) {
	h.autopushMu.Lock()
	h.autopush = enabled
	h.autopushMu.Unlock()
}

// openWatcherBreaker opens the circuit breaker for a specific watcher.
// It does NOT disable other watchers. Returns true if the breaker was
// previously closed (i.e., this is a new failure).
func (h *Handler) openWatcherBreaker(watcherName string, taskID *uuid.UUID, reason string) bool {
	wb, ok := h.breakers[watcherName]
	if !ok {
		logger.Handler.Error("unknown watcher breaker", "watcher", watcherName)
		return false
	}
	wasHealthy := !wb.isOpen()
	failures := wb.recordFailure(taskID, reason)
	if taskID != nil {
		h.store.InsertEvent(context.Background(), *taskID, store.EventTypeSystem, map[string]string{
			"result": fmt.Sprintf("[%s] circuit breaker opened: %s", watcherName, reason),
		})
	}
	logger.Handler.Warn("watcher circuit breaker opened",
		"watcher", watcherName,
		"task", taskID,
		"reason", reason,
		"failures", failures,
	)
	return wasHealthy
}

// pauseAllAutomation opens the circuit breaker for the watcher that failed.
// It no longer disables all board-level toggles; the circuit breaker is a
// transient, auto-healing layer that suppresses only the affected watcher.
func (h *Handler) pauseAllAutomation(taskID *uuid.UUID, watcher, reason string) bool {
	return h.openWatcherBreaker(watcher, taskID, reason)
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
