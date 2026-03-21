package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/logger"
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

	sort.Slice(spans, func(i, j int) bool {
		return spans[i].StartedAt.Before(spans[j].StartedAt)
	})

	return spans, nil
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

type numberedTraceFile struct {
	name string
	seq  int
}

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

// currentMaxEventSeq reads the traces directory for taskID and returns the
// highest sequence number among all numbered trace files (e.g. 0005.json → 5).
// Returns 0 if the directory is empty, does not exist, or contains no numbered
// files. This is a pure filesystem read with no in-memory side effects.
func (s *Store) currentMaxEventSeq(taskID uuid.UUID) (int64, error) {
	tracesDir := filepath.Join(s.dir, taskID.String(), "traces")
	entries, err := os.ReadDir(tracesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var maxSeq int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		tf, ok := parseNumberedTraceFile(entry.Name())
		if !ok {
			continue
		}
		if int64(tf.seq) > maxSeq {
			maxSeq = int64(tf.seq)
		}
	}
	return maxSeq, nil
}

// compactTaskEvents merges all numbered trace files whose sequence number is
// ≤ maxSeq into a single compact.ndjson file, then removes those individual
// files. Files beyond maxSeq are left untouched, preserving session boundaries
// when a task is retried immediately after completion.
func (s *Store) compactTaskEvents(taskID uuid.UUID, maxSeq int64) error {
	tracesDir := filepath.Join(s.dir, taskID.String(), "traces")
	entries, err := os.ReadDir(tracesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var traceFiles []numberedTraceFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		traceFile, ok := parseNumberedTraceFile(entry.Name())
		if !ok {
			continue
		}
		if int64(traceFile.seq) > maxSeq {
			continue // beyond the session boundary; leave for the next session
		}
		traceFiles = append(traceFiles, traceFile)
	}
	if len(traceFiles) == 0 {
		return nil
	}

	sort.Slice(traceFiles, func(i, j int) bool {
		return traceFiles[i].seq < traceFiles[j].seq
	})

	var compact []byte
	for _, traceFile := range traceFiles {
		path := filepath.Join(tracesDir, traceFile.name)
		raw, err := os.ReadFile(path)
		if err != nil {
			logger.Store.Warn("compact: skipping unreadable trace", "task", taskID, "trace", traceFile.name, "error", err)
			continue
		}

		var evt TaskEvent
		if err := json.Unmarshal(raw, &evt); err != nil {
			logger.Store.Warn("compact: skipping corrupt trace", "task", taskID, "trace", traceFile.name, "error", err)
			continue
		}

		line, err := json.Marshal(evt)
		if err != nil {
			logger.Store.Warn("compact: skipping unmarshalable trace", "task", taskID, "trace", traceFile.name, "error", err)
			continue
		}
		compact = append(compact, line...)
		compact = append(compact, '\n')
	}

	tmpPath := filepath.Join(tracesDir, "compact.ndjson.tmp")
	compactPath := filepath.Join(tracesDir, "compact.ndjson")
	if err := os.WriteFile(tmpPath, compact, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, compactPath); err != nil {
		return err
	}

	for _, traceFile := range traceFiles {
		if err := os.Remove(filepath.Join(tracesDir, traceFile.name)); err != nil && !os.IsNotExist(err) {
			logger.Store.Warn("compact: failed to remove trace", "task", taskID, "trace", traceFile.name, "error", err)
		}
	}

	return nil
}
