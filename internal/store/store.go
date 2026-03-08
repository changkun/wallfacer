package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"changkun.de/wallfacer/internal/logger"
	"github.com/google/uuid"
)

// Store is the in-memory task database backed by per-task directory persistence.
// All mutations are atomic (temp-file + rename) and guarded by a RWMutex.
type Store struct {
	mu      sync.RWMutex
	dir     string
	tasks   map[uuid.UUID]*Task
	events  map[uuid.UUID][]TaskEvent
	nextSeq map[uuid.UUID]int

	subMu       sync.Mutex
	subscribers map[int]chan TaskDelta
	nextSubID   int
}

// NewStore loads (or creates) a Store rooted at dir.
func NewStore(dir string) (*Store, error) {
	s := &Store{
		dir:         dir,
		tasks:       make(map[uuid.UUID]*Task),
		events:      make(map[uuid.UUID][]TaskEvent),
		nextSeq:     make(map[uuid.UUID]int),
		subscribers: make(map[int]chan TaskDelta),
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
		var task Task
		if err := jsonUnmarshal(raw, &task); err != nil {
			logger.Store.Warn("skipping task", "name", entry.Name(), "error", err)
			continue
		}
		s.tasks[id] = &task

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
