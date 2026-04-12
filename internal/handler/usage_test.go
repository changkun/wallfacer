package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/store"
)

func TestGetUsageStats_EmptyStore(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/usage", nil)
	w := httptest.NewRecorder()
	h.GetUsageStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp usageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TaskCount != 0 {
		t.Errorf("expected 0 tasks, got %d", resp.TaskCount)
	}
	if resp.Total.InputTokens != 0 {
		t.Errorf("expected 0 input tokens, got %d", resp.Total.InputTokens)
	}
}

func TestGetUsageStats_DefaultSevenDays(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.AccumulateSubAgentUsage(ctx, task.ID, "implementation", store.TaskUsage{
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      0.01,
	}); err != nil {
		t.Fatalf("AccumulateSubAgentUsage: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/usage", nil)
	w := httptest.NewRecorder()
	h.GetUsageStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp usageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.PeriodDays != 7 {
		t.Errorf("expected default period_days=7, got %d", resp.PeriodDays)
	}
	if resp.TaskCount != 1 {
		t.Errorf("expected 1 task, got %d", resp.TaskCount)
	}
	if resp.Total.InputTokens != 100 {
		t.Errorf("expected 100 input tokens, got %d", resp.Total.InputTokens)
	}
	if resp.Total.OutputTokens != 50 {
		t.Errorf("expected 50 output tokens, got %d", resp.Total.OutputTokens)
	}
}

func TestGetUsageStats_AllTime_Days0(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "old task", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.AccumulateSubAgentUsage(ctx, task.ID, "implementation", store.TaskUsage{InputTokens: 200}); err != nil {
		t.Fatalf("AccumulateSubAgentUsage: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/usage?days=0", nil)
	w := httptest.NewRecorder()
	h.GetUsageStats(w, req)

	var resp usageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.PeriodDays != 0 {
		t.Errorf("expected period_days=0, got %d", resp.PeriodDays)
	}
	if resp.TaskCount != 1 {
		t.Errorf("expected 1 task, got %d", resp.TaskCount)
	}
	if resp.Total.InputTokens != 200 {
		t.Errorf("expected 200 input tokens, got %d", resp.Total.InputTokens)
	}
}

func TestGetUsageStats_ByStatus(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	backlog, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "backlog task", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask backlog: %v", err)
	}
	if err := h.store.AccumulateSubAgentUsage(ctx, backlog.ID, "implementation", store.TaskUsage{InputTokens: 10}); err != nil {
		t.Fatalf("AccumulateSubAgentUsage backlog: %v", err)
	}

	done, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "done task", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask done: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, done.ID, store.TaskStatusDone); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	if err := h.store.AccumulateSubAgentUsage(ctx, done.ID, "implementation", store.TaskUsage{InputTokens: 20}); err != nil {
		t.Fatalf("AccumulateSubAgentUsage done: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/usage?days=0", nil)
	w := httptest.NewRecorder()
	h.GetUsageStats(w, req)

	var resp usageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total.InputTokens != 30 {
		t.Errorf("total input tokens: expected 30, got %d", resp.Total.InputTokens)
	}
	if resp.ByStatus[store.TaskStatusBacklog].InputTokens != 10 {
		t.Errorf("backlog input tokens: expected 10, got %d", resp.ByStatus[store.TaskStatusBacklog].InputTokens)
	}
	if resp.ByStatus[store.TaskStatusDone].InputTokens != 20 {
		t.Errorf("done input tokens: expected 20, got %d", resp.ByStatus[store.TaskStatusDone].InputTokens)
	}
}

func TestGetUsageStats_BySubAgent(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task with breakdown", Timeout: 30, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := h.store.AccumulateSubAgentUsage(ctx, task.ID, "implementation", store.TaskUsage{InputTokens: 100, OutputTokens: 50}); err != nil {
		t.Fatalf("AccumulateSubAgentUsage implementation: %v", err)
	}
	if err := h.store.AccumulateSubAgentUsage(ctx, task.ID, "test", store.TaskUsage{InputTokens: 30, OutputTokens: 10}); err != nil {
		t.Fatalf("AccumulateSubAgentUsage test: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/usage?days=0", nil)
	w := httptest.NewRecorder()
	h.GetUsageStats(w, req)

	var resp usageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := resp.BySubAgent["implementation"].InputTokens; got != 100 {
		t.Errorf("implementation input tokens: expected 100, got %d", got)
	}
	if got := resp.BySubAgent["test"].InputTokens; got != 30 {
		t.Errorf("test input tokens: expected 30, got %d", got)
	}
}

func TestGetUsageStats_InvalidDaysParam(t *testing.T) {
	h := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/usage?days=abc", nil)
	w := httptest.NewRecorder()
	h.GetUsageStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp usageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Invalid parse falls back to default of 7.
	if resp.PeriodDays != 7 {
		t.Errorf("expected period_days=7 on invalid param, got %d", resp.PeriodDays)
	}
}

func TestGetUsageStats_MultipleTasksAggregated(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task", Timeout: 30, Kind: store.TaskKindTask})
		if err != nil {
			t.Fatalf("CreateTask %d: %v", i, err)
		}
		if err := h.store.AccumulateSubAgentUsage(ctx, task.ID, "implementation", store.TaskUsage{InputTokens: 10, CostUSD: 0.001}); err != nil {
			t.Fatalf("AccumulateSubAgentUsage %d: %v", i, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/usage?days=0", nil)
	w := httptest.NewRecorder()
	h.GetUsageStats(w, req)

	var resp usageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TaskCount != 3 {
		t.Errorf("expected 3 tasks, got %d", resp.TaskCount)
	}
	if resp.Total.InputTokens != 30 {
		t.Errorf("expected 30 total input tokens, got %d", resp.Total.InputTokens)
	}
}

// --- Planning merge ---

// usageResponseFromHandler calls GetUsageStats with the given URL and decodes
// the response.
func usageResponseFromHandler(t *testing.T, h *Handler, url string) usageResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	h.GetUsageStats(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp usageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func TestUsage_NoPlanningRecords(t *testing.T) {
	h := newTestHandler(t)
	// No planning dir on disk — response must not include a planning key.
	resp := usageResponseFromHandler(t, h, "/api/usage?days=0")
	if _, ok := resp.BySubAgent[store.SandboxActivityPlanning]; ok {
		t.Errorf("BySubAgent[planning] should be absent, got %+v", resp.BySubAgent[store.SandboxActivityPlanning])
	}
	if resp.Total.CostUSD != 0 || resp.Total.InputTokens != 0 {
		t.Errorf("Total should be zero, got %+v", resp.Total)
	}
}

func TestUsage_PlanningMergedIntoBySubAgent(t *testing.T) {
	h := newTestHandler(t)
	key := store.PlanningGroupKey([]string{"/repo/a"})
	now := time.Now().UTC()

	for _, rec := range []store.TurnUsageRecord{
		{Turn: 1, Timestamp: now, InputTokens: 100, OutputTokens: 40, CostUSD: 0.05},
		{Turn: 2, Timestamp: now.Add(time.Minute), InputTokens: 60, OutputTokens: 25, CacheReadInputTokens: 10, CostUSD: 0.03},
	} {
		if err := store.AppendPlanningUsage(h.configDir, key, rec); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	resp := usageResponseFromHandler(t, h, "/api/usage?days=0")

	got, ok := resp.BySubAgent[store.SandboxActivityPlanning]
	if !ok {
		t.Fatal("BySubAgent[planning] missing")
	}
	if got.InputTokens != 160 || got.OutputTokens != 65 || got.CacheReadInputTokens != 10 {
		t.Errorf("planning sums wrong: %+v", got)
	}
	if got.CostUSD != 0.08 {
		t.Errorf("planning cost = %v, want 0.08", got.CostUSD)
	}
	if resp.Total.InputTokens != 160 || resp.Total.OutputTokens != 65 || resp.Total.CostUSD != 0.08 {
		t.Errorf("Total should include planning usage: %+v", resp.Total)
	}
}

func TestUsage_PlanningRespectsDaysWindow(t *testing.T) {
	h := newTestHandler(t)
	key := store.PlanningGroupKey([]string{"/repo/a"})
	now := time.Now().UTC()

	// An "old" record 10 days back and a "new" record now.
	if err := store.AppendPlanningUsage(h.configDir, key, store.TurnUsageRecord{
		Turn: 1, Timestamp: now.AddDate(0, 0, -10), InputTokens: 999, OutputTokens: 999, CostUSD: 9.99,
	}); err != nil {
		t.Fatalf("append old: %v", err)
	}
	if err := store.AppendPlanningUsage(h.configDir, key, store.TurnUsageRecord{
		Turn: 2, Timestamp: now, InputTokens: 10, OutputTokens: 5, CostUSD: 0.01,
	}); err != nil {
		t.Fatalf("append new: %v", err)
	}

	resp := usageResponseFromHandler(t, h, "/api/usage?days=1")

	got := resp.BySubAgent[store.SandboxActivityPlanning]
	if got.InputTokens != 10 || got.OutputTokens != 5 {
		t.Errorf("expected only recent record included, got %+v", got)
	}
	if got.CostUSD != 0.01 {
		t.Errorf("expected cost 0.01, got %v", got.CostUSD)
	}
}

func TestUsage_PlanningAcrossMultipleGroups(t *testing.T) {
	h := newTestHandler(t)
	keyA := store.PlanningGroupKey([]string{"/repo/a"})
	keyB := store.PlanningGroupKey([]string{"/repo/b"})
	now := time.Now().UTC()

	if err := store.AppendPlanningUsage(h.configDir, keyA, store.TurnUsageRecord{
		Turn: 1, Timestamp: now, InputTokens: 30, OutputTokens: 10, CostUSD: 0.02,
	}); err != nil {
		t.Fatalf("append A: %v", err)
	}
	if err := store.AppendPlanningUsage(h.configDir, keyB, store.TurnUsageRecord{
		Turn: 1, Timestamp: now, InputTokens: 70, OutputTokens: 20, CostUSD: 0.04,
	}); err != nil {
		t.Fatalf("append B: %v", err)
	}

	resp := usageResponseFromHandler(t, h, "/api/usage?days=0")
	got := resp.BySubAgent[store.SandboxActivityPlanning]
	if got.InputTokens != 100 || got.OutputTokens != 30 {
		t.Errorf("planning tokens across groups = %+v, want sum", got)
	}
	if got.CostUSD != 0.06 {
		t.Errorf("planning cost = %v, want 0.06", got.CostUSD)
	}
}

func TestUsage_TaskCountUnchangedByPlanning(t *testing.T) {
	h := newTestHandler(t)
	key := store.PlanningGroupKey([]string{"/repo/a"})
	if err := store.AppendPlanningUsage(h.configDir, key, store.TurnUsageRecord{
		Turn: 1, Timestamp: time.Now().UTC(), InputTokens: 10, CostUSD: 0.01,
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	resp := usageResponseFromHandler(t, h, "/api/usage?days=0")
	if resp.TaskCount != 0 {
		t.Errorf("TaskCount = %d, want 0 (planning rounds must not count as tasks)", resp.TaskCount)
	}
}
