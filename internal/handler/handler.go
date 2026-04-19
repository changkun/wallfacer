package handler

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/metrics"
	"changkun.de/x/wallfacer/internal/oauth"
	"changkun.de/x/wallfacer/internal/pkg/circuitbreaker"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/pkg/lazyval"
	"changkun.de/x/wallfacer/internal/planner"
	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/routine"
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
	autosync   atomic.Bool
	autopush   atomic.Bool

	// breakers holds per-watcher circuit breakers. Keyed by watcher name
	// (e.g. "auto-promote"). These are transient and auto-heal; they do not
	// affect the user-controlled toggle flags.
	breakers map[string]*watcherBreaker

	oauthManager *oauth.Manager

	// auth provides latere.ai OIDC sign-in when cloud mode is active. Nil
	// (untyped) means auth is not configured; handlers short-circuit to 503
	// or 204 accordingly. Wired via SetAuth from the CLI boot path.
	auth AuthProvider
	// authURL caches the auth service base URL for /api/config responses so
	// handlers don't call back into AuthProvider for every config request.
	authURL string

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

	// groupLimitsMu guards groupLimits, the in-memory cache of per-workspace-
	// group concurrency overrides loaded from workspace-groups.json. Refreshed
	// by reloadGroupLimits on startup and after each group save.
	groupLimitsMu sync.RWMutex
	groupLimits   map[string]groupLimitEntry

	// ideationExploitRatio controls the exploitation fraction for the idea-
	// agent prompt (0.0 = fully exploratory, 1.0 = fully exploitative). It
	// is prompt-building state used by runner.BuildIdeationPrompt, not a
	// user-facing toggle — the Automation menu and settings panel both
	// dropped their ideation controls when ideation moved to the standard
	// composer + optional routine flow.
	ideationMu           sync.Mutex
	ideationExploitRatio float64

	planner         *planner.Planner
	commandRegistry *planner.CommandRegistry

	// routineEngine multiplexes per-routine scheduled fires. Nil until
	// StartRoutineEngine runs at server start; guarded by routineMu so
	// tests that reinitialize the engine don't race with reconcile calls.
	routineMu     sync.Mutex
	routineEngine *routine.Engine

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
	oauthMgr := oauth.NewManager()
	oauthMgr.TokenWriter = newOAuthTokenWriter(h.envFile)
	h.oauthManager = oauthMgr
	h.cachedMaxParallel = lazyval.New(func() int {
		cfg, err := envconfig.Parse(h.envFile)
		limit := constants.DefaultMaxConcurrentTasks
		if err == nil && cfg.MaxParallelTasks > 0 {
			limit = cfg.MaxParallelTasks
		}
		// Host mode caps parallelism to 1 unless the user explicitly
		// opted into more. The claude/codex CLIs share ~/.claude and
		// ~/.codex state (session dir, settings cache) across concurrent
		// invocations — running more than one at a time can race on the
		// shared SQLite settings DB and statsig telemetry files. Users
		// who have verified their CLI tolerates parallel runs can
		// override via WALLFACER_MAX_PARALLEL=N.
		if h.runner != nil && h.runner.HostMode() && cfg.MaxParallelTasks <= 0 {
			return 1
		}
		return limit
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
	// Populate the per-group concurrency override cache from disk.
	h.reloadGroupLimits()
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

// SetPlanner sets the planner instance for planning sandbox operations.
// Called by the server after both the handler and planner are constructed.
func (h *Handler) SetPlanner(p *planner.Planner) {
	h.planner = p
	h.commandRegistry = planner.NewCommandRegistry()
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

// CurrentWorkspaces returns the active workspace paths. Exported so that
// cross-package hooks (e.g., the spec completion callback) can resolve
// workspace-relative paths at call time.
func (h *Handler) CurrentWorkspaces() []string {
	return h.currentWorkspaces()
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

	// Update the planner's workspaces when the workspace group changes.
	if h.planner != nil {
		h.planner.UpdateWorkspaces(snap.Workspaces, snap.Key)
	}

	// Switching groups must also swap the automation toggle state so a
	// group the user expected to operate manually does not inherit an
	// autopilot-on flag from the previous group. Autopush is a global
	// env-file flag and is not touched here.
	h.applyGroupToggles(snap.Workspaces)
}

// applyGroupToggles syncs the in-memory automation toggle atomics to the
// values persisted for the given workspace group. A group whose fields
// are nil (never configured or saved pre-migration) resets every toggle
// to off, which matches the user expectation that a brand-new group
// starts fully manual.
func (h *Handler) applyGroupToggles(ws []string) {
	var g *workspace.Group
	if len(ws) > 0 {
		groups, err := workspace.LoadGroups(h.configDir)
		if err == nil {
			key := workspace.GroupKey(ws)
			for i := range groups {
				if workspace.GroupKey(groups[i].Workspaces) == key {
					g = &groups[i]
					break
				}
			}
		}
	}
	pick := func(v *bool) bool {
		if v == nil {
			return false
		}
		return *v
	}
	if g == nil {
		h.autopilot.Store(false)
		h.autotest.Store(false)
		h.autosubmit.Store(false)
		h.autosync.Store(false)
		return
	}
	h.autopilot.Store(pick(g.Autopilot))
	h.autotest.Store(pick(g.Autotest))
	h.autosubmit.Store(pick(g.Autosubmit))
	h.autosync.Store(pick(g.Autosync))
}

// persistCurrentGroupToggles writes the current in-memory toggle state
// into the workspace-groups.json entry for the currently viewed group,
// creating a new entry if the group is not yet persisted. Called from
// UpdateConfig after the user flips any automation toggle so switching
// to another group and back restores their choice.
func (h *Handler) persistCurrentGroupToggles() {
	ws := h.currentWorkspaces()
	if len(ws) == 0 {
		return
	}
	groups, err := workspace.LoadGroups(h.configDir)
	if err != nil {
		logger.Handler.Warn("persist group toggles: load groups", "error", err)
		return
	}
	key := workspace.GroupKey(ws)
	b := func(v bool) *bool { x := v; return &x }
	autopilot := b(h.autopilot.Load())
	autotest := b(h.autotest.Load())
	autosubmit := b(h.autosubmit.Load())
	autosync := b(h.autosync.Load())
	found := false
	for i := range groups {
		if workspace.GroupKey(groups[i].Workspaces) == key {
			groups[i].Autopilot = autopilot
			groups[i].Autotest = autotest
			groups[i].Autosubmit = autosubmit
			groups[i].Autosync = autosync
			found = true
			break
		}
	}
	if !found {
		groups = append([]workspace.Group{{
			Workspaces: ws,
			Autopilot:  autopilot,
			Autotest:   autotest,
			Autosubmit: autosubmit,
			Autosync:   autosync,
		}}, groups...)
	}
	if err := workspace.SaveGroups(h.configDir, groups); err != nil {
		logger.Handler.Warn("persist group toggles: save groups", "error", err)
	}
}

// groupLimitEntry caches the optional concurrency overrides for a single
// workspace group. nil means "inherit the env-file default"; 0 means
// "unlimited for this group"; positive means "hard cap".
type groupLimitEntry struct {
	maxParallel     *int
	maxTestParallel *int
}

// reloadGroupLimits re-reads workspace-groups.json and rebuilds the in-memory
// groupLimits cache. Safe to call concurrently; a read error collapses the
// cache to empty (so callers fall back to the env-file default). Called at
// handler construction and from UpdateConfig after each SaveGroups.
func (h *Handler) reloadGroupLimits() {
	groups, err := workspace.LoadGroups(h.configDir)
	limits := make(map[string]groupLimitEntry, len(groups))
	if err == nil {
		for _, g := range groups {
			if g.MaxParallel == nil && g.MaxTestParallel == nil {
				continue
			}
			limits[workspace.GroupKey(g.Workspaces)] = groupLimitEntry{
				maxParallel:     g.MaxParallel,
				maxTestParallel: g.MaxTestParallel,
			}
		}
	}
	h.groupLimitsMu.Lock()
	h.groupLimits = limits
	h.groupLimitsMu.Unlock()
}

// currentGroupParallelLimit returns the per-group concurrency override for
// the currently viewed workspace group, if one is set. The second return
// value reports whether an override applies; when false, callers should fall
// back to the env-file default. testRun selects between the regular
// (MaxParallel) and test-run (MaxTestParallel) overrides.
func (h *Handler) currentGroupParallelLimit(testRun bool) (int, bool) {
	ws := h.currentWorkspaces()
	if len(ws) == 0 {
		return 0, false
	}
	key := workspace.GroupKey(ws)
	h.groupLimitsMu.RLock()
	entry, ok := h.groupLimits[key]
	h.groupLimitsMu.RUnlock()
	if !ok {
		return 0, false
	}
	var v *int
	if testRun {
		v = entry.maxTestParallel
	} else {
		v = entry.maxParallel
	}
	if v == nil {
		return 0, false
	}
	// A stored value of 0 is a deliberate "unlimited" override. Return a
	// large sentinel so the caller's "in-progress >= limit" guard never
	// trips on this group.
	if *v == 0 {
		return math.MaxInt32, true
	}
	return *v, true
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

// forCurrentStore calls fn only for the currently viewed workspace group's
// store. This scopes automation actions (auto-promote, auto-retry, auto-test,
// auto-submit, auto-sync, auto-refine) to the viewed group: tasks already
// running in other groups finish as normal, but no new automation fires on
// their backlogs. It is the action-taking counterpart to forEachActiveStore,
// which remains in use for global concurrency counting.
func (h *Handler) forCurrentStore(fn func(s *store.Store, ws []string)) {
	s, ok := h.currentStore()
	if !ok || s == nil {
		return
	}
	ws := h.currentWorkspaces()
	fn(s, ws)
}

// countGlobalInProgress returns the number of non-test in-progress tasks in
// the currently viewed workspace group. Parallel limits
// (WALLFACER_MAX_PARALLEL) are scoped per group so each group has its own
// independent concurrency budget; tasks still running in other groups after
// a switch do not consume this budget.
func (h *Handler) countGlobalInProgress() int {
	total := 0
	h.forCurrentStore(func(s *store.Store, _ []string) {
		total += s.CountRegularInProgress()
	})
	return total
}

// countGlobalTestsInProgress returns the number of test-run in-progress tasks
// in the currently viewed workspace group. Scoped per-group for the same
// reason as countGlobalInProgress.
func (h *Handler) countGlobalTestsInProgress(ctx context.Context) int {
	total := 0
	h.forCurrentStore(func(s *store.Store, _ []string) {
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

// IdeationEnabled always returns false. The toggle-based ideation flow
// was retired when ideation moved to a regular task the user creates
// from the composer. The accessor is retained so callers threading the
// old config shape keep compiling.
func (h *Handler) IdeationEnabled() bool { return false }

// SetIdeation is a no-op. Kept for call-site compatibility while the
// remaining references migrate off the legacy toggle.
func (h *Handler) SetIdeation(bool) {}

// IdeationInterval always returns zero. Recurring ideation is now
// expressed as a user-created routine, not a global config knob.
func (h *Handler) IdeationInterval() time.Duration { return 0 }

// SetIdeationInterval is a no-op, retained for the same reason as the
// other ideation shims above.
func (h *Handler) SetIdeationInterval(time.Duration) {}

// IdeationNextRun always returns the zero time. There is no longer a
// system-wide pending ideation fire; each user-created idea-agent task
// is scheduled individually.
func (h *Handler) IdeationNextRun() time.Time { return time.Time{} }

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
