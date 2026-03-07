package handler

import "net/http"

// GetContainers returns the list of wallfacer sandbox containers visible to the
// container runtime, mimicking `docker ps -a --filter name=wallfacer`.
// Each entry is enriched with task_title from the store so the UI can display
// a meaningful name without a separate task lookup round-trip.
func (h *Handler) GetContainers(w http.ResponseWriter, r *http.Request) {
	containers, err := h.runner.ListContainers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Enrich containers with task title from the store so the monitor can
	// show a human-readable title even when window.state.tasks is stale.
	if len(containers) > 0 {
		tasks, listErr := h.store.ListTasks(r.Context(), false)
		if listErr == nil {
			titleByID := make(map[string]string, len(tasks))
			for _, t := range tasks {
				if t.Title != "" {
					titleByID[t.ID.String()] = t.Title
				} else if t.Prompt != "" {
					// Fall back to a truncated prompt when the title hasn't
					// been generated yet (e.g. shortly after task creation).
					p := t.Prompt
					if len(p) > 60 {
						p = p[:60] + "…"
					}
					titleByID[t.ID.String()] = p
				}
			}
			for i, c := range containers {
				if title, ok := titleByID[c.TaskID]; ok {
					containers[i].TaskTitle = title
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, containers)
}
