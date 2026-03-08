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
// returns a sorted slice of SpanRecords. Unpaired starts are discarded; the
// most-recent start wins when a phase+label key is seen multiple times.
func computeSpans(events []store.TaskEvent) []SpanRecord {
	type spanKey struct {
		phase string
		label string
	}
	// startTimes maps phase+label to the timestamp of its most recent span_start.
	// We use the most recent start to handle potential retries cleanly.
	startTimes := make(map[spanKey]time.Time)
	var spans []SpanRecord

	for _, ev := range events {
		if ev.EventType != store.EventTypeSpanStart && ev.EventType != store.EventTypeSpanEnd {
			continue
		}
		var data store.SpanData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue
		}
		key := spanKey{phase: data.Phase, label: data.Label}
		if ev.EventType == store.EventTypeSpanStart {
			startTimes[key] = ev.CreatedAt
		} else {
			// span_end: pair with the matching span_start if present.
			if startedAt, ok := startTimes[key]; ok {
				spans = append(spans, SpanRecord{
					Phase:      data.Phase,
					Label:      data.Label,
					StartedAt:  startedAt,
					EndedAt:    ev.CreatedAt,
					DurationMs: ev.CreatedAt.Sub(startedAt).Milliseconds(),
				})
				delete(startTimes, key)
			}
		}
	}

	sort.Slice(spans, func(i, j int) bool {
		return spans[i].StartedAt.Before(spans[j].StartedAt)
	})

	return spans
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
