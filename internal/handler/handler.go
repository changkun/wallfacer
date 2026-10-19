package handler

import (
	"math"
	"net/http"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"latere.ai/x/pkg/httpjson"
	"latere.ai/x/pkg/metrics"

	"latere.ai/x/wallfacer/internal/agentsession"
	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/oauth"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/workspace"
)

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
	// scopedDataDir mirrors the active workspace snapshot's per-workspace-key
	// data directory (~/.wallfacer/data/<key>/). Empty when no workspace is
	// configured. Used by workspace-level (non-task) blob storage such as the
	// whiteboard scene.
	scopedDataDir string
	envFile       string
	startTime     time.Time
	reg           *metrics.Registry

	oauthManager *oauth.Manager

	diffCache *diffCache
	fileIndex *fileIndex
	spanCache spanStatsCache

	// explorerCommitMu serializes explorer auto-commits per workspace so a
	// burst of rapid saves queues on the git index instead of colliding on
	// index.lock and silently dropping a commit. Keyed by resolved workspace
	// path; values are *sync.Mutex.
	explorerCommitMu sync.Map

	// cachedMaxParallel caches the configured parallel task limit so that
	// maxConcurrentTasks does not re-parse the env file on every call.
	// Invalidate on env config update.
	cachedMaxParallel *lazy[int]

	// groupLimitsMu guards groupLimits, the in-memory cache of per-workspace-
	// group concurrency overrides loaded from workspace-groups.json. Refreshed
	// by reloadGroupLimits on startup and after each group save.
	groupLimitsMu sync.RWMutex
	groupLimits   map[string]*int

	agentSession    *agentsession.Runtime
	commandRegistry *agentsession.CommandRegistry

	sandboxTestMu     sync.RWMutex
	sandboxTestPassed map[harness.ID]bool
}

// NewHandler constructs a Handler with the given dependencies.
func NewHandler(s *store.Store, r runner.Interface, configDir string, workspaces []string, reg *metrics.Registry) *Handler {
	wsMgr := (*workspace.Manager)(nil)
	if r != nil {
		wsMgr = r.WorkspaceManager()
	}
	if wsMgr == nil {
		wsMgr = workspace.NewStatic(s, workspaces)
	}
	h := &Handler{
		store:      s,
		workspace:  wsMgr,
		runner:     r,
		configDir:  configDir,
		workspaces: workspaces,
		envFile:    r.EnvFile(),
		diffCache:  newDiffCache(),
		fileIndex:  newFileIndex(),
		startTime:  time.Now(),
		reg:        reg,
		sandboxTestPassed: map[harness.ID]bool{
			harness.Claude: false,
			harness.Codex:  false,
		},
	}
	oauthMgr := oauth.NewManager()
	oauthMgr.TokenWriter = newOAuthTokenWriter(h.envFile)
	h.oauthManager = oauthMgr
	h.cachedMaxParallel = newLazy(func() int {
		cfg, err := envconfig.Parse(h.envFile)
		limit := constants.DefaultMaxConcurrentTasks
		if err == nil && cfg.MaxParallelTasks > 0 {
			limit = cfg.MaxParallelTasks
		}
		// Parallelism caps to 1 unless the user explicitly opted into
		// more. The claude/codex CLIs share ~/.claude and ~/.codex state
		// (session dir, settings cache) across concurrent invocations —
		// running more than one at a time can race on the shared SQLite
		// settings DB and statsig telemetry files. Users who have verified
		// their CLI tolerates parallel runs can override via
		// WALLFACER_MAX_PARALLEL=N.
		if h.runner != nil && cfg.MaxParallelTasks <= 0 {
			return 1
		}
		return limit
	})
	// Populate the per-group concurrency override cache from disk.
	h.reloadGroupLimits()
	// Initialize handler state from the current workspace snapshot.
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

// SetAgentSession sets the agent-session runtime for agent-session operations.
// Called by the server after both the handler and runtime are constructed.
func (h *Handler) SetAgentSession(p *agentsession.Runtime) {
	h.agentSession = p
	h.commandRegistry = agentsession.NewCommandRegistry()
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

// activeDataKey returns the active workspace's stable storage key (DataKey),
// which addresses both data/<key>/ and the agent-session storage directory.
// Unlike recomputing the key from the current folder paths, this stays stable
// when a workspace's folders are edited, so transcripts and usage stay attached.
// Returns "" when no workspace is active.
func (h *Handler) activeDataKey() string {
	if h.workspace == nil {
		h.snapshotMu.RLock()
		dir := h.scopedDataDir
		h.snapshotMu.RUnlock()
		if dir == "" {
			return ""
		}
		return filepath.Base(dir)
	}
	return h.workspace.Snapshot().Key
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
	h.scopedDataDir = snap.ScopedDataDir
	h.snapshotMu.Unlock()

	// Update the agent session's workspaces when the workspace group changes.
	if h.agentSession != nil {
		h.agentSession.UpdateWorkspaces(snap.Workspaces, snap.Key)
	}
}

// reloadGroupLimits re-reads workspace-groups.json and rebuilds the in-memory
// groupLimits cache. Safe to call concurrently; a read error collapses the
// cache to empty (so callers fall back to the env-file default). Called at
// handler construction and from UpdateConfig after each SaveGroups.
func (h *Handler) reloadGroupLimits() {
	groups, err := workspace.LoadGroups(h.configDir)
	limits := make(map[string]*int, len(groups))
	if err == nil {
		for _, g := range groups {
			if g.MaxParallel == nil {
				continue
			}
			limits[workspace.GroupKey(g.Folders)] = g.MaxParallel
		}
	}
	h.groupLimitsMu.Lock()
	h.groupLimits = limits
	h.groupLimitsMu.Unlock()
}

// currentGroupParallelLimit returns the per-group concurrency override for
// the currently viewed workspace group, if one is set. The second return
// value reports whether an override applies; when false, callers should fall
// back to the env-file default. A nil limit means "inherit the env-file
// default"; 0 means "unlimited for this group"; positive means "hard cap".
func (h *Handler) currentGroupParallelLimit() (int, bool) {
	ws := h.currentWorkspaces()
	if len(ws) == 0 {
		return 0, false
	}
	key := workspace.GroupKey(ws)
	h.groupLimitsMu.RLock()
	v, ok := h.groupLimits[key]
	h.groupLimitsMu.RUnlock()
	if !ok || v == nil {
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

// countGlobalInProgress returns the number of non-test in-progress tasks in
// the currently viewed workspace group. Parallel limits
// (WALLFACER_MAX_PARALLEL) are scoped per group so each group has its own
// independent concurrency budget; tasks still running in other groups after
// a switch do not consume this budget.
func (h *Handler) countGlobalInProgress() int {
	s, ok := h.currentStore()
	if !ok || s == nil {
		return 0
	}
	return s.CountRegularInProgress()
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

// setSandboxTestPassed records whether the given sandbox type has passed its
// connectivity test. Protected by sandboxTestMu for concurrent access.
func (h *Handler) setSandboxTestPassed(sb harness.ID, passed bool) {
	s := normalizeSandbox(string(sb))
	h.sandboxTestMu.Lock()
	h.sandboxTestPassed[s] = passed
	h.sandboxTestMu.Unlock()
}

// sandboxTestPassedState reports whether the given sandbox type has passed
// its connectivity test.
func (h *Handler) sandboxTestPassedState(sb harness.ID) bool {
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
		h.setSandboxTestPassed(harness.Codex, true)
	}
}
