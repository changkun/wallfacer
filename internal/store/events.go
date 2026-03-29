package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/pkg/pagination"
	"github.com/google/uuid"
)

// InsertEvent appends a new event to the task's audit trail.
func (s *Store) InsertEvent(_ context.Context, taskID uuid.UUID, eventType EventType, data any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tasks[taskID]; !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}

	s.ensureEventsLoadedLocked(taskID)
	seq := s.nextSeq[taskID]
	event := TaskEvent{
		ID:        int64(seq),
		TaskID:    taskID,
		EventType: eventType,
		Data:      jsonData,
		CreatedAt: time.Now(),
	}

	if err := s.backend.SaveEvent(taskID, seq, event); err != nil {
		return err
	}

	s.events[taskID] = append(s.events[taskID], event)
	s.nextSeq[taskID] = seq + 1
	return nil
}

// GetEvents returns a copy of all events for a task in order.
// If events have not been loaded yet (lazy loading for terminal tasks),
// this method upgrades from a read lock to a write lock, loads the events,
// then downgrades back to a read lock. The brief window between RUnlock
// and Lock where s.mu is unheld is safe because ensureEventsLoadedLocked
// is idempotent — a concurrent goroutine loading the same events is harmless.
func (s *Store) GetEvents(_ context.Context, taskID uuid.UUID) ([]TaskEvent, error) {
	s.mu.RLock()
	if !s.eventsLoaded[taskID] {
		// Events not yet loaded — release read lock, acquire write lock to load.
		s.mu.RUnlock()
		s.mu.Lock()
		s.ensureEventsLoadedLocked(taskID)
		s.mu.Unlock()
		s.mu.RLock()
	}
	defer s.mu.RUnlock()

	events := s.events[taskID]
	out := slices.Clone(events)
	return out, nil
}

// EventsPage holds the result of a paginated event query.
type EventsPage struct {
	Events        []TaskEvent
	NextAfter     int64
	HasMore       bool
	TotalFiltered int
}

// GetEventsPage returns a filtered, paginated page of events for a task.
//
// afterID is the exclusive cursor: only events with ID > afterID are returned.
// Use 0 to start from the beginning.
//
// limit caps the number of returned events. Values ≤ 0 default to 200; the
// maximum accepted value is 1000.
//
// typeSet restricts results to the given event types. A nil or empty map means
// all event types are included.
func (s *Store) GetEventsPage(_ context.Context, taskID uuid.UUID, afterID int64, limit int, typeSet map[EventType]struct{}) (EventsPage, error) {
	s.mu.RLock()
	if !s.eventsLoaded[taskID] {
		s.mu.RUnlock()
		s.mu.Lock()
		s.ensureEventsLoadedLocked(taskID)
		s.mu.Unlock()
		s.mu.RLock()
	}
	defer s.mu.RUnlock()

	var filter func(TaskEvent) bool
	if len(typeSet) > 0 {
		filter = func(ev TaskEvent) bool {
			_, ok := typeSet[ev.EventType]
			return ok
		}
	}

	// Paginate using event ID as the cursor key.
	// Default page size is 200, hard max is 1000.
	p := pagination.Paginate(
		s.events[taskID],
		func(ev TaskEvent) int64 { return ev.ID },
		afterID, limit, 200, 1000,
		filter,
	)

	return EventsPage{
		Events:        p.Items,
		NextAfter:     p.NextCursor,
		HasMore:       p.HasMore,
		TotalFiltered: p.TotalFiltered,
	}, nil
}

// SpanResult holds the paired timing data for a single execution span.
// EndedAt is zero and DurationMS is 0 for unclosed spans (no matching span_end).
type SpanResult struct {
	Phase      string    `json:"phase"`
	Label      string    `json:"label"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at"`
	DurationMS int64     `json:"duration_ms"`
}

// ComputeSpans pairs span_start/span_end events from the provided slice and
// returns a []SpanResult sorted by StartedAt. Unclosed spans are included
// with a zero EndedAt and DurationMS=0. When a phase+label key has multiple
// span_start events before a span_end, the most recent start wins.
func ComputeSpans(events []TaskEvent) ([]SpanResult, error) {
	type spanKey struct {
		phase string
		label string
	}
	startTimes := make(map[spanKey]time.Time)
	var spans []SpanResult

	for _, ev := range events {
		if ev.EventType != EventTypeSpanStart && ev.EventType != EventTypeSpanEnd {
			continue
		}
		var data SpanData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue
		}
		key := spanKey{phase: data.Phase, label: data.Label}
		if ev.EventType == EventTypeSpanStart {
			// If the same phase+label starts again before ending, the
			// previous start is overwritten (most recent start wins).
			startTimes[key] = ev.CreatedAt
		} else {
			if startedAt, ok := startTimes[key]; ok {
				spans = append(spans, SpanResult{
					Phase:      data.Phase,
					Label:      data.Label,
					StartedAt:  startedAt,
					EndedAt:    ev.CreatedAt,
					DurationMS: ev.CreatedAt.Sub(startedAt).Milliseconds(),
				})
				delete(startTimes, key)
			}
		}
	}

	// Include unclosed spans (span_start with no matching span_end).
	for key, startedAt := range startTimes {
		spans = append(spans, SpanResult{
			Phase:      key.phase,
			Label:      key.label,
			StartedAt:  startedAt,
			EndedAt:    time.Time{},
			DurationMS: 0,
		})
	}

	slices.SortFunc(spans, func(a, b SpanResult) int {
		return a.StartedAt.Compare(b.StartedAt)
	})

	return spans, nil
}

// numberedTraceFile represents a parsed trace file name (e.g. "0042.json")
// with its extracted sequence number. Used by FilesystemBackend to identify
// individual event files that have not yet been compacted.
type numberedTraceFile struct {
	name string // original file name, e.g. "0042.json"
	seq  int    // parsed sequence number, e.g. 42
}

// parseNumberedTraceFile extracts the sequence number from a trace file name
// like "0042.json". Returns false for non-numeric or non-.json file names
// (e.g. "compact.ndjson", "README.txt").
func parseNumberedTraceFile(name string) (numberedTraceFile, bool) {
	if !strings.HasSuffix(name, ".json") {
		return numberedTraceFile{}, false
	}
	base := strings.TrimSuffix(name, ".json")
	if base == "" {
		return numberedTraceFile{}, false
	}
	seq, err := strconv.Atoi(base)
	if err != nil {
		return numberedTraceFile{}, false
	}
	return numberedTraceFile{name: name, seq: seq}, true
}

// compactTaskEvents delegates to the backend to compact events for a task.
// The events to compact are taken from the in-memory event list, filtered
// to include only events with ID ≤ maxSeq.
func (s *Store) compactTaskEvents(taskID uuid.UUID, maxSeq int64) error {
	// Read events from memory. This is called from a background goroutine
	// after the lock has been released, so we need to acquire a read lock.
	s.mu.RLock()
	allEvents := s.events[taskID]
	var eventsToCompact []TaskEvent
	for _, evt := range allEvents {
		if evt.ID <= maxSeq {
			eventsToCompact = append(eventsToCompact, evt)
		}
	}
	s.mu.RUnlock()

	if len(eventsToCompact) == 0 {
		return nil
	}

	return s.backend.CompactEvents(taskID, eventsToCompact)
}
