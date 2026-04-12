package handler

import (
	"cmp"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/store"
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
	Planning          map[string]PlanningGroupStat        `json:"planning"`
}

// PlanningGroupStat aggregates planning round usage for one workspace group.
// The server resolves Paths and Label for the currently active group; stale
// group keys (past groups no longer in use) are surfaced with Label == key
// and Paths == nil so the UI can still render totals.
type PlanningGroupStat struct {
	Label      string       `json:"label"`
	Paths      []string     `json:"paths"`
	Usage      UsageStat    `json:"usage"`
	RoundCount int          `json:"round_count"`
	Timeline   []RoundPoint `json:"timeline"`
}

// RoundPoint is a single point on the per-group planning-cost sparkline.
type RoundPoint struct {
	Timestamp time.Time `json:"timestamp"`
	CostUSD   float64   `json:"cost_usd"`
	Tokens    int       `json:"tokens"`
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
// ByActivity and TotalCostUSD are used in place of the live task fields. This
// avoids reading task.json for completed tasks on the hot path; summary.json
// is a small, immutable, separately cached file. Pass nil to always use the
// live Task struct (backward-compatible fallback, used in tests).
func aggregateStats(tasks []store.Task, loadSummary func(id uuid.UUID) (*store.TaskSummary, error)) StatsResponse {
	resp := StatsResponse{
		ByStatus:          make(map[store.TaskStatus]UsageStat),
		ByActivity:        make(map[store.SandboxActivity]UsageStat),
		ByWorkspace:       make(map[string]UsageStat),
		ByFailureCategory: make(map[store.FailureCategory]UsageStat),
		Planning:          make(map[string]PlanningGroupStat),
	}

	dailyMap := make(map[string]*DayStat)

	for _, t := range tasks {
		u := t.Usage
		breakdown := t.UsageBreakdown

		// For immutable done tasks, prefer the cached summary when available.
		if t.Status == store.TaskStatusDone && loadSummary != nil {
			if summary, err := loadSummary(t.ID); err == nil && summary != nil {
				u.CostUSD = summary.TotalCostUSD
				breakdown = summary.ByActivity
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

		// ByWorkspace buckets: one entry per repo root in WorktreePaths.
		// Tasks that never ran (empty WorktreePaths) are excluded.
		for repoPath := range t.WorktreePaths {
			ws := resp.ByWorkspace[repoPath]
			ws.CostUSD += u.CostUSD
			ws.InputTokens += u.InputTokens
			ws.OutputTokens += u.OutputTokens
			ws.CacheReadTokens += t.Usage.CacheReadInputTokens
			ws.CacheCreationTokens += t.Usage.CacheCreationTokens
			ws.Count++
			resp.ByWorkspace[repoPath] = ws
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
	sorted := slices.Clone(tasks)
	slices.SortFunc(sorted, func(a, b store.Task) int {
		return cmp.Compare(b.Usage.CostUSD, a.Usage.CostUSD)
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

// aggregatePlanningStats scans `<configDir>/planning/*/usage.jsonl` for per-
// group planning round records and folds them into PlanningGroupStat values.
// Only records whose Timestamp is strictly after since are included (zero
// since means all records). The currently active workspace group is resolved
// to a friendly label and path list; stale group keys are returned with
// Label == key and Paths == nil.
//
// An empty or missing planning directory yields an empty map (never nil) so
// the JSON response serializes as "planning":{} rather than "planning":null.
func aggregatePlanningStats(configDir string, activeWorkspaces []string, since time.Time) map[string]PlanningGroupStat {
	result := make(map[string]PlanningGroupStat)
	if configDir == "" {
		return result
	}
	planningDir := filepath.Join(configDir, "planning")
	entries, err := os.ReadDir(planningDir)
	if err != nil {
		return result
	}

	activeKey := ""
	if len(activeWorkspaces) > 0 {
		activeKey = store.PlanningGroupKey(activeWorkspaces)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		key := e.Name()
		recs, err := store.ReadPlanningUsage(configDir, key, since)
		if err != nil || len(recs) == 0 {
			continue
		}
		stat := PlanningGroupStat{RoundCount: len(recs)}
		timeline := make([]RoundPoint, 0, len(recs))
		for _, rec := range recs {
			stat.Usage.CostUSD += rec.CostUSD
			stat.Usage.InputTokens += rec.InputTokens
			stat.Usage.OutputTokens += rec.OutputTokens
			stat.Usage.CacheReadTokens += rec.CacheReadInputTokens
			stat.Usage.CacheCreationTokens += rec.CacheCreationTokens
			timeline = append(timeline, RoundPoint{
				Timestamp: rec.Timestamp,
				CostUSD:   rec.CostUSD,
				Tokens:    rec.InputTokens + rec.OutputTokens,
			})
		}
		stat.Usage.Count = len(recs)
		slices.SortFunc(timeline, func(a, b RoundPoint) int {
			return a.Timestamp.Compare(b.Timestamp)
		})
		stat.Timeline = timeline
		if key == activeKey {
			stat.Paths = slices.Clone(activeWorkspaces)
			stat.Label = planningGroupLabel(activeWorkspaces)
		} else {
			stat.Label = key
		}
		result[key] = stat
	}
	return result
}

// planningGroupLabel builds a human-readable label from a workspace group's
// paths by joining their basenames. Empty input yields an empty string.
func planningGroupLabel(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, filepath.Base(p))
	}
	return strings.Join(names, ", ")
}

// GetStats aggregates token/cost data across all tasks (including archived)
// and returns a rolled-up analytics summary.
//
// Optional query params:
//   - ?workspace=<repo-root-path> — restrict task aggregation to tasks that
//     have that path as a WorktreePaths key. Returns 400 if present but no
//     tasks match it.
//   - ?days=N — restrict planning aggregation to rounds newer than N days
//     ago. Omitted or 0 means all time. Does not affect task buckets
//     (execution analytics are unchanged).
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
	resp := aggregateStats(tasks, h.store.LoadSummary)

	since := time.Time{}
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 {
			since = time.Now().UTC().AddDate(0, 0, -n)
		}
	}
	resp.Planning = aggregatePlanningStats(h.configDir, h.currentWorkspaces(), since)

	httpjson.Write(w, http.StatusOK, resp)
}
