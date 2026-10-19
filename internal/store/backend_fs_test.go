package store

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/logger"
)

// levelCaptureHandler records the levels and messages of emitted slog records
// so tests can assert on log severity.
type levelCaptureHandler struct {
	records []slog.Record
}

func (h *levelCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *levelCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *levelCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *levelCaptureHandler) WithGroup(string) slog.Handler      { return h }

func (h *levelCaptureHandler) countAtLevel(level slog.Level) int {
	n := 0
	for _, r := range h.records {
		if r.Level == level {
			n++
		}
	}
	return n
}

// newTestBackend creates a FilesystemBackend rooted in a fresh temporary directory.
func newTestBackend(t *testing.T) *FilesystemBackend {
	t.Helper()
	b, err := NewFilesystemBackend(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFilesystemBackend_Init(t *testing.T) {
	b := newTestBackend(t)
	id := uuid.New()

	if err := b.Init(id); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Verify task dir and traces subdir exist.
	tracesDir := filepath.Join(b.dir, id.String(), "traces")
	if fi, err := os.Stat(tracesDir); err != nil {
		t.Fatalf("traces dir not created: %v", err)
	} else if !fi.IsDir() {
		t.Fatal("traces path is not a directory")
	}
}

// TestFilesystemBackend_LoadAll_SkipsOrphanDirQuietly pins the fix for noisy
// load warnings: a UUID directory without task.json (an incomplete task, e.g.
// Init ran before SaveTask) must be skipped without a WARN, while a task.json
// that exists but cannot be parsed still warns. Valid tasks load regardless.
func TestFilesystemBackend_LoadAll_SkipsOrphanDirQuietly(t *testing.T) {
	b := newTestBackend(t)

	// Valid task.
	valid := &Task{
		ID:            uuid.New(),
		Prompt:        "valid",
		Status:        TaskStatusBacklog,
		SchemaVersion: constants.CurrentTaskSchemaVersion,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := b.Init(valid.ID); err != nil {
		t.Fatal(err)
	}
	if err := b.SaveTask(valid); err != nil {
		t.Fatal(err)
	}

	// Orphan: UUID dir with no task.json (mirrors the reported warnings).
	orphan := uuid.New()
	if err := b.Init(orphan); err != nil {
		t.Fatal(err)
	}

	// Corrupt: UUID dir with an unparseable task.json.
	corrupt := uuid.New()
	if err := os.MkdirAll(filepath.Join(b.dir, corrupt.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.dir, corrupt.String(), "task.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	capture := &levelCaptureHandler{}
	orig := logger.Store
	logger.Store = slog.New(capture)
	t.Cleanup(func() { logger.Store = orig })

	tasks, err := b.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != valid.ID {
		t.Fatalf("expected only the valid task, got %d tasks", len(tasks))
	}

	// The orphan must not warn; the corrupt task must.
	if got := capture.countAtLevel(slog.LevelWarn); got != 1 {
		t.Errorf("expected exactly 1 WARN (the corrupt task), got %d", got)
	}
	if got := capture.countAtLevel(slog.LevelDebug); got != 1 {
		t.Errorf("expected exactly 1 DEBUG (the orphan dir), got %d", got)
	}
}

// TestFilesystemBackend_LoadAll_LeavesRoutineCardsUnread writes two routine
// cards in an older schema, which migrateTaskJSON would otherwise rewrite,
// next to an ordinary task. LoadAll returns only the ordinary task, warns
// once with the count, and leaves both routine files byte-for-byte unchanged.
func TestFilesystemBackend_LoadAll_LeavesRoutineCardsUnread(t *testing.T) {
	b := newTestBackend(t)

	valid := &Task{
		ID:            uuid.New(),
		Prompt:        "valid",
		Status:        TaskStatusBacklog,
		SchemaVersion: constants.CurrentTaskSchemaVersion,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := b.Init(valid.ID); err != nil {
		t.Fatal(err)
	}
	if err := b.SaveTask(valid); err != nil {
		t.Fatal(err)
	}

	routines := map[uuid.UUID][]byte{}
	for range 2 {
		id := uuid.New()
		raw := []byte(`{"id":"` + id.String() + `","prompt":"nightly","status":"backlog","kind":"routine",` +
			`"routine_interval_seconds":3600,"routine_enabled":true}`)
		if err := os.MkdirAll(filepath.Join(b.dir, id.String()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(b.dir, id.String(), "task.json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		routines[id] = raw
	}

	capture := &levelCaptureHandler{}
	orig := logger.Store
	logger.Store = slog.New(capture)
	t.Cleanup(func() { logger.Store = orig })

	tasks, err := b.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != valid.ID {
		t.Fatalf("expected only the ordinary task, got %d tasks", len(tasks))
	}
	if got := capture.countAtLevel(slog.LevelWarn); got != 1 {
		t.Errorf("expected exactly 1 WARN for the routine cards, got %d", got)
	}
	for id, want := range routines {
		got, err := os.ReadFile(filepath.Join(b.dir, id.String(), "task.json"))
		if err != nil {
			t.Fatalf("read routine card %s: %v", id, err)
		}
		if string(got) != string(want) {
			t.Errorf("routine card %s was rewritten:\n got %s\nwant %s", id, got, want)
		}
	}
}

// removedTaskFields are task.json keys that records written by earlier
// releases may carry but Task no longer declares.
var removedTaskFields = map[string]string{
	"depends_on":            `["11111111-1111-1111-1111-111111111111"]`,
	"title_generating":      `true`,
	"is_test_run":           `true`,
	"last_test_result":      `"fail"`,
	"test_run_start_turn":   `3`,
	"pending_test_feedback": `"tests fail"`,
	"test_fail_count":       `2`,
	"custom_pass_patterns":  `["ok"]`,
	"custom_fail_patterns":  `["FAIL"]`,
}

// removedActivities are sandbox_by_activity keys that records written by
// earlier releases may carry but no activity routes by any more.
var removedActivities = []string{"title", "testing"}

// TestFilesystemBackend_LoadAll_DropsRemovedTaskFields writes two records
// carrying every removed field and every removed activity: one in an older
// schema, which LoadAll migrates and rewrites, and one in the current schema,
// which LoadAll leaves on disk until the task's next save. Both load with the
// removed activities dropped from their routing, and no removed key survives
// a save.
func TestFilesystemBackend_LoadAll_DropsRemovedTaskFields(t *testing.T) {
	b := newTestBackend(t)

	write := func(schema int) uuid.UUID {
		t.Helper()
		id := uuid.New()
		raw := `{"id":"` + id.String() + `","prompt":"stored","status":"backlog","timeout":60,` +
			`"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z",` +
			`"auto_retry_budget":{"container_crash":2},"schema_version":` + strconv.Itoa(schema) +
			`,"sandbox_by_activity":{"implementation":"claude"`
		for _, a := range removedActivities {
			raw += `,"` + a + `":"codex"`
		}
		raw += "}"
		for k, v := range removedTaskFields {
			raw += `,"` + k + `":` + v
		}
		raw += "}"
		if err := os.MkdirAll(filepath.Join(b.dir, id.String()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(b.dir, id.String(), "task.json"), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		return id
	}
	older := write(1)
	current := write(constants.CurrentTaskSchemaVersion)

	tasks, err := b.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected both stored tasks to load, got %d", len(tasks))
	}
	for _, task := range tasks {
		want := map[SandboxActivity]harness.ID{SandboxActivityImplementation: harness.Claude}
		if !maps.Equal(task.SandboxByActivity, want) {
			t.Errorf("task %s sandbox_by_activity = %v, want %v", task.ID, task.SandboxByActivity, want)
		}
		if task.ID == current {
			if err := b.SaveTask(task); err != nil {
				t.Fatalf("SaveTask: %v", err)
			}
		}
	}

	for _, id := range []uuid.UUID{older, current} {
		raw, err := os.ReadFile(filepath.Join(b.dir, id.String(), "task.json"))
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		var saved map[string]json.RawMessage
		if err := json.Unmarshal(raw, &saved); err != nil {
			t.Fatalf("parse saved %s: %v", id, err)
		}
		for k := range removedTaskFields {
			if _, ok := saved[k]; ok {
				t.Errorf("task %s still carries %q after a save", id, k)
			}
		}
	}
}

func TestFilesystemBackend_SaveTaskLoadAll(t *testing.T) {
	b := newTestBackend(t)
	id := uuid.New()

	if err := b.Init(id); err != nil {
		t.Fatal(err)
	}

	task := &Task{
		ID:            id,
		Prompt:        "test prompt",
		Status:        TaskStatusBacklog,
		SchemaVersion: constants.CurrentTaskSchemaVersion,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := b.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	tasks, err := b.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("LoadAll returned %d tasks, want 1", len(tasks))
	}
	if tasks[0].ID != id {
		t.Errorf("loaded task ID = %s, want %s", tasks[0].ID, id)
	}
	if tasks[0].Prompt != "test prompt" {
		t.Errorf("loaded task Prompt = %q, want %q", tasks[0].Prompt, "test prompt")
	}
}

func TestFilesystemBackend_RemoveTask(t *testing.T) {
	b := newTestBackend(t)
	id := uuid.New()

	if err := b.Init(id); err != nil {
		t.Fatal(err)
	}
	task := &Task{ID: id, Status: TaskStatusBacklog, SchemaVersion: constants.CurrentTaskSchemaVersion, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := b.SaveTask(task); err != nil {
		t.Fatal(err)
	}

	if err := b.RemoveTask(id); err != nil {
		t.Fatalf("RemoveTask: %v", err)
	}

	taskDir := filepath.Join(b.dir, id.String())
	if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
		t.Error("task directory still exists after RemoveTask")
	}
}

func TestFilesystemBackend_SaveLoadEvents(t *testing.T) {
	b := newTestBackend(t)
	id := uuid.New()
	if err := b.Init(id); err != nil {
		t.Fatal(err)
	}

	// No events: should return empty.
	events, maxSeq, err := b.LoadEvents(id)
	if err != nil {
		t.Fatalf("LoadEvents (empty): %v", err)
	}
	if len(events) != 0 || maxSeq != 0 {
		t.Errorf("empty: got %d events, maxSeq=%d; want 0, 0", len(events), maxSeq)
	}

	// Save 3 events.
	for i := 1; i <= 3; i++ {
		evt := TaskEvent{ID: int64(i), TaskID: id, EventType: EventTypeOutput, CreatedAt: time.Now()}
		if err := b.SaveEvent(id, i, evt); err != nil {
			t.Fatalf("SaveEvent(%d): %v", i, err)
		}
	}

	events, maxSeq, err = b.LoadEvents(id)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("got %d events, want 3", len(events))
	}
	if maxSeq != 3 {
		t.Errorf("maxSeq = %d, want 3", maxSeq)
	}
}

func TestFilesystemBackend_CompactEvents(t *testing.T) {
	b := newTestBackend(t)
	id := uuid.New()
	if err := b.Init(id); err != nil {
		t.Fatal(err)
	}

	// Save 5 events.
	var allEvents []TaskEvent
	for i := 1; i <= 5; i++ {
		evt := TaskEvent{ID: int64(i), TaskID: id, EventType: EventTypeOutput, CreatedAt: time.Now()}
		if err := b.SaveEvent(id, i, evt); err != nil {
			t.Fatal(err)
		}
		allEvents = append(allEvents, evt)
	}

	// Compact events 1-3.
	if err := b.CompactEvents(id, allEvents[:3]); err != nil {
		t.Fatalf("CompactEvents: %v", err)
	}

	// Verify compact.ndjson exists.
	compactPath := filepath.Join(b.dir, id.String(), "traces", "compact.ndjson")
	if _, err := os.Stat(compactPath); err != nil {
		t.Fatalf("compact.ndjson not found: %v", err)
	}

	// Verify numbered files 1-3 are removed, 4-5 remain.
	tracesDir := filepath.Join(b.dir, id.String(), "traces")
	for i := 1; i <= 3; i++ {
		name := filepath.Join(tracesDir, formatTraceFileName(i))
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Errorf("trace file %d still exists after compaction", i)
		}
	}
	for i := 4; i <= 5; i++ {
		name := filepath.Join(tracesDir, formatTraceFileName(i))
		if _, err := os.Stat(name); err != nil {
			t.Errorf("trace file %d missing after compaction: %v", i, err)
		}
	}

	// LoadEvents should still return all 5 events.
	events, maxSeq, err := b.LoadEvents(id)
	if err != nil {
		t.Fatalf("LoadEvents after compact: %v", err)
	}
	if len(events) != 5 {
		t.Errorf("got %d events after compact, want 5", len(events))
	}
	if maxSeq != 5 {
		t.Errorf("maxSeq after compact = %d, want 5", maxSeq)
	}
}

// formatTraceFileName builds a zero-padded 4-digit trace file name like "0042.json".
func formatTraceFileName(seq int) string {
	return filepath.Base(filepath.Join(".", func() string {
		s := "0000"
		n := seq
		for i := len(s) - 1; i >= 0 && n > 0; i-- {
			s = s[:i] + string(rune('0'+n%10)) + s[i+1:]
			n /= 10
		}
		return s
	}()+".json"))
}

func TestFilesystemBackend_BlobRoundTrip(t *testing.T) {
	b := newTestBackend(t)
	id := uuid.New()
	if err := b.Init(id); err != nil {
		t.Fatal(err)
	}

	key := "oversight.json"
	data := []byte(`{"status":"complete"}`)

	if err := b.SaveBlob(id, key, data); err != nil {
		t.Fatalf("SaveBlob: %v", err)
	}

	got, err := b.ReadBlob(id, key)
	if err != nil {
		t.Fatalf("ReadBlob: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("ReadBlob = %q, want %q", got, data)
	}

	if err := b.DeleteBlob(id, key); err != nil {
		t.Fatalf("DeleteBlob: %v", err)
	}

	_, err = b.ReadBlob(id, key)
	if !os.IsNotExist(err) {
		t.Errorf("ReadBlob after delete: got err=%v, want os.ErrNotExist", err)
	}
}

func TestFilesystemBackend_SaveBlob_NestedKey(t *testing.T) {
	b := newTestBackend(t)
	id := uuid.New()
	if err := b.Init(id); err != nil {
		t.Fatal(err)
	}

	key := "outputs/turn-0001.json"
	data := []byte(`{"output": true}`)

	if err := b.SaveBlob(id, key, data); err != nil {
		t.Fatalf("SaveBlob (nested): %v", err)
	}

	got, err := b.ReadBlob(id, key)
	if err != nil {
		t.Fatalf("ReadBlob (nested): %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("ReadBlob = %q, want %q", got, data)
	}
}

func TestFilesystemBackend_ReadBlob_NotFound(t *testing.T) {
	b := newTestBackend(t)
	id := uuid.New()
	if err := b.Init(id); err != nil {
		t.Fatal(err)
	}

	_, err := b.ReadBlob(id, "nonexistent.json")
	if !os.IsNotExist(err) {
		t.Errorf("ReadBlob(missing): got err=%v, want os.ErrNotExist", err)
	}
}

func TestFilesystemBackend_ListBlobs(t *testing.T) {
	b := newTestBackend(t)
	id := uuid.New()
	if err := b.Init(id); err != nil {
		t.Fatal(err)
	}

	// Write some output blobs.
	for _, key := range []string{
		"outputs/turn-0001.json",
		"outputs/turn-0001.stderr.txt",
		"outputs/turn-0002.json",
		"oversight.json",
	} {
		if err := b.SaveBlob(id, key, []byte("data")); err != nil {
			t.Fatal(err)
		}
	}

	// List with prefix "outputs/turn-" should return 3 keys.
	keys, err := b.ListBlobs(id, "outputs/turn-")
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	if len(keys) != 3 {
		t.Errorf("got %d keys, want 3: %v", len(keys), keys)
	}

	// List with prefix "outputs/" should return all 3 output files.
	keys, err = b.ListBlobs(id, "outputs/")
	if err != nil {
		t.Fatalf("ListBlobs (all outputs): %v", err)
	}
	if len(keys) != 3 {
		t.Errorf("got %d keys, want 3: %v", len(keys), keys)
	}

	// List missing directory should return nil, nil.
	keys, err = b.ListBlobs(id, "nonexistent/")
	if err != nil {
		t.Fatalf("ListBlobs (missing dir): %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys for missing dir, got %d", len(keys))
	}
}

func TestFilesystemBackend_ListBlobOwners(t *testing.T) {
	b := newTestBackend(t)
	id1, id2, id3 := uuid.New(), uuid.New(), uuid.New()

	for _, id := range []uuid.UUID{id1, id2, id3} {
		if err := b.Init(id); err != nil {
			t.Fatal(err)
		}
	}

	// Only id1 and id3 get a tombstone.json blob.
	if err := b.SaveBlob(id1, "tombstone.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := b.SaveBlob(id3, "tombstone.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}

	owners, err := b.ListBlobOwners("tombstone.json")
	if err != nil {
		t.Fatalf("ListBlobOwners: %v", err)
	}
	if len(owners) != 2 {
		t.Fatalf("got %d owners, want 2", len(owners))
	}

	ownerSet := map[uuid.UUID]bool{}
	for _, o := range owners {
		ownerSet[o] = true
	}
	if !ownerSet[id1] || !ownerSet[id3] {
		t.Errorf("owners = %v, want {%s, %s}", owners, id1, id3)
	}
	if ownerSet[id2] {
		t.Errorf("id2 should not be an owner")
	}
}

func TestFilesystemBackend_LoadEvents_MissingTask(t *testing.T) {
	b := newTestBackend(t)
	id := uuid.New()

	// Task directory doesn't exist — should return empty, no error.
	events, maxSeq, err := b.LoadEvents(id)
	if err != nil {
		t.Fatalf("LoadEvents (missing task): %v", err)
	}
	if len(events) != 0 || maxSeq != 0 {
		t.Errorf("got %d events, maxSeq=%d; want 0, 0", len(events), maxSeq)
	}
}
