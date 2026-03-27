package handler

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"

	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// GetTurnUsage returns token usage for a specific task turn.
func (h *Handler) GetTurnUsage(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid task id", http.StatusBadRequest)
		return
	}
	records, err := h.store.GetTurnUsages(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, records)
}

// eventsPageResponse is the JSON envelope returned when pagination params are present.
type eventsPageResponse struct {
	Events        []store.TaskEvent `json:"events"`
	NextAfter     int64             `json:"next_after"`
	HasMore       bool              `json:"has_more"`
	TotalFiltered int               `json:"total_filtered"`
}

// validEventTypes is the set of known event type strings for param validation.
var validEventTypes = map[string]store.EventType{
	string(store.EventTypeStateChange): store.EventTypeStateChange,
	string(store.EventTypeOutput):      store.EventTypeOutput,
	string(store.EventTypeFeedback):    store.EventTypeFeedback,
	string(store.EventTypeError):       store.EventTypeError,
	string(store.EventTypeSystem):      store.EventTypeSystem,
	string(store.EventTypeSpanStart):   store.EventTypeSpanStart,
	string(store.EventTypeSpanEnd):     store.EventTypeSpanEnd,
}

// GetEvents returns the event timeline for a task.
//
// Without query params, the full event list is returned as a JSON array
// (backward-compatible behaviour).
//
// With any of after, limit, or types present, a paginated envelope is returned:
//
//	{"events": [...], "next_after": <int64>, "has_more": <bool>, "total_filtered": <int>}
//
// Query params:
//   - after  – exclusive event ID cursor; only events with ID > after are returned (default 0)
//   - limit  – max events per page, 1–1000 (default 200)
//   - types  – comma-separated event types to include (default: all types)
func (h *Handler) GetEvents(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	q := r.URL.Query()
	isPaged := q.Has("after") || q.Has("limit") || q.Has("types")

	if !isPaged {
		// Backward-compatible: return the full list as a plain JSON array.
		events, err := h.store.GetEvents(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if events == nil {
			events = []store.TaskEvent{}
		}
		writeJSON(w, http.StatusOK, events)
		return
	}

	// Parse and validate pagination params.
	var afterID int64
	if v := q.Get("after"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			http.Error(w, "after must be a non-negative integer", http.StatusBadRequest)
			return
		}
		afterID = n
	}

	limit := 200 // default
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
			return
		}
		if n > 1000 {
			n = 1000
		}
		limit = n
	}

	var typeSet map[store.EventType]struct{}
	if v := q.Get("types"); v != "" {
		typeSet = make(map[store.EventType]struct{})
		for raw := range strings.SplitSeq(v, ",") {
			t := strings.TrimSpace(raw)
			if t == "" {
				continue
			}
			et, ok := validEventTypes[t]
			if !ok {
				http.Error(w, "unknown event type: "+t, http.StatusBadRequest)
				return
			}
			typeSet[et] = struct{}{}
		}
		if len(typeSet) == 0 {
			typeSet = nil
		}
	}

	page, err := h.store.GetEventsPage(r.Context(), id, afterID, limit, typeSet)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	events := page.Events
	if events == nil {
		events = []store.TaskEvent{}
	}
	writeJSON(w, http.StatusOK, eventsPageResponse{
		Events:        events,
		NextAfter:     page.NextAfter,
		HasMore:       page.HasMore,
		TotalFiltered: page.TotalFiltered,
	})
}

// ServeOutput serves a raw turn output file for a task.
func (h *Handler) ServeOutput(w http.ResponseWriter, _ *http.Request, id uuid.UUID, filename string) {
	// Validate filename to prevent path traversal.
	if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	data, err := h.store.ReadBlob(id, "outputs/"+filename)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Detect server-side truncation by checking the tail of the data.
	// A truncation_notice sentinel is appended by SaveTurnOutput when the output
	// exceeds the WALLFACER_MAX_TURN_OUTPUT_BYTES budget.
	if len(data) > 0 {
		tailSize := 256
		if len(data) < tailSize {
			tailSize = len(data)
		}
		if bytes.Contains(data[len(data)-tailSize:], []byte(`"truncation_notice"`)) {
			w.Header().Set("X-Wallfacer-Truncated", "true")
		}
	}

	switch {
	case strings.HasSuffix(filename, ".json"):
		w.Header().Set("Content-Type", "application/json")
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	_, _ = w.Write(data)
}

// GenerateMissingTitles triggers background title generation for untitled tasks.
func (h *Handler) GenerateMissingTitles(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			limit = n
		}
	}

	tasks, err := h.store.ListTasks(r.Context(), true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var untitled []store.Task
	for _, t := range tasks {
		if t.Title == "" {
			untitled = append(untitled, t)
		}
	}

	total := len(untitled)
	if limit > 0 && len(untitled) > limit {
		untitled = untitled[:limit]
	}

	taskIDs := make([]string, len(untitled))
	for i, t := range untitled {
		taskIDs[i] = t.ID.String()
		h.runner.GenerateTitleBackground(t.ID, t.Prompt)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"queued":              len(untitled),
		"total_without_title": total,
		"task_ids":            taskIDs,
	})
}
