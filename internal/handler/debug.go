package handler

import (
	"encoding/json"
	"math"
	"net/http"
	"runtime"
	"sort"
	"sync"
	"time"

	"changkun.de/wallfacer/internal/runner"
	"changkun.de/wallfacer/internal/store"
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
	Goroutines        int                      `json:"goroutines"`
	TasksByStatus     map[store.TaskStatus]int `json:"tasks_by_status"`
	RunningContainers runningContainerInfo     `json:"running_containers"`
	UptimeSeconds     float64                  `json:"uptime_seconds"`
}

// phaseStats holds aggregate latency statistics for a single execution phase.
type phaseStats struct {
	Count int   `json:"count"`
	SumMs int64 `json:"sum_ms"`
	MinMs int64 `json:"min_ms"`
	P50Ms int64 `json:"p50_ms"`
	P95Ms int64 `json:"p95_ms"`
	P99Ms int64 `json:"p99_ms"`
	MaxMs int64 `json:"max_ms"`
}

// spanStatsCache holds a single cached GetSpanStats response with a TTL.
type spanStatsCache struct {
	mu        sync.Mutex
	resp      *spanStatsResponse
	expiresAt time.Time
}

// DayCount holds the number of completed tasks for a single UTC calendar day.
type DayCount struct {
	Date  string `json:"date"`  // "2006-01-02"
	Count int    `json:"count"`
}

// taskThroughput aggregates task completion metrics across all done/failed tasks.
type taskThroughput struct {
	TotalCompleted   int        `json:"total_completed"`
	TotalFailed      int        `json:"total_failed"`
	SuccessRatePct   float64    `json:"success_rate_pct"`
	AvgExecutionS    float64    `json:"avg_execution_s"`
	MedianExecutionS float64    `json:"median_execution_s"`
	P95ExecutionS    float64    `json:"p95_execution_s"`
	DailyCompletions []DayCount `json:"daily_completions"`
}

// spanStatsResponse is the JSON shape returned by GET /api/debug/spans.
type spanStatsResponse struct {
	Phases       map[string]phaseStats `json:"phases"`
	TasksScanned int                   `json:"tasks_scanned"`
	SpansTotal   int                   `json:"spans_total"`
	Throughput   taskThroughput        `json:"throughput"`
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
// and returns per-phase latency statistics (count, sum, min, p50, p95, p99, max).
// Results are cached for 60 seconds so repeated dashboard refreshes are free.
func (h *Handler) GetSpanStats(w http.ResponseWriter, r *http.Request) {
	h.spanCache.mu.Lock()
	defer h.spanCache.mu.Unlock()

	if h.spanCache.resp != nil && time.Now().Before(h.spanCache.expiresAt) {
		writeJSON(w, http.StatusOK, h.spanCache.resp)
		return
	}

	tasks, _ := h.store.ListTasks(r.Context(), true)
	durations := make(map[string][]int64) // phase → []durationMs
	spansTotal := 0

	// Throughput tracking.
	var execDurations []float64
	dailyBuckets := make(map[string]int) // "2006-01-02" → count
	totalCompleted := 0
	totalFailed := 0

	for _, t := range tasks {
		events, err := h.store.GetEvents(r.Context(), t.ID)
		if err != nil {
			continue
		}
		spans, _ := store.ComputeSpans(events)
		for _, sr := range spans {
			durations[sr.Phase] = append(durations[sr.Phase], sr.DurationMS)
			spansTotal++
		}

		switch t.Status {
		case store.TaskStatusDone:
			totalCompleted++
			summary, _ := h.store.LoadSummary(t.ID)
			var execS float64
			if summary != nil {
				if summary.ExecutionDurationSeconds > 0 {
					execS = summary.ExecutionDurationSeconds
				} else {
					execS = summary.DurationSeconds
				}
				dayKey := summary.CompletedAt.UTC().Format("2006-01-02")
				dailyBuckets[dayKey]++
			}
			if execS > 0 {
				execDurations = append(execDurations, execS)
			}
		case store.TaskStatusFailed:
			totalFailed++
		}
	}

	// Compute percentiles for execution durations.
	sort.Float64s(execDurations)
	var medianExecS, p95ExecS, avgExecS float64
	if n := len(execDurations); n > 0 {
		medianExecS = execDurations[percentileIndex(n, 50)]
		p95ExecS = execDurations[percentileIndex(n, 95)]
		var sum float64
		for _, d := range execDurations {
			sum += d
		}
		avgExecS = sum / float64(n)
	}

	// Compute success rate.
	var successRatePct float64
	if total := totalCompleted + totalFailed; total > 0 {
		successRatePct = float64(totalCompleted) / float64(total) * 100.0
	}

	// Build 30-day daily completions, always emitting exactly 30 entries.
	now := time.Now().UTC()
	dailyCompletions := make([]DayCount, 30)
	for i := 0; i < 30; i++ {
		day := now.AddDate(0, 0, -(29 - i))
		key := day.Format("2006-01-02")
		dailyCompletions[i] = DayCount{Date: key, Count: dailyBuckets[key]}
	}

	phases := make(map[string]phaseStats, len(durations))
	for phase, ds := range durations {
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		n := len(ds)
		var sumMs int64
		for _, d := range ds {
			sumMs += d
		}
		phases[phase] = phaseStats{
			Count: n,
			SumMs: sumMs,
			MinMs: ds[0],
			P50Ms: ds[percentileIndex(n, 50)],
			P95Ms: ds[percentileIndex(n, 95)],
			P99Ms: ds[percentileIndex(n, 99)],
			MaxMs: ds[n-1],
		}
	}

	resp := &spanStatsResponse{
		Phases:       phases,
		TasksScanned: len(tasks),
		SpansTotal:   spansTotal,
		Throughput: taskThroughput{
			TotalCompleted:   totalCompleted,
			TotalFailed:      totalFailed,
			SuccessRatePct:   successRatePct,
			AvgExecutionS:    avgExecS,
			MedianExecutionS: medianExecS,
			P95ExecutionS:    p95ExecS,
			DailyCompletions: dailyCompletions,
		},
	}
	h.spanCache.resp = resp
	h.spanCache.expiresAt = time.Now().Add(60 * time.Second)
	writeJSON(w, http.StatusOK, resp)
}

// Health returns a lightweight operational health snapshot:
//   - number of live goroutines
//   - task counts grouped by status
//   - running container count and IDs
//   - server uptime in seconds
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	// Task counts by status.
	tasks, _ := h.store.ListTasks(r.Context(), false)
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
	writeJSON(w, http.StatusOK, resp)
}
