package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"changkun.de/wallfacer/internal/logger"
	"github.com/google/uuid"
)

// indexedTaskText holds pre-lowercased searchable text for a single task.
// It is kept in sync with task mutations so that SearchTasks can perform
// in-memory matching without per-query disk I/O or repeated lowercasing.
type indexedTaskText struct {
	title        string // strings.ToLower(task.Title)
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
	tasks   map[uuid.UUID]*Task
	events  map[uuid.UUID][]TaskEvent
	nextSeq map[uuid.UUID]int

	// searchIndex holds pre-lowercased text for fast in-memory search.
	// Entries are created/updated in all task mutation methods and in
	// SaveOversight. Guarded by mu.
	searchIndex map[uuid.UUID]indexedTaskText

	// deltaSeq is a monotonically increasing counter stamped on every TaskDelta.
	// It is incremented inside notify, which is always called while s.mu is
	// write-locked, so reads under s.mu.RLock() yield a consistent snapshot.
	deltaSeq atomic.Int64

	// replayBuf holds the most recent replayBufMax SequencedDeltas so that
	// reconnecting SSE clients can catch up without a full snapshot.
	replayMu  sync.RWMutex
	replayBuf []SequencedDelta

	subMu       sync.Mutex
	subscribers map[int]chan SequencedDelta
	nextSubID   int
}

// NewStore loads (or creates) a Store rooted at dir.
func NewStore(dir string) (*Store, error) {
	s := &Store{
		dir:         dir,
		tasks:       make(map[uuid.UUID]*Task),
		events:      make(map[uuid.UUID][]TaskEvent),
		nextSeq:     make(map[uuid.UUID]int),
		searchIndex: make(map[uuid.UUID]indexedTaskText),
		subscribers: make(map[int]chan SequencedDelta),
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	if err := s.loadAll(); err != nil {
		return nil, fmt.Errorf("load store: %w", err)
	}

	return s, nil
}

// Close is a no-op placeholder for future resource cleanup.
func (s *Store) Close() {}

// OutputsDir returns the path to the outputs directory for a task.
// Handlers use this to serve turn output files without accessing Store internals.
func (s *Store) OutputsDir(taskID uuid.UUID) string {
	return filepath.Join(s.dir, taskID.String(), "outputs")
}

// loadAll scans the data directory and populates in-memory maps.
func (s *Store) loadAll() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id, err := uuid.Parse(entry.Name())
		if err != nil {
			continue // skip non-UUID directories
		}

		taskPath := filepath.Join(s.dir, entry.Name(), "task.json")
		raw, err := os.ReadFile(taskPath)
		if err != nil {
			logger.Store.Warn("skipping task", "name", entry.Name(), "error", err)
			continue
		}

		// Determine file mod time for defaulting missing timestamps.
		var modTime time.Time
		if fi, err := os.Stat(taskPath); err == nil {
			modTime = fi.ModTime()
		} else {
			modTime = time.Now()
		}

		task, changed, err := migrateTaskJSON(raw, modTime)
		if err != nil {
			logger.Store.Warn("skipping task", "name", entry.Name(), "error", err)
			continue
		}
		s.tasks[id] = &task

		// Persist the migrated task back to disk so future loads skip migration.
		if changed {
			if err := s.saveTask(id, &task); err != nil {
				logger.Store.Warn("failed to persist migrated task", "name", entry.Name(), "error", err)
			}
		}

		// Build search index entry. Oversight read errors are non-fatal;
		// the task remains indexed without oversight text.
		oversightRaw, oversightErr := s.LoadOversightText(id)
		if oversightErr != nil && !os.IsNotExist(oversightErr) {
			logger.Store.Warn("startup: failed to load oversight for search index",
				"task", id, "error", oversightErr)
		}
		s.searchIndex[id] = buildIndexEntry(&task, oversightRaw)

		if err := s.loadEvents(id, entry.Name()); err != nil {
			return err
		}
	}

	return nil
}

// loadEvents reads trace files for a single task into memory.
func (s *Store) loadEvents(id uuid.UUID, dirName string) error {
	tracesDir := filepath.Join(s.dir, dirName, "traces")
	traceEntries, err := os.ReadDir(tracesDir)
	if err != nil {
		if os.IsNotExist(err) {
			s.nextSeq[id] = 1
			return nil
		}
		return err
	}

	maxSeq := 0
	for _, te := range traceEntries {
		if te.IsDir() || !strings.HasSuffix(te.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(tracesDir, te.Name()))
		if err != nil {
			logger.Store.Warn("skipping trace", "task", dirName, "trace", te.Name(), "error", err)
			continue
		}
		var evt TaskEvent
		if err := jsonUnmarshal(raw, &evt); err != nil {
			logger.Store.Warn("skipping trace", "task", dirName, "trace", te.Name(), "error", err)
			continue
		}
		s.events[id] = append(s.events[id], evt)

		base := strings.TrimSuffix(te.Name(), ".json")
		if seq, err := strconv.Atoi(base); err == nil && seq > maxSeq {
			maxSeq = seq
		}
	}

	// Sort events by ID for consistent ordering.
	sort.Slice(s.events[id], func(i, j int) bool {
		return s.events[id][i].ID < s.events[id][j].ID
	})

	s.nextSeq[id] = maxSeq + 1
	return nil
}
