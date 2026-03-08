package handler

import (
	"net/http"
	"sort"
	"time"

	"changkun.de/wallfacer/internal/store"
)

// StatsResponse is the JSON body returned by GET /api/stats.
type StatsResponse struct {
	TotalCostUSD      float64             `json:"total_cost_usd"`
	TotalInputTokens  int                 `json:"total_input_tokens"`
	TotalOutputTokens int                 `json:"total_output_tokens"`
	TotalCacheTokens  int                 `json:"total_cache_tokens"`
	ByStatus          map[string]UsageStat `json:"by_status"`
	ByActivity        map[string]UsageStat `json:"by_activity"`
	TopTasks          []TaskCostEntry      `json:"top_tasks"`
	DailyUsage        []DayStat            `json:"daily_usage"`
}

// UsageStat holds aggregated token/cost data for a single bucket.
type UsageStat struct {
	CostUSD      float64 `json:"cost_usd"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
}

// TaskCostEntry holds abbreviated task information for the top-cost list.
type TaskCostEntry struct {
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Status  string  `json:"status"`
	CostUSD float64 `json:"cost_usd"`
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
func aggregateStats(tasks []store.Task) StatsResponse {
	resp := StatsResponse{
		ByStatus:   make(map[string]UsageStat),
		ByActivity: make(map[string]UsageStat),
	}

	dailyMap := make(map[string]*DayStat)

	for _, t := range tasks {
		u := t.Usage

		// Global totals.
		resp.TotalCostUSD += u.CostUSD
		resp.TotalInputTokens += u.InputTokens
		resp.TotalOutputTokens += u.OutputTokens
		resp.TotalCacheTokens += u.CacheReadInputTokens + u.CacheCreationTokens

		// ByStatus bucket.
		statusKey := string(t.Status)
		s := resp.ByStatus[statusKey]
		s.CostUSD += u.CostUSD
		s.InputTokens += u.InputTokens
		s.OutputTokens += u.OutputTokens
		resp.ByStatus[statusKey] = s

		// ByActivity buckets from per-task breakdown.
		for activity, au := range t.UsageBreakdown {
			a := resp.ByActivity[activity]
			a.CostUSD += au.CostUSD
			a.InputTokens += au.InputTokens
			a.OutputTokens += au.OutputTokens
			resp.ByActivity[activity] = a
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
			Status:  string(t.Status),
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

// GetStats aggregates token/cost data across all tasks (including archived)
// and returns a rolled-up analytics summary.
func (h *Handler) GetStats(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.store.ListTasks(r.Context(), true /* includeArchived */)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, aggregateStats(tasks))
}
