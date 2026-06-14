package handler

import (
	"net/http"
	"strconv"

	"latere.ai/x/wallfacer/internal/pkg/httpjson"
	"latere.ai/x/wallfacer/internal/store"
)

// runBackfillBatch is the shared scaffolding for "scan all tasks, queue
// background work for those missing X" endpoints (GenerateMissingTitles,
// GenerateMissingOversight, and any future backfills with the same
// shape).
//
// limit comes from ?limit=N with a default of 10 and 0 meaning "no cap".
// eligible decides which tasks qualify (consulted after the full task
// list is loaded); queue performs the side effect for each accepted
// task — typically a background goroutine that calls the runner.
//
// totalKey names the response field that carries the unfiltered eligible
// count, e.g. "total_without_title" or "total_without_oversight", so the
// JSON envelope stays callable from the existing frontend.
func (h *Handler) runBackfillBatch(
	w http.ResponseWriter, r *http.Request,
	totalKey string,
	eligible func(store.Task) bool,
	queue func(store.Task),
) {
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

	var picked []store.Task
	for _, t := range tasks {
		if eligible(t) {
			picked = append(picked, t)
		}
	}

	total := len(picked)
	if limit > 0 && len(picked) > limit {
		picked = picked[:limit]
	}

	taskIDs := make([]string, len(picked))
	for i, t := range picked {
		taskIDs[i] = t.ID.String()
		queue(t)
	}

	httpjson.Write(w, http.StatusOK, map[string]any{
		"queued":   len(picked),
		totalKey:   total,
		"task_ids": taskIDs,
	})
}
