package handler

import (
	"net/http"
	"runtime"
	"time"
)

// containerSummary is a compact representation of a running container for the
// health endpoint.
type containerSummary struct {
	TaskID string `json:"task_id"`
	Name   string `json:"name"`
	State  string `json:"state"`
}

// runningContainerInfo bundles the count and list of running containers.
type runningContainerInfo struct {
	Count int                `json:"count"`
	Items []containerSummary `json:"items"`
}

// healthResponse is the JSON shape returned by GET /api/debug/health.
type healthResponse struct {
	Goroutines        int                  `json:"goroutines"`
	TasksByStatus     map[string]int       `json:"tasks_by_status"`
	RunningContainers runningContainerInfo `json:"running_containers"`
	UptimeSeconds     float64              `json:"uptime_seconds"`
}

// Health returns a lightweight operational health snapshot:
//   - number of live goroutines
//   - task counts grouped by status
//   - running container count and IDs
//   - server uptime in seconds
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	// Task counts by status.
	tasks, _ := h.store.ListTasks(r.Context(), false)
	tasksByStatus := make(map[string]int)
	for _, t := range tasks {
		tasksByStatus[string(t.Status)]++
	}

	// Running containers (errors treated as empty list).
	containers, _ := h.runner.ListContainers()
	runningItems := make([]containerSummary, 0)
	for _, c := range containers {
		if c.State == "running" {
			runningItems = append(runningItems, containerSummary{
				TaskID: c.TaskID,
				Name:   c.Name,
				State:  c.State,
			})
		}
	}

	resp := healthResponse{
		Goroutines:    runtime.NumGoroutine(),
		TasksByStatus: tasksByStatus,
		RunningContainers: runningContainerInfo{
			Count: len(runningItems),
			Items: runningItems,
		},
		UptimeSeconds: time.Since(h.startTime).Seconds(),
	}
	writeJSON(w, http.StatusOK, resp)
}
