package handler

import "net/http"

// GetContainers returns the list of wallfacer sandbox containers visible to the
// container runtime, mimicking `docker ps -a --filter name=wallfacer`.
func (h *Handler) GetContainers(w http.ResponseWriter, r *http.Request) {
	containers, err := h.runner.ListContainers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, containers)
}
