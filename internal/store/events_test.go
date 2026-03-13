// Tests for events.go: InsertEvent, GetEvents, ComputeSpans, and event persistence/reload.
package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func insertOutputEvents(t *testing.T, s *Store, taskID uuid.UUID, count int) {
	t.Helper()
	for i := 1; i <= count; i++ {
		if err := s.InsertEvent(bg(), taskID, EventTypeOutput, map[string]int{"n": i}); err != nil {
			t.Fatalf("InsertEvent[%d]: %v", i, err)
		}
	}
}

func readCompactEvents(t *testing.T, path string) ([]TaskEvent, []byte) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	lines := bytes.Split(raw, []byte{'\n'})
	var events []TaskEvent
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var evt TaskEvent
		if err := json.Unmarshal(line, &evt); err != nil {
			t.Fatalf("unmarshal compact line: %v", err)
		}
		events = append(events, evt)
	}
	return events, raw
}

// makeSpanEvt constructs a TaskEvent for span testing without a real store.
func makeSpanEvt(eventType EventType, phase, label string, ts time.Time) TaskEvent {
	data, _ := json.Marshal(SpanData{Phase: phase, Label: label})
	return TaskEvent{
		EventType: eventType,
		Data:      data,
		CreatedAt: ts,
	}
}

func TestComputeSpans_TwoAgentTurns(t *testing.T) {
	t0 := time.Now()
	events := []TaskEvent{
		makeSpanEvt(EventTypeSpanStart, "worktree_setup", "worktree_setup", t0),
		makeSpanEvt(EventTypeSpanEnd, "worktree_setup", "worktree_setup", t0.Add(10*time.Millisecond)),
		makeSpanEvt(EventTypeSpanStart, "agent_turn", "agent_turn_1", t0.Add(20*time.Millisecond)),
		makeSpanEvt(EventTypeSpanEnd, "agent_turn", "agent_turn_1", t0.Add(30*time.Millisecond)),
		makeSpanEvt(EventTypeSpanStart, "agent_turn", "agent_turn_2", t0.Add(40*time.Millisecond)),
		makeSpanEvt(EventTypeSpanEnd, "agent_turn", "agent_turn_2", t0.Add(50*time.Millisecond)),
	}
	spans, err := ComputeSpans(events)
	if err != nil {
		t.Fatalf("ComputeSpans returned error: %v", err)
	}
	if len(spans) != 3 {
		t.Fatalf("expected 3 spans, got %d", len(spans))
	}
	// Verify sorted by StartedAt ascending.
	for i := 1; i < len(spans); i++ {
		if spans[i].StartedAt.Before(spans[i-1].StartedAt) {
			t.Errorf("spans not sorted by StartedAt at index %d", i)
		}
	}
	// Verify phases and labels.
	expected := []struct{ phase, label string }{
		{"worktree_setup", "worktree_setup"},
		{"agent_turn", "agent_turn_1"},
		{"agent_turn", "agent_turn_2"},
	}
	for i, e := range expected {
		if spans[i].Phase != e.phase {
			t.Errorf("span[%d].Phase = %q, want %q", i, spans[i].Phase, e.phase)
		}
		if spans[i].Label != e.label {
			t.Errorf("span[%d].Label = %q, want %q", i, spans[i].Label, e.label)
		}
		if spans[i].DurationMS < 0 {
			t.Errorf("span[%d].DurationMS = %d, want >= 0", i, spans[i].DurationMS)
		}
	}
}

func TestComputeSpans_UnclosedSpanIncluded(t *testing.T) {
	t0 := time.Now()
	events := []TaskEvent{
		makeSpanEvt(EventTypeSpanStart, "agent_turn", "agent_turn_1", t0),
		// no matching span_end
	}
	spans, err := ComputeSpans(events)
	if err != nil {
		t.Fatalf("ComputeSpans returned error: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("expected 1 span for unclosed start, got %d", len(spans))
	}
	if !spans[0].EndedAt.IsZero() {
		t.Errorf("expected EndedAt to be zero for unclosed span, got %v", spans[0].EndedAt)
	}
	if spans[0].DurationMS != 0 {
		t.Errorf("expected DurationMS=0 for unclosed span, got %d", spans[0].DurationMS)
	}
}

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

func TestCompactTaskEvents_Basic(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(context.Background(), "p", 5, false, "", TaskKindTask)
	insertOutputEvents(t, s, task.ID, 10)

	if err := s.compactTaskEvents(task.ID); err != nil {
		t.Fatalf("compactTaskEvents: %v", err)
	}

	tracesDir := filepath.Join(s.dir, task.ID.String(), "traces")
	compactPath := filepath.Join(tracesDir, "compact.ndjson")
	events, _ := readCompactEvents(t, compactPath)
	if len(events) != 10 {
		t.Fatalf("expected 10 compacted events, got %d", len(events))
	}
	for i, evt := range events {
		if evt.ID != int64(i+1) {
			t.Fatalf("events[%d].ID = %d, want %d", i, evt.ID, i+1)
		}
	}

	entries, err := os.ReadDir(tracesDir)
	if err != nil {
		t.Fatalf("ReadDir(traces): %v", err)
	}
	for _, entry := range entries {
		if entry.Name() == "compact.ndjson" {
			continue
		}
		if _, ok := parseNumberedTraceFile(entry.Name()); ok {
			t.Fatalf("numbered trace file still exists after compaction: %s", entry.Name())
		}
	}
}

func TestCompactTaskEvents_LoadEventsAfterCompaction(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	task, _ := s.CreateTask(context.Background(), "p", 5, false, "", TaskKindTask)
	insertOutputEvents(t, s, task.ID, 10)
	if err := s.compactTaskEvents(task.ID); err != nil {
		t.Fatalf("compactTaskEvents: %v", err)
	}

	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	events, err := s2.GetEvents(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 10 {
		t.Fatalf("expected 10 events, got %d", len(events))
	}
	for i, evt := range events {
		if evt.ID != int64(i+1) {
			t.Fatalf("events[%d].ID = %d, want %d", i, evt.ID, i+1)
		}
	}
	if got := s2.nextSeq[task.ID]; got != 11 {
		t.Fatalf("nextSeq = %d, want 11", got)
	}
}

func TestCompactTaskEvents_HybridLoad(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	task, _ := s.CreateTask(context.Background(), "p", 5, false, "", TaskKindTask)
	insertOutputEvents(t, s, task.ID, 8)
	if err := s.compactTaskEvents(task.ID); err != nil {
		t.Fatalf("compactTaskEvents: %v", err)
	}

	tracesDir := filepath.Join(dir, task.ID.String(), "traces")
	for i := 9; i <= 10; i++ {
		event := TaskEvent{
			ID:        int64(i),
			TaskID:    task.ID,
			EventType: EventTypeOutput,
			Data:      json.RawMessage([]byte(`{"n":` + strconv.Itoa(i) + `}`)),
			CreatedAt: time.Now(),
		}
		if err := atomicWriteJSON(filepath.Join(tracesDir, fmt.Sprintf("%04d.json", i)), event); err != nil {
			t.Fatalf("atomicWriteJSON(%d): %v", i, err)
		}
	}

	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	events, err := s2.GetEvents(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 10 {
		t.Fatalf("expected 10 events, got %d", len(events))
	}
	for i, evt := range events {
		if evt.ID != int64(i+1) {
			t.Fatalf("events[%d].ID = %d, want %d", i, evt.ID, i+1)
		}
	}
	if got := s2.nextSeq[task.ID]; got != 11 {
		t.Fatalf("nextSeq = %d, want 11", got)
	}
}

func TestCompactTaskEvents_Idempotent(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(context.Background(), "p", 5, false, "", TaskKindTask)
	insertOutputEvents(t, s, task.ID, 10)

	if err := s.compactTaskEvents(task.ID); err != nil {
		t.Fatalf("first compactTaskEvents: %v", err)
	}
	tracesDir := filepath.Join(s.dir, task.ID.String(), "traces")
	compactPath := filepath.Join(tracesDir, "compact.ndjson")
	_, before := readCompactEvents(t, compactPath)

	if err := s.compactTaskEvents(task.ID); err != nil {
		t.Fatalf("second compactTaskEvents: %v", err)
	}
	_, after := readCompactEvents(t, compactPath)
	if !bytes.Equal(before, after) {
		t.Fatal("compact.ndjson changed after idempotent compaction run")
	}

	entries, err := os.ReadDir(tracesDir)
	if err != nil {
		t.Fatalf("ReadDir(traces): %v", err)
	}
	for _, entry := range entries {
		if _, ok := parseNumberedTraceFile(entry.Name()); ok {
			t.Fatalf("numbered trace file reappeared after second compaction: %s", entry.Name())
		}
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
