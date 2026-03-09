package handler

import (
	"net/http"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// GetTaskSpans reads all events for a task, pairs span_start/span_end events
// by phase+label, and returns a JSON object {"spans": [...]} sorted by started_at.
// Unclosed spans are included with a zero ended_at and duration_ms=0.
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

	spans, _ := store.ComputeSpans(events)
	if spans == nil {
		spans = []store.SpanResult{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"spans": spans})
}
