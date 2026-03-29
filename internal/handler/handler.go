package handler

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/metrics"
	"changkun.de/x/wallfacer/internal/pkg/circuitbreaker"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/pkg/lazyval"
	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
	"changkun.de/x/wallfacer/internal/workspace"
	"github.com/google/uuid"
)

// watcherBreaker is a per-watcher circuit breaker that suppresses a specific
// watcher when it encounters repeated failures. Unlike pauseAllAutomation it
// leaves all other watchers unaffected and auto-heals after a backoff period.
//
// It wraps the generic [circuitbreaker.BackoffBreaker] and adds domain-specific
// metadata (last failure reason and task ID) for health reporting.
type watcherBreaker struct {
	breaker    *circuitbreaker.BackoffBreaker
	mu         sync.Mutex
	lastReason string
	lastTaskID *uuid.UUID
}

// newWatcherBreaker creates a watcherBreaker with default backoff configuration.
func newWatcherBreaker() *watcherBreaker {
	return &watcherBreaker{
		breaker: circuitbreaker.NewBackoff(circuitbreaker.BackoffConfig{}),
	}
}

// isOpen reports whether the breaker is currently in the open (tripped) state.
func (wb *watcherBreaker) isOpen() bool {
	return wb.breaker.IsOpen()
}

// recordFailure records a failure with metadata for health reporting and
// delegates to the underlying backoff breaker. Returns the new failure count.
func (wb *watcherBreaker) recordFailure(taskID *uuid.UUID, reason string) int {
	wb.mu.Lock()
	wb.lastReason = reason
	if taskID != nil {
		cp := *taskID
		wb.lastTaskID = &cp
	} else {
		wb.lastTaskID = nil
	}
	wb.mu.Unlock()
	return wb.breaker.RecordFailure()
}

// recordSuccess resets the failure state and clears the last failure metadata.
func (wb *watcherBreaker) recordSuccess() {
	wb.breaker.RecordSuccess()
	wb.mu.Lock()
	wb.lastReason = ""
	wb.lastTaskID = nil
	wb.mu.Unlock()
}

// watcherHealthEntry is the per-watcher health state returned by GET /api/config.
type watcherHealthEntry struct {
	Name       string     `json:"name"`
	Healthy    bool       `json:"healthy"`
	Failures   int        `json:"failures,omitempty"`
	RetryAt    *time.Time `json:"retry_at,omitempty"`
	LastReason string     `json:"last_reason,omitempty"`
}

// healthEntry builds a watcherHealthEntry snapshot for inclusion in the
// GET /api/config response, reporting current breaker state and last failure.
func (wb *watcherBreaker) healthEntry(name string) watcherHealthEntry {
	open := wb.breaker.IsOpen()
	entry := watcherHealthEntry{
		Name:    name,
		Healthy: !open,
	}
	entry.Failures = wb.breaker.Failures()
	if open {
		if retryAt, ok := wb.breaker.RetryAt(); ok {
			entry.RetryAt = &retryAt
		}
		wb.mu.Lock()
		entry.LastReason = wb.lastReason
		wb.mu.Unlock()
	}
	return entry
}

// Handler holds dependencies for all HTTP API handlers.
type Handler struct {
	// snapshotMu guards the store and workspaces mirror fields, which are
	// written by the workspace subscription goroutine (via applySnapshot) and
	// read by HTTP handler goroutines. All other fields are either set once at
	// construction time or protected by their own mutex.
	snapshotMu sync.RWMutex
	store      *store.Store
	workspace  *workspace.Manager
	runner     runner.Interface
	configDir  string
	workspaces []string
	envFile    string
	startTime  time.Time
	reg        *metrics.Registry

	autopilot  atomic.Bool
	autotest   atomic.Bool
	autosubmit atomic.Bool
	autorefine atomic.Bool
	autosync   atomic.Bool
	autopush   atomic.Bool

	// breakers holds per-watcher circuit breakers. Keyed by watcher name
	// (e.g. "auto-promote"). These are transient and auto-heal; they do not
	// affect the user-controlled toggle flags.
	breakers map[string]*watcherBreaker

	diffCache          *diffCache
	commitsBehindCache *commitsBehindCache
	fileIndex          *fileIndex
	pulls              *pullTracker
	spanCache          spanStatsCache

	// cachedMaxParallel and cachedMaxTestParallel cache the configured parallel
	// task limits so that maxConcurrentTasks/maxTestConcurrentTasks do not
	// re-parse the env file on every call. Invalidate on env config update.
	cachedMaxParallel     *lazyval.Value[int]
	cachedMaxTestParallel *lazyval.Value[int]

	// ideationEnabled controls whether brainstorm auto-repeat is active.
	// ideationInterval is the delay between consecutive brainstorm runs (0 = run immediately on completion).
	// ideationNextRun is when the pending timer will fire (zero if not scheduled).
	// ideationTimer is a non-nil pending AfterFunc timer while a delayed run is waiting.
	// All fields are serialised by ideationMu.
	ideationMu           sync.Mutex
	ideationEnabled      bool
	ideationInterval     time.Duration
	ideationNextRun      time.Time
	ideationTimer        *time.Timer
	ideationExploitRatio float64 // 0.0–1.0; default 0.8 (80% exploitation)

	sandboxTestMu     sync.RWMutex
	sandboxTestPassed map[sandbox.Type]bool
	// scheduledPromoteMu guards scheduledPromoteTimer, which fires
	// tryAutoPromote precisely when the soonest scheduled task becomes due.
	scheduledPromoteMu    sync.Mutex
	scheduledPromoteTimer *time.Timer

	// testPhase1Done is called by tryAutoPromote after Phase 1 completes and
	// before Phase 2 begins. It is nil in production; tests set it to
	// coordinate goroutine timing and verify Phase 1 runs concurrently.
	testPhase1Done func()
}

// NewHandler constructs a Handler with the given dependencies.
func NewHandler(s *store.Store, r runner.Interface, configDir string, workspaces []string, reg *metrics.Registry) *Handler {
	wsMgr := (*workspace.Manager)(nil)
	if r != nil {
		wsMgr = r.WorkspaceManager()
	}
	if wsMgr == nil {
		wsMgr = workspace.NewStatic(s, workspaces, prompts.InstructionsFilePath(configDir, workspaces))
	}
	h := &Handler{
		store:                s,
		workspace:            wsMgr,
		runner:               r,
		configDir:            configDir,
		workspaces:           workspaces,
		envFile:              r.EnvFile(),
		diffCache:            newDiffCache(),
		commitsBehindCache:   newCommitsBehindCache(constants.CommitsBehindCacheTTL),
		fileIndex:            newFileIndex(),
		pulls:                newPullTracker(),
		startTime:            time.Now(),
		ideationEnabled:      false,
		ideationInterval:     constants.DefaultIdeationInterval,
		ideationExploitRatio: constants.DefaultIdeationExploitRatio,
		reg:                  reg,
		sandboxTestPassed: map[sandbox.Type]bool{
			sandbox.Claude: false,
			sandbox.Codex:  false,
		},
		breakers: map[string]*watcherBreaker{
			"auto-promote": newWatcherBreaker(),
			"auto-retry":   newWatcherBreaker(),
			"auto-test":    newWatcherBreaker(),
			"auto-submit":  newWatcherBreaker(),
			"auto-sync":    newWatcherBreaker(),
			"auto-refine":  newWatcherBreaker(),
		},
	}
	h.cachedMaxParallel = lazyval.New(func() int {
		cfg, err := envconfig.Parse(h.envFile)
		if err != nil || cfg.MaxParallelTasks <= 0 {
			return constants.DefaultMaxConcurrentTasks
		}
		return cfg.MaxParallelTasks
	})
	h.cachedMaxTestParallel = lazyval.New(func() int {
		cfg, err := envconfig.Parse(h.envFile)
		if err != nil || cfg.MaxTestParallelTasks <= 0 {
			return constants.DefaultMaxTestConcurrentTasks
		}
		return cfg.MaxTestParallelTasks
	})
	// Initialize auto-push from env config so the header toggle reflects the persisted state.
	if envCfg, err := envconfig.Parse(r.EnvFile()); err == nil {
		h.autopush.Store(envCfg.AutoPushEnabled)
	}
	// Initialise handler state from the current workspace snapshot.
	h.applySnapshot(wsMgr.Snapshot())
	if wsMgr != nil {
		// Subscribe to workspace changes so that when the user switches workspace
		// groups, the handler's store/workspaces fields are updated asynchronously.
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

// currentStore returns the active store, preferring the workspace manager's
// store when available. Returns (nil, false) when no store is configured.
func (h *Handler) currentStore() (*store.Store, bool) {
	if h.workspace != nil {
		return h.workspace.Store()
	}
	h.snapshotMu.RLock()
	s := h.store
	h.snapshotMu.RUnlock()
	return s, s != nil
}

// requireStore returns the active store or writes a 503 Service Unavailable
// response if no store is configured. Returns (nil, false) on failure.
func (h *Handler) requireStore(w http.ResponseWriter) (*store.Store, bool) {
	s, ok := h.currentStore()
	if !ok || s == nil {
		httpjson.Write(w, http.StatusServiceUnavailable, map[string]string{"error": "no workspaces configured"})
		return nil, false
	}
	return s, true
}

// currentWorkspaces returns the active workspace directory paths. Returns nil
// when no workspaces are configured. The returned slice is a clone.
func (h *Handler) currentWorkspaces() []string {
	if h.workspace != nil {
		return h.workspace.Workspaces()
	}
	h.snapshotMu.RLock()
	ws := h.workspaces
	h.snapshotMu.RUnlock()
	if len(ws) == 0 {
		return nil
	}
	return slices.Clone(ws)
}

// currentInstructionsPath returns the filesystem path to the active
// workspace AGENTS.md, or "" if no workspace manager is available.
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
	h.snapshotMu.Lock()
	h.store = snap.Store
	h.workspaces = snap.Workspaces
	h.snapshotMu.Unlock()
}

// forEachActiveStore calls fn for every active workspace group's store.
// When no workspace manager is configured, falls back to the viewed store.
func (h *Handler) forEachActiveStore(fn func(s *store.Store, ws []string)) {
	if h.workspace == nil {
		h.snapshotMu.RLock()
		s, ws := h.store, h.workspaces
		h.snapshotMu.RUnlock()
		if s != nil {
			fn(s, ws)
		}
		return
	}
	for _, snap := range h.workspace.AllActiveSnapshots() {
		if snap.Store != nil {
			fn(snap.Store, snap.Workspaces)
		}
	}
}

// countGlobalInProgress returns the total number of non-test in-progress tasks
// across ALL active workspace groups.
func (h *Handler) countGlobalInProgress() int {
	total := 0
	h.forEachActiveStore(func(s *store.Store, _ []string) {
		total += s.CountRegularInProgress()
	})
	return total
}

// countGlobalTestsInProgress returns the total number of test-run in-progress
// tasks across ALL active workspace groups.
func (h *Handler) countGlobalTestsInProgress(ctx context.Context) int {
	total := 0
	h.forEachActiveStore(func(s *store.Store, _ []string) {
		inProgress, _ := s.ListTasksByStatus(ctx, store.TaskStatusInProgress)
		for i := range inProgress {
			if inProgress[i].IsTestRun {
				total++
			}
		}
	})
	return total
}

// hasStore reports whether the handler has a configured store.
func (h *Handler) hasStore() bool {
	_, ok := h.currentStore()
	return ok
}

// RequireStoreMiddleware rejects requests with 503 when no store is configured.
func (h *Handler) RequireStoreMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.hasStore() {
			httpjson.Write(w, http.StatusServiceUnavailable, map[string]string{"error": "no workspaces configured"})
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

// setSandboxTestPassed records whether the given sandbox type has passed its
// connectivity test. Protected by sandboxTestMu for concurrent access.
func (h *Handler) setSandboxTestPassed(sb sandbox.Type, passed bool) {
	s := normalizeSandbox(string(sb))
	h.sandboxTestMu.Lock()
	h.sandboxTestPassed[s] = passed
	h.sandboxTestMu.Unlock()
}

// sandboxTestPassedState reports whether the given sandbox type has passed
// its connectivity test.
func (h *Handler) sandboxTestPassedState(sb sandbox.Type) bool {
	s := normalizeSandbox(string(sb))
	h.sandboxTestMu.RLock()
	defer h.sandboxTestMu.RUnlock()
	return h.sandboxTestPassed[s]
}

// refreshCodexBootstrapAuthState checks host-level Codex authentication
// (~/.codex/auth.json) and marks the Codex sandbox as test-passed if valid.
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
func (h *Handler) AutopilotEnabled() bool { return h.autopilot.Load() }

// SetAutopilot enables or disables autopilot mode.
func (h *Handler) SetAutopilot(enabled bool) { h.autopilot.Store(enabled) }

// AutotestEnabled returns whether auto-test mode is active.
func (h *Handler) AutotestEnabled() bool { return h.autotest.Load() }

// SetAutotest enables or disables auto-test mode.
func (h *Handler) SetAutotest(enabled bool) { h.autotest.Store(enabled) }

// AutosubmitEnabled returns whether auto-submit mode is active.
func (h *Handler) AutosubmitEnabled() bool { return h.autosubmit.Load() }

// SetAutosubmit enables or disables auto-submit mode.
func (h *Handler) SetAutosubmit(enabled bool) { h.autosubmit.Store(enabled) }

// AutorefineEnabled returns whether auto-refinement mode is active.
func (h *Handler) AutorefineEnabled() bool { return h.autorefine.Load() }

// SetAutorefine enables or disables auto-refinement mode.
func (h *Handler) SetAutorefine(enabled bool) { h.autorefine.Store(enabled) }

// AutosyncEnabled returns whether auto-sync (catch up) mode is active.
func (h *Handler) AutosyncEnabled() bool { return h.autosync.Load() }

// SetAutosync enables or disables auto-sync (catch up) mode.
func (h *Handler) SetAutosync(enabled bool) { h.autosync.Store(enabled) }

// AutopushEnabled returns whether auto-push mode is active.
func (h *Handler) AutopushEnabled() bool { return h.autopush.Load() }

// SetAutopush enables or disables auto-push mode.
func (h *Handler) SetAutopush(enabled bool) { h.autopush.Store(enabled) }

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
		h.insertEventOrLog(h.runner.ShutdownCtx(), *taskID, store.EventTypeSystem, map[string]string{
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
// Despite the name (kept for backward compatibility), it no longer disables
// all board-level toggles; the circuit breaker is a transient, auto-healing
// layer that suppresses only the affected watcher.
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

// IdeationExploitRatio returns the exploitation fraction (0.0–1.0).
func (h *Handler) IdeationExploitRatio() float64 {
	h.ideationMu.Lock()
	defer h.ideationMu.Unlock()
	return h.ideationExploitRatio
}

// SetIdeationExploitRatio updates the exploitation fraction, clamped to [0,1].
func (h *Handler) SetIdeationExploitRatio(r float64) {
	if r < 0 {
		r = 0
	}
	if r > 1 {
		r = 1
	}
	h.ideationMu.Lock()
	h.ideationExploitRatio = r
	h.ideationMu.Unlock()
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
