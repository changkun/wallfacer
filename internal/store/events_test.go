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
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")

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
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")

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
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")
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
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")
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
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")

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
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")

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
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")
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
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")

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

// --- GetEventsPage tests ---

func TestGetEventsPage_AllEventsNoFilter(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")
	for i := 0; i < 5; i++ {
		s.InsertEvent(bg(), task.ID, EventTypeOutput, i)
	}

	page, err := s.GetEventsPage(bg(), task.ID, 0, 0, nil)
	if err != nil {
		t.Fatalf("GetEventsPage: %v", err)
	}
	if len(page.Events) != 5 {
		t.Errorf("expected 5 events, got %d", len(page.Events))
	}
	if page.HasMore {
		t.Error("expected HasMore=false")
	}
	if page.TotalFiltered != 5 {
		t.Errorf("expected TotalFiltered=5, got %d", page.TotalFiltered)
	}
}

func TestGetEventsPage_OrderedByID(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")
	for i := 0; i < 5; i++ {
		s.InsertEvent(bg(), task.ID, EventTypeOutput, i)
	}

	page, err := s.GetEventsPage(bg(), task.ID, 0, 0, nil)
	if err != nil {
		t.Fatalf("GetEventsPage: %v", err)
	}
	for i := 1; i < len(page.Events); i++ {
		if page.Events[i].ID <= page.Events[i-1].ID {
			t.Errorf("events not in ascending ID order at index %d: %d <= %d",
				i, page.Events[i].ID, page.Events[i-1].ID)
		}
	}
}

func TestGetEventsPage_CursorAfterExclusive(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")
	for i := 0; i < 5; i++ {
		s.InsertEvent(bg(), task.ID, EventTypeOutput, i)
	}

	// Get the first 3 events to find the cursor.
	page1, _ := s.GetEventsPage(bg(), task.ID, 0, 3, nil)
	if len(page1.Events) != 3 {
		t.Fatalf("expected 3 events in page1, got %d", len(page1.Events))
	}
	if !page1.HasMore {
		t.Error("expected HasMore=true for page1")
	}
	cursor := page1.NextAfter

	// Use the cursor to get the remaining events.
	page2, _ := s.GetEventsPage(bg(), task.ID, cursor, 10, nil)
	if len(page2.Events) != 2 {
		t.Errorf("expected 2 events in page2, got %d", len(page2.Events))
	}
	if page2.HasMore {
		t.Error("expected HasMore=false for page2")
	}
	// Verify cursor exclusion: all page2 events have ID > cursor.
	for _, ev := range page2.Events {
		if ev.ID <= cursor {
			t.Errorf("event ID %d should be > cursor %d", ev.ID, cursor)
		}
	}
}

func TestGetEventsPage_CursorNextAfterIsLastID(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")
	for i := 0; i < 5; i++ {
		s.InsertEvent(bg(), task.ID, EventTypeOutput, i)
	}

	page, _ := s.GetEventsPage(bg(), task.ID, 0, 3, nil)
	want := page.Events[len(page.Events)-1].ID
	if page.NextAfter != want {
		t.Errorf("NextAfter = %d, want last event ID %d", page.NextAfter, want)
	}
}

func TestGetEventsPage_NextAfterZeroWhenEmpty(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")

	page, err := s.GetEventsPage(bg(), task.ID, 0, 10, nil)
	if err != nil {
		t.Fatalf("GetEventsPage: %v", err)
	}
	if page.NextAfter != 0 {
		t.Errorf("NextAfter = %d, want 0 for empty result", page.NextAfter)
	}
}

func TestGetEventsPage_TypeFilter(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")
	s.InsertEvent(bg(), task.ID, EventTypeStateChange, "a")
	s.InsertEvent(bg(), task.ID, EventTypeOutput, "b")
	s.InsertEvent(bg(), task.ID, EventTypeError, "c")
	s.InsertEvent(bg(), task.ID, EventTypeOutput, "d")

	typeSet := map[EventType]struct{}{EventTypeOutput: {}}
	page, err := s.GetEventsPage(bg(), task.ID, 0, 100, typeSet)
	if err != nil {
		t.Fatalf("GetEventsPage: %v", err)
	}
	if len(page.Events) != 2 {
		t.Errorf("expected 2 output events, got %d", len(page.Events))
	}
	for _, ev := range page.Events {
		if ev.EventType != EventTypeOutput {
			t.Errorf("unexpected event type %q, want output", ev.EventType)
		}
	}
	if page.TotalFiltered != 2 {
		t.Errorf("TotalFiltered = %d, want 2", page.TotalFiltered)
	}
}

func TestGetEventsPage_MultiTypeFilter(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")
	s.InsertEvent(bg(), task.ID, EventTypeStateChange, "s")
	s.InsertEvent(bg(), task.ID, EventTypeOutput, "o")
	s.InsertEvent(bg(), task.ID, EventTypeError, "e")
	s.InsertEvent(bg(), task.ID, EventTypeFeedback, "f")

	typeSet := map[EventType]struct{}{
		EventTypeStateChange: {},
		EventTypeFeedback:    {},
	}
	page, err := s.GetEventsPage(bg(), task.ID, 0, 100, typeSet)
	if err != nil {
		t.Fatalf("GetEventsPage: %v", err)
	}
	if len(page.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(page.Events))
	}
	for _, ev := range page.Events {
		if ev.EventType != EventTypeStateChange && ev.EventType != EventTypeFeedback {
			t.Errorf("unexpected event type %q", ev.EventType)
		}
	}
}

func TestGetEventsPage_LimitDefault(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")
	for i := 0; i < 5; i++ {
		s.InsertEvent(bg(), task.ID, EventTypeOutput, i)
	}

	// limit=0 should default to 200, returning all 5.
	page, err := s.GetEventsPage(bg(), task.ID, 0, 0, nil)
	if err != nil {
		t.Fatalf("GetEventsPage: %v", err)
	}
	if len(page.Events) != 5 {
		t.Errorf("expected 5 events with default limit, got %d", len(page.Events))
	}
}

func TestGetEventsPage_LimitCappedAt1000(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")
	for i := 0; i < 10; i++ {
		s.InsertEvent(bg(), task.ID, EventTypeOutput, i)
	}

	// limit=5000 should be capped to 1000, returning all 10 events.
	page, err := s.GetEventsPage(bg(), task.ID, 0, 5000, nil)
	if err != nil {
		t.Fatalf("GetEventsPage: %v", err)
	}
	if len(page.Events) != 10 {
		t.Errorf("expected all 10 events (limit capped), got %d", len(page.Events))
	}
}

func TestGetEventsPage_LimitTruncatesPage(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")
	for i := 0; i < 10; i++ {
		s.InsertEvent(bg(), task.ID, EventTypeOutput, i)
	}

	page, err := s.GetEventsPage(bg(), task.ID, 0, 4, nil)
	if err != nil {
		t.Fatalf("GetEventsPage: %v", err)
	}
	if len(page.Events) != 4 {
		t.Errorf("expected 4 events, got %d", len(page.Events))
	}
	if !page.HasMore {
		t.Error("expected HasMore=true when limit < total")
	}
	if page.TotalFiltered != 10 {
		t.Errorf("TotalFiltered = %d, want 10", page.TotalFiltered)
	}
}

func TestGetEventsPage_HasMoreFalseWhenExact(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")
	for i := 0; i < 5; i++ {
		s.InsertEvent(bg(), task.ID, EventTypeOutput, i)
	}

	page, err := s.GetEventsPage(bg(), task.ID, 0, 5, nil)
	if err != nil {
		t.Fatalf("GetEventsPage: %v", err)
	}
	if page.HasMore {
		t.Error("expected HasMore=false when limit == total")
	}
}

func TestGetEventsPage_TypeFilterWithCursor(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")
	s.InsertEvent(bg(), task.ID, EventTypeOutput, 1)      // ID 1
	s.InsertEvent(bg(), task.ID, EventTypeStateChange, 2) // ID 2
	s.InsertEvent(bg(), task.ID, EventTypeOutput, 3)      // ID 3
	s.InsertEvent(bg(), task.ID, EventTypeOutput, 4)      // ID 4

	// After ID=2, output only → should get IDs 3 and 4.
	typeSet := map[EventType]struct{}{EventTypeOutput: {}}
	page, err := s.GetEventsPage(bg(), task.ID, 2, 100, typeSet)
	if err != nil {
		t.Fatalf("GetEventsPage: %v", err)
	}
	if len(page.Events) != 2 {
		t.Errorf("expected 2 output events after cursor 2, got %d", len(page.Events))
	}
	for _, ev := range page.Events {
		if ev.ID <= 2 {
			t.Errorf("event ID %d should be > 2", ev.ID)
		}
		if ev.EventType != EventTypeOutput {
			t.Errorf("unexpected event type %q", ev.EventType)
		}
	}
}

func TestGetEventsPage_EmptyTask(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")

	page, err := s.GetEventsPage(bg(), task.ID, 0, 10, nil)
	if err != nil {
		t.Fatalf("GetEventsPage: %v", err)
	}
	if len(page.Events) != 0 {
		t.Errorf("expected 0 events for empty task, got %d", len(page.Events))
	}
	if page.HasMore {
		t.Error("expected HasMore=false for empty task")
	}
	if page.TotalFiltered != 0 {
		t.Errorf("TotalFiltered = %d, want 0", page.TotalFiltered)
	}
}

func TestGetEventsPage_FullPaginationWalk(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5, false, "", "")
	const total = 7
	for i := 0; i < total; i++ {
		s.InsertEvent(bg(), task.ID, EventTypeOutput, i)
	}

	// Walk pages of size 3.
	var collected []int64
	var cursor int64
	for {
		page, err := s.GetEventsPage(bg(), task.ID, cursor, 3, nil)
		if err != nil {
			t.Fatalf("GetEventsPage cursor=%d: %v", cursor, err)
		}
		for _, ev := range page.Events {
			collected = append(collected, ev.ID)
		}
		if !page.HasMore {
			break
		}
		cursor = page.NextAfter
	}

	if len(collected) != total {
		t.Errorf("expected %d total events across pages, got %d", total, len(collected))
	}
	// Verify all IDs are unique and ascending.
	for i := 1; i < len(collected); i++ {
		if collected[i] <= collected[i-1] {
			t.Errorf("IDs not strictly ascending at index %d: %d <= %d",
				i, collected[i], collected[i-1])
		}
	}
}
