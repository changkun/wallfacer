package handler

import (
	"net/http"

	"changkun.de/x/wallfacer/internal/logger"
)

// rebuildIndexResponse is the JSON shape returned by POST /api/admin/rebuild-index.
type rebuildIndexResponse struct {
	Repaired int `json:"repaired"`
}

// RebuildIndex rebuilds the in-memory search index from disk. It is safe to
// call at any time; it holds locks only for the minimum duration per task.
func (h *Handler) RebuildIndex(w http.ResponseWriter, r *http.Request) {
	repaired, err := h.store.RebuildSearchIndex(r.Context())
	if err != nil {
		http.Error(w, "rebuild failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	logger.Handler.Info("search index rebuild complete", "repaired", repaired)
	writeJSON(w, http.StatusOK, rebuildIndexResponse{Repaired: repaired})
}
