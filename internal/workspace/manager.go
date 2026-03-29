package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/pkg/set"
	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/store"
)

// Snapshot holds the immutable state of a workspace configuration at a point in time.
// Callers receive copies (via cloneSnapshot) so they cannot mutate manager internals.
type Snapshot struct {
	Workspaces       []string     // sorted, deduplicated absolute paths
	Store            *store.Store // scoped task store for this workspace set (may be shared across snapshots)
	InstructionsPath string       // path to the merged AGENTS.md; empty when Workspaces is empty
	ScopedDataDir    string       // per-workspace-key data directory under the global data dir
	Key              string       // deterministic key derived from sorted workspace paths (via prompts.InstructionsKey)
	Generation       uint64       // monotonically increasing counter; incremented on each successful Switch
}

// activeGroup tracks a workspace group whose store is still open, either
// because it is the currently viewed group or because it has active tasks.
type activeGroup struct {
	snapshot  Snapshot
	taskCount atomic.Int32 // in-progress + committing tasks (managed by Runner)
}

// storeHasActiveTasks reports whether the store has any tasks in a non-terminal
// state (backlog, in_progress, committing, waiting, failed). Used alongside
// taskCount to decide whether a store can be safely closed.
func storeHasActiveTasks(s *store.Store) bool {
	if s == nil {
		return false
	}
	return s.CountByStatus(store.TaskStatusInProgress) > 0 ||
		s.CountByStatus(store.TaskStatusCommitting) > 0 ||
		s.CountByStatus(store.TaskStatusWaiting) > 0
}

// pendingSwap carries the before/after snapshots and a cleanup callback
// for an in-flight Switch operation. cleanup closes next.Store on failure
// paths; it is cleared to a no-op once the swap commits successfully.
type pendingSwap struct {
	previous Snapshot
	next     Snapshot
	cleanup  func() // closes next.Store; set to a no-op after successful commit
}

// Manager coordinates workspace switching, store lifecycle, and change subscriptions.
//
// Lock ordering: mu must be acquired before subsMu. The two locks are kept
// separate so that publishing to subscribers (which only needs subsMu) does not
// block concurrent Snapshot reads (which only need mu.RLock).
type Manager struct {
	configDir string // ~/.wallfacer/ — workspace groups and instructions live here
	dataDir   string // per-workspace scoped data directories
	envFile   string // .env file path for persisting WALLFACER_WORKSPACES

	mu           sync.RWMutex            // guards current, nextGen, and activeGroups
	current      Snapshot                // the currently "viewed" workspace group
	nextGen      uint64                  // next generation counter to assign
	activeGroups map[string]*activeGroup // key = Snapshot.Key; groups with open stores

	subsMu    sync.Mutex // guards subs and nextSubID; separate from mu to avoid lock ordering issues
	subs      map[int]chan Snapshot
	nextSubID int

	// newStore is the factory used to open scoped stores. It defaults to
	// store.NewFileStore and can be replaced in tests to intercept created stores.
	newStore func(dir string) (*store.Store, error)
}

// NewManager creates a Manager and switches to the initial workspace set.
func NewManager(configDir, dataDir, envFile string, initial []string) (*Manager, error) {
	m := &Manager{
		configDir:    configDir,
		dataDir:      dataDir,
		envFile:      envFile,
		subs:         make(map[int]chan Snapshot),
		activeGroups: make(map[string]*activeGroup),
		newStore:     store.NewFileStore,
	}
	initial = m.startupWorkspaces(initial)
	if _, err := m.Switch(initial); err != nil {
		return nil, err
	}
	return m, nil
}

// startupWorkspaces determines the initial workspace set. The nil vs empty-slice
// distinction is intentional: nil means "restore last session" (load from disk),
// while an empty slice means "start with no workspaces" (suppress restore).
func (m *Manager) startupWorkspaces(initial []string) []string {
	if initial != nil {
		return initial
	}
	groups, err := LoadGroups(m.configDir)
	if err != nil || len(groups) == 0 {
		return nil
	}
	return cloneStrings(groups[0].Workspaces)
}

// NewStatic creates a Manager with a fixed workspace set that cannot be switched.
// It bypasses path validation, instructions setup, and env persistence — useful
// for testing and for CLI subcommands that operate on a known workspace.
func NewStatic(store *store.Store, workspaces []string, instructionsPath string) *Manager {
	m := &Manager{
		subs:         make(map[int]chan Snapshot),
		activeGroups: make(map[string]*activeGroup),
	}
	ws := cloneStrings(workspaces)
	m.current = Snapshot{
		Workspaces:       ws,
		Store:            store,
		InstructionsPath: instructionsPath,
		Generation:       1,
	}
	if len(ws) > 0 {
		m.current.Key = prompts.InstructionsKey(ws)
	}
	if store != nil {
		m.current.ScopedDataDir = store.DataDir()
	}
	m.nextGen = m.current.Generation
	m.activeGroups[m.current.Key] = &activeGroup{snapshot: m.current}
	return m
}

// Snapshot returns a copy of the current workspace state.
func (m *Manager) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneSnapshot(m.current)
}

// Store returns the current scoped store, if any.
func (m *Manager) Store() (*store.Store, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.Store, m.current.Store != nil
}

// Workspaces returns a copy of the current workspace paths.
func (m *Manager) Workspaces() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneStrings(m.current.Workspaces)
}

// InstructionsPath returns the path to the merged instructions file.
func (m *Manager) InstructionsPath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.InstructionsPath
}

// HasStore reports whether a scoped store is currently available.
func (m *Manager) HasStore() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.Store != nil
}

// Subscribe returns a channel that receives snapshots on workspace changes.
// The channel is buffered (capacity 8) so slow consumers do not block the
// manager; if the buffer is full, the snapshot is silently dropped.
func (m *Manager) Subscribe() (int, <-chan Snapshot) {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	id := m.nextSubID
	m.nextSubID++
	ch := make(chan Snapshot, 8)
	m.subs[id] = ch
	return id, ch
}

// Unsubscribe removes a subscription and closes its channel.
func (m *Manager) Unsubscribe(id int) {
	m.subsMu.Lock()
	ch, ok := m.subs[id]
	delete(m.subs, id)
	m.subsMu.Unlock()
	if ok {
		close(ch)
	}
}

// Switch validates and normalizes paths, then transitions the manager to the
// new workspace set. It short-circuits when the normalized set matches the
// current workspaces. All external side effects (store creation, instructions,
// workspace groups, env file) are applied before the atomic swap; every
// failure path closes the candidate store so it does not accumulate.
//
// Multi-store lifecycle: if the previous group has running tasks
// (taskCount > 0), its store is kept open in activeGroups. If switching to a
// key that is already in activeGroups, the existing store is reused.
func (m *Manager) Switch(paths []string) (Snapshot, error) {
	validated, err := validate(paths)
	if err != nil {
		return Snapshot{}, err
	}

	// Short-circuit: no-op when the normalized workspace set is unchanged.
	// Only after the first successful Switch (generation > 0) so that the
	// initial Switch call in NewManager always proceeds and creates the store.
	m.mu.RLock()
	sameSet := m.current.Generation > 0 && workspacesEqual(validated, m.current.Workspaces)
	m.mu.RUnlock()
	if sameSet {
		return m.Snapshot(), nil
	}

	// Determine the factory to use (supports injection in tests).
	newStoreFn := m.newStore
	if newStoreFn == nil {
		newStoreFn = store.NewFileStore
	}

	// Build the candidate snapshot. All external side effects happen here,
	// before the atomic swap, so the manager is never left in a partial state.
	key := prompts.InstructionsKey(validated)
	swap := pendingSwap{
		next: Snapshot{
			Key:           key,
			ScopedDataDir: filepath.Join(m.dataDir, key),
		},
	}

	// Check if a store for this key is already active (e.g. switching back
	// to a group that still has running tasks). Reuse it instead of creating
	// a new one.
	var reusedStore bool
	m.mu.RLock()
	if ag, ok := m.activeGroups[key]; ok && ag.snapshot.Store != nil && !ag.snapshot.Store.IsClosed() {
		swap.next.Store = ag.snapshot.Store
		reusedStore = true
	}
	m.mu.RUnlock()

	if !reusedStore {
		s, err := newStoreFn(swap.next.ScopedDataDir)
		if err != nil {
			return Snapshot{}, fmt.Errorf("open scoped store: %w", err)
		}
		swap.next.Store = s
	}

	// cleanup is idempotent; called on every failure path to release the
	// candidate store. Cleared to a no-op after the swap commits. Reused
	// stores must NOT be closed on failure — they belong to an active group.
	closed := false
	swap.cleanup = func() {
		if !closed && !reusedStore {
			closed = true
			swap.next.Store.Close()
		}
	}

	if len(validated) > 0 {
		instructionsPath, err := prompts.EnsureInstructions(m.configDir, validated)
		if err != nil {
			swap.cleanup()
			return Snapshot{}, fmt.Errorf("ensure instructions: %w", err)
		}
		swap.next.InstructionsPath = instructionsPath
	}
	if err := UpsertGroup(m.configDir, validated); err != nil {
		swap.cleanup()
		return Snapshot{}, fmt.Errorf("persist workspace group: %w", err)
	}
	if m.envFile != "" {
		encoded := envconfig.FormatWorkspaces(validated)
		if err := envconfig.Update(m.envFile, envconfig.Updates{
			Workspaces: &encoded,
		}); err != nil {
			swap.cleanup()
			return Snapshot{}, fmt.Errorf("persist workspaces: %w", err)
		}
	}

	// All external effects succeeded: atomically install the new snapshot
	// and update activeGroups. The write lock ensures no concurrent reader
	// sees a partially updated state (e.g. current.Store pointing to the
	// previous group while current.Key already identifies the new one).
	m.mu.Lock()
	m.nextGen++
	swap.next.Generation = m.nextGen
	swap.next.Workspaces = validated
	swap.previous = m.current // capture actual current under write lock (not the earlier RLock copy)
	m.current = swap.next

	// Update activeGroups: add/update the new group's entry.
	if ag, ok := m.activeGroups[key]; ok {
		ag.snapshot = swap.next // update snapshot but preserve taskCount
	} else {
		m.activeGroups[key] = &activeGroup{snapshot: swap.next}
	}

	// Decide whether the previous group's store should be closed.
	// Keep the store alive if it has running tasks (taskCount > 0) OR
	// non-terminal tasks (waiting, in_progress, committing) that watchers
	// need to process.
	previousKey := swap.previous.Key
	var closePrevious bool
	var previousStore *store.Store
	if previousKey != key {
		if ag, ok := m.activeGroups[previousKey]; ok {
			if ag.taskCount.Load() == 0 && !storeHasActiveTasks(ag.snapshot.Store) {
				closePrevious = true
				previousStore = ag.snapshot.Store
				delete(m.activeGroups, previousKey)
			}
		}
	}

	snapshot := cloneSnapshot(m.current)
	m.mu.Unlock()

	// Mark the swap as committed so the cleanup no-op is safe, then publish.
	swap.cleanup = func() {} // no-op: next.Store is now owned by m.current
	m.publish(snapshot)

	// Close the previous store outside the lock if it was marked for cleanup.
	if closePrevious && previousStore != nil {
		previousStore.Close()
	}

	return snapshot, nil
}

// AllActiveSnapshots returns cloned snapshots for all groups with open stores
// (the viewed group plus any groups with running tasks).
func (m *Manager) AllActiveSnapshots() []Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Snapshot, 0, len(m.activeGroups))
	for _, ag := range m.activeGroups {
		out = append(out, cloneSnapshot(ag.snapshot))
	}
	return out
}

// StoreForKey returns the store for a workspace key, if it is still active.
func (m *Manager) StoreForKey(key string) (*store.Store, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ag, ok := m.activeGroups[key]
	if !ok {
		return nil, false
	}
	return ag.snapshot.Store, ag.snapshot.Store != nil
}

// ActiveGroupKeys returns the workspace keys for all groups with open stores.
func (m *Manager) ActiveGroupKeys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.activeGroups))
	for k := range m.activeGroups {
		keys = append(keys, k)
	}
	return keys
}

// IncrementTaskCount marks a new running task in the given workspace group.
// RLock suffices because activeGroup.taskCount is an atomic.Int32, and the
// activeGroups map entry is only deleted by DecrementAndCleanup (under write lock)
// which waits until the count reaches zero first.
func (m *Manager) IncrementTaskCount(key string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if ag, ok := m.activeGroups[key]; ok {
		ag.taskCount.Add(1)
	}
}

// DecrementAndCleanup decrements the task count for the given workspace group.
// If the count reaches zero and the group is not the currently viewed group,
// the group's store is closed and the entry is removed from activeGroups.
// A write lock is used to atomically check the count and remove the entry,
// preventing a race where a new task could be created between decrement and cleanup.
func (m *Manager) DecrementAndCleanup(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ag, ok := m.activeGroups[key]
	if !ok {
		return
	}
	newCount := ag.taskCount.Add(-1)
	if newCount > 0 {
		return
	}
	// Count reached zero. Clean up if this is not the viewed group AND the
	// store has no non-terminal tasks. The write lock here is critical: it
	// prevents a race where IncrementTaskCount (under RLock) could bump the
	// count between our decrement and the delete below.
	if key != m.current.Key && !storeHasActiveTasks(ag.snapshot.Store) {
		if ag.snapshot.Store != nil {
			ag.snapshot.Store.Close()
		}
		delete(m.activeGroups, key)
	}
}

// publish sends the snapshot to all active subscribers. Non-blocking: if a
// subscriber's channel buffer is full, that subscriber's notification is dropped.
func (m *Manager) publish(snapshot Snapshot) {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	for _, ch := range m.subs {
		select {
		case ch <- cloneSnapshot(snapshot):
		default:
		}
	}
}

// validate checks that all workspace paths are absolute, clean, existing
// directories and returns a deduplicated, sorted slice. Returns an error
// for any invalid path.
func validate(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	seen := set.New[string]()
	validated := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("workspace path must be absolute: %s", path)
		}
		clean := filepath.Clean(path)
		if clean != path {
			return nil, fmt.Errorf("workspace path must be clean: %s", path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("workspace path invalid: %w", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("workspace path is not a directory: %s", path)
		}
		if seen.Has(path) {
			continue
		}
		seen.Add(path)
		validated = append(validated, path)
	}
	slices.Sort(validated)
	return validated, nil
}

// workspacesEqual reports whether two validated (sorted, deduplicated) workspace
// slices represent the same set.
func workspacesEqual(a, b []string) bool {
	return slices.Equal(a, b)
}

// cloneSnapshot creates a shallow copy of s with the Workspaces slice cloned
// so the caller cannot mutate the manager's internal state.
func cloneSnapshot(s Snapshot) Snapshot {
	s.Workspaces = cloneStrings(s.Workspaces)
	return s
}

// cloneStrings returns a copy of in, or nil if in is empty.
func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return slices.Clone(in)
}
