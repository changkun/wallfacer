package handler

import (
	"net/http"

	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// oversightResponse wraps TaskOversight with a precomputed phase_count field
// so the card accordion can show the count without counting phases client-side.
type oversightResponse struct {
	store.TaskOversight
	PhaseCount int `json:"phase_count"`
}

// GetOversight returns the oversight summary for a task. The `?phase=` query
// parameter selects between the implementation-phase summary (default `impl`,
// generated asynchronously when the task transitions to waiting or done) and
// the test-phase summary (`test`, generated when a test run transitions back
// to waiting). Both phases share the same JSON envelope.
func (h *Handler) GetOversight(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if _, err := h.store.GetTask(r.Context(), id); err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	read := h.store.GetOversight
	errMsg := "failed to read oversight"
	if r.URL.Query().Get("phase") == "test" {
		read = h.store.GetTestOversight
		errMsg = "failed to read test oversight"
	}

	oversight, err := read(id)
	if err != nil {
		http.Error(w, errMsg, http.StatusInternalServerError)
		return
	}

	httpjson.Write(w, http.StatusOK, oversightResponse{
		TaskOversight: *oversight,
		PhaseCount:    len(oversight.Phases),
	})
}

// GenerateMissingOversight triggers background oversight generation for completed
// tasks that do not yet have a ready oversight summary (status pending or failed).
// Only tasks in a terminal state (done, waiting, failed, cancelled) with at least
// one turn are eligible, since there must be agent activity to summarize.
func (h *Handler) GenerateMissingOversight(w http.ResponseWriter, r *http.Request) {
	terminal := map[store.TaskStatus]bool{
		store.TaskStatusDone:      true,
		store.TaskStatusWaiting:   true,
		store.TaskStatusFailed:    true,
		store.TaskStatusCancelled: true,
	}
	h.runBackfillBatch(w, r, "total_without_oversight",
		func(t store.Task) bool {
			if !terminal[t.Status] || t.Turns == 0 {
				return false
			}
			o, err := h.store.GetOversight(t.ID)
			if err != nil {
				return false
			}
			return o.Status != store.OversightStatusReady && o.Status != store.OversightStatusGenerating
		},
		func(t store.Task) { go h.runner.GenerateOversight(t.ID) },
	)
}
