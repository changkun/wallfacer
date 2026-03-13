package handler

import (
	"net/http"
	"runtime"
	"time"

	"changkun.de/wallfacer/internal/store"
)

// containerCircuitStatus is the JSON shape for the container circuit breaker
// snapshot inside runtimeStatusResponse.
type containerCircuitStatus struct {
	State    string `json:"state"`
	Failures int    `json:"failures"`
}

// runtimeStatusResponse is the JSON shape returned by GET /api/debug/runtime.
type runtimeStatusResponse struct {
	Goroutines       []string               `json:"goroutines"`
	GoGoroutineCount int                    `json:"go_goroutine_count"`
	GoHeapAllocBytes uint64                 `json:"go_heap_alloc_bytes"`
	TaskStates       map[string]int         `json:"task_states"`
	ActiveContainers int                    `json:"active_containers"`
	ContainerCircuit containerCircuitStatus `json:"container_circuit"`
	Timestamp        time.Time              `json:"timestamp"`
}

// GetRuntimeStatus returns a live snapshot of server internals for operational
// debugging: in-flight background goroutine labels, Go runtime memory and
// goroutine counts, task counts by status, and the number of running containers.
func (h *Handler) GetRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	// In-flight background goroutine labels.
	goroutines := h.runner.PendingGoroutines()
	if goroutines == nil {
		goroutines = []string{}
	}

	// Go runtime stats.
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Task counts grouped by status (include archived tasks).
	tasks, _ := h.store.ListTasks(r.Context(), true)
	taskStates := map[string]int{
		string(store.TaskStatusBacklog):    0,
		string(store.TaskStatusInProgress): 0,
		string(store.TaskStatusWaiting):    0,
		string(store.TaskStatusDone):       0,
		string(store.TaskStatusFailed):     0,
		string(store.TaskStatusCancelled):  0,
		string(store.TaskStatusCommitting): 0,
	}
	for _, t := range tasks {
		taskStates[string(t.Status)]++
	}

	// Count running containers (errors treated as zero).
	containers, _ := h.runner.ListContainers()
	activeContainers := 0
	for _, c := range containers {
		if c.State == "running" {
			activeContainers++
		}
	}

	writeJSON(w, http.StatusOK, runtimeStatusResponse{
		Goroutines:       goroutines,
		GoGoroutineCount: runtime.NumGoroutine(),
		GoHeapAllocBytes: m.HeapAlloc,
		TaskStates:       taskStates,
		ActiveContainers: activeContainers,
		ContainerCircuit: containerCircuitStatus{
			State:    h.runner.ContainerCircuitState(),
			Failures: h.runner.ContainerCircuitFailures(),
		},
		Timestamp: time.Now().UTC(),
	})
}
