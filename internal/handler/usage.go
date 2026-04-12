package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/store"
)

// usageResponse is the JSON body returned by GET /api/usage.
type usageResponse struct {
	Total      store.TaskUsage                           `json:"total"`
	ByStatus   map[store.TaskStatus]store.TaskUsage      `json:"by_status"`
	BySubAgent map[store.SandboxActivity]store.TaskUsage `json:"by_sub_agent"`
	TaskCount  int                                       `json:"task_count"`
	PeriodDays int                                       `json:"period_days"`
}

// addUsage accumulates all token and cost fields from src into dst.
func addUsage(dst *store.TaskUsage, src store.TaskUsage) {
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.CacheReadInputTokens += src.CacheReadInputTokens
	dst.CacheCreationTokens += src.CacheCreationTokens
	dst.CostUSD += src.CostUSD
}

// planningRecordAsUsage projects a TurnUsageRecord into the TaskUsage shape
// so it can flow through the same addUsage helper as task-side data.
func planningRecordAsUsage(rec store.TurnUsageRecord) store.TaskUsage {
	return store.TaskUsage{
		InputTokens:          rec.InputTokens,
		OutputTokens:         rec.OutputTokens,
		CacheReadInputTokens: rec.CacheReadInputTokens,
		CacheCreationTokens:  rec.CacheCreationTokens,
		CostUSD:              rec.CostUSD,
	}
}

// mergePlanningUsage scans <configDir>/planning/<group>/usage.jsonl files
// and merges records into BySubAgent["planning"] and Total, honoring the
// caller's cutoff. It is a no-op when configDir is empty or the planning
// directory does not exist. TaskCount is deliberately untouched — it counts
// tasks, not planning rounds.
func mergePlanningUsage(resp *usageResponse, configDir string, cutoff time.Time) {
	if configDir == "" {
		return
	}
	entries, err := os.ReadDir(filepath.Join(configDir, "planning"))
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		recs, err := store.ReadPlanningUsage(configDir, e.Name(), cutoff)
		if err != nil {
			continue
		}
		for _, rec := range recs {
			u := planningRecordAsUsage(rec)
			a := resp.BySubAgent[store.SandboxActivityPlanning]
			addUsage(&a, u)
			resp.BySubAgent[store.SandboxActivityPlanning] = a
			addUsage(&resp.Total, u)
		}
	}
}

// GetUsageStats aggregates token/cost data across tasks and returns a summary.
// Query param: days=N (0 = all time, default 7).
func (h *Handler) GetUsageStats(w http.ResponseWriter, r *http.Request) {
	daysStr := r.URL.Query().Get("days")
	days := 7
	if daysStr != "" {
		if v, err := strconv.Atoi(daysStr); err == nil {
			days = v
		}
	}

	tasks, err := h.store.ListTasks(r.Context(), true /* includeArchived */)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var cutoff time.Time
	if days > 0 {
		cutoff = time.Now().UTC().AddDate(0, 0, -days)
	}

	resp := usageResponse{
		ByStatus:   make(map[store.TaskStatus]store.TaskUsage),
		BySubAgent: make(map[store.SandboxActivity]store.TaskUsage),
		PeriodDays: days,
	}

	for _, t := range tasks {
		if days > 0 && t.UpdatedAt.Before(cutoff) {
			continue
		}
		resp.TaskCount++

		addUsage(&resp.Total, t.Usage)

		s := resp.ByStatus[t.Status]
		addUsage(&s, t.Usage)
		resp.ByStatus[t.Status] = s

		for agent, u := range t.UsageBreakdown {
			a := resp.BySubAgent[agent]
			addUsage(&a, u)
			resp.BySubAgent[agent] = a
		}
	}

	mergePlanningUsage(&resp, h.configDir, cutoff)

	httpjson.Write(w, http.StatusOK, resp)
}
