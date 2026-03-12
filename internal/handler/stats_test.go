package handler

import (
	"testing"
	"time"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// noSummary is a loadSummary stub that always returns (nil, nil), simulating
// the backward-compatible behaviour when no summary.json exists.
func noSummary(_ uuid.UUID) (*store.TaskSummary, error) { return nil, nil }

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

	resp := aggregateStats(tasks, noSummary)

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

// TestAggregateStats_ByWorkspace verifies workspace-level cost and token bucketing.
func TestAggregateStats_ByWorkspace(t *testing.T) {
	now := time.Now().UTC()

	repoA := "/home/user/project-a"
	repoB := "/home/user/project-b"

	tasks := []store.Task{
		{
			// Task with two worktree paths — contributes to both repoA and repoB.
			ID:        uuid.New(),
			Title:     "Task 1 — two repos",
			Status:    store.TaskStatusDone,
			CreatedAt: now,
			Usage: store.TaskUsage{
				InputTokens:          1000,
				OutputTokens:         500,
				CacheReadInputTokens: 100,
				CacheCreationTokens:  50,
				CostUSD:              0.10,
			},
			UsageBreakdown: map[string]store.TaskUsage{
				"implementation": {InputTokens: 1000, OutputTokens: 500, CostUSD: 0.10},
			},
			WorktreePaths: map[string]string{
				repoA: "/worktrees/a1",
				repoB: "/worktrees/b1",
			},
		},
		{
			// Task with only repoA.
			ID:        uuid.New(),
			Title:     "Task 2 — repo-a only",
			Status:    store.TaskStatusFailed,
			CreatedAt: now,
			Usage: store.TaskUsage{
				InputTokens:          400,
				OutputTokens:         200,
				CacheReadInputTokens: 20,
				CostUSD:              0.04,
			},
			UsageBreakdown: map[string]store.TaskUsage{
				"implementation": {InputTokens: 400, OutputTokens: 200, CostUSD: 0.04},
			},
			WorktreePaths: map[string]string{
				repoA: "/worktrees/a2",
			},
		},
		{
			// Task with empty WorktreePaths — excluded from ByWorkspace, counted in ByStatus.
			ID:        uuid.New(),
			Title:     "Task 3 — never ran",
			Status:    store.TaskStatusCancelled,
			CreatedAt: now,
			Usage: store.TaskUsage{
				InputTokens:  100,
				OutputTokens: 50,
				CostUSD:      0.01,
			},
			WorktreePaths: nil,
		},
	}

	resp := aggregateStats(tasks, noSummary)

	// --- ByWorkspace ---

	if len(resp.ByWorkspace) != 2 {
		t.Fatalf("ByWorkspace len = %d, want 2 (repoA and repoB)", len(resp.ByWorkspace))
	}

	// repoA: task1 + task2
	wsA, ok := resp.ByWorkspace[repoA]
	if !ok {
		t.Fatalf("ByWorkspace missing %q", repoA)
	}
	if wsA.Count != 2 {
		t.Errorf("ByWorkspace[repoA].Count = %d, want 2", wsA.Count)
	}
	wantACost := 0.10 + 0.04
	if diff := wsA.CostUSD - wantACost; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByWorkspace[repoA].CostUSD = %v, want %v", wsA.CostUSD, wantACost)
	}
	if wsA.InputTokens != 1000+400 {
		t.Errorf("ByWorkspace[repoA].InputTokens = %d, want %d", wsA.InputTokens, 1000+400)
	}
	if wsA.OutputTokens != 500+200 {
		t.Errorf("ByWorkspace[repoA].OutputTokens = %d, want %d", wsA.OutputTokens, 500+200)
	}
	// CacheReadTokens: 100 (task1) + 20 (task2)
	if wsA.CacheReadTokens != 100+20 {
		t.Errorf("ByWorkspace[repoA].CacheReadTokens = %d, want %d", wsA.CacheReadTokens, 100+20)
	}
	// CacheCreationTokens: 50 (task1) + 0 (task2)
	if wsA.CacheCreationTokens != 50 {
		t.Errorf("ByWorkspace[repoA].CacheCreationTokens = %d, want 50", wsA.CacheCreationTokens)
	}

	// repoB: task1 only
	wsB, ok := resp.ByWorkspace[repoB]
	if !ok {
		t.Fatalf("ByWorkspace missing %q", repoB)
	}
	if wsB.Count != 1 {
		t.Errorf("ByWorkspace[repoB].Count = %d, want 1", wsB.Count)
	}
	if diff := wsB.CostUSD - 0.10; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByWorkspace[repoB].CostUSD = %v, want 0.10", wsB.CostUSD)
	}

	// --- Task 3 (no WorktreePaths) is excluded from ByWorkspace ---
	// It should still appear in ByStatus[cancelled].
	cancelled, ok := resp.ByStatus["cancelled"]
	if !ok {
		t.Fatal("ByStatus missing 'cancelled'")
	}
	if diff := cancelled.CostUSD - 0.01; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByStatus[cancelled].CostUSD = %v, want 0.01", cancelled.CostUSD)
	}
}

// TestFilterTasksByWorkspace verifies the workspace query-param filter helper.
func TestFilterTasksByWorkspace(t *testing.T) {
	repoA := "/home/user/project-a"
	repoB := "/home/user/project-b"

	tasks := []store.Task{
		{
			ID: uuid.New(), Title: "Task A",
			WorktreePaths: map[string]string{repoA: "/worktrees/a"},
		},
		{
			ID: uuid.New(), Title: "Task B",
			WorktreePaths: map[string]string{repoB: "/worktrees/b"},
		},
		{
			ID:            uuid.New(), Title: "Task None",
			WorktreePaths: nil,
		},
	}

	tests := []struct {
		name      string
		workspace string
		wantCount int
		wantOK    bool
	}{
		{"empty filter returns all", "", 3, true},
		{"filter by repoA", repoA, 1, true},
		{"filter by repoB", repoB, 1, true},
		{"filter by unknown path returns false", "/unknown/path", 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := filterTasksByWorkspace(tasks, tc.workspace)
			if ok != tc.wantOK {
				t.Errorf("filterTasksByWorkspace ok = %v, want %v", ok, tc.wantOK)
			}
			if len(got) != tc.wantCount {
				t.Errorf("filterTasksByWorkspace count = %d, want %d", len(got), tc.wantCount)
			}
		})
	}

	// Verify aggregateStats on the filtered result contains only repoA.
	filteredA, _ := filterTasksByWorkspace(tasks, repoA)
	resp := aggregateStats(filteredA, noSummary)
	if _, ok := resp.ByWorkspace[repoA]; !ok {
		t.Errorf("after filtering by repoA, ByWorkspace should contain %q", repoA)
	}
	if _, ok := resp.ByWorkspace[repoB]; ok {
		t.Errorf("after filtering by repoA, ByWorkspace should NOT contain %q", repoB)
	}
}

// TestAggregateStats_SummaryFallback verifies that aggregateStats uses a
// summary's ByActivity and TotalCostUSD for done tasks when a summary is
// available, while still accumulating live data for non-done tasks.
func TestAggregateStats_SummaryFallback(t *testing.T) {
	now := time.Now().UTC()

	doneID := uuid.New()
	inProgID := uuid.New()

	tasks := []store.Task{
		{
			ID:        doneID,
			Title:     "Done task",
			Status:    store.TaskStatusDone,
			CreatedAt: now,
			Usage: store.TaskUsage{
				InputTokens:  1000,
				OutputTokens: 500,
				CostUSD:      0.10, // will be overridden by summary
			},
			UsageBreakdown: map[string]store.TaskUsage{
				"implementation": {InputTokens: 1000, OutputTokens: 500, CostUSD: 0.10},
			},
		},
		{
			ID:        inProgID,
			Title:     "In-progress task",
			Status:    store.TaskStatusInProgress,
			CreatedAt: now,
			Usage: store.TaskUsage{
				InputTokens:  200,
				OutputTokens: 100,
				CostUSD:      0.05,
			},
			UsageBreakdown: map[string]store.TaskUsage{
				"implementation": {InputTokens: 200, OutputTokens: 100, CostUSD: 0.05},
			},
		},
	}

	// Summary for the done task with different ByActivity (e.g. after re-calc).
	summaryByActivity := map[string]store.TaskUsage{
		"implementation": {InputTokens: 800, OutputTokens: 400, CostUSD: 0.08},
		"oversight":      {InputTokens: 200, OutputTokens: 100, CostUSD: 0.02},
	}
	summaryTotalCost := 0.10 // same total, different breakdown

	loadSummary := func(id uuid.UUID) (*store.TaskSummary, error) {
		if id == doneID {
			return &store.TaskSummary{
				TaskID:       doneID,
				Status:       store.TaskStatusDone,
				TotalCostUSD: summaryTotalCost,
				ByActivity:   summaryByActivity,
			}, nil
		}
		return nil, nil // no summary for in-progress task
	}

	resp := aggregateStats(tasks, loadSummary)

	// Total cost: summary's 0.10 + in-prog's 0.05 = 0.15
	wantTotal := 0.15
	if diff := resp.TotalCostUSD - wantTotal; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("TotalCostUSD = %v, want %v", resp.TotalCostUSD, wantTotal)
	}

	// ByActivity should reflect the summary's breakdown for the done task
	// plus the live breakdown for the in-progress task.
	// implementation: summary 0.08 + in-prog 0.05 = 0.13
	wantImplCost := 0.08 + 0.05
	implStat := resp.ByActivity["implementation"]
	if diff := implStat.CostUSD - wantImplCost; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByActivity[implementation].CostUSD = %v, want %v", implStat.CostUSD, wantImplCost)
	}
	// oversight: 0.02 (from summary only)
	oversightStat := resp.ByActivity["oversight"]
	if diff := oversightStat.CostUSD - 0.02; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByActivity[oversight].CostUSD = %v, want 0.02", oversightStat.CostUSD)
	}

	// In-progress token totals still come from live task.Usage.
	wantInputTokens := 1000 + 200 // done task.Usage.InputTokens + in-prog.Usage.InputTokens
	if resp.TotalInputTokens != wantInputTokens {
		t.Errorf("TotalInputTokens = %d, want %d", resp.TotalInputTokens, wantInputTokens)
	}
}
