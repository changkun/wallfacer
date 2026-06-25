package handler

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"latere.ai/x/wallfacer/internal/pkg/httpjson"
	"latere.ai/x/wallfacer/internal/store"
)

// usageResponse is the JSON body returned by GET /api/usage.
type usageResponse struct {
	Total      store.TaskUsage                           `json:"total"`
	ByStatus   map[store.TaskStatus]store.TaskUsage      `json:"by_status"`
	BySubAgent map[store.SandboxActivity]store.TaskUsage `json:"by_sub_agent"`
	TaskCount  int                                       `json:"task_count"`
	PeriodDays int                                       `json:"period_days"`
}

// agentSessionRecordAsUsage projects a TurnUsageRecord into the TaskUsage shape
// so it can flow through the same TaskUsage.Add path as task-side data.
func agentSessionRecordAsUsage(rec store.TurnUsageRecord) store.TaskUsage {
	return store.TaskUsage{
		InputTokens:          rec.InputTokens,
		OutputTokens:         rec.OutputTokens,
		CacheReadInputTokens: rec.CacheReadInputTokens,
		CacheCreationTokens:  rec.CacheCreationTokens,
		CostUSD:              rec.CostUSD,
	}
}

// mergeAgentSessionUsage scans <configDir>/agent-sessions/<group>/usage.jsonl files
// and merges records into BySubAgent["agent-session"] and Total, honoring the
// caller's cutoff. It is a no-op when configDir is empty or the agent-sessions
// directory does not exist. TaskCount is deliberately untouched — it counts
// tasks, not agent-session rounds.
func mergeAgentSessionUsage(resp *usageResponse, configDir string, cutoff time.Time) {
	if configDir == "" {
		return
	}
	entries, err := os.ReadDir(store.AgentSessionsRoot(configDir))
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		recs, err := store.ReadAgentSessionUsage(configDir, e.Name(), cutoff)
		if err != nil {
			continue
		}
		for _, rec := range recs {
			u := agentSessionRecordAsUsage(rec)
			a := resp.BySubAgent[store.SandboxActivityAgentSession]
			a.Add(u)
			resp.BySubAgent[store.SandboxActivityAgentSession] = a
			resp.Total.Add(u)
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

	s, ok := h.requireStore(w)
	if !ok {
		return
	}

	tasks, err := s.ListTasks(r.Context(), true /* includeArchived */)
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

		resp.Total.Add(t.Usage)

		s := resp.ByStatus[t.Status]
		s.Add(t.Usage)
		resp.ByStatus[t.Status] = s

		for agent, u := range t.UsageBreakdown {
			a := resp.BySubAgent[agent]
			a.Add(u)
			resp.BySubAgent[agent] = a
		}
	}

	mergeAgentSessionUsage(&resp, h.configDir, cutoff)

	httpjson.Write(w, http.StatusOK, resp)
}
