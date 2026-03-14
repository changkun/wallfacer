package handler

import (
	"net/http"
	"sort"
	"time"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// StatsResponse is the JSON body returned by GET /api/stats.
type StatsResponse struct {
	TotalCostUSD      float64                             `json:"total_cost_usd"`
	TotalInputTokens  int                                 `json:"total_input_tokens"`
	TotalOutputTokens int                                 `json:"total_output_tokens"`
	TotalCacheTokens  int                                 `json:"total_cache_tokens"`
	ByStatus          map[store.TaskStatus]UsageStat      `json:"by_status"`
	ByActivity        map[store.SandboxActivity]UsageStat `json:"by_activity"`
	ByWorkspace       map[string]UsageStat                `json:"by_workspace"`
	ByFailureCategory map[store.FailureCategory]UsageStat `json:"by_failure_category"`
	TopTasks          []TaskCostEntry                     `json:"top_tasks"`
	DailyUsage        []DayStat                           `json:"daily_usage"`
}

// UsageStat holds aggregated token/cost data for a single bucket.
type UsageStat struct {
	CostUSD             float64 `json:"cost_usd"`
	InputTokens         int     `json:"input_tokens"`
	OutputTokens        int     `json:"output_tokens"`
	CacheReadTokens     int     `json:"cache_read_tokens"`
	CacheCreationTokens int     `json:"cache_creation_tokens"`
	Count               int     `json:"count"`
}

// TaskCostEntry holds abbreviated task information for the top-cost list.
type TaskCostEntry struct {
	ID      string           `json:"id"`
	Title   string           `json:"title"`
	Status  store.TaskStatus `json:"status"`
	CostUSD float64          `json:"cost_usd"`
}

// DayStat holds usage totals for a single calendar day.
type DayStat struct {
	Date         string  `json:"date"` // "2006-01-02"
	CostUSD      float64 `json:"cost_usd"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
}

// aggregateStats computes a StatsResponse from the provided tasks.
// Extracted as a pure function for testability.
//
// loadSummary is an optional function that loads a TaskSummary for a given task
// ID. When non-nil and a summary exists for a done task, the summary's
// ByActivity and TotalCostUSD are used in place of the live task fields,
// keeping the hot path for completed tasks out of task.json. Pass nil to
// always use the live Task struct (backward-compatible fallback, used in tests).
func aggregateStats(tasks []store.Task, loadSummary func(id uuid.UUID) (*store.TaskSummary, error)) StatsResponse {
	resp := StatsResponse{
		ByStatus:          make(map[store.TaskStatus]UsageStat),
		ByActivity:        make(map[store.SandboxActivity]UsageStat),
		ByWorkspace:       make(map[string]UsageStat),
		ByFailureCategory: make(map[store.FailureCategory]UsageStat),
	}

	dailyMap := make(map[string]*DayStat)

	for _, t := range tasks {
		u := t.Usage
		breakdown := t.UsageBreakdown
		wsBreakdown := t.WorkspaceUsageBreakdown

		// For immutable done tasks, prefer the cached summary when available.
		if t.Status == store.TaskStatusDone && loadSummary != nil {
			if summary, err := loadSummary(t.ID); err == nil && summary != nil {
				u.CostUSD = summary.TotalCostUSD
				breakdown = summary.ByActivity
				if summary.WorkspaceUsageBreakdown != nil {
					wsBreakdown = summary.WorkspaceUsageBreakdown
				}
			}
		}

		// Global totals.
		resp.TotalCostUSD += u.CostUSD
		resp.TotalInputTokens += u.InputTokens
		resp.TotalOutputTokens += u.OutputTokens
		resp.TotalCacheTokens += u.CacheReadInputTokens + u.CacheCreationTokens

		// ByStatus bucket.
		s := resp.ByStatus[t.Status]
		s.CostUSD += u.CostUSD
		s.InputTokens += u.InputTokens
		s.OutputTokens += u.OutputTokens
		resp.ByStatus[t.Status] = s

		// ByActivity buckets from per-task breakdown.
		for activity, au := range breakdown {
			a := resp.ByActivity[activity]
			a.CostUSD += au.CostUSD
			a.InputTokens += au.InputTokens
			a.OutputTokens += au.OutputTokens
			resp.ByActivity[activity] = a
		}

		// ByWorkspace: use the stored per-workspace breakdown when available so
		// that a task touching N repos contributes proportionally to each bucket
		// rather than duplicating its full usage N times.
		//
		// Fallback (when no breakdown is stored): split usage equally across all
		// repos in WorktreePaths. Tasks that never ran (empty WorktreePaths) are
		// excluded entirely.
		if len(wsBreakdown) > 0 {
			for repoPath, wu := range wsBreakdown {
				ws := resp.ByWorkspace[repoPath]
				ws.CostUSD += wu.CostUSD
				ws.InputTokens += wu.InputTokens
				ws.OutputTokens += wu.OutputTokens
				ws.CacheReadTokens += wu.CacheReadInputTokens
				ws.CacheCreationTokens += wu.CacheCreationTokens
				ws.Count++
				resp.ByWorkspace[repoPath] = ws
			}
		} else if len(t.WorktreePaths) > 0 {
			n := float64(len(t.WorktreePaths))
			for repoPath := range t.WorktreePaths {
				ws := resp.ByWorkspace[repoPath]
				ws.CostUSD += u.CostUSD / n
				ws.InputTokens += int(float64(u.InputTokens) / n)
				ws.OutputTokens += int(float64(u.OutputTokens) / n)
				ws.CacheReadTokens += int(float64(u.CacheReadInputTokens) / n)
				ws.CacheCreationTokens += int(float64(u.CacheCreationTokens) / n)
				ws.Count++
				resp.ByWorkspace[repoPath] = ws
			}
		}

		// ByFailureCategory: bucket all tasks (including retried ones that are now done)
		// by their current FailureCategory. For done/cancelled tasks whose FailureCategory
		// is now empty (cleared on retry), use the RetryHistory to backfill the last
		// known category.
		effectiveCat := t.FailureCategory
		if effectiveCat == "" && len(t.RetryHistory) > 0 {
			last := t.RetryHistory[len(t.RetryHistory)-1]
			effectiveCat = last.FailureCategory
		}
		if effectiveCat != "" {
			fc := resp.ByFailureCategory[effectiveCat]
			fc.CostUSD += u.CostUSD
			fc.InputTokens += u.InputTokens
			fc.OutputTokens += u.OutputTokens
			fc.Count++
			resp.ByFailureCategory[effectiveCat] = fc
		}

		// Daily accumulation keyed by creation date.
		day := t.CreatedAt.UTC().Format("2006-01-02")
		if dailyMap[day] == nil {
			dailyMap[day] = &DayStat{Date: day}
		}
		dailyMap[day].CostUSD += u.CostUSD
		dailyMap[day].InputTokens += u.InputTokens
		dailyMap[day].OutputTokens += u.OutputTokens
	}

	// TopTasks: sort all tasks by cost descending, take top 10.
	sorted := make([]store.Task, len(tasks))
	copy(sorted, tasks)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Usage.CostUSD > sorted[j].Usage.CostUSD
	})
	top := min(10, len(sorted))
	resp.TopTasks = make([]TaskCostEntry, top)
	for i := range top {
		t := sorted[i]
		title := t.Title
		if title == "" {
			runes := []rune(t.Prompt)
			if len(runes) > 60 {
				runes = runes[:60]
			}
			title = string(runes)
		}
		resp.TopTasks[i] = TaskCostEntry{
			ID:      t.ID.String(),
			Title:   title,
			Status:  t.Status,
			CostUSD: t.Usage.CostUSD,
		}
	}

	// DailyUsage: last 30 calendar days ascending, zero-filled for missing days.
	now := time.Now().UTC()
	resp.DailyUsage = make([]DayStat, 30)
	for i := range 30 {
		day := now.AddDate(0, 0, -(29 - i)).Format("2006-01-02")
		if stat := dailyMap[day]; stat != nil {
			resp.DailyUsage[i] = *stat
		} else {
			resp.DailyUsage[i] = DayStat{Date: day}
		}
	}

	return resp
}

// filterTasksByWorkspace returns the subset of tasks whose WorktreePaths map
// contains ws as a key. When ws is empty the full slice is returned unchanged.
// The second return value is false only when ws is non-empty but no tasks match,
// which the caller should treat as a 400 Bad Request.
func filterTasksByWorkspace(tasks []store.Task, ws string) ([]store.Task, bool) {
	if ws == "" {
		return tasks, true
	}
	var filtered []store.Task
	for _, t := range tasks {
		if _, ok := t.WorktreePaths[ws]; ok {
			filtered = append(filtered, t)
		}
	}
	return filtered, len(filtered) > 0
}

// GetStats aggregates token/cost data across all tasks (including archived)
// and returns a rolled-up analytics summary.
//
// Optional query param: ?workspace=<repo-root-path> — restrict aggregation to
// tasks that have that path as a WorktreePaths key. Returns 400 if the
// workspace param is present but no tasks match it.
func (h *Handler) GetStats(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.store.ListTasks(r.Context(), true /* includeArchived */)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if ws := r.URL.Query().Get("workspace"); ws != "" {
		var ok bool
		tasks, ok = filterTasksByWorkspace(tasks, ws)
		if !ok {
			http.Error(w, "no tasks found for workspace: "+ws, http.StatusBadRequest)
			return
		}
	}
	writeJSON(w, http.StatusOK, aggregateStats(tasks, h.store.LoadSummary))
}
