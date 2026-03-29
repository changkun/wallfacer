package handler

import (
	"net/http"
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

	httpjson.Write(w, http.StatusOK, resp)
}
