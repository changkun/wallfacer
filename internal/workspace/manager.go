// Package workspace manages workspace lifecycle and scoped store switching.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/pkg/set"
	"changkun.de/x/wallfacer/prompts"
	"changkun.de/x/wallfacer/internal/store"
)

// Snapshot holds the immutable state of a workspace configuration at a point in time.
type Snapshot struct {
	Workspaces       []string
	Store            *store.Store
	InstructionsPath string
	ScopedDataDir    string
	Key              string
	Generation       uint64
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
type Manager struct {
	configDir string
	dataDir   string
	envFile   string

	mu        sync.RWMutex
	current   Snapshot
	nextGen   uint64
	subsMu    sync.Mutex
	subs      map[int]chan Snapshot
	nextSubID int

	// newStore is the factory used to open scoped stores. It defaults to
	// store.NewStore and can be replaced in tests to intercept created stores.
	newStore func(dir string) (*store.Store, error)
}

// NewManager creates a Manager and switches to the initial workspace set.
func NewManager(configDir, dataDir, envFile string, initial []string) (*Manager, error) {
	m := &Manager{
		configDir: configDir,
		dataDir:   dataDir,
		envFile:   envFile,
		subs:      make(map[int]chan Snapshot),
		newStore:  store.NewStore,
	}
	initial = m.startupWorkspaces(initial)
	if _, err := m.Switch(initial); err != nil {
		return nil, err
	}
	return m, nil
}

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

// NewStatic creates a Manager with a fixed workspace set, useful for testing.
func NewStatic(store *store.Store, workspaces []string, instructionsPath string) *Manager {
	m := &Manager{subs: make(map[int]chan Snapshot)}
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
// failure path closes the candidate store so it does not accumulate. After a
// successful swap the previous store is closed outside the lock.
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
		newStoreFn = store.NewStore
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

	s, err := newStoreFn(swap.next.ScopedDataDir)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open scoped store: %w", err)
	}
	swap.next.Store = s

	// cleanup is idempotent; called on every failure path to release the
	// candidate store. Cleared to a no-op after the swap commits.
	closed := false
	swap.cleanup = func() {
		if !closed {
			closed = true
			s.Close()
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

	// All external effects succeeded: atomically install the new snapshot.
	m.mu.Lock()
	m.nextGen++
	swap.next.Generation = m.nextGen
	swap.next.Workspaces = validated
	swap.previous = m.current // capture actual current under write lock
	m.current = swap.next
	snapshot := cloneSnapshot(m.current)
	m.mu.Unlock()

	// Mark the swap as committed so the cleanup no-op is safe, then publish.
	swap.cleanup = func() {} // no-op: next.Store is now owned by m.current
	m.publish(snapshot)

	// Close the previous store outside the lock so old scoped stores do not
	// accumulate. Subscribers already received the new snapshot via publish.
	if swap.previous.Store != nil {
		swap.previous.Store.Close()
	}

	return snapshot, nil
}

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

func cloneSnapshot(s Snapshot) Snapshot {
	s.Workspaces = cloneStrings(s.Workspaces)
	return s
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return slices.Clone(in)
}
