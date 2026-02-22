// Tests for events.go: InsertEvent, GetEvents, and event persistence/reload.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestInsertEvent_Basic(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5)

	if err := s.InsertEvent(bg(), task.ID, EventTypeStateChange, map[string]string{"status": "in_progress"}); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	events, _ := s.GetEvents(bg(), task.ID)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventType != EventTypeStateChange {
		t.Errorf("EventType = %q, want 'state_change'", events[0].EventType)
	}
	if events[0].TaskID != task.ID {
		t.Error("TaskID mismatch")
	}
	if events[0].ID != 1 {
		t.Errorf("event ID = %d, want 1", events[0].ID)
	}
}

func TestInsertEvent_SequentialIDs(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5)

	for i := 0; i < 5; i++ {
		if err := s.InsertEvent(bg(), task.ID, EventTypeOutput, i); err != nil {
			t.Fatalf("InsertEvent[%d]: %v", i, err)
		}
	}

	events, _ := s.GetEvents(bg(), task.ID)
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}
	for i, e := range events {
		if e.ID != int64(i+1) {
			t.Errorf("events[%d].ID = %d, want %d", i, e.ID, i+1)
		}
	}
}

func TestInsertEvent_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.InsertEvent(bg(), uuid.New(), EventTypeStateChange, nil); err == nil {
		t.Error("expected error for unknown task")
	}
}

func TestInsertEvent_PersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	task, _ := s.CreateTask(bg(), "p", 5)
	s.InsertEvent(bg(), task.ID, EventTypeOutput, "hello world")

	s2, _ := NewStore(dir)
	events, _ := s2.GetEvents(bg(), task.ID)
	if len(events) != 1 {
		t.Fatalf("expected 1 event after reload, got %d", len(events))
	}

	var data string
	json.Unmarshal(events[0].Data, &data)
	if data != "hello world" {
		t.Errorf("event data = %q, want 'hello world'", data)
	}
}

func TestGetEvents_ReturnsCopy(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5)
	s.InsertEvent(bg(), task.ID, EventTypeStateChange, "test")

	events, _ := s.GetEvents(bg(), task.ID)
	events[0].EventType = "mutated"

	events2, _ := s.GetEvents(bg(), task.ID)
	if events2[0].EventType != EventTypeStateChange {
		t.Error("GetEvents returned a reference, not a copy")
	}
}

func TestGetEvents_SortedByIDAfterReload(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	task, _ := s.CreateTask(bg(), "p", 5)

	for i := 0; i < 5; i++ {
		s.InsertEvent(bg(), task.ID, EventTypeOutput, i)
	}

	s2, _ := NewStore(dir)
	events, _ := s2.GetEvents(bg(), task.ID)
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}
	for i, e := range events {
		if e.ID != int64(i+1) {
			t.Errorf("events[%d].ID = %d, want %d", i, e.ID, i+1)
		}
	}
}

func TestLoadEvents_SkipsNonJSONFiles(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	task, _ := s.CreateTask(bg(), "p", 5)

	tracesDir := filepath.Join(dir, task.ID.String(), "traces")
	os.WriteFile(filepath.Join(tracesDir, "README.txt"), []byte("not json"), 0644)

	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore after injecting non-JSON: %v", err)
	}
	events, _ := s2.GetEvents(bg(), task.ID)
	if len(events) != 0 {
		t.Errorf("expected 0 events (txt file skipped), got %d", len(events))
	}
}

func TestLoadEvents_SkipsCorruptTraceFiles(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	task, _ := s.CreateTask(bg(), "p", 5)
	s.InsertEvent(bg(), task.ID, EventTypeStateChange, "good")

	tracesDir := filepath.Join(dir, task.ID.String(), "traces")
	os.WriteFile(filepath.Join(tracesDir, "0001.json"), []byte("{bad json}"), 0644)

	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore with corrupt trace: %v", err)
	}
	events, _ := s2.GetEvents(bg(), task.ID)
	if len(events) != 0 {
		t.Errorf("expected 0 events (corrupt trace skipped), got %d", len(events))
	}
}

func TestConcurrentInsertEvent(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5)

	var wg sync.WaitGroup
	const n = 10
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s.InsertEvent(bg(), task.ID, EventTypeOutput, idx)
		}(i)
	}
	wg.Wait()

	events, _ := s.GetEvents(bg(), task.ID)
	if len(events) != n {
		t.Errorf("expected %d events, got %d", n, len(events))
	}
}
