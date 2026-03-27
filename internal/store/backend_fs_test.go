package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"github.com/google/uuid"
)

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

func TestFilesystemBackend_Dir(t *testing.T) {
	dir := t.TempDir()
	b, err := NewFilesystemBackend(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b.Dir() != dir {
		t.Errorf("Dir() = %q, want %q", b.Dir(), dir)
	}
}
