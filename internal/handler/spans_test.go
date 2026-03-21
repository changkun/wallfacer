package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// spansEnvelope mirrors the {"spans": [...]} response shape.
type spansEnvelope struct {
	Spans []store.SpanResult `json:"spans"`
}

func makeSpanEvent(eventType store.EventType, phase, label string, ts time.Time) store.TaskEvent {
	data, _ := json.Marshal(store.SpanData{Phase: phase, Label: label})
	return store.TaskEvent{
		EventType: eventType,
		Data:      data,
		CreatedAt: ts,
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
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/spans", nil)
	w := httptest.NewRecorder()
	h.GetTaskSpans(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var env spansEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Spans) != 0 {
		t.Errorf("expected 0 spans, got %d", len(env.Spans))
	}
}

func TestGetTaskSpans_PairsSingleSpan(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{Phase: "worktree_setup", Label: "worktree_setup"})

	time.Sleep(5 * time.Millisecond) // ensure measurable duration
	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanEnd, store.SpanData{Phase: "worktree_setup", Label: "worktree_setup"})


	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/spans", nil)
	w := httptest.NewRecorder()
	h.GetTaskSpans(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var env spansEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(env.Spans))
	}
	if env.Spans[0].Phase != "worktree_setup" {
		t.Errorf("expected phase 'worktree_setup', got %q", env.Spans[0].Phase)
	}
	if env.Spans[0].Label != "worktree_setup" {
		t.Errorf("expected label 'worktree_setup', got %q", env.Spans[0].Label)
	}
	if env.Spans[0].DurationMS < 0 {
		t.Errorf("expected non-negative duration, got %d", env.Spans[0].DurationMS)
	}
}

func TestGetTaskSpans_MultipleSpansSortedByStartTime(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	// Insert spans for two turns in chronological order.
	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{Phase: "worktree_setup", Label: "worktree_setup"})

	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanEnd, store.SpanData{Phase: "worktree_setup", Label: "worktree_setup"})


	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{Phase: "agent_turn", Label: "agent_turn_1"})

	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanEnd, store.SpanData{Phase: "agent_turn", Label: "agent_turn_1"})


	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{Phase: "agent_turn", Label: "agent_turn_2"})

	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanEnd, store.SpanData{Phase: "agent_turn", Label: "agent_turn_2"})


	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{Phase: "commit", Label: "commit"})

	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanEnd, store.SpanData{Phase: "commit", Label: "commit"})


	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/spans", nil)
	w := httptest.NewRecorder()
	h.GetTaskSpans(w, req, task.ID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var env spansEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Spans) != 4 {
		t.Fatalf("expected 4 spans, got %d", len(env.Spans))
	}

	// Verify ordering by started_at (ascending).
	for i := 1; i < len(env.Spans); i++ {
		if env.Spans[i].StartedAt.Before(env.Spans[i-1].StartedAt) {
			t.Errorf("span %d started before span %d: %v < %v",
				i, i-1, env.Spans[i].StartedAt, env.Spans[i-1].StartedAt)
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
		if env.Spans[i].Phase != e.phase {
			t.Errorf("span %d: expected phase %q, got %q", i, e.phase, env.Spans[i].Phase)
		}
		if env.Spans[i].Label != e.label {
			t.Errorf("span %d: expected label %q, got %q", i, e.label, env.Spans[i].Label)
		}
	}
}

func TestGetTaskSpans_DurationMSCorrect(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	before := time.Now()
	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{Phase: "commit", Label: "commit"})

	time.Sleep(10 * time.Millisecond)
	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanEnd, store.SpanData{Phase: "commit", Label: "commit"})

	after := time.Now()

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/spans", nil)
	w := httptest.NewRecorder()
	h.GetTaskSpans(w, req, task.ID)

	var env spansEnvelope
	_ = json.NewDecoder(w.Body).Decode(&env)

	if len(env.Spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(env.Spans))
	}
	maxExpected := after.Sub(before).Milliseconds() + 5 // small tolerance
	if env.Spans[0].DurationMS < 10 || env.Spans[0].DurationMS > maxExpected {
		t.Errorf("duration_ms %d out of expected range [10, %d]", env.Spans[0].DurationMS, maxExpected)
	}
}

func TestGetTaskSpans_UnclosedSpanIncluded(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	// Start without end.
	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{Phase: "commit", Label: "commit"})


	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/spans", nil)
	w := httptest.NewRecorder()
	h.GetTaskSpans(w, req, task.ID)

	var env spansEnvelope
	_ = json.NewDecoder(w.Body).Decode(&env)

	if len(env.Spans) != 1 {
		t.Errorf("expected 1 span for unclosed start, got %d", len(env.Spans))
	}
	if len(env.Spans) == 1 {
		if !env.Spans[0].EndedAt.IsZero() {
			t.Errorf("expected zero EndedAt for unclosed span, got %v", env.Spans[0].EndedAt)
		}
		if env.Spans[0].DurationMS != 0 {
			t.Errorf("expected DurationMS=0 for unclosed span, got %d", env.Spans[0].DurationMS)
		}
	}
}

// TestComputeSpans_AllSandboxActivities verifies that container_run spans for
// all SandboxActivity constants are correctly paired and returned.
func TestComputeSpans_AllSandboxActivities(t *testing.T) {
	t0 := time.Now()
	activities := store.SandboxActivities
	var events []store.TaskEvent
	for i, act := range activities {
		offset := time.Duration(i*10) * time.Millisecond
		events = append(events,
			makeSpanEvent(store.EventTypeSpanStart, "container_run", string(act), t0.Add(offset)),
			makeSpanEvent(store.EventTypeSpanEnd, "container_run", string(act), t0.Add(offset+5*time.Millisecond)),
		)
	}
	spans, _ := store.ComputeSpans(events)
	if len(spans) != len(activities) {
		t.Fatalf("expected %d spans (one per activity), got %d", len(activities), len(spans))
	}
	// Verify each activity has a matching span.
	found := make(map[string]bool)
	for _, s := range spans {
		if s.Phase != "container_run" {
			t.Errorf("expected phase 'container_run', got %q", s.Phase)
		}
		found[s.Label] = true
	}
	for _, act := range activities {
		if !found[string(act)] {
			t.Errorf("no span found for activity %q", act)
		}
	}
}

func TestGetTaskSpans_NonSpanEventsIgnored(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	task, _ := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test", Timeout: 15})

	// Mix span events with non-span events.
	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeStateChange, map[string]string{"to": "in_progress"})

	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanStart, store.SpanData{Phase: "agent_turn", Label: "agent_turn_1"})

	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeOutput, map[string]string{"result": "done"})

	_ = h.store.InsertEvent(ctx, task.ID, store.EventTypeSpanEnd, store.SpanData{Phase: "agent_turn", Label: "agent_turn_1"})


	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID.String()+"/spans", nil)
	w := httptest.NewRecorder()
	h.GetTaskSpans(w, req, task.ID)

	var env spansEnvelope
	_ = json.NewDecoder(w.Body).Decode(&env)

	if len(env.Spans) != 1 {
		t.Errorf("expected 1 span (non-span events ignored), got %d", len(env.Spans))
	}
	if len(env.Spans) == 1 && env.Spans[0].Phase != "agent_turn" {
		t.Errorf("expected phase 'agent_turn', got %q", env.Spans[0].Phase)
	}
}
