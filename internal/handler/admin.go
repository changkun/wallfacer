package handler

import (
	"net/http"

	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/pkg/httpjson"
)

// rebuildIndexResponse is the JSON shape returned by POST /api/admin/rebuild-index.
type rebuildIndexResponse struct {
	Repaired int `json:"repaired"`
}

// RebuildIndex rebuilds the in-memory search index from disk. It is safe to
// call at any time; it holds locks only for the minimum duration per task.
func (h *Handler) RebuildIndex(w http.ResponseWriter, r *http.Request) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	repaired, err := s.RebuildSearchIndex(r.Context())
	if err != nil {
		http.Error(w, "rebuild failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	logger.Handler.Info("search index rebuild complete", "repaired", repaired)
	httpjson.Write(w, http.StatusOK, rebuildIndexResponse{Repaired: repaired})
}
