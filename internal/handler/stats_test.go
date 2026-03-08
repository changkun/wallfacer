package handler

import (
	"testing"
	"time"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

func TestAggregateStats(t *testing.T) {
	now := time.Now().UTC()

	tasks := []store.Task{
		{
			ID:        uuid.New(),
			Title:     "Task 1 — done high cost",
			Status:    store.TaskStatusDone,
			CreatedAt: now,
			Usage: store.TaskUsage{
				InputTokens:          1000,
				OutputTokens:         500,
				CacheReadInputTokens: 200,
				CostUSD:              0.10,
			},
			UsageBreakdown: map[string]store.TaskUsage{
				"implementation": {InputTokens: 800, OutputTokens: 400, CostUSD: 0.08},
				"test":           {InputTokens: 200, OutputTokens: 100, CostUSD: 0.02},
			},
		},
		{
			ID:        uuid.New(),
			Prompt:    "Task 2 prompt — failed task with no title set at all for testing the fallback path",
			Status:    store.TaskStatusFailed,
			CreatedAt: now.AddDate(0, 0, -1),
			Usage: store.TaskUsage{
				InputTokens:  500,
				OutputTokens: 200,
				CostUSD:      0.04,
			},
			UsageBreakdown: map[string]store.TaskUsage{
				"implementation": {InputTokens: 500, OutputTokens: 200, CostUSD: 0.04},
			},
		},
		{
			ID:        uuid.New(),
			Title:     "Task 3 — done medium",
			Status:    store.TaskStatusDone,
			CreatedAt: now.AddDate(0, 0, -2),
			Usage: store.TaskUsage{
				InputTokens:         2000,
				OutputTokens:        800,
				CacheCreationTokens: 100,
				CostUSD:             0.20,
			},
			UsageBreakdown: map[string]store.TaskUsage{
				"implementation": {InputTokens: 1500, OutputTokens: 600, CostUSD: 0.15},
				"oversight":      {InputTokens: 500, OutputTokens: 200, CostUSD: 0.05},
			},
		},
		{
			ID:        uuid.New(),
			Title:     "Task 4 — waiting",
			Status:    store.TaskStatusWaiting,
			CreatedAt: now.AddDate(0, 0, -3),
			Usage: store.TaskUsage{
				InputTokens:  300,
				OutputTokens: 100,
				CostUSD:      0.01,
			},
			UsageBreakdown: map[string]store.TaskUsage{
				"title": {InputTokens: 300, OutputTokens: 100, CostUSD: 0.01},
			},
		},
		{
			ID:        uuid.New(),
			Title:     "Task 5 — cancelled archived",
			Status:    store.TaskStatusCancelled,
			Archived:  true,
			CreatedAt: now.AddDate(0, 0, -5),
			Usage: store.TaskUsage{
				InputTokens:  150,
				OutputTokens: 50,
				CostUSD:      0.005,
			},
			UsageBreakdown: map[string]store.TaskUsage{
				"implementation": {InputTokens: 150, OutputTokens: 50, CostUSD: 0.005},
			},
		},
	}

	resp := aggregateStats(tasks)

	// --- TotalCostUSD ---
	wantTotal := 0.10 + 0.04 + 0.20 + 0.01 + 0.005
	if diff := resp.TotalCostUSD - wantTotal; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("TotalCostUSD = %v, want %v", resp.TotalCostUSD, wantTotal)
	}

	// --- TotalInputTokens ---
	wantInput := 1000 + 500 + 2000 + 300 + 150
	if resp.TotalInputTokens != wantInput {
		t.Errorf("TotalInputTokens = %d, want %d", resp.TotalInputTokens, wantInput)
	}

	// --- TotalOutputTokens ---
	wantOutput := 500 + 200 + 800 + 100 + 50
	if resp.TotalOutputTokens != wantOutput {
		t.Errorf("TotalOutputTokens = %d, want %d", resp.TotalOutputTokens, wantOutput)
	}

	// --- TotalCacheTokens (cache read + cache creation) ---
	wantCache := 200 + 100 // task1 cache_read + task3 cache_creation
	if resp.TotalCacheTokens != wantCache {
		t.Errorf("TotalCacheTokens = %d, want %d", resp.TotalCacheTokens, wantCache)
	}

	// --- ByStatus ---
	doneStat, ok := resp.ByStatus["done"]
	if !ok {
		t.Fatal("ByStatus missing 'done'")
	}
	wantDoneCost := 0.10 + 0.20
	if diff := doneStat.CostUSD - wantDoneCost; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByStatus[done].CostUSD = %v, want %v", doneStat.CostUSD, wantDoneCost)
	}
	wantDoneInput := 1000 + 2000
	if doneStat.InputTokens != wantDoneInput {
		t.Errorf("ByStatus[done].InputTokens = %d, want %d", doneStat.InputTokens, wantDoneInput)
	}

	failedStat, ok := resp.ByStatus["failed"]
	if !ok {
		t.Fatal("ByStatus missing 'failed'")
	}
	if diff := failedStat.CostUSD - 0.04; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByStatus[failed].CostUSD = %v, want 0.04", failedStat.CostUSD)
	}

	cancelledStat, ok := resp.ByStatus["cancelled"]
	if !ok {
		t.Fatal("ByStatus missing 'cancelled'")
	}
	if diff := cancelledStat.CostUSD - 0.005; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByStatus[cancelled].CostUSD = %v, want 0.005", cancelledStat.CostUSD)
	}

	// --- ByActivity ---
	// implementation total: 0.08 + 0.04 + 0.15 + 0.005 = 0.275
	implStat, ok := resp.ByActivity["implementation"]
	if !ok {
		t.Fatal("ByActivity missing 'implementation'")
	}
	wantImplCost := 0.08 + 0.04 + 0.15 + 0.005
	if diff := implStat.CostUSD - wantImplCost; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByActivity[implementation].CostUSD = %v, want %v", implStat.CostUSD, wantImplCost)
	}
	wantImplInput := 800 + 500 + 1500 + 150
	if implStat.InputTokens != wantImplInput {
		t.Errorf("ByActivity[implementation].InputTokens = %d, want %d", implStat.InputTokens, wantImplInput)
	}

	testStat, ok := resp.ByActivity["test"]
	if !ok {
		t.Fatal("ByActivity missing 'test'")
	}
	if diff := testStat.CostUSD - 0.02; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByActivity[test].CostUSD = %v, want 0.02", testStat.CostUSD)
	}

	oversightStat, ok := resp.ByActivity["oversight"]
	if !ok {
		t.Fatal("ByActivity missing 'oversight'")
	}
	if diff := oversightStat.CostUSD - 0.05; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByActivity[oversight].CostUSD = %v, want 0.05", oversightStat.CostUSD)
	}

	titleStat, ok := resp.ByActivity["title"]
	if !ok {
		t.Fatal("ByActivity missing 'title'")
	}
	if diff := titleStat.CostUSD - 0.01; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByActivity[title].CostUSD = %v, want 0.01", titleStat.CostUSD)
	}

	// --- TopTasks: ordered by cost descending, capped at 10 ---
	if len(resp.TopTasks) > 10 {
		t.Errorf("TopTasks len = %d, exceeds cap of 10", len(resp.TopTasks))
	}
	if len(resp.TopTasks) != 5 {
		t.Errorf("TopTasks len = %d, want 5 (total tasks)", len(resp.TopTasks))
	}
	// Verify descending order.
	for i := 1; i < len(resp.TopTasks); i++ {
		if resp.TopTasks[i].CostUSD > resp.TopTasks[i-1].CostUSD {
			t.Errorf("TopTasks not sorted descending at index %d: cost %v > cost %v",
				i, resp.TopTasks[i].CostUSD, resp.TopTasks[i-1].CostUSD)
		}
	}
	// Highest cost task is task 3 (0.20).
	if diff := resp.TopTasks[0].CostUSD - 0.20; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("TopTasks[0].CostUSD = %v, want 0.20", resp.TopTasks[0].CostUSD)
	}
	// Task 2 has no title — should fall back to first 60 chars of prompt.
	for _, entry := range resp.TopTasks {
		if entry.Title == "" {
			t.Errorf("TopTasks entry id=%s has empty title (prompt fallback failed)", entry.ID)
		}
	}

	// --- DailyUsage: exactly 30 entries ---
	if len(resp.DailyUsage) != 30 {
		t.Errorf("DailyUsage len = %d, want 30", len(resp.DailyUsage))
	}
	// Ascending date order.
	for i := 1; i < len(resp.DailyUsage); i++ {
		if resp.DailyUsage[i].Date <= resp.DailyUsage[i-1].Date {
			t.Errorf("DailyUsage not ascending: [%d].Date=%s <= [%d].Date=%s",
				i, resp.DailyUsage[i].Date, i-1, resp.DailyUsage[i-1].Date)
		}
	}
	// Last entry must be today.
	today := time.Now().UTC().Format("2006-01-02")
	if resp.DailyUsage[29].Date != today {
		t.Errorf("DailyUsage[29].Date = %s, want %s (today)", resp.DailyUsage[29].Date, today)
	}
	// Tasks created within the window should contribute to daily totals.
	// Task 1 is created today — its cost should appear in DailyUsage[29].
	if diff := resp.DailyUsage[29].CostUSD - 0.10; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("DailyUsage[29].CostUSD = %v, want 0.10 (task 1 today)", resp.DailyUsage[29].CostUSD)
	}
}
