package handler

import (
	"net/http"

	"github.com/google/uuid"
	"latere.ai/x/pkg/httpjson"

	"latere.ai/x/wallfacer/internal/runner"
)

// TaskCommits serves the selected workspace's task history, including live
// commits that have not reached an execution checkpoint yet.
func (h *Handler) TaskCommits(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	if _, err := s.GetTask(r.Context(), id); err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	commits, err := runner.TaskCommits(r.Context(), s, id)
	if err != nil {
		http.Error(w, "could not read task commit history: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpjson.Write(w, http.StatusOK, commits)
}
