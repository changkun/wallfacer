package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

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

	seq := s.nextSeq[taskID]
	event := TaskEvent{
		ID:        int64(seq),
		TaskID:    taskID,
		EventType: eventType,
		Data:      jsonData,
		CreatedAt: time.Now(),
	}

	if err := s.saveEvent(taskID, seq, event); err != nil {
		return err
	}

	s.events[taskID] = append(s.events[taskID], event)
	s.nextSeq[taskID] = seq + 1
	return nil
}

// GetEvents returns a copy of all events for a task in order.
func (s *Store) GetEvents(_ context.Context, taskID uuid.UUID) ([]TaskEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events := s.events[taskID]
	out := make([]TaskEvent, len(events))
	copy(out, events)
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
	const defaultLimit = 200
	const maxLimit = 1000

	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Events are already sorted by ID (guaranteed by loadEvents and append order).
	var filtered []TaskEvent
	for _, ev := range s.events[taskID] {
		if ev.ID <= afterID {
			continue
		}
		if len(typeSet) > 0 {
			if _, ok := typeSet[ev.EventType]; !ok {
				continue
			}
		}
		filtered = append(filtered, ev)
	}

	total := len(filtered)
	hasMore := total > limit

	page := filtered
	if total > limit {
		page = filtered[:limit]
	}

	var nextAfter int64
	if len(page) > 0 {
		nextAfter = page[len(page)-1].ID
	}

	return EventsPage{
		Events:        page,
		NextAfter:     nextAfter,
		HasMore:       hasMore,
		TotalFiltered: total,
	}, nil
}

// saveEvent writes a single event to the task's traces directory.
// Must be called with s.mu held for writing.
func (s *Store) saveEvent(taskID uuid.UUID, seq int, event TaskEvent) error {
	tracesDir := filepath.Join(s.dir, taskID.String(), "traces")
	if err := os.MkdirAll(tracesDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(tracesDir, fmt.Sprintf("%04d.json", seq))
	return atomicWriteJSON(path, event)
}
