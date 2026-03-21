package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
