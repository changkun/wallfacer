package handler

import (
	"net/http"

	"github.com/google/uuid"
)

// GetOversight returns the aggregated oversight summary for a task.
// The summary is generated asynchronously when the task transitions to waiting
// or done; this endpoint returns the current state (pending/generating/ready/failed).
func (h *Handler) GetOversight(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if _, err := h.store.GetTask(r.Context(), id); err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	oversight, err := h.store.GetOversight(id)
	if err != nil {
		http.Error(w, "failed to read oversight", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, oversight)
}
