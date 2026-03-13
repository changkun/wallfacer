package handler

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// SpanRecord holds the paired timing data for a single execution phase span.
type SpanRecord struct {
	Phase      string    `json:"phase"`
	Label      string    `json:"label"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at"`
	DurationMs int64     `json:"duration_ms"`
}

// computeSpans pairs span_start/span_end events from the given event slice and
// returns a sorted slice of SpanRecords. Unpaired starts are discarded; when
// multiple span_starts share the same phase+label key, they are matched to
// span_ends in FIFO order so every completed span is captured.
func computeSpans(events []store.TaskEvent) []SpanRecord {
	// pending[key] holds unmatched span_start events in arrival order (FIFO).
	pending := map[string][]*store.TaskEvent{}
	var records []SpanRecord

	for i := range events {
		e := &events[i]
		if e.EventType != store.EventTypeSpanStart && e.EventType != store.EventTypeSpanEnd {
			continue
		}
		var data store.SpanData
		if err := json.Unmarshal(e.Data, &data); err != nil {
			continue
		}
		key := data.Phase + ":" + data.Label
		switch e.EventType {
		case store.EventTypeSpanStart:
			pending[key] = append(pending[key], e)
		case store.EventTypeSpanEnd:
			if q := pending[key]; len(q) > 0 {
				start := q[0]
				pending[key] = q[1:]
				records = append(records, SpanRecord{
					Phase:      data.Phase,
					Label:      data.Label,
					StartedAt:  start.CreatedAt,
					EndedAt:    e.CreatedAt,
					DurationMs: e.CreatedAt.Sub(start.CreatedAt).Milliseconds(),
				})
			}
		}
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].StartedAt.Before(records[j].StartedAt)
	})

	return records
}

// GetTaskSpans reads all events for a task, pairs span_start/span_end events
// by phase+label, and returns a JSON array of SpanRecords sorted by started_at.
func (h *Handler) GetTaskSpans(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if _, err := h.store.GetTask(r.Context(), id); err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	events, err := h.store.GetEvents(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to read events", http.StatusInternalServerError)
		return
	}

	spans := computeSpans(events)
	if spans == nil {
		spans = []SpanRecord{}
	}
	writeJSON(w, http.StatusOK, spans)
}
