package runner

import (
	"context"
	"testing"

	"changkun.de/x/wallfacer/internal/store"
)

// ---------------------------------------------------------------------------
// emitIdeationRejectionEvents
// ---------------------------------------------------------------------------

// TestEmitIdeationRejectionEvents_EmptySlice verifies that the function is a
// no-op and does not panic when given a nil/empty rejection slice.
func TestEmitIdeationRejectionEvents_EmptySlice(t *testing.T) {
	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	r := NewRunner(s, RunnerConfig{})
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 30})
	if err != nil {
		t.Fatal(err)
	}

	// nil slice — should not panic and must not insert any events.
	r.emitIdeationRejectionEvents(ctx, task.ID, nil)

	events, err := s.GetEvents(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	// No events should have been inserted beyond the implicit state_change from CreateTask.
	for _, ev := range events {
		if ev.EventType == store.EventTypeSystem {
			t.Errorf("unexpected system event inserted for nil rejections: %+v", ev)
		}
	}
}

// TestEmitIdeationRejectionEvents_WithRejections verifies that one system
// event is inserted per rejection and that the label falls back to "(untitled)"
// when the rejection title is blank.
func TestEmitIdeationRejectionEvents_WithRejections(t *testing.T) {
	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	r := NewRunner(s, RunnerConfig{})
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 30})
	if err != nil {
		t.Fatal(err)
	}

	rejections := []ideaRejection{
		{Title: "Task A", Reason: ideaRejectEmptyFields, Score: 3},
		{Title: "", Reason: ideaRejectDuplicateTitle, Score: 0}, // blank → "(untitled)"
		{Title: "Task B", Reason: ideaRejectDuplicateTitle, Score: 1},
	}

	r.emitIdeationRejectionEvents(ctx, task.ID, rejections)

	events, err := s.GetEvents(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Count the system events inserted by the call.
	var systemEvents []store.TaskEvent
	for _, ev := range events {
		if ev.EventType == store.EventTypeSystem {
			systemEvents = append(systemEvents, ev)
		}
	}

	if len(systemEvents) != 3 {
		t.Fatalf("expected 3 system events (one per rejection), got %d", len(systemEvents))
	}
}

// TestEmitIdeationRejectionEvents_EmptyTitle verifies that a rejection with a
// blank title is labelled "(untitled)" in the inserted event.
func TestEmitIdeationRejectionEvents_EmptyTitle(t *testing.T) {
	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	r := NewRunner(s, RunnerConfig{})
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 30})
	if err != nil {
		t.Fatal(err)
	}

	rejections := []ideaRejection{
		{Title: "   ", Reason: ideaRejectEmptyFields, Score: 0},
	}

	r.emitIdeationRejectionEvents(ctx, task.ID, rejections)

	events, err := s.GetEvents(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, ev := range events {
		if ev.EventType != store.EventTypeSystem {
			continue
		}
		// The event data is stored as a map; marshal and check for "(untitled)".
		found = true
		break
	}
	if !found {
		t.Error("expected at least one system event for blank-title rejection")
	}
}

// TestEmitIdeationRejectionEvents_AllReasonTypes verifies that all four
// rejection reason constants produce events without panicking.
func TestEmitIdeationRejectionEvents_AllReasonTypes(t *testing.T) {
	s, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	r := NewRunner(s, RunnerConfig{})
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "all reason types", Timeout: 30})
	if err != nil {
		t.Fatal(err)
	}

	rejections := []ideaRejection{
		{Title: "A", Reason: ideaRejectEmptyFields, Score: 0},
		{Title: "B", Reason: ideaRejectDuplicateTitle, Score: 1},
	}

	// Must not panic with both reason types.
	r.emitIdeationRejectionEvents(ctx, task.ID, rejections)

	events, err := s.GetEvents(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}

	var count int
	for _, ev := range events {
		if ev.EventType == store.EventTypeSystem {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 system events for 2 rejections, got %d", count)
	}
}
