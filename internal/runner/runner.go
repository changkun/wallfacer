package runner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/envutil"
	"changkun.de/x/wallfacer/internal/metrics"
	"changkun.de/x/wallfacer/internal/pkg/circuitbreaker"
	"changkun.de/x/wallfacer/internal/pkg/keyedmu"
	"changkun.de/x/wallfacer/internal/pkg/pubsub"
	"changkun.de/x/wallfacer/internal/pkg/syncmap"
	"changkun.de/x/wallfacer/internal/pkg/trackedwg"
	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
	"changkun.de/x/wallfacer/internal/workspace"
	"github.com/google/uuid"
)

// ListContainers returns structured info for each wallfacer container known
// to the sandbox backend. Supports both Podman and Docker JSON output formats.
func (r *Runner) ListContainers() ([]sandbox.ContainerInfo, error) {
	return r.backend.List(context.Background())
}

// ContainerName returns the active container name for a task.
// It first checks the in-memory map populated when a container is launched,
// then falls back to scanning the container list and matching by task ID label.
// Returns an empty string if no container is found.
func (r *Runner) ContainerName(taskID uuid.UUID) string {
	if name, ok := r.taskContainers.Get(taskID); ok {
		return name
	}
	// Fallback: search all wallfacer containers by label.
	containers, err := r.ListContainers()
	if err != nil {
		return ""
	}
	for _, c := range containers {
		if c.TaskID == taskID.String() {
			return c.Name
		}
	}
	return ""
}

// TaskLogReader returns a new liveLogReader for the currently-running turn
// of the given task. Returns nil when no live log is active (the task is
// between turns or not running). Each call returns an independent reader
// positioned at the start of the current turn's output.
func (r *Runner) TaskLogReader(taskID uuid.UUID) *LiveLogReader {
	ll, ok := r.liveLogs.Load(taskID)
	if !ok {
		return nil
	}
	return ll.NewReader()
}

// RunnerConfig holds all configuration needed to construct a Runner.
//
//nolint:revive // name stutters but renaming would be too invasive
type RunnerConfig struct {
	Command          string
	SandboxImage     string
	EnvFile          string
	Workspaces       []string // workspace directory paths
	WorktreesDir     string
	InstructionsPath string
	CodexAuthPath    string           // host path to codex auth cache directory (default: ~/.codex)
	SandboxBackend   string           // "local" (default) — selects the SandboxBackend implementation
	ContainerNetwork string           // --network value for task containers (empty = read from env file, fallback "host")
	ContainerCPUs    string           // --cpus value for task containers (empty = read from env file, no limit)
	ContainerMemory  string           // --memory value for task containers (empty = read from env file, no limit)
	TmpDir           string           // base dir for ephemeral files bind-mounted into containers (must be Docker-accessible)
	Prompts          *prompts.Manager // prompt template manager; nil = use prompts.Default
	WorkspaceManager *workspace.Manager
	Reg              *metrics.Registry // optional metrics registry; nil disables metric collection
}

// Runner orchestrates agent container execution for tasks.
// It manages worktree isolation, container lifecycle, and the commit pipeline.
type Runner struct {
	store                  *store.Store
	storeMu                sync.RWMutex
	wsKey                  string   // workspace key of the currently viewed group (guarded by storeMu)
	taskWSKey              sync.Map // uuid.UUID → string: maps task IDs to their workspace group key
	command                string
	sandboxImage           string
	envFile                string
	workspaces             []string
	worktreesDir           string
	tmpDir                 string
	instructionsPath       string
	workspaceManager       *workspace.Manager
	codexAuthPath          string
	containerNetwork       string                           // --network override; empty = read from env file
	containerCPUs          string                           // --cpus override; empty = read from env file
	containerMemory        string                           // --memory override; empty = read from env file
	promptsMgr             *prompts.Manager                 // prompt template manager
	worktreeMu             sync.Mutex                       // serializes all worktree filesystem operations on worktreesDir
	repoMu                 keyedmu.Map[string]              // per-repo mutex for serializing rebase+merge
	taskContainers         *containerRegistry               // taskID → container name
	refineContainers       *containerRegistry               // taskID → refinement container name
	ideateContainer        *containerRegistry               // singleton: ideation container name
	liveLogs               syncmap.Map[uuid.UUID, *liveLog] // live log buffers for in-progress turns
	oversightMu            keyedmu.Map[string]              // per-task mutex for serializing oversight generation
	containerCB            *circuitbreaker.Breaker          // circuit breaker for container launch operations
	backend                sandbox.Backend                  // pluggable sandbox backend (local podman/docker, future: k8s)
	backgroundWg           trackedwg.WaitGroup              // tracks fire-and-forget background goroutines
	stopReasonMu           sync.RWMutex
	onStopReason           func(taskID uuid.UUID, stopReason string)
	autosubmitFn           func() bool    // returns true when auto-submit is enabled
	ideationExploitRatioFn func() float64 // returns the current exploitation ratio (0–1)

	// Board context cache: avoids redundant store.ListTasks calls on every turn
	// when no task has changed since the last generation. Keyed by
	// (boardChangeSeq, selfTaskID): a cache hit means no store mutation
	// occurred and the requesting task hasn't changed, so the serialized
	// board.json and sibling mount map can be reused as-is.
	boardCache struct {
		mu         sync.Mutex
		seq        uint64                       // snapshot of boardChangeSeq at generation time
		selfTaskID uuid.UUID                    // which task the cached result was built for
		json       []byte                       // serialized board.json
		mounts     map[string]map[string]string // shortID → (repoPath → worktreePath)
	}
	boardChangeSeq      atomic.Uint64  // incremented on every store notification
	shutdownCh          chan struct{}  // closed by Shutdown to stop the subscription goroutine
	shutdownOnce        sync.Once      // ensures Shutdown runs at most once
	boardSubscriptionWg sync.WaitGroup // tracks the board-cache-invalidator goroutine only
	shutdownCtx         context.Context
	shutdownCancel      context.CancelFunc
	reg                 *metrics.Registry // optional; nil disables metric collection
}

// ShutdownCtx returns the runner's shutdown context. It is cancelled when
// Shutdown() is called, allowing background goroutines and store operations
// to be cancelled on graceful shutdown.
func (r *Runner) ShutdownCtx() context.Context {
	return r.shutdownCtx
}

// ContainerCircuitAllow returns true when the container circuit breaker
// permits a new launch. Use this in handlers before promoting a task to
// in-progress to avoid firing launches that would immediately fail.
func (r *Runner) ContainerCircuitAllow() bool {
	return r.containerCB.Allow()
}

// ContainerCircuitOpen reports whether the container launch circuit breaker
// is currently open (runtime considered unavailable). The inverse of ContainerCircuitAllow.
func (r *Runner) ContainerCircuitOpen() bool {
	return !r.ContainerCircuitAllow()
}

// RecordContainerFailure records a single container launch failure against
// the circuit breaker. Exposed primarily for testing.
func (r *Runner) RecordContainerFailure() {
	r.containerCB.RecordFailure()
}

// ContainerCircuitState returns the human-readable state of the container
// circuit breaker ("closed", "open", or "half-open").
func (r *Runner) ContainerCircuitState() string {
	return r.containerCB.State().String()
}

// ContainerCircuitFailures returns the current consecutive failure count of
// the container circuit breaker.
func (r *Runner) ContainerCircuitFailures() int {
	return r.containerCB.Failures()
}

// WaitBackground blocks until all fire-and-forget background goroutines
// (RunBackground, oversight generation, etc.) have completed. Intended for
// use in tests to avoid cleanup races with goroutines that write to
// temporary directories.
func (r *Runner) WaitBackground() {
	r.backgroundWg.Wait()
}

// PendingGoroutines returns a sorted slice of labels for all background
// goroutines that have been started but not yet completed.
func (r *Runner) PendingGoroutines() []string {
	return r.backgroundWg.Pending()
}

// SetStopReasonHandler registers a callback that is notified when a non-terminal
// stop_reason is encountered (for example max_tokens).
func (r *Runner) SetStopReasonHandler(fn func(taskID uuid.UUID, stopReason string)) {
	r.stopReasonMu.Lock()
	r.onStopReason = fn
	r.stopReasonMu.Unlock()
}

// notifyStopReason invokes the registered stop-reason callback (if any) under
// a read lock. Used to inform handlers when max_tokens triggers auto-continue.
func (r *Runner) notifyStopReason(taskID uuid.UUID, stopReason string) {
	r.stopReasonMu.RLock()
	fn := r.onStopReason
	r.stopReasonMu.RUnlock()
	if fn != nil {
		fn(taskID, stopReason)
	}
}

// SetAutosubmitFunc registers a callback that reports whether auto-submit is
// currently enabled. The ideation pipeline uses this to decide whether to
// create backlog tasks immediately or wait for manual approval.
func (r *Runner) SetAutosubmitFunc(fn func() bool) {
	r.autosubmitFn = fn
}

// isAutosubmitEnabled returns whether auto-submit is currently enabled.
// Defaults to true for backward compatibility when no callback is registered.
func (r *Runner) isAutosubmitEnabled() bool {
	if r.autosubmitFn == nil {
		return true // default to auto-create for backward compatibility
	}
	return r.autosubmitFn()
}

// SetIdeationExploitRatioFunc registers a callback that returns the current
// exploitation ratio (0–1) for the ideation prompt. Default is 0.8.
func (r *Runner) SetIdeationExploitRatioFunc(fn func() float64) {
	r.ideationExploitRatioFn = fn
}

// ideationExploitRatio returns the current exploitation ratio (0-1) for ideation.
// Defaults to 0.8 when no callback is registered.
func (r *Runner) ideationExploitRatio() float64 {
	if r.ideationExploitRatioFn == nil {
		return 0.8
	}
	return r.ideationExploitRatioFn()
}

// Shutdown waits for all tracked background goroutines to complete before
// returning. Call this after the HTTP server has stopped accepting new requests
// to ensure that oversight generation, title generation, and other
// fire-and-forget work finishes before the process exits.
// In-progress task containers are intentionally left running; they continue
// to completion independently and will be recovered on the next server start.
// must be called at most once.
func (r *Runner) Shutdown() {
	r.shutdownOnce.Do(func() {
		r.shutdownCancel()
		// Stop all per-task worker containers before waiting for background goroutines.
		if wm, ok := r.backend.(sandbox.WorkerManager); ok {
			wm.ShutdownWorkers()
		}
		// Signal the board-cache-invalidator goroutine to exit and wait for it.
		close(r.shutdownCh)
		r.boardSubscriptionWg.Wait()

		done := make(chan struct{})
		go func() {
			r.backgroundWg.Wait()
			close(done)
		}()

		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if pending := r.backgroundWg.Pending(); len(pending) > 0 {
					logger.Main.Info("shutdown waiting for background goroutines", "pending", strings.Join(pending, ", "))
				}
			}
		}
	})
}

// RunBackground launches Run in a background goroutine tracked by backgroundWg.
// Callers (handlers, autopilot) should use this instead of a bare "go r.Run(...)"
// so that WaitBackground can drain all outstanding work — particularly useful
// in tests to prevent cleanup races with temp-dir removal.
func (r *Runner) RunBackground(taskID uuid.UUID, prompt, sessionID string, resumedFromWaiting bool) {
	// Capture the current workspace key at dispatch time so the task uses the
	// correct store even if the user switches workspaces during execution.
	wsKey := r.currentWSKey()
	r.taskWSKey.Store(taskID, wsKey)
	if r.workspaceManager != nil {
		r.workspaceManager.IncrementTaskCount(wsKey)
	}

	r.backgroundWg.Go("run:"+taskID.String()[:8], func() {
		defer r.taskWSKey.Delete(taskID)
		defer func() {
			if r.workspaceManager != nil {
				r.workspaceManager.DecrementAndCleanup(wsKey)
			}
		}()
		// Note: StopTaskWorker is NOT deferred here because title, oversight,
		// and commit agents run in separate background goroutines after Run()
		// returns and need the worker container alive. Worker cleanup happens
		// in CleanupWorktrees (commit pipeline), CancelTask, and Shutdown.
		r.Run(taskID, prompt, sessionID, resumedFromWaiting)
	})
}

// SyncWorktreesBackground launches SyncWorktrees in a background goroutine
// tracked by backgroundWg so that WaitBackground can drain it before cleanup.
// The optional onDone callbacks are called after SyncWorktrees returns.
func (r *Runner) SyncWorktreesBackground(taskID uuid.UUID, sessionID string, prevStatus store.TaskStatus, onDone ...func()) {
	r.backgroundWg.Go("sync:"+taskID.String()[:8], func() {
		r.SyncWorktrees(taskID, sessionID, prevStatus)
		for _, fn := range onDone {
			fn()
		}
	})
}

// RunRefinementBackground launches RunRefinement in a background goroutine
// tracked by backgroundWg so that WaitBackground can drain it before cleanup.
func (r *Runner) RunRefinementBackground(taskID uuid.UUID, userInstructions string) {
	r.backgroundWg.Go("refine:"+taskID.String()[:8], func() {
		r.RunRefinement(taskID, userInstructions)
	})
}

// GenerateOversightBackground launches GenerateOversight in a background goroutine
// tracked by backgroundWg so that WaitBackground can drain it before cleanup.
func (r *Runner) GenerateOversightBackground(taskID uuid.UUID) {
	r.backgroundWg.Go("oversight:"+taskID.String()[:8], func() {
		r.GenerateOversight(taskID)
	})
}

// GenerateTitleBackground launches GenerateTitle in a background goroutine
// tracked by backgroundWg so that WaitBackground can drain it before cleanup.
func (r *Runner) GenerateTitleBackground(taskID uuid.UUID, prompt string) {
	r.backgroundWg.Go("title:"+taskID.String()[:8], func() {
		r.GenerateTitle(taskID, prompt)
	})
}

// NewRunner constructs a Runner from the given store and config. The returned
// Runner is ready for use: it has an initialised circuit breaker, sandbox
// backend, and a background goroutine watching for store mutations to
// invalidate the board context cache. Call Shutdown() to drain background work.
func NewRunner(s *store.Store, cfg RunnerConfig) *Runner {
	mgr := cfg.Prompts
	if mgr == nil {
		mgr = prompts.Default
	}
	r := &Runner{
		store:            s,
		command:          cfg.Command,
		sandboxImage:     cfg.SandboxImage,
		envFile:          cfg.EnvFile,
		workspaces:       cfg.Workspaces,
		worktreesDir:     cfg.WorktreesDir,
		tmpDir:           cfg.TmpDir,
		instructionsPath: cfg.InstructionsPath,
		codexAuthPath:    strings.TrimSpace(cfg.CodexAuthPath),
		containerNetwork: cfg.ContainerNetwork,
		containerCPUs:    cfg.ContainerCPUs,
		containerMemory:  cfg.ContainerMemory,
		promptsMgr:       mgr,
		workspaceManager: cfg.WorkspaceManager,
		taskContainers:   &containerRegistry{},
		refineContainers: &containerRegistry{},
		ideateContainer:  &containerRegistry{},
		shutdownCh:       make(chan struct{}),
	}
	r.shutdownCtx, r.shutdownCancel = context.WithCancel(context.Background())

	// Initialise container circuit breaker.
	// Defaults: 5 consecutive failures trip the breaker; it stays open for
	// 30 s before allowing a single probe (half-open).
	// Both values can be overridden via environment variables.
	cbThreshold := envutil.IntMin("WALLFACER_CONTAINER_CB_THRESHOLD", constants.DefaultCBThreshold, 1)
	cbOpenSec := envutil.IntMin("WALLFACER_CONTAINER_CB_OPEN_SECONDS", 30, 1)
	r.containerCB = circuitbreaker.New(cbThreshold, time.Duration(cbOpenSec)*time.Second)
	localCfg := sandbox.LocalBackendConfig{
		EnableTaskWorkers: true, // default; overridden by envconfig if available
		Reg:               cfg.Reg,
	}
	// Read WALLFACER_TASK_WORKERS from the env file if available.
	if cfg.EnvFile != "" {
		if parsed, err := envconfig.Parse(cfg.EnvFile); err == nil {
			localCfg.EnableTaskWorkers = parsed.TaskWorkers
		}
	}
	switch cfg.SandboxBackend {
	case "", "local":
		r.backend = sandbox.NewLocalBackend(r.command, localCfg)
	default:
		logger.Runner.Warn("unknown sandbox backend, falling back to local", "backend", cfg.SandboxBackend)
		r.backend = sandbox.NewLocalBackend(r.command, localCfg)
	}
	r.reg = cfg.Reg

	if r.workspaceManager != nil {
		snap := r.workspaceManager.Snapshot()
		r.applyWorkspaceSnapshot(snap)
	}

	// Subscribe to store changes to drive the board-context cache invalidation.
	// Each store mutation increments boardChangeSeq so generateBoardContextAndMounts
	// knows when a cached result is stale.
	// Tracked by boardSubscriptionWg (not backgroundWg) so that WaitBackground()
	// remains unaffected — tests often call WaitBackground() directly in the test
	// body before cleanup has a chance to close shutdownCh.
	// Guard against nil store (some tests construct a Runner without a store).
	r.startBoardSubscriptionLoop(s)

	return r
}

// WorkspaceManager returns the runner's workspace manager.
func (r *Runner) WorkspaceManager() *workspace.Manager {
	return r.workspaceManager
}

// applyWorkspaceSnapshot atomically replaces the runner's store, workspace paths,
// and instructions path from a workspace manager snapshot. Called when the active
// workspace group changes at runtime.
func (r *Runner) applyWorkspaceSnapshot(s workspace.Snapshot) {
	r.storeMu.Lock()
	r.store = s.Store
	r.wsKey = s.Key
	r.workspaces = s.Workspaces
	r.instructionsPath = s.InstructionsPath
	r.storeMu.Unlock()
}

// currentStore returns the runner's active store under a read lock.
// The store may change when workspaces are switched at runtime.
func (r *Runner) currentStore() *store.Store {
	r.storeMu.RLock()
	defer r.storeMu.RUnlock()
	return r.store
}

// currentWSKey returns the workspace key of the currently viewed group.
func (r *Runner) currentWSKey() string {
	r.storeMu.RLock()
	defer r.storeMu.RUnlock()
	return r.wsKey
}

// currentWorkspaces returns the runner's workspace paths under a read lock.
// The slice may change when workspaces are switched at runtime via
// applyWorkspaceSnapshot, so callers outside storeMu must use this accessor
// instead of reading r.workspaces directly.
func (r *Runner) currentWorkspaces() []string {
	r.storeMu.RLock()
	defer r.storeMu.RUnlock()
	return r.workspaces
}

// currentInstructionsPath returns the runner's instructions file path under
// a read lock. Like currentWorkspaces, this is updated by
// applyWorkspaceSnapshot and must not be read without the lock.
func (r *Runner) currentInstructionsPath() string {
	r.storeMu.RLock()
	defer r.storeMu.RUnlock()
	return r.instructionsPath
}

// taskStore returns the store for the workspace group that owns the given task.
// It first checks the task-to-group mapping, then falls back to the currently
// viewed store if the mapping is missing or the group is no longer active.
func (r *Runner) taskStore(taskID uuid.UUID) *store.Store {
	if key, ok := r.taskWSKey.Load(taskID); ok {
		if r.workspaceManager != nil {
			if s, ok := r.workspaceManager.StoreForKey(key.(string)); ok {
				return s
			}
		}
	}
	return r.currentStore()
}

// startBoardSubscriptionLoop spawns a goroutine that listens for store task
// mutations and workspace switches, incrementing boardChangeSeq on each event
// so that generateBoardContextAndMounts can detect stale cache entries.
func (r *Runner) startBoardSubscriptionLoop(initial *store.Store) {
	r.boardSubscriptionWg.Add(1)
	go func() {
		defer r.boardSubscriptionWg.Done()

		var (
			wsSubID int
			wsCh    <-chan workspace.Snapshot
			subID   int
			subCh   <-chan pubsub.Sequenced[store.TaskDelta]
			cur     = initial
		)
		if r.workspaceManager != nil {
			wsSubID, wsCh = r.workspaceManager.Subscribe()
			defer r.workspaceManager.Unsubscribe(wsSubID)
			cur = r.workspaceManager.Snapshot().Store
		}
		subscribeStore := func(s *store.Store) {
			if cur != nil && subCh != nil {
				cur.Unsubscribe(subID)
			}
			cur = s
			subCh = nil
			subID = 0
			if s != nil {
				subID, subCh = s.Subscribe()
			}
		}
		subscribeStore(cur)
		defer func() {
			if cur != nil && subCh != nil {
				cur.Unsubscribe(subID)
			}
		}()

		for {
			select {
			case <-r.shutdownCh:
				return
			case <-subCh:
				r.boardChangeSeq.Add(1)
			case snap, ok := <-wsCh:
				if !ok {
					wsCh = nil
					continue
				}
				r.applyWorkspaceSnapshot(snap)
				r.boardChangeSeq.Add(1)
				subscribeStore(snap.Store)
			}
		}
	}()
}

// resolvedContainerNetwork returns the --network value to use for task containers.
// Priority: explicit RunnerConfig value > WALLFACER_CONTAINER_NETWORK from env file > "host".
func (r *Runner) resolvedContainerNetwork() string {
	if r.containerNetwork != "" {
		return r.containerNetwork
	}
	if r.envFile != "" {
		if cfg, err := envconfig.Parse(r.envFile); err == nil && cfg.ContainerNetwork != "" {
			return cfg.ContainerNetwork
		}
	}
	return "host"
}

// resolvedContainerCPUs returns the --cpus value to use for task containers.
// Priority: explicit RunnerConfig value > WALLFACER_CONTAINER_CPUS from env file > "" (no limit).
func (r *Runner) resolvedContainerCPUs() string {
	if r.containerCPUs != "" {
		return r.containerCPUs
	}
	if r.envFile != "" {
		if cfg, err := envconfig.Parse(r.envFile); err == nil {
			return cfg.ContainerCPUs
		}
	}
	return ""
}

// resolvedContainerMemory returns the --memory value to use for task containers.
// Priority: explicit RunnerConfig value > WALLFACER_CONTAINER_MEMORY from env file > "" (no limit).
func (r *Runner) resolvedContainerMemory() string {
	if r.containerMemory != "" {
		return r.containerMemory
	}
	if r.envFile != "" {
		if cfg, err := envconfig.Parse(r.envFile); err == nil {
			return cfg.ContainerMemory
		}
	}
	return ""
}

// Command returns the container runtime binary path (podman/docker).
func (r *Runner) Command() string {
	return r.command
}

// EnvFile returns the path to the env file used for containers.
func (r *Runner) EnvFile() string {
	return r.envFile
}

// WorktreesDir returns the directory where task worktrees are created.
func (r *Runner) WorktreesDir() string {
	return r.worktreesDir
}

// TmpDir returns the base directory for ephemeral files bind-mounted into containers.
func (r *Runner) TmpDir() string {
	return r.tmpDir
}

// InstructionsPath returns the host path mounted as /workspace/AGENTS.md.
func (r *Runner) InstructionsPath() string {
	if r.workspaceManager != nil {
		return r.workspaceManager.InstructionsPath()
	}
	return r.currentInstructionsPath()
}

// Prompts returns the prompt template Manager used by this runner.
func (r *Runner) Prompts() *prompts.Manager {
	return r.promptsMgr
}

// SandboxImage returns the container image used for task execution.
func (r *Runner) SandboxImage() string {
	return r.sandboxImage
}

// SandboxBackend returns the sandbox backend used for container operations.
func (r *Runner) SandboxBackend() sandbox.Backend {
	return r.backend
}

// HasHostCodexAuth reports whether a usable host Codex auth cache exists.
func (r *Runner) HasHostCodexAuth() bool {
	ok, _ := r.HostCodexAuthStatus(time.Now())
	return ok
}

// CodexAuthPath returns the validated host path used for codex auth cache
// mounts, or an empty string when unavailable.
func (r *Runner) CodexAuthPath() string {
	return r.hostCodexAuthPath()
}

// HostCodexAuthStatus validates the host codex auth cache and returns whether
// it appears usable for sandbox auth, plus a reason when unusable.
func (r *Runner) HostCodexAuthStatus(now time.Time) (bool, string) {
	path := r.hostCodexAuthPath()
	if path == "" {
		return false, "host codex auth cache not found"
	}
	raw, err := os.ReadFile(filepath.Join(path, "auth.json"))
	if err != nil {
		return false, "failed to read host codex auth cache"
	}
	var parsed struct {
		AuthMode string `json:"auth_mode"`
		Tokens   struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return false, "host codex auth cache is malformed"
	}
	access := strings.TrimSpace(parsed.Tokens.AccessToken)
	refresh := strings.TrimSpace(parsed.Tokens.RefreshToken)
	if access == "" && refresh == "" {
		return false, "host codex auth cache has no tokens"
	}
	if access != "" && isJWTExpired(access, now) && refresh == "" {
		return false, "host codex access token is expired and no refresh token is present"
	}
	return true, ""
}

// Workspaces returns the list of configured workspace paths.
// When a workspace manager is present, it delegates to the manager (which
// has its own lock). Otherwise it reads the runner's field under storeMu
// to avoid racing with applyWorkspaceSnapshot.
func (r *Runner) Workspaces() []string {
	if r.workspaceManager != nil {
		return r.workspaceManager.Workspaces()
	}
	ws := r.currentWorkspaces()
	if len(ws) == 0 {
		return nil
	}
	return ws
}

// hostCodexAuthPath validates and returns the host Codex auth cache directory
// path, or "" if the path is empty, doesn't exist, or lacks an auth.json file.
func (r *Runner) hostCodexAuthPath() string {
	path := strings.TrimSpace(r.codexAuthPath)
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return ""
	}
	authFile := filepath.Join(path, "auth.json")
	if stat, err := os.Stat(authFile); err == nil && !stat.IsDir() {
		return path
	}
	return ""
}

// isJWTExpired checks whether a JWT's "exp" claim is at or past now.
// Returns false for malformed tokens (non-3-segment, invalid base64, missing exp).
func isJWTExpired(jwt string, now time.Time) bool {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp <= 0 {
		return false
	}
	return now.Unix() >= claims.Exp
}

// repoLock returns a per-repo mutex, creating one on first access.
// Used to serialize rebase+merge operations on the same repository.
func (r *Runner) repoLock(repoPath string) *sync.Mutex {
	return r.repoMu.Get(repoPath)
}

// oversightLock returns the per-task mutex for serialising oversight generation.
// The mutex is created on first access and stored in oversightMu.
func (r *Runner) oversightLock(taskID uuid.UUID) *sync.Mutex {
	return r.oversightMu.Get(taskID.String())
}

// RefineContainerName returns the active refinement container name for a task.
// Returns an empty string if no refinement container is running.
func (r *Runner) RefineContainerName(taskID uuid.UUID) string {
	if name, ok := r.refineContainers.Get(taskID); ok {
		return name
	}
	return ""
}

// KillContainer sends a kill signal to the running container for a task.
// Kill goes through the SandboxHandle when registered; otherwise it is a no-op
// (container already exited or was never launched).
// Safe to call when no container is running — errors are silently ignored.
func (r *Runner) KillContainer(taskID uuid.UUID) {
	if h := r.taskContainers.GetHandle(taskID); h != nil {
		_ = h.Kill()
	}
}

// WorkerStats returns aggregate worker lifecycle statistics. Returns an empty
// result when the backend does not support worker management.
func (r *Runner) WorkerStats() sandbox.WorkerStatsInfo {
	if wm, ok := r.backend.(sandbox.WorkerManager); ok {
		return wm.WorkerStats()
	}
	return sandbox.WorkerStatsInfo{}
}

// StopTaskWorker stops the per-task worker container for the given task, if
// the backend supports worker management. No-op when the backend does not
// implement sandbox.WorkerManager or when no worker exists for the task.
func (r *Runner) StopTaskWorker(taskID uuid.UUID) {
	if wm, ok := r.backend.(sandbox.WorkerManager); ok {
		wm.StopTaskWorker(taskID.String())
	}
}

// KillRefineContainer sends a kill signal to the running refinement container.
// Kill goes through the SandboxHandle when registered; otherwise it is a no-op.
// Safe to call when no refinement container is running.
func (r *Runner) KillRefineContainer(taskID uuid.UUID) {
	if h := r.refineContainers.GetHandle(taskID); h != nil {
		_ = h.Kill()
	}
}

// IdeateContainerName returns the name of the currently running ideation container,
// or an empty string if no ideation is in progress.
func (r *Runner) IdeateContainerName() string {
	if name, ok := r.ideateContainer.GetSingleton(); ok {
		return name
	}
	return ""
}

// KillIdeateContainer sends a kill signal to the running ideation container.
// Kill goes through the SandboxHandle when registered; otherwise it is a no-op.
// Safe to call when no ideation container is running.
func (r *Runner) KillIdeateContainer() {
	if h := r.ideateContainer.GetSingletonHandle(); h != nil {
		_ = h.Kill()
	}
}
