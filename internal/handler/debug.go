package handler

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/pkg/httpjson"

	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
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
	Goroutines        int                      `json:"goroutines"`
	TasksByStatus     map[store.TaskStatus]int `json:"tasks_by_status"`
	RunningContainers runningContainerInfo     `json:"running_containers"`
	UptimeSeconds     float64                  `json:"uptime_seconds"`
}

// boardManifestResponse is the JSON envelope returned by the board manifest endpoints.
type boardManifestResponse struct {
	Manifest  *runner.BoardManifest `json:"manifest"`
	SizeBytes int                   `json:"size_bytes"`
	SizeWarn  bool                  `json:"size_warn"` // true when indented JSON exceeds 64 KB
}

// BoardManifest returns the board manifest as it would appear to a newly-started
// task: no self-task. Useful for debugging the board state without running an
// agent.
func (h *Handler) BoardManifest(w http.ResponseWriter, r *http.Request) {
	manifest, err := h.runner.GenerateBoardManifest(r.Context(), uuid.Nil)
	if err != nil {
		http.Error(w, "failed to generate board manifest: "+err.Error(), http.StatusInternalServerError)
		return
	}
	b, _ := json.MarshalIndent(manifest, "", "  ")
	const maxBytes = 64 * 1024
	httpjson.Write(w, http.StatusOK, boardManifestResponse{
		Manifest:  manifest,
		SizeBytes: len(b),
		SizeWarn:  len(b) > maxBytes,
	})
}

// TaskBoardManifest returns the board manifest as it would appear to the
// specified task: is_self=true for that task's entry.
func (h *Handler) TaskBoardManifest(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	task, err := s.GetTask(r.Context(), id)
	if err != nil || task == nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	manifest, err := h.runner.GenerateBoardManifest(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to generate board manifest: "+err.Error(), http.StatusInternalServerError)
		return
	}
	b, _ := json.MarshalIndent(manifest, "", "  ")
	const maxBytes = 64 * 1024
	httpjson.Write(w, http.StatusOK, boardManifestResponse{
		Manifest:  manifest,
		SizeBytes: len(b),
		SizeWarn:  len(b) > maxBytes,
	})
}

// Health returns a lightweight operational health snapshot:
//   - number of live goroutines
//   - task counts grouped by status
//   - running container count and IDs
//   - server uptime in seconds
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}

	// Task counts by status.
	tasks, _ := s.ListTasks(r.Context(), false)
	tasksByStatus := make(map[store.TaskStatus]int)
	for _, t := range tasks {
		tasksByStatus[t.Status]++
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
	httpjson.Write(w, http.StatusOK, resp)
}
