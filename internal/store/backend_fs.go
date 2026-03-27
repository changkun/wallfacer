package store

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/atomicfile"
	"changkun.de/x/wallfacer/internal/pkg/ndjson"
	"github.com/google/uuid"
)

// FilesystemBackend implements StorageBackend using per-task directories
// on the local filesystem. Each task gets a directory named by its UUID
// under the root data directory.
type FilesystemBackend struct {
	dir string // root data directory, e.g. ~/.wallfacer/data/<workspace-key>/
}

// NewFilesystemBackend creates a FilesystemBackend rooted at dir.
// The directory is created if it does not exist.
func NewFilesystemBackend(dir string) (*FilesystemBackend, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	return &FilesystemBackend{dir: dir}, nil
}

// Dir returns the root data directory path. Used by Store.OutputsDir and
// Store.DataDir for backward compatibility until those are removed.
func (b *FilesystemBackend) Dir() string { return b.dir }

// Init creates the task directory and traces subdirectory.
func (b *FilesystemBackend) Init(taskID uuid.UUID) error {
	tracesDir := filepath.Join(b.dir, taskID.String(), "traces")
	return os.MkdirAll(tracesDir, 0755)
}

// LoadAll scans the data directory and returns all tasks found in task.json
// files. Tasks are parsed and migrated to the current schema version.
// Errors for individual tasks are logged and skipped.
func (b *FilesystemBackend) LoadAll() ([]*Task, error) {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return nil, err
	}

	var tasks []*Task
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := uuid.Parse(entry.Name()); err != nil {
			continue // skip non-UUID directories
		}

		taskPath := filepath.Join(b.dir, entry.Name(), "task.json")
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

		// Persist migrated task back to disk so future loads skip migration.
		if changed {
			if err := b.SaveTask(&task); err != nil {
				logger.Store.Warn("failed to persist migrated task", "name", entry.Name(), "error", err)
			}
		}

		tasks = append(tasks, &task)
	}
	return tasks, nil
}

// SaveTask atomically writes a task's metadata to its task.json file.
func (b *FilesystemBackend) SaveTask(t *Task) error {
	path := filepath.Join(b.dir, t.ID.String(), "task.json")
	return atomicfile.WriteJSON(path, t, 0644)
}

// RemoveTask permanently removes a task's directory and all its data.
func (b *FilesystemBackend) RemoveTask(taskID uuid.UUID) error {
	taskDir := filepath.Join(b.dir, taskID.String())
	return os.RemoveAll(taskDir)
}

// SaveEvent writes a single event to the task's traces directory.
func (b *FilesystemBackend) SaveEvent(taskID uuid.UUID, seq int, event TaskEvent) error {
	tracesDir := filepath.Join(b.dir, taskID.String(), "traces")
	if err := os.MkdirAll(tracesDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(tracesDir, fmt.Sprintf("%04d.json", seq))
	return atomicfile.WriteJSON(path, event, 0644)
}

// LoadEvents reads all events for a task from compact.ndjson and individual
// trace files. Returns the sorted events and the highest sequence number.
func (b *FilesystemBackend) LoadEvents(taskID uuid.UUID) ([]TaskEvent, int64, error) {
	dirName := taskID.String()
	tracesDir := filepath.Join(b.dir, dirName, "traces")
	traceEntries, err := os.ReadDir(tracesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}

	compactPath := filepath.Join(tracesDir, "compact.ndjson")
	events, err := ndjson.ReadFile[TaskEvent](compactPath,
		ndjson.WithBufferSize(64*1024, 1024*1024),
		ndjson.WithOnError(func(lineNum int, err error) {
			logger.Store.Warn("skipping compact trace line", "task", dirName, "trace", "compact.ndjson", "line", lineNum, "error", err)
		}),
	)
	if err != nil {
		return nil, 0, err
	}

	compactMaxID := int64(0)
	for _, evt := range events {
		if evt.ID > compactMaxID {
			compactMaxID = evt.ID
		}
	}

	maxSeq := compactMaxID
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
		if int64(traceFile.seq) > maxSeq {
			maxSeq = int64(traceFile.seq)
		}
	}

	// Sort events by ID for consistent ordering.
	slices.SortFunc(events, func(a, b TaskEvent) int {
		return cmp.Compare(a.ID, b.ID)
	})

	return events, maxSeq, nil
}

// CompactEvents writes the provided events as compact.ndjson and removes
// all numbered trace files whose sequence number is ≤ the highest event ID.
func (b *FilesystemBackend) CompactEvents(taskID uuid.UUID, events []TaskEvent) error {
	tracesDir := filepath.Join(b.dir, taskID.String(), "traces")
	dirEntries, err := os.ReadDir(tracesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// Determine the highest event ID — this is the compaction boundary.
	var maxID int64
	for _, evt := range events {
		if evt.ID > maxID {
			maxID = evt.ID
		}
	}

	// Build compact.ndjson from the provided events.
	var compact []byte
	for _, evt := range events {
		line, err := json.Marshal(evt)
		if err != nil {
			logger.Store.Warn("compact: skipping unmarshalable event", "task", taskID, "event_id", evt.ID, "error", err)
			continue
		}
		compact = append(compact, line...)
		compact = append(compact, '\n')
	}

	compactPath := filepath.Join(tracesDir, "compact.ndjson")
	if err := atomicfile.Write(compactPath, compact, 0644); err != nil {
		return err
	}

	// Remove individual trace files that are now covered by the compact file.
	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}
		traceFile, ok := parseNumberedTraceFile(entry.Name())
		if !ok {
			continue
		}
		if int64(traceFile.seq) > maxID {
			continue // beyond the compaction boundary
		}
		if err := os.Remove(filepath.Join(tracesDir, entry.Name())); err != nil && !os.IsNotExist(err) {
			logger.Store.Warn("compact: failed to remove trace", "task", taskID, "trace", entry.Name(), "error", err)
		}
	}
	return nil
}

// SaveBlob writes arbitrary named data under the task's directory.
// Parent directories are created as needed (e.g., for key "outputs/turn-0001.json").
func (b *FilesystemBackend) SaveBlob(taskID uuid.UUID, key string, data []byte) error {
	path := filepath.Join(b.dir, taskID.String(), key)
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return atomicfile.Write(path, data, 0644)
}

// ReadBlob reads named data from the task's directory.
func (b *FilesystemBackend) ReadBlob(taskID uuid.UUID, key string) ([]byte, error) {
	path := filepath.Join(b.dir, taskID.String(), key)
	return os.ReadFile(path)
}

// DeleteBlob removes named data from the task's directory.
func (b *FilesystemBackend) DeleteBlob(taskID uuid.UUID, key string) error {
	path := filepath.Join(b.dir, taskID.String(), key)
	return os.Remove(path)
}

// ListBlobOwners returns the UUIDs of all tasks that have the given blob key.
func (b *FilesystemBackend) ListBlobOwners(key string) ([]uuid.UUID, error) {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return nil, err
	}

	var owners []uuid.UUID
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id, err := uuid.Parse(entry.Name())
		if err != nil {
			continue
		}
		blobPath := filepath.Join(b.dir, entry.Name(), key)
		if _, err := os.Stat(blobPath); err == nil {
			owners = append(owners, id)
		}
	}
	return owners, nil
}
