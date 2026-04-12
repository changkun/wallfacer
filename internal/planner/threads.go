package planner

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"changkun.de/x/wallfacer/internal/pkg/atomicfile"
	"github.com/google/uuid"
)

// ThreadMeta describes a single planning chat thread.
type ThreadMeta struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	Archived bool      `json:"archived,omitempty"`
}

// threadManifest is the on-disk shape of threads.json.
type threadManifest struct {
	Version int          `json:"version"`
	Threads []ThreadMeta `json:"threads"`
}

// activeThreadFile is the on-disk shape of active.json.
type activeThreadFile struct {
	ActiveThreadID string `json:"active_thread_id"`
}

const (
	threadsManifestFile = "threads.json"
	activeThreadJSON    = "active.json"
	threadsSubdir       = "threads"
	threadManifestV1    = 1
)

// ThreadManager owns the set of planning chat threads for a single
// workspace-group fingerprint. Each thread has its own on-disk
// [ConversationStore] under threads/<id>/. The manager persists the
// ordered list of threads in threads.json and the UI's active-thread
// preference in active.json.
//
// ThreadManager is safe for concurrent use.
type ThreadManager struct {
	root string

	mu       sync.Mutex
	manifest threadManifest
	active   string
	stores   map[string]*ConversationStore
}

// NewThreadManager opens (or creates) a thread manager rooted at root.
// On first open of a legacy single-thread layout — messages.jsonl and/or
// session.json sitting directly in root — the manager migrates those
// files into a new thread named "Chat 1" using a crash-safe copy/write/
// delete sequence.
func NewThreadManager(root string) (*ThreadManager, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	tm := &ThreadManager{root: root, stores: map[string]*ConversationStore{}}
	if err := tm.loadOrMigrate(); err != nil {
		return nil, err
	}
	return tm, nil
}

// loadOrMigrate reads threads.json when present; otherwise migrates from
// the single-thread layout, or creates a fresh manifest with one thread.
func (m *ThreadManager) loadOrMigrate() error {
	manifestPath := filepath.Join(m.root, threadsManifestFile)
	data, err := os.ReadFile(manifestPath)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &m.manifest); err != nil {
			return fmt.Errorf("thread manifest: %w", err)
		}
		// Clean up any legacy files that survived a crashed migration:
		// threads.json is the commit point, so if it exists the copies
		// under threads/<id>/ are authoritative and the originals in
		// root are safe to remove on reload.
		m.removeLegacyFiles()
	case errors.Is(err, os.ErrNotExist):
		if err := m.migrateOrInit(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("read thread manifest: %w", err)
	}

	if err := m.loadActive(); err != nil {
		return err
	}
	m.ensureValidActive()
	return nil
}

// migrateOrInit handles the first-load case: either there's a legacy
// layout to migrate, or there's nothing and we create a fresh thread.
func (m *ThreadManager) migrateOrInit() error {
	legacyMsgs := filepath.Join(m.root, messagesFile)
	legacySess := filepath.Join(m.root, sessionFile)

	hasLegacy := fileExists(legacyMsgs) || fileExists(legacySess)
	id := newThreadID()
	threadDir := filepath.Join(m.root, threadsSubdir, id)
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		return fmt.Errorf("migrate: mkdir thread: %w", err)
	}

	if hasLegacy {
		// Copy (not move) so a crash between copy and manifest write
		// leaves the originals intact and re-running restarts cleanly.
		if err := copyIfExists(legacyMsgs, filepath.Join(threadDir, messagesFile)); err != nil {
			return fmt.Errorf("migrate: copy messages: %w", err)
		}
		if err := copyIfExists(legacySess, filepath.Join(threadDir, sessionFile)); err != nil {
			return fmt.Errorf("migrate: copy session: %w", err)
		}
	}

	now := time.Now().UTC()
	m.manifest = threadManifest{
		Version: threadManifestV1,
		Threads: []ThreadMeta{{
			ID:      id,
			Name:    "Chat 1",
			Created: now,
			Updated: now,
		}},
	}
	if err := m.writeManifest(); err != nil {
		return err
	}
	m.active = id
	if err := m.writeActive(); err != nil {
		return err
	}

	// Commit point passed — delete legacy files.
	if hasLegacy {
		_ = os.Remove(legacyMsgs)
		_ = os.Remove(legacySess)
	}
	return nil
}

// removeLegacyFiles deletes leftover root-level messages.jsonl /
// session.json when the manifest already exists (crash between the
// manifest write and the delete step of migration).
func (m *ThreadManager) removeLegacyFiles() {
	_ = os.Remove(filepath.Join(m.root, messagesFile))
	_ = os.Remove(filepath.Join(m.root, sessionFile))
}

func (m *ThreadManager) loadActive() error {
	data, err := os.ReadFile(filepath.Join(m.root, activeThreadJSON))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var f activeThreadFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil // fall back to default
	}
	m.active = f.ActiveThreadID
	return nil
}

// ensureValidActive falls back to the first non-archived thread when
// the stored active thread is missing or archived.
func (m *ThreadManager) ensureValidActive() {
	if m.active != "" {
		for _, t := range m.manifest.Threads {
			if t.ID == m.active && !t.Archived {
				return
			}
		}
	}
	for _, t := range m.manifest.Threads {
		if !t.Archived {
			m.active = t.ID
			_ = m.writeActive()
			return
		}
	}
	m.active = ""
}

// List returns the threads in manifest order, optionally including
// archived ones.
func (m *ThreadManager) List(includeArchived bool) []ThreadMeta {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ThreadMeta, 0, len(m.manifest.Threads))
	for _, t := range m.manifest.Threads {
		if !includeArchived && t.Archived {
			continue
		}
		out = append(out, t)
	}
	return out
}

// Create adds a new thread with the given display name (default "Chat N")
// and returns its metadata. The new thread is not automatically made
// active — the caller chooses.
func (m *ThreadManager) Create(name string) (ThreadMeta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if name == "" {
		name = m.nextDefaultName()
	}
	now := time.Now().UTC()
	meta := ThreadMeta{
		ID:      newThreadID(),
		Name:    name,
		Created: now,
		Updated: now,
	}
	if err := os.MkdirAll(m.threadDir(meta.ID), 0o755); err != nil {
		return ThreadMeta{}, err
	}
	m.manifest.Threads = append(m.manifest.Threads, meta)
	if err := m.writeManifest(); err != nil {
		return ThreadMeta{}, err
	}
	return meta, nil
}

// nextDefaultName returns "Chat N" where N is the next unused index
// based on existing "Chat <n>" names.
func (m *ThreadManager) nextDefaultName() string {
	highest := 0
	for _, t := range m.manifest.Threads {
		if n, ok := parseChatN(t.Name); ok && n > highest {
			highest = n
		}
	}
	return fmt.Sprintf("Chat %d", highest+1)
}

// Rename updates a thread's display name.
func (m *ThreadManager) Rename(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("thread: name is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.manifest.Threads {
		if m.manifest.Threads[i].ID == id {
			m.manifest.Threads[i].Name = name
			m.manifest.Threads[i].Updated = time.Now().UTC()
			return m.writeManifest()
		}
	}
	return ErrThreadNotFound
}

// Archive marks a thread archived. Files on disk are retained.
func (m *ThreadManager) Archive(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.manifest.Threads {
		if m.manifest.Threads[i].ID == id {
			m.manifest.Threads[i].Archived = true
			m.manifest.Threads[i].Updated = time.Now().UTC()
			if err := m.writeManifest(); err != nil {
				return err
			}
			if m.active == id {
				// Pick the first non-archived thread as the new active.
				m.active = ""
				for _, t := range m.manifest.Threads {
					if !t.Archived {
						m.active = t.ID
						break
					}
				}
				_ = m.writeActive()
			}
			return nil
		}
	}
	return ErrThreadNotFound
}

// Unarchive returns a thread to the visible set.
func (m *ThreadManager) Unarchive(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.manifest.Threads {
		if m.manifest.Threads[i].ID == id {
			m.manifest.Threads[i].Archived = false
			m.manifest.Threads[i].Updated = time.Now().UTC()
			return m.writeManifest()
		}
	}
	return ErrThreadNotFound
}

// Meta returns the metadata for a thread.
func (m *ThreadManager) Meta(id string) (ThreadMeta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.manifest.Threads {
		if t.ID == id {
			return t, nil
		}
	}
	return ThreadMeta{}, ErrThreadNotFound
}

// Store returns the on-disk conversation store for a thread, creating
// it lazily on first access. Returns ErrThreadNotFound if id is unknown.
func (m *ThreadManager) Store(id string) (*ConversationStore, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.storeLocked(id)
}

func (m *ThreadManager) storeLocked(id string) (*ConversationStore, error) {
	if s, ok := m.stores[id]; ok {
		return s, nil
	}
	found := false
	for _, t := range m.manifest.Threads {
		if t.ID == id {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrThreadNotFound
	}
	s, err := NewConversationStore(m.threadDir(id))
	if err != nil {
		return nil, err
	}
	m.stores[id] = s
	return s, nil
}

// ActiveID returns the UI's current active thread ID. Empty string if
// no threads exist (e.g. all archived).
func (m *ThreadManager) ActiveID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

// SetActiveID records a new active thread. Rejects archived or unknown IDs.
func (m *ThreadManager) SetActiveID(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.manifest.Threads {
		if t.ID == id {
			if t.Archived {
				return fmt.Errorf("thread: cannot activate archived thread")
			}
			m.active = id
			return m.writeActive()
		}
	}
	return ErrThreadNotFound
}

// Touch marks a thread's Updated timestamp so the UI can sort by recent
// activity. Call sites: after appending a message, after rename, etc.
func (m *ThreadManager) Touch(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.manifest.Threads {
		if m.manifest.Threads[i].ID == id {
			m.manifest.Threads[i].Updated = time.Now().UTC()
			_ = m.writeManifest()
			return
		}
	}
}

// ErrThreadNotFound is returned for any operation on an unknown thread ID.
var ErrThreadNotFound = errors.New("thread: not found")

// --- internal helpers ----------------------------------------------------

func (m *ThreadManager) threadDir(id string) string {
	return filepath.Join(m.root, threadsSubdir, id)
}

func (m *ThreadManager) writeManifest() error {
	// Sort for deterministic serialization: by Created timestamp.
	sort.SliceStable(m.manifest.Threads, func(i, j int) bool {
		return m.manifest.Threads[i].Created.Before(m.manifest.Threads[j].Created)
	})
	m.manifest.Version = threadManifestV1
	return atomicfile.WriteJSON(filepath.Join(m.root, threadsManifestFile), m.manifest, 0o644)
}

func (m *ThreadManager) writeActive() error {
	return atomicfile.WriteJSON(filepath.Join(m.root, activeThreadJSON), activeThreadFile{ActiveThreadID: m.active}, 0o644)
}

func newThreadID() string {
	id, err := uuid.NewV7()
	if err != nil {
		// NewV7 only errors on entropy failure; fall back to random v4.
		return uuid.New().String()
	}
	return id.String()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func copyIfExists(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// parseChatN returns N if name is "Chat <N>" with integer N; otherwise
// (0, false). Used to derive the next default name.
func parseChatN(name string) (int, bool) {
	const prefix = "Chat "
	if !strings.HasPrefix(name, prefix) {
		return 0, false
	}
	n := 0
	rest := name[len(prefix):]
	if rest == "" {
		return 0, false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}
