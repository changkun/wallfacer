package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/instructions"
	"changkun.de/wallfacer/internal/store"
	"changkun.de/wallfacer/internal/workspacegroups"
)

type Snapshot struct {
	Workspaces       []string
	Store            *store.Store
	InstructionsPath string
	ScopedDataDir    string
	Key              string
	Generation       uint64
}

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
}

func NewManager(configDir, dataDir, envFile string, initial []string) (*Manager, error) {
	m := &Manager{
		configDir: configDir,
		dataDir:   dataDir,
		envFile:   envFile,
		subs:      make(map[int]chan Snapshot),
	}
	if _, err := m.Switch(initial); err != nil {
		return nil, err
	}
	return m, nil
}

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
		m.current.Key = instructions.Key(ws)
	}
	if store != nil {
		m.current.ScopedDataDir = store.DataDir()
	}
	m.nextGen = m.current.Generation
	return m
}

func (m *Manager) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneSnapshot(m.current)
}

func (m *Manager) Store() (*store.Store, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.Store, m.current.Store != nil
}

func (m *Manager) Workspaces() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneStrings(m.current.Workspaces)
}

func (m *Manager) InstructionsPath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.InstructionsPath
}

func (m *Manager) HasStore() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.Store != nil
}

func (m *Manager) Subscribe() (int, <-chan Snapshot) {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	id := m.nextSubID
	m.nextSubID++
	ch := make(chan Snapshot, 8)
	m.subs[id] = ch
	return id, ch
}

func (m *Manager) Unsubscribe(id int) {
	m.subsMu.Lock()
	ch, ok := m.subs[id]
	delete(m.subs, id)
	m.subsMu.Unlock()
	if ok {
		close(ch)
	}
}

func (m *Manager) Switch(paths []string) (Snapshot, error) {
	validated, err := validate(paths)
	if err != nil {
		return Snapshot{}, err
	}

	next := Snapshot{
		Key:           instructions.Key(validated),
		ScopedDataDir: filepath.Join(m.dataDir, instructions.Key(validated)),
	}
	s, err := store.NewStore(next.ScopedDataDir)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open scoped store: %w", err)
	}
	next.Store = s
	if len(validated) > 0 {
		instructionsPath, err := instructions.Ensure(m.configDir, validated)
		if err != nil {
			return Snapshot{}, fmt.Errorf("ensure instructions: %w", err)
		}
		next.InstructionsPath = instructionsPath
	}
	if err := workspacegroups.Upsert(m.configDir, validated); err != nil {
		return Snapshot{}, fmt.Errorf("persist workspace group: %w", err)
	}
	if m.envFile != "" {
		encoded := envconfig.FormatWorkspaces(validated)
		if err := envconfig.Update(
			m.envFile,
			nil, nil, nil, nil, nil, nil, nil, nil, nil,
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			nil,
			&encoded,
		); err != nil {
			return Snapshot{}, fmt.Errorf("persist workspaces: %w", err)
		}
	}

	m.mu.Lock()
	m.nextGen++
	next.Generation = m.nextGen
	next.Workspaces = validated
	m.current = next
	snapshot := cloneSnapshot(m.current)
	m.mu.Unlock()

	m.publish(snapshot)
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
	seen := map[string]struct{}{}
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
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		validated = append(validated, path)
	}
	slices.Sort(validated)
	return validated, nil
}

func cloneSnapshot(s Snapshot) Snapshot {
	s.Workspaces = cloneStrings(s.Workspaces)
	return s
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
