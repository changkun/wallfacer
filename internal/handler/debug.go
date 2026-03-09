package handler

import (
	"encoding/json"
	"math"
	"net/http"
	"runtime"
	"sort"
	"time"

	"changkun.de/wallfacer/internal/runner"
	"github.com/google/uuid"
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

// phaseStats holds aggregate latency statistics for a single execution phase.
type phaseStats struct {
	Count int   `json:"count"`
	MinMs int64 `json:"min_ms"`
	P50Ms int64 `json:"p50_ms"`
	P95Ms int64 `json:"p95_ms"`
	P99Ms int64 `json:"p99_ms"`
	MaxMs int64 `json:"max_ms"`
}

// spanStatsResponse is the JSON shape returned by GET /api/debug/spans.
type spanStatsResponse struct {
	Phases       map[string]phaseStats `json:"phases"`
	TasksScanned int                   `json:"tasks_scanned"`
	SpansTotal   int                   `json:"spans_total"`
}

// percentileIndex returns the slice index for the given percentile (0–100)
// using the nearest-rank method, clamped to a valid range.
// With N=1, all percentiles resolve to index 0 (the only element).
func percentileIndex(n, pct int) int {
	idx := int(math.Ceil(float64(pct)/100.0*float64(n))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return idx
}

// boardManifestResponse is the JSON envelope returned by the board manifest endpoints.
type boardManifestResponse struct {
	Manifest  *runner.BoardManifest `json:"manifest"`
	SizeBytes int                   `json:"size_bytes"`
	SizeWarn  bool                  `json:"size_warn"` // true when indented JSON exceeds 64 KB
}

// BoardManifest returns the board manifest as it would appear to a newly-started
// task: no self-task, no worktree mounts. Useful for debugging the board state
// without spinning up a container.
func (h *Handler) BoardManifest(w http.ResponseWriter, r *http.Request) {
	manifest, err := h.runner.GenerateBoardManifest(r.Context(), uuid.Nil, false)
	if err != nil {
		http.Error(w, "failed to generate board manifest: "+err.Error(), http.StatusInternalServerError)
		return
	}
	b, _ := json.MarshalIndent(manifest, "", "  ")
	const maxBytes = 64 * 1024
	writeJSON(w, http.StatusOK, boardManifestResponse{
		Manifest:  manifest,
		SizeBytes: len(b),
		SizeWarn:  len(b) > maxBytes,
	})
}

// TaskBoardManifest returns the board manifest as it would appear to the
// specified task: is_self=true for that task's entry, MountWorktrees matching
// the task's setting.
func (h *Handler) TaskBoardManifest(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil || task == nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	manifest, err := h.runner.GenerateBoardManifest(r.Context(), id, task.MountWorktrees)
	if err != nil {
		http.Error(w, "failed to generate board manifest: "+err.Error(), http.StatusInternalServerError)
		return
	}
	b, _ := json.MarshalIndent(manifest, "", "  ")
	const maxBytes = 64 * 1024
	writeJSON(w, http.StatusOK, boardManifestResponse{
		Manifest:  manifest,
		SizeBytes: len(b),
		SizeWarn:  len(b) > maxBytes,
	})
}

// GetSpanStats aggregates span timing data across all tasks (including archived)
// and returns per-phase latency statistics (count, min, p50, p95, p99, max).
func (h *Handler) GetSpanStats(w http.ResponseWriter, r *http.Request) {
	tasks, _ := h.store.ListTasks(r.Context(), true)
	durations := make(map[string][]int64) // phase → []durationMs
	spansTotal := 0

	for _, t := range tasks {
		events, err := h.store.GetEvents(r.Context(), t.ID)
		if err != nil {
			continue
		}
		for _, sr := range computeSpans(events) {
			durations[sr.Phase] = append(durations[sr.Phase], sr.DurationMs)
			spansTotal++
		}
	}

	phases := make(map[string]phaseStats, len(durations))
	for phase, ds := range durations {
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		n := len(ds)
		phases[phase] = phaseStats{
			Count: n,
			MinMs: ds[0],
			P50Ms: ds[percentileIndex(n, 50)],
			P95Ms: ds[percentileIndex(n, 95)],
			P99Ms: ds[percentileIndex(n, 99)],
			MaxMs: ds[n-1],
		}
	}

	writeJSON(w, http.StatusOK, spanStatsResponse{
		Phases:       phases,
		TasksScanned: len(tasks),
		SpansTotal:   spansTotal,
	})
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
