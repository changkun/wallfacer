package store

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/envutil"
	"changkun.de/x/wallfacer/internal/pkg/pubsub"
	"github.com/google/uuid"
)

// indexedTaskText holds pre-lowercased searchable text for a single task.
// It is kept in sync with task mutations so that SearchTasks can perform
// in-memory matching without per-query disk I/O or repeated lowercasing.
type indexedTaskText struct {
	title        string // strings.ToLower(task.Title)
	goal         string // strings.ToLower(task.Goal)
	prompt       string // strings.ToLower(task.Prompt)
	tags         string // strings.ToLower(strings.Join(task.Tags, " "))
	oversight    string // strings.ToLower(oversightRaw)
	oversightRaw string // original oversight text for snippet generation
}

// buildIndexEntry creates an indexedTaskText from a task and its raw oversight text.
// oversightRaw should be the concatenated phase titles/summaries (not lowercased).
func buildIndexEntry(t *Task, oversightRaw string) indexedTaskText {
	return indexedTaskText{
		title:        strings.ToLower(t.Title),
		goal:         strings.ToLower(t.Goal),
		prompt:       strings.ToLower(t.Prompt),
		tags:         strings.ToLower(strings.Join(t.Tags, " ")),
		oversight:    strings.ToLower(oversightRaw),
		oversightRaw: oversightRaw,
	}
}

// Store is the in-memory task database backed by per-task directory persistence.
// All mutations are atomic (temp-file + rename) and guarded by a RWMutex.
type Store struct {
	mu      sync.RWMutex
	dir     string
	backend StorageBackend
	closed  atomic.Bool
	tasks   map[uuid.UUID]*Task
	deleted map[uuid.UUID]*Task // tombstoned tasks (soft-deleted, not yet purged)
	events  map[uuid.UUID][]TaskEvent
	nextSeq map[uuid.UUID]int // next event sequence number to assign per task

	// tasksByStatus is a secondary index from status → set of task IDs.
	// It enables O(1) CountByStatus and O(k) ListTasksByStatus (where k is the
	// count for that status) instead of O(n) full-map scans.
	// Always accessed under s.mu (read or write lock). Inner maps are never nil
	// after initialisation — use addToStatusIndex / removeFromStatusIndex.
	tasksByStatus map[TaskStatus]map[uuid.UUID]struct{}

	// searchIndex holds pre-lowercased text for fast in-memory search.
	// Entries are created/updated in all task mutation methods and in
	// SaveOversight. Guarded by mu.
	searchIndex map[uuid.UUID]indexedTaskText

	// hub is the generic pub/sub hub for task change notifications.
	// Every mutation that persists a task also calls hub.Publish via notify().
	hub *pubsub.Hub[TaskDelta]

	// Payload pruning limits. A value of 0 disables pruning for that field.
	// Configured at startup from environment variables with fallback to the
	// Default* constants in models.go.
	retryHistoryLimit   int
	refineSessionsLimit int
	promptHistoryLimit  int

	// maxTurnOutputBytes is the effective per-turn output size limit read from
	// WALLFACER_MAX_TURN_OUTPUT_BYTES. 0 means unlimited.
	maxTurnOutputBytes int

	// compactWg tracks background compaction goroutines so tests can wait
	// for them to finish before cleaning up temp directories.
	compactWg sync.WaitGroup

	// eventsLoaded tracks which tasks have had their events loaded into
	// memory. Tasks in terminal states (done, failed, cancelled) skip
	// event loading at startup and are loaded lazily on first access.
	// This avoids reading potentially large trace files for completed
	// tasks that are unlikely to be queried during normal operation.
	eventsLoaded map[uuid.UUID]bool
}

// NewStore creates a Store backed by the given StorageBackend, loading all
// existing tasks from the backend into memory.
func NewStore(backend StorageBackend) (*Store, error) {
	s := &Store{
		backend:             backend,
		tasks:               make(map[uuid.UUID]*Task),
		deleted:             make(map[uuid.UUID]*Task),
		events:              make(map[uuid.UUID][]TaskEvent),
		nextSeq:             make(map[uuid.UUID]int),
		eventsLoaded:        make(map[uuid.UUID]bool),
		tasksByStatus:       make(map[TaskStatus]map[uuid.UUID]struct{}),
		searchIndex:         make(map[uuid.UUID]indexedTaskText),
		hub:                 pubsub.NewHub[TaskDelta](pubsub.WithClone(cloneTaskDelta)),
		retryHistoryLimit:   envutil.Int("WALLFACER_RETRY_HISTORY_LIMIT", constants.DefaultRetryHistoryLimit),
		refineSessionsLimit: envutil.Int("WALLFACER_REFINE_SESSIONS_LIMIT", constants.DefaultRefineSessionsLimit),
		promptHistoryLimit:  envutil.Int("WALLFACER_PROMPT_HISTORY_LIMIT", constants.DefaultPromptHistoryLimit),
		maxTurnOutputBytes:  envutil.Int("WALLFACER_MAX_TURN_OUTPUT_BYTES", constants.DefaultMaxTurnOutputBytes),
	}

	if err := s.loadAll(); err != nil {
		return nil, fmt.Errorf("load store: %w", err)
	}

	// Build the secondary status index from the tasks loaded above.
	for id, t := range s.tasks {
		s.addToStatusIndex(t.Status, id)
	}

	return s, nil
}

// NewFileStore creates a Store backed by a FilesystemBackend rooted at dir.
// This is the standard constructor for local deployments.
func NewFileStore(dir string) (*Store, error) {
	backend, err := NewFilesystemBackend(dir)
	if err != nil {
		return nil, err
	}
	s, err := NewStore(backend)
	if err != nil {
		return nil, err
	}
	s.dir = dir
	return s, nil
}

// Close marks the store as closed. It sets an internal flag that callers can
// query via IsClosed; it does not interrupt any in-flight operations.
func (s *Store) Close() { s.closed.Store(true) }

// IsClosed reports whether Close has been called on this store.
func (s *Store) IsClosed() bool { return s.closed.Load() }

// WaitCompaction blocks until all background compaction goroutines finish.
func (s *Store) WaitCompaction() { s.compactWg.Wait() }

// addToStatusIndex inserts id into tasksByStatus[status].
// Must be called while s.mu is held for writing.
func (s *Store) addToStatusIndex(status TaskStatus, id uuid.UUID) {
	if s.tasksByStatus[status] == nil {
		s.tasksByStatus[status] = make(map[uuid.UUID]struct{})
	}
	s.tasksByStatus[status][id] = struct{}{}
}

// removeFromStatusIndex removes id from tasksByStatus[status].
// Must be called while s.mu is held for writing.
func (s *Store) removeFromStatusIndex(status TaskStatus, id uuid.UUID) {
	delete(s.tasksByStatus[status], id)
}

// GetPayloadLimits returns the effective pruning limits for the three
// unboundedly-growing task slice fields. These are reported via GET /api/config
// so the UI can display contextual "showing last N entries" messages.
func (s *Store) GetPayloadLimits() PayloadLimits {
	return PayloadLimits{
		RetryHistory:   s.retryHistoryLimit,
		RefineSessions: s.refineSessionsLimit,
		PromptHistory:  s.promptHistoryLimit,
	}
}

// ReadBlob reads a named blob for a task, delegating to the storage backend.
func (s *Store) ReadBlob(taskID uuid.UUID, key string) ([]byte, error) {
	return s.backend.ReadBlob(taskID, key)
}

// ListBlobs returns blob keys for a task matching a prefix, delegating to the backend.
func (s *Store) ListBlobs(taskID uuid.UUID, prefix string) ([]string, error) {
	return s.backend.ListBlobs(taskID, prefix)
}

// DataDir returns the root data directory path for this store.
func (s *Store) DataDir() string {
	return s.dir
}

// loadAll delegates to the backend to load all tasks, then populates
// in-memory maps (tombstone detection, search index, event loading).
func (s *Store) loadAll() error {
	allTasks, err := s.backend.LoadAll()
	if err != nil {
		return err
	}

	for _, task := range allTasks {
		id := task.ID

		// Check for a tombstone marker; if present this task is soft-deleted.
		// Soft-deleted tasks are kept in s.deleted (not s.tasks) so they are
		// excluded from ListTasks but can still be restored or purged.
		if tombRaw, err := s.backend.ReadBlob(id, "tombstone.json"); err == nil {
			var tomb Tombstone
			if jsonUnmarshal(tombRaw, &tomb) == nil {
				s.deleted[id] = task
				s.eventsLoaded[id] = false
				continue
			}
		}

		// Prune oversized slices on load so the in-memory task is bounded from
		// the first read.
		s.pruneTaskPayload(task)

		// Build search index entry. Oversight read errors are non-fatal.
		oversightRaw, oversightErr := s.LoadOversightText(id)
		if oversightErr != nil && !os.IsNotExist(oversightErr) {
			logger.Store.Warn("startup: failed to load oversight for search index",
				"task", id, "error", oversightErr)
		}
		indexEntry := buildIndexEntry(task, oversightRaw)

		s.tasks[id] = task
		s.searchIndex[id] = indexEntry

		// Eagerly load events only for tasks that may still be active.
		if isTerminalStatus(task.Status) || task.Archived {
			s.eventsLoaded[id] = false
		} else {
			if err := s.loadEvents(id); err != nil {
				return err
			}
			s.eventsLoaded[id] = true
		}
	}

	return nil
}

// isTerminalStatus reports whether a task status indicates the task is
// no longer executing and will not produce new events without explicit
// user action (retry/resume).
func isTerminalStatus(status TaskStatus) bool {
	switch status {
	case TaskStatusDone, TaskStatusFailed, TaskStatusCancelled:
		return true
	}
	return false
}

// mutateTask acquires the write lock, finds the task by id, calls fn to mutate
// it (fn may return an error to abort without saving), sets UpdatedAt, persists
// with saveTask, and notifies subscribers. fn must not acquire s.mu itself.
func (s *Store) mutateTask(id uuid.UUID, fn func(t *Task) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if err := fn(t); err != nil {
		return err
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify(t, false)
	return nil
}

// ensureEventsLoadedLocked lazily loads events for a task if they haven't been
// loaded yet. Must be called while s.mu is held for writing.
func (s *Store) ensureEventsLoadedLocked(id uuid.UUID) {
	if s.eventsLoaded[id] {
		return
	}
	if err := s.loadEvents(id); err != nil {
		logger.Store.Warn("lazy event load failed", "task", id, "error", err)
	}
	s.eventsLoaded[id] = true
}

// loadEvents delegates to the backend to read all events for a task.
func (s *Store) loadEvents(id uuid.UUID) error {
	events, maxSeq, err := s.backend.LoadEvents(id)
	if err != nil {
		return err
	}
	s.events[id] = events
	if maxSeq == 0 && len(events) == 0 {
		s.nextSeq[id] = 1
	} else {
		s.nextSeq[id] = int(maxSeq) + 1
	}
	return nil
}
