package store

import (
	"bufio"
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
	closed  atomic.Bool
	tasks   map[uuid.UUID]*Task
	deleted map[uuid.UUID]*Task // tombstoned tasks (soft-deleted, not yet purged)
	events  map[uuid.UUID][]TaskEvent
	nextSeq map[uuid.UUID]int

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

	// Payload pruning limits. A value of 0 disables pruning for that field.
	// Configured at startup from environment variables with fallback to the
	// Default* constants in models.go.
	retryHistoryLimit   int
	refineSessionsLimit int
	promptHistoryLimit  int

	// maxTurnOutputBytes is the effective per-turn output size limit read from
	// WALLFACER_MAX_TURN_OUTPUT_BYTES. 0 means unlimited.
	maxTurnOutputBytes int
}

// readEnvInt reads an integer from an environment variable. If the variable is
// absent or cannot be parsed as an integer, defaultVal is returned.
func readEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}

// NewStore loads (or creates) a Store rooted at dir.
func NewStore(dir string) (*Store, error) {
	s := &Store{
		dir:                 dir,
		tasks:               make(map[uuid.UUID]*Task),
		deleted:             make(map[uuid.UUID]*Task),
		events:              make(map[uuid.UUID][]TaskEvent),
		nextSeq:             make(map[uuid.UUID]int),
		tasksByStatus:       make(map[TaskStatus]map[uuid.UUID]struct{}),
		searchIndex:         make(map[uuid.UUID]indexedTaskText),
		subscribers:         make(map[int]chan SequencedDelta),
		retryHistoryLimit:   readEnvInt("WALLFACER_RETRY_HISTORY_LIMIT", DefaultRetryHistoryLimit),
		refineSessionsLimit: readEnvInt("WALLFACER_REFINE_SESSIONS_LIMIT", DefaultRefineSessionsLimit),
		promptHistoryLimit:  readEnvInt("WALLFACER_PROMPT_HISTORY_LIMIT", DefaultPromptHistoryLimit),
		maxTurnOutputBytes:  readEnvInt("WALLFACER_MAX_TURN_OUTPUT_BYTES", DefaultMaxTurnOutputBytes),
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
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

// Close marks the store as closed. It sets an internal flag that callers can
// query via IsClosed; it does not interrupt any in-flight operations.
func (s *Store) Close() { s.closed.Store(true) }

// IsClosed reports whether Close has been called on this store.
func (s *Store) IsClosed() bool { return s.closed.Load() }

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

// OutputsDir returns the path to the outputs directory for a task.
// Handlers use this to serve turn output files without accessing Store internals.
func (s *Store) OutputsDir(taskID uuid.UUID) string {
	return filepath.Join(s.dir, taskID.String(), "outputs")
}

// DataDir returns the root data directory path for this store.
func (s *Store) DataDir() string {
	return s.dir
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

		// Check for a tombstone marker; if present this task is soft-deleted.
		tombPath := filepath.Join(s.dir, entry.Name(), "tombstone.json")
		if tombRaw, err := os.ReadFile(tombPath); err == nil {
			var tomb Tombstone
			if jsonUnmarshal(tombRaw, &tomb) == nil {
				s.deleted[id] = &task
				// Load events so deleted tasks' history remains accessible.
				if err := s.loadEvents(id, entry.Name()); err != nil {
					return err
				}
				continue
			}
		}

		// Prune oversized slices on load so the in-memory task is bounded from
		// the first read. This migrates existing large files written before the
		// retention limits were introduced without requiring a schema bump.
		s.pruneTaskPayload(&task)

		// Build search index entry before updating the in-memory maps.
		// Oversight read errors are non-fatal; the task remains indexed without
		// oversight text. Doing this before the map update keeps expensive disk
		// I/O and strings.ToLower work outside any future lock scope.
		oversightRaw, oversightErr := s.LoadOversightText(id)
		if oversightErr != nil && !os.IsNotExist(oversightErr) {
			logger.Store.Warn("startup: failed to load oversight for search index",
				"task", id, "error", oversightErr)
		}
		indexEntry := buildIndexEntry(&task, oversightRaw)

		s.tasks[id] = &task

		// Persist the migrated task back to disk so future loads skip migration.
		if changed {
			if err := s.saveTask(id, &task); err != nil {
				logger.Store.Warn("failed to persist migrated task", "name", entry.Name(), "error", err)
			}
		}

		s.searchIndex[id] = indexEntry

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

	var events []TaskEvent
	compactMaxID := int64(0)
	compactPath := filepath.Join(tracesDir, "compact.ndjson")
	if _, err := os.Stat(compactPath); err == nil {
		f, err := os.Open(compactPath)
		if err != nil {
			return err
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var evt TaskEvent
			if err := jsonUnmarshal([]byte(line), &evt); err != nil {
				logger.Store.Warn("skipping compact trace line", "task", dirName, "trace", "compact.ndjson", "error", err)
				continue
			}
			events = append(events, evt)
			if evt.ID > compactMaxID {
				compactMaxID = evt.ID
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	maxSeq := int(compactMaxID)
	for _, te := range traceEntries {
		if te.IsDir() {
			continue
		}
		traceFile, ok := parseNumberedTraceFile(te.Name())
		if !ok || int64(traceFile.seq) <= compactMaxID {
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
		events = append(events, evt)
		if traceFile.seq > maxSeq {
			maxSeq = traceFile.seq
		}
	}

	// Sort events by ID for consistent ordering.
	sort.Slice(events, func(i, j int) bool {
		return events[i].ID < events[j].ID
	})

	s.events[id] = events
	s.nextSeq[id] = maxSeq + 1
	return nil
}
