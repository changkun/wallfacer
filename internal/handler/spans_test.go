package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// --- computeSpans unit tests ---

func makeSpanEvent(eventType store.EventType, phase, label string, ts time.Time) store.TaskEvent {
	data, _ := json.Marshal(store.SpanData{Phase: phase, Label: label})
	return store.TaskEvent{
		EventType: eventType,
		Data:      data,
		CreatedAt: ts,
	}
}

func TestComputeSpans_PairedSpan(t *testing.T) {
	t0 := time.Now()
	t1 := t0.Add(50 * time.Millisecond)
	events := []store.TaskEvent{
		makeSpanEvent(store.EventTypeSpanStart, "worktree_setup", "worktree_setup", t0),
		makeSpanEvent(store.EventTypeSpanEnd, "worktree_setup", "worktree_setup", t1),
	}
	spans := computeSpans(events)
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Phase != "worktree_setup" {
		t.Errorf("expected phase 'worktree_setup', got %q", spans[0].Phase)
	}
	if spans[0].DurationMs != 50 {
		t.Errorf("expected DurationMs=50, got %d", spans[0].DurationMs)
	}
}

func TestComputeSpans_UnpairedStartOmitted(t *testing.T) {
	t0 := time.Now()
	events := []store.TaskEvent{
		makeSpanEvent(store.EventTypeSpanStart, "agent_turn", "agent_turn_1", t0),
		// no matching span_end
	}
	spans := computeSpans(events)
	if len(spans) != 0 {
		t.Errorf("expected 0 spans for unpaired start, got %d", len(spans))
	}
}

func TestComputeSpans_MostRecentStartWins(t *testing.T) {
	t0 := time.Now()
	t1 := t0.Add(10 * time.Millisecond)
	t2 := t1.Add(100 * time.Millisecond)
	// Two starts for the same key; the second (t1) should win.
	events := []store.TaskEvent{
		makeSpanEvent(store.EventTypeSpanStart, "agent_turn", "agent_turn_1", t0),
		makeSpanEvent(store.EventTypeSpanStart, "agent_turn", "agent_turn_1", t1),
		makeSpanEvent(store.EventTypeSpanEnd, "agent_turn", "agent_turn_1", t2),
	}
	spans := computeSpans(events)
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	// Duration must be measured from t1 (most recent start) to t2 = 100ms.
	if spans[0].DurationMs != 100 {
		t.Errorf("expected DurationMs=100, got %d", spans[0].DurationMs)
	}
}

func TestComputeSpans_MultiplePhases(t *testing.T) {
	t0 := time.Now()
	events := []store.TaskEvent{
		makeSpanEvent(store.EventTypeSpanStart, "worktree_setup", "worktree_setup", t0),
		makeSpanEvent(store.EventTypeSpanEnd, "worktree_setup", "worktree_setup", t0.Add(10*time.Millisecond)),
		makeSpanEvent(store.EventTypeSpanStart, "agent_turn", "agent_turn_1", t0.Add(20*time.Millisecond)),
		makeSpanEvent(store.EventTypeSpanEnd, "agent_turn", "agent_turn_1", t0.Add(30*time.Millisecond)),
		makeSpanEvent(store.EventTypeSpanStart, "container_run", "container_run", t0.Add(40*time.Millisecond)),
		makeSpanEvent(store.EventTypeSpanEnd, "container_run", "container_run", t0.Add(50*time.Millisecond)),
	}
	spans := computeSpans(events)
	if len(spans) != 3 {
		t.Fatalf("expected 3 spans, got %d", len(spans))
	}
	// Verify sorted by StartedAt.
	for i := 1; i < len(spans); i++ {
		if spans[i].StartedAt.Before(spans[i-1].StartedAt) {
			t.Errorf("spans not sorted by StartedAt at index %d", i)
		}
	}
}

// --- GetTaskSpans HTTP handler tests ---

func TestGetTaskSpans_NotFound(t *testing.T) {
	h := newTestHandler(t)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+id.String()+"/spans", nil)
	w := httptest.NewRecorder()
	h.GetTaskSpans(w, req, id)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestGetTaskSpans_EmptyWhenNoSpanEvents(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/spans", nil)
	w := httptest.NewRecorder()
	h.GetTaskSpans(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var spans []SpanRecord
	if err := json.NewDecoder(w.Body).Decode(&spans); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(spans) != 0 {
		t.Errorf("expected 0 spans, got %d", len(spans))
	}
}

func TestGetTaskSpans_PairsSingleSpan(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{Phase: "worktree_setup", Label: "worktree_setup"})
	time.Sleep(5 * time.Millisecond) // ensure measurable duration
	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanEnd, store.SpanData{Phase: "worktree_setup", Label: "worktree_setup"})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/spans", nil)
	w := httptest.NewRecorder()
	h.GetTaskSpans(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var spans []SpanRecord
	if err := json.NewDecoder(w.Body).Decode(&spans); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Phase != "worktree_setup" {
		t.Errorf("expected phase 'worktree_setup', got %q", spans[0].Phase)
	}
	if spans[0].Label != "worktree_setup" {
		t.Errorf("expected label 'worktree_setup', got %q", spans[0].Label)
	}
	if spans[0].DurationMs < 0 {
		t.Errorf("expected non-negative duration, got %d", spans[0].DurationMs)
	}
}

func TestGetTaskSpans_MultipleSpansSortedByStartTime(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	// Insert spans for two turns in chronological order.
	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{Phase: "worktree_setup", Label: "worktree_setup"})
	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanEnd, store.SpanData{Phase: "worktree_setup", Label: "worktree_setup"})

	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{Phase: "agent_turn", Label: "agent_turn_1"})
	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanEnd, store.SpanData{Phase: "agent_turn", Label: "agent_turn_1"})

	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{Phase: "agent_turn", Label: "agent_turn_2"})
	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanEnd, store.SpanData{Phase: "agent_turn", Label: "agent_turn_2"})

	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{Phase: "commit", Label: "commit"})
	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanEnd, store.SpanData{Phase: "commit", Label: "commit"})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/spans", nil)
	w := httptest.NewRecorder()
	h.GetTaskSpans(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var spans []SpanRecord
	if err := json.NewDecoder(w.Body).Decode(&spans); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(spans) != 4 {
		t.Fatalf("expected 4 spans, got %d", len(spans))
	}

	// Verify ordering by started_at (ascending).
	for i := 1; i < len(spans); i++ {
		if spans[i].StartedAt.Before(spans[i-1].StartedAt) {
			t.Errorf("span %d started before span %d: %v < %v",
				i, i-1, spans[i].StartedAt, spans[i-1].StartedAt)
		}
	}

	// Verify phase and label values.
	expected := []struct{ phase, label string }{
		{"worktree_setup", "worktree_setup"},
		{"agent_turn", "agent_turn_1"},
		{"agent_turn", "agent_turn_2"},
		{"commit", "commit"},
	}
	for i, e := range expected {
		if spans[i].Phase != e.phase {
			t.Errorf("span %d: expected phase %q, got %q", i, e.phase, spans[i].Phase)
		}
		if spans[i].Label != e.label {
			t.Errorf("span %d: expected label %q, got %q", i, e.label, spans[i].Label)
		}
	}
}

func TestGetTaskSpans_DurationMsCorrect(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	before := time.Now()
	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{Phase: "commit", Label: "commit"})
	time.Sleep(10 * time.Millisecond)
	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanEnd, store.SpanData{Phase: "commit", Label: "commit"})
	after := time.Now()

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/spans", nil)
	w := httptest.NewRecorder()
	h.GetTaskSpans(w, req, task.ID)

	var spans []SpanRecord
	json.NewDecoder(w.Body).Decode(&spans)
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	maxExpected := after.Sub(before).Milliseconds() + 5 // small tolerance
	if spans[0].DurationMs < 10 || spans[0].DurationMs > maxExpected {
		t.Errorf("duration_ms %d out of expected range [10, %d]", spans[0].DurationMs, maxExpected)
	}
}

func TestGetTaskSpans_UnpairedStartIgnored(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	// Start without end.
	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{Phase: "commit", Label: "commit"})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/spans", nil)
	w := httptest.NewRecorder()
	h.GetTaskSpans(w, req, task.ID)

	var spans []SpanRecord
	json.NewDecoder(w.Body).Decode(&spans)
	if len(spans) != 0 {
		t.Errorf("expected 0 spans for unpaired start, got %d", len(spans))
	}
}

func TestGetTaskSpans_NonSpanEventsIgnored(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTask(ctx, "test", 15, false, "", "")

	// Mix span events with non-span events.
	h.store.InsertEvent(ctx, task.ID, store.EventTypeStateChange, map[string]string{"to": "in_progress"})
	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{Phase: "agent_turn", Label: "agent_turn_1"})
	h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, map[string]string{"result": "done"})
	h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanEnd, store.SpanData{Phase: "agent_turn", Label: "agent_turn_1"})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/spans", nil)
	w := httptest.NewRecorder()
	h.GetTaskSpans(w, req, task.ID)

	var spans []SpanRecord
	json.NewDecoder(w.Body).Decode(&spans)
	if len(spans) != 1 {
		t.Errorf("expected 1 span (non-span events ignored), got %d", len(spans))
	}
	if spans[0].Phase != "agent_turn" {
		t.Errorf("expected phase 'agent_turn', got %q", spans[0].Phase)
	}
}
