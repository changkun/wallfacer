// Package runner orchestrates sandbox containers for task execution.
package runner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/metrics"
	"changkun.de/wallfacer/internal/store"
	"changkun.de/wallfacer/internal/workspace"
	"changkun.de/wallfacer/prompts"
	"github.com/google/uuid"
)

// DefaultCBThreshold is the number of consecutive container launch failures
// required to open the circuit breaker. It can be overridden at startup via
// WALLFACER_CONTAINER_CB_THRESHOLD.
const DefaultCBThreshold = 5

// trackedWg is a sync.WaitGroup that also records the label of each
// outstanding goroutine so that Shutdown can report what it is waiting for.
type trackedWg struct {
	mu      sync.Mutex
	pending map[string]int
	wg      sync.WaitGroup
}

// Add increments the wait group counter and records label as pending.
func (t *trackedWg) Add(label string) {
	t.mu.Lock()
	if t.pending == nil {
		t.pending = make(map[string]int)
	}
	t.pending[label]++
	t.mu.Unlock()
	t.wg.Add(1)
}

// Done decrements the wait group counter and removes label from pending.
func (t *trackedWg) Done(label string) {
	t.mu.Lock()
	t.pending[label]--
	if t.pending[label] <= 0 {
		delete(t.pending, label)
	}
	t.mu.Unlock()
	t.wg.Done()
}

// Wait blocks until all tracked goroutines have called Done.
func (t *trackedWg) Wait() {
	t.wg.Wait()
}

// Pending returns a sorted slice of labels (with counts >1 shown as "label×N")
// for all goroutines that have not yet called Done.
func (t *trackedWg) Pending() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]string, 0, len(t.pending))
	for label, count := range t.pending {
		if count == 1 {
			result = append(result, label)
		} else {
			result = append(result, fmt.Sprintf("%s×%d", label, count))
		}
	}
	sort.Strings(result)
	return result
}

// ContainerInfo represents a single sandbox container returned by ListContainers.
type ContainerInfo struct {
	ID        string `json:"id"`         // short container ID
	Name      string `json:"name"`       // full container name (e.g. wallfacer-<slug>-<uuid8>)
	TaskID    string `json:"task_id"`    // task UUID from label, empty if not a task container
	TaskTitle string `json:"task_title"` // task title populated by the handler from the store
	Image     string `json:"image"`      // image name
	State     string `json:"state"`      // running | exited | paused | …
	Status    string `json:"status"`     // human-readable status (e.g. "Up 5 minutes")
	CreatedAt int64  `json:"created_at"` // unix timestamp
}

// containerJSON is used to unmarshal `podman/docker ps --format json` output.
// Podman and Docker use different JSON formats:
//   - Podman outputs a JSON array; Docker outputs one JSON object per line (NDJSON).
//   - Podman's "Names" is []string; Docker's "Names" is a single string.
//   - Podman's "Created" is int64 (unix timestamp); Docker's "CreatedAt" is a string.
//
// We use json.RawMessage for Names and any for Created to handle both.
type containerJSON struct {
	ID        string            `json:"Id"`
	Names     json.RawMessage   `json:"Names"`
	Image     string            `json:"Image"`
	State     string            `json:"State"`
	Status    string            `json:"Status"`
	Created   any               `json:"Created"`
	CreatedAt string            `json:"CreatedAt"` // Docker uses CreatedAt (string) instead of Created
	Labels    map[string]string `json:"Labels"`    // task metadata labels (wallfacer.task.id, etc.)
}

// name extracts the container name from the Names field, handling both
// Podman ([]string) and Docker (string) formats.
// Returns an error if Names is non-nil but cannot be decoded as either format.
func (c *containerJSON) name() (string, error) {
	if c.Names == nil {
		return "", nil
	}
	// Try []string first (Podman format).
	var names []string
	if err := json.Unmarshal(c.Names, &names); err == nil && len(names) > 0 {
		return strings.TrimPrefix(names[0], "/"), nil
	}
	// Try single string (Docker format).
	var name string
	if err := json.Unmarshal(c.Names, &name); err == nil {
		return strings.TrimPrefix(name, "/"), nil
	}
	return "", fmt.Errorf("containerJSON.name: cannot decode Names field: %s", c.Names)
}

// createdUnix returns the creation time as a unix timestamp.
// Podman provides Created as a numeric unix timestamp; Docker provides it
// as a float or string. We handle both gracefully.
func (c *containerJSON) createdUnix() int64 {
	// Podman: Created is a JSON number (int64 or float64).
	if c.Created != nil {
		switch v := c.Created.(type) {
		case float64:
			return int64(v)
		case json.Number:
			if n, err := v.Int64(); err == nil {
				return n
			}
		}
	}
	return 0
}

// parseContainerList parses the JSON output of `ps --format json`, handling
// both Podman (JSON array) and Docker (NDJSON, one object per line) formats.
func parseContainerList(out []byte) ([]containerJSON, error) {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}

	// Podman: JSON array.
	if trimmed[0] == '[' {
		var containers []containerJSON
		if err := json.Unmarshal(out, &containers); err != nil {
			return nil, fmt.Errorf("parse container list (array): %w", err)
		}
		return containers, nil
	}

	// Docker: NDJSON (one JSON object per line).
	var containers []containerJSON
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] != '{' {
			continue
		}
		var c containerJSON
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("parse container list (ndjson line): %w", err)
		}
		containers = append(containers, c)
	}
	return containers, nil
}

// ListContainers runs `<runtime> ps -a --filter name=wallfacer --format json`
// and returns structured info for each matching container.
// Supports both Podman and Docker JSON output formats.
func (r *Runner) ListContainers() ([]ContainerInfo, error) {
	out, err := exec.Command(r.command, "ps", "-a",
		"--filter", "name=wallfacer",
		"--format", "json",
	).Output()
	if err != nil {
		return nil, err
	}

	raw, err := parseContainerList(out)
	if err != nil {
		return nil, err
	}

	result := make([]ContainerInfo, 0, len(raw))
	for _, c := range raw {
		name, err := c.name()
		if err != nil {
			logger.Runner.Warn("ListContainers: skipping malformed container entry", "error", err)
			continue
		}

		// Primary: extract task UUID from the wallfacer.task.id label.
		// This works regardless of the container name format.
		taskID := ""
		if c.Labels != nil {
			taskID = c.Labels["wallfacer.task.id"]
		}
		// Fallback for containers created without labels (old format wallfacer-<uuid>):
		// try stripping the "wallfacer-" prefix and check if the remainder is a UUID.
		if taskID == "" {
			candidate := strings.TrimPrefix(name, "wallfacer-")
			if candidate != name && isUUID(candidate) {
				taskID = candidate
			}
		}

		result = append(result, ContainerInfo{
			ID:        c.ID,
			Name:      name,
			TaskID:    taskID,
			Image:     c.Image,
			State:     c.State,
			Status:    c.Status,
			CreatedAt: c.createdUnix(),
		})
	}
	return result, nil
}

// isUUID returns true if s looks like a standard UUID (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
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

const (
	maxRebaseRetries   = 3
	defaultTaskTimeout = 60 * time.Minute
)

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
	ContainerNetwork string           // --network value for task containers (empty = read from env file, fallback "host")
	ContainerCPUs    string           // --cpus value for task containers (empty = read from env file, no limit)
	ContainerMemory  string           // --memory value for task containers (empty = read from env file, no limit)
	Prompts          *prompts.Manager // prompt template manager; nil = use prompts.Default
	WorkspaceManager *workspace.Manager
	Reg              *metrics.Registry // optional metrics registry; nil disables metric collection
}

// Runner orchestrates agent container execution for tasks.
// It manages worktree isolation, container lifecycle, and the commit pipeline.
type Runner struct {
	store            *store.Store
	storeMu          sync.RWMutex
	command          string
	sandboxImage     string
	envFile          string
	workspaces       []string
	worktreesDir     string
	instructionsPath string
	workspaceManager *workspace.Manager
	codexAuthPath    string
	containerNetwork string             // --network override; empty = read from env file
	containerCPUs    string             // --cpus override; empty = read from env file
	containerMemory  string             // --memory override; empty = read from env file
	promptsMgr       *prompts.Manager   // prompt template manager
	worktreeMu       sync.Mutex         // serializes all worktree filesystem operations on worktreesDir
	repoMu           sync.Map           // per-repo *sync.Mutex for serializing rebase+merge
	taskContainers   *containerRegistry // taskID → container name
	refineContainers *containerRegistry // taskID → refinement container name
	ideateContainer  *containerRegistry // singleton: ideation container name
	oversightMu      sync.Map           // taskID (string) → *sync.Mutex for serializing oversight generation
	containerCB      *CircuitBreaker    // circuit breaker for container launch operations
	executor         ContainerExecutor  // abstracts container runtime calls for testing
	backgroundWg     trackedWg          // tracks fire-and-forget background goroutines
	stopReasonMu     sync.RWMutex
	onStopReason     func(taskID uuid.UUID, stopReason string)
	autosubmitFn     func() bool // returns true when auto-submit is enabled

	// Board context cache: avoids redundant store.ListTasks calls on every turn
	// when no task has changed since the last generation.
	boardCache struct {
		mu         sync.Mutex
		seq        uint64
		selfTaskID uuid.UUID
		json       []byte
		mounts     map[string]map[string]string
	}
	boardChangeSeq      atomic.Uint64  // incremented on every store notification
	shutdownCh          chan struct{}  // closed by Shutdown to stop the subscription goroutine
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
	return r.containerCB.State()
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

func (r *Runner) isAutosubmitEnabled() bool {
	if r.autosubmitFn == nil {
		return true // default to auto-create for backward compatibility
	}
	return r.autosubmitFn()
}

// Shutdown waits for all tracked background goroutines to complete before
// returning. Call this after the HTTP server has stopped accepting new requests
// to ensure that oversight generation, title generation, and other
// fire-and-forget work finishes before the process exits.
// In-progress task containers are intentionally left running; they continue
// to completion independently and will be recovered on the next server start.
// must be called at most once.
func (r *Runner) Shutdown() {
	r.shutdownCancel()
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
}

// RunBackground launches Run in a background goroutine tracked by backgroundWg.
// Callers (handlers, autopilot) should use this instead of a bare "go r.Run(...)"
// so that WaitBackground can drain all outstanding work — particularly useful
// in tests to prevent cleanup races with temp-dir removal.
func (r *Runner) RunBackground(taskID uuid.UUID, prompt, sessionID string, resumedFromWaiting bool) {
	label := "run:" + taskID.String()[:8]
	r.backgroundWg.Add(label)
	go func() {
		defer r.backgroundWg.Done(label)
		r.Run(taskID, prompt, sessionID, resumedFromWaiting)
	}()
}

// SyncWorktreesBackground launches SyncWorktrees in a background goroutine
// tracked by backgroundWg so that WaitBackground can drain it before cleanup.
// The optional onDone callbacks are called after SyncWorktrees returns.
func (r *Runner) SyncWorktreesBackground(taskID uuid.UUID, sessionID string, prevStatus store.TaskStatus, onDone ...func()) {
	label := "sync:" + taskID.String()[:8]
	r.backgroundWg.Add(label)
	go func() {
		defer r.backgroundWg.Done(label)
		r.SyncWorktrees(taskID, sessionID, prevStatus)
		for _, fn := range onDone {
			fn()
		}
	}()
}

// RunRefinementBackground launches RunRefinement in a background goroutine
// tracked by backgroundWg so that WaitBackground can drain it before cleanup.
func (r *Runner) RunRefinementBackground(taskID uuid.UUID, userInstructions string) {
	label := "refine:" + taskID.String()[:8]
	r.backgroundWg.Add(label)
	go func() {
		defer r.backgroundWg.Done(label)
		r.RunRefinement(taskID, userInstructions)
	}()
}

// GenerateOversightBackground launches GenerateOversight in a background goroutine
// tracked by backgroundWg so that WaitBackground can drain it before cleanup.
func (r *Runner) GenerateOversightBackground(taskID uuid.UUID) {
	label := "oversight:" + taskID.String()[:8]
	r.backgroundWg.Add(label)
	go func() {
		defer r.backgroundWg.Done(label)
		r.GenerateOversight(taskID)
	}()
}

// GenerateTitleBackground launches GenerateTitle in a background goroutine
// tracked by backgroundWg so that WaitBackground can drain it before cleanup.
func (r *Runner) GenerateTitleBackground(taskID uuid.UUID, prompt string) {
	label := "title:" + taskID.String()[:8]
	r.backgroundWg.Add(label)
	go func() {
		defer r.backgroundWg.Done(label)
		r.GenerateTitle(taskID, prompt)
	}()
}

// NewRunner constructs a Runner from the given store and config.
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
	cbThreshold := DefaultCBThreshold
	cbOpenSec := 30
	if v := os.Getenv("WALLFACER_CONTAINER_CB_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cbThreshold = n
		}
	}
	if v := os.Getenv("WALLFACER_CONTAINER_CB_OPEN_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cbOpenSec = n
		}
	}
	r.containerCB = NewCircuitBreaker(cbThreshold, time.Duration(cbOpenSec)*time.Second)
	r.executor = &osContainerExecutor{command: r.command}
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

func (r *Runner) applyWorkspaceSnapshot(s workspace.Snapshot) {
	r.storeMu.Lock()
	r.store = s.Store
	r.workspaces = s.Workspaces
	r.instructionsPath = s.InstructionsPath
	r.storeMu.Unlock()
}

func (r *Runner) currentStore() *store.Store {
	r.storeMu.RLock()
	defer r.storeMu.RUnlock()
	return r.store
}

func (r *Runner) startBoardSubscriptionLoop(initial *store.Store) {
	r.boardSubscriptionWg.Add(1)
	go func() {
		defer r.boardSubscriptionWg.Done()

		var (
			wsSubID int
			wsCh    <-chan workspace.Snapshot
			subID   int
			subCh   <-chan store.SequencedDelta
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

// InstructionsPath returns the host path mounted as /workspace/AGENTS.md.
func (r *Runner) InstructionsPath() string {
	if r.workspaceManager != nil {
		return r.workspaceManager.InstructionsPath()
	}
	return r.instructionsPath
}

// Prompts returns the prompt template Manager used by this runner.
func (r *Runner) Prompts() *prompts.Manager {
	return r.promptsMgr
}

// SandboxImage returns the container image used for task execution.
func (r *Runner) SandboxImage() string {
	return r.sandboxImage
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
func (r *Runner) Workspaces() []string {
	if r.workspaceManager != nil {
		return r.workspaceManager.Workspaces()
	}
	if len(r.workspaces) == 0 {
		return nil
	}
	return r.workspaces
}

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
	v, _ := r.repoMu.LoadOrStore(repoPath, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// oversightLock returns the per-task mutex for serialising oversight generation.
// The mutex is created on first access and stored in oversightMu.
func (r *Runner) oversightLock(taskID uuid.UUID) *sync.Mutex {
	v, _ := r.oversightMu.LoadOrStore(taskID.String(), &sync.Mutex{})
	return v.(*sync.Mutex)
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
// Safe to call when no container is running — errors are silently ignored.
func (r *Runner) KillContainer(taskID uuid.UUID) {
	name := r.ContainerName(taskID)
	if name == "" {
		return
	}
	_ = exec.Command(r.command, "kill", name).Run()

}

// KillRefineContainer sends a kill signal to the running refinement container.
// Safe to call when no refinement container is running — errors are silently ignored.
func (r *Runner) KillRefineContainer(taskID uuid.UUID) {
	name := r.RefineContainerName(taskID)
	if name == "" {
		return
	}
	_ = exec.Command(r.command, "kill", name).Run()

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
// Safe to call when no ideation container is running.
func (r *Runner) KillIdeateContainer() {
	name := r.IdeateContainerName()
	if name == "" {
		return
	}
	_ = exec.Command(r.command, "kill", name).Run()

}
