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
			UsageBreakdown: map[store.SandboxActivity]store.TaskUsage{
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
			UsageBreakdown: map[store.SandboxActivity]store.TaskUsage{
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
			UsageBreakdown: map[store.SandboxActivity]store.TaskUsage{
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
			UsageBreakdown: map[store.SandboxActivity]store.TaskUsage{
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
			UsageBreakdown: map[store.SandboxActivity]store.TaskUsage{
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

// TestAggregateStats_ByWorkspace verifies workspace-level cost and token bucketing
// using the equal-split fallback (no WorkspaceUsageBreakdown set).
func TestAggregateStats_ByWorkspace(t *testing.T) {
	now := time.Now().UTC()

	repoA := "/home/user/project-a"
	repoB := "/home/user/project-b"

	tasks := []store.Task{
		{
			// Task with two worktree paths and no stored breakdown.
			// Falls back to equal split: each repo gets half the usage.
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
			UsageBreakdown: map[store.SandboxActivity]store.TaskUsage{
				"implementation": {InputTokens: 1000, OutputTokens: 500, CostUSD: 0.10},
			},
			WorktreePaths: map[string]string{
				repoA: "/worktrees/a1",
				repoB: "/worktrees/b1",
			},
		},
		{
			// Task with only repoA — 100% of usage goes to repoA.
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
			UsageBreakdown: map[store.SandboxActivity]store.TaskUsage{
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

	// Task 1 has no breakdown → equal split: repoA and repoB each get half.
	// Task 2 has only repoA → repoA gets 100%.
	wsA, ok := resp.ByWorkspace[repoA]
	if !ok {
		t.Fatalf("ByWorkspace missing %q", repoA)
	}
	if wsA.Count != 2 {
		t.Errorf("ByWorkspace[repoA].Count = %d, want 2", wsA.Count)
	}
	// 0.05 (half of task1) + 0.04 (all of task2) = 0.09
	wantACost := 0.10/2 + 0.04
	if diff := wsA.CostUSD - wantACost; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByWorkspace[repoA].CostUSD = %v, want %v", wsA.CostUSD, wantACost)
	}
	// int(1000/2) + 400 = 500 + 400
	if wsA.InputTokens != 500+400 {
		t.Errorf("ByWorkspace[repoA].InputTokens = %d, want %d", wsA.InputTokens, 500+400)
	}
	// int(500/2) + 200 = 250 + 200
	if wsA.OutputTokens != 250+200 {
		t.Errorf("ByWorkspace[repoA].OutputTokens = %d, want %d", wsA.OutputTokens, 250+200)
	}
	// CacheReadTokens: int(100/2) + 20 = 50 + 20
	if wsA.CacheReadTokens != 50+20 {
		t.Errorf("ByWorkspace[repoA].CacheReadTokens = %d, want %d", wsA.CacheReadTokens, 50+20)
	}
	// CacheCreationTokens: int(50/2) + 0 = 25
	if wsA.CacheCreationTokens != 25 {
		t.Errorf("ByWorkspace[repoA].CacheCreationTokens = %d, want 25", wsA.CacheCreationTokens)
	}

	// repoB: task1 equal-split half only → 0.05
	wsB, ok := resp.ByWorkspace[repoB]
	if !ok {
		t.Fatalf("ByWorkspace missing %q", repoB)
	}
	if wsB.Count != 1 {
		t.Errorf("ByWorkspace[repoB].Count = %d, want 1", wsB.Count)
	}
	if diff := wsB.CostUSD - 0.05; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByWorkspace[repoB].CostUSD = %v, want 0.05", wsB.CostUSD)
	}

	// Total across all workspace buckets must equal tasks that have WorktreePaths
	// (task1 + task2 = 0.14), not doubled.
	totalWS := wsA.CostUSD + wsB.CostUSD
	wantWStotal := 0.10 + 0.04 // task3 excluded (no WorktreePaths)
	if diff := totalWS - wantWStotal; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByWorkspace total cost = %v, want %v (no doubling)", totalWS, wantWStotal)
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

// TestAggregateStats_ByWorkspace_NoDoubling is the focused regression test that
// proves tasks touching multiple repos no longer double total_cost_usd,
// input_tokens, or cache tokens in ByWorkspace.
func TestAggregateStats_ByWorkspace_NoDoubling(t *testing.T) {
	now := time.Now().UTC()

	repoA := "/repo/alpha"
	repoB := "/repo/beta"

	// Task 1 touches both repos with an explicit 75/25 breakdown.
	task1 := store.Task{
		ID:        uuid.New(),
		Title:     "Task 1 — two repos with breakdown",
		Status:    store.TaskStatusDone,
		CreatedAt: now,
		Usage: store.TaskUsage{
			InputTokens:          2000,
			OutputTokens:         800,
			CacheReadInputTokens: 400,
			CacheCreationTokens:  200,
			CostUSD:              0.20,
		},
		WorktreePaths: map[string]string{
			repoA: "/worktrees/a",
			repoB: "/worktrees/b",
		},
		// Breakdown: repoA 75%, repoB 25%.
		WorkspaceUsageBreakdown: map[string]store.TaskUsage{
			repoA: {InputTokens: 1500, OutputTokens: 600, CacheReadInputTokens: 300, CacheCreationTokens: 150, CostUSD: 0.15},
			repoB: {InputTokens: 500, OutputTokens: 200, CacheReadInputTokens: 100, CacheCreationTokens: 50, CostUSD: 0.05},
		},
	}

	// Task 2 touches only repoA.
	task2 := store.Task{
		ID:        uuid.New(),
		Title:     "Task 2 — single repo",
		Status:    store.TaskStatusDone,
		CreatedAt: now,
		Usage: store.TaskUsage{
			InputTokens:          500,
			OutputTokens:         200,
			CacheReadInputTokens: 50,
			CostUSD:              0.05,
		},
		WorktreePaths: map[string]string{
			repoA: "/worktrees/a2",
		},
		WorkspaceUsageBreakdown: map[string]store.TaskUsage{
			repoA: {InputTokens: 500, OutputTokens: 200, CacheReadInputTokens: 50, CostUSD: 0.05},
		},
	}

	tasks := []store.Task{task1, task2}
	resp := aggregateStats(tasks, noSummary)

	// Global total: task1 + task2, not doubled.
	wantGlobalCost := 0.20 + 0.05
	if diff := resp.TotalCostUSD - wantGlobalCost; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("TotalCostUSD = %v, want %v", resp.TotalCostUSD, wantGlobalCost)
	}

	// ByWorkspace totals across all repos must equal the global total (no duplication).
	var wsTotalCost float64
	var wsTotalInput int
	var wsTotalCacheRead int
	for _, ws := range resp.ByWorkspace {
		wsTotalCost += ws.CostUSD
		wsTotalInput += ws.InputTokens
		wsTotalCacheRead += ws.CacheReadTokens
	}
	if diff := wsTotalCost - wantGlobalCost; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByWorkspace total cost = %v, want %v (no doubling)", wsTotalCost, wantGlobalCost)
	}
	wantGlobalInput := 2000 + 500
	if wsTotalInput != wantGlobalInput {
		t.Errorf("ByWorkspace total input_tokens = %d, want %d (no doubling)", wsTotalInput, wantGlobalInput)
	}
	wantGlobalCacheRead := 400 + 50
	if wsTotalCacheRead != wantGlobalCacheRead {
		t.Errorf("ByWorkspace total cache_read_tokens = %d, want %d (no doubling)", wsTotalCacheRead, wantGlobalCacheRead)
	}

	// repoA: task1 75% ($0.15) + task2 100% ($0.05) = $0.20
	wsA, ok := resp.ByWorkspace[repoA]
	if !ok {
		t.Fatalf("ByWorkspace missing %q", repoA)
	}
	wantACost := 0.15 + 0.05
	if diff := wsA.CostUSD - wantACost; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByWorkspace[repoA].CostUSD = %v, want %v", wsA.CostUSD, wantACost)
	}
	if wsA.InputTokens != 1500+500 {
		t.Errorf("ByWorkspace[repoA].InputTokens = %d, want %d", wsA.InputTokens, 1500+500)
	}
	if wsA.CacheReadTokens != 300+50 {
		t.Errorf("ByWorkspace[repoA].CacheReadTokens = %d, want %d", wsA.CacheReadTokens, 300+50)
	}
	if wsA.Count != 2 {
		t.Errorf("ByWorkspace[repoA].Count = %d, want 2", wsA.Count)
	}

	// repoB: task1 25% only ($0.05)
	wsB, ok := resp.ByWorkspace[repoB]
	if !ok {
		t.Fatalf("ByWorkspace missing %q", repoB)
	}
	if diff := wsB.CostUSD - 0.05; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByWorkspace[repoB].CostUSD = %v, want 0.05", wsB.CostUSD)
	}
	if wsB.InputTokens != 500 {
		t.Errorf("ByWorkspace[repoB].InputTokens = %d, want 500", wsB.InputTokens)
	}
	if wsB.Count != 1 {
		t.Errorf("ByWorkspace[repoB].Count = %d, want 1", wsB.Count)
	}
}

// TestAggregateStats_ByWorkspace_EqualSplitFallback verifies that tasks without
// a stored WorkspaceUsageBreakdown fall back to equal splitting, keeping totals
// internally consistent (no N-fold amplification).
func TestAggregateStats_ByWorkspace_EqualSplitFallback(t *testing.T) {
	now := time.Now().UTC()

	repoA := "/repo/a"
	repoB := "/repo/b"
	repoC := "/repo/c"

	tasks := []store.Task{
		{
			// Task touching 3 repos, no breakdown → equal split (1/3 each).
			ID:        uuid.New(),
			Status:    store.TaskStatusInProgress,
			CreatedAt: now,
			Usage: store.TaskUsage{
				InputTokens:          900,
				OutputTokens:         300,
				CacheReadInputTokens: 150,
				CostUSD:              0.09,
			},
			WorktreePaths: map[string]string{
				repoA: "/wt/a",
				repoB: "/wt/b",
				repoC: "/wt/c",
			},
			// WorkspaceUsageBreakdown intentionally nil.
		},
	}

	resp := aggregateStats(tasks, noSummary)

	// Total across all workspace buckets must equal the single task cost (not 3×).
	var totalCost float64
	var totalInput int
	var totalCacheRead int
	for _, ws := range resp.ByWorkspace {
		totalCost += ws.CostUSD
		totalInput += ws.InputTokens
		totalCacheRead += ws.CacheReadTokens
	}
	// Allow small rounding tolerance from integer division.
	if diff := totalCost - 0.09; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByWorkspace total cost = %v, want 0.09 (no tripling)", totalCost)
	}
	// int(900/3) * 3 = 300 * 3 = 900
	if totalInput != 900 {
		t.Errorf("ByWorkspace total input_tokens = %d, want 900 (no tripling)", totalInput)
	}

	// Each repo should have the same share.
	wantPerRepo := 300 // int(900/3)
	for _, repo := range []string{repoA, repoB, repoC} {
		ws, ok := resp.ByWorkspace[repo]
		if !ok {
			t.Fatalf("ByWorkspace missing %q", repo)
		}
		if ws.InputTokens != wantPerRepo {
			t.Errorf("ByWorkspace[%s].InputTokens = %d, want %d", repo, ws.InputTokens, wantPerRepo)
		}
	}
}

// TestAggregateStats_ByWorkspace_SummaryBreakdown verifies that for done tasks,
// the workspace breakdown from the cached summary is preferred over the live
// task breakdown, and that totals remain consistent.
func TestAggregateStats_ByWorkspace_SummaryBreakdown(t *testing.T) {
	now := time.Now().UTC()

	repoA := "/repo/a"
	repoB := "/repo/b"
	doneID := uuid.New()

	tasks := []store.Task{
		{
			ID:        doneID,
			Status:    store.TaskStatusDone,
			CreatedAt: now,
			Usage: store.TaskUsage{
				InputTokens: 1000,
				CostUSD:     0.10,
			},
			WorktreePaths: map[string]string{
				repoA: "/wt/a",
				repoB: "/wt/b",
			},
			// Live task has 50/50 breakdown, but summary should win (80/20).
			WorkspaceUsageBreakdown: map[string]store.TaskUsage{
				repoA: {InputTokens: 500, CostUSD: 0.05},
				repoB: {InputTokens: 500, CostUSD: 0.05},
			},
		},
	}

	// Summary has an 80/20 breakdown.
	summaryBreakdown := map[string]store.TaskUsage{
		repoA: {InputTokens: 800, CostUSD: 0.08},
		repoB: {InputTokens: 200, CostUSD: 0.02},
	}
	loadSummary := func(id uuid.UUID) (*store.TaskSummary, error) {
		if id == doneID {
			return &store.TaskSummary{
				TaskID:                  doneID,
				Status:                  store.TaskStatusDone,
				TotalCostUSD:            0.10,
				WorkspaceUsageBreakdown: summaryBreakdown,
			}, nil
		}
		return nil, nil
	}

	resp := aggregateStats(tasks, loadSummary)

	// Summary breakdown should win over the live task breakdown.
	wsA := resp.ByWorkspace[repoA]
	if wsA.InputTokens != 800 {
		t.Errorf("ByWorkspace[repoA].InputTokens = %d, want 800 (from summary)", wsA.InputTokens)
	}
	wsB := resp.ByWorkspace[repoB]
	if wsB.InputTokens != 200 {
		t.Errorf("ByWorkspace[repoB].InputTokens = %d, want 200 (from summary)", wsB.InputTokens)
	}

	// Total must not double.
	total := wsA.CostUSD + wsB.CostUSD
	if diff := total - 0.10; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByWorkspace total cost = %v, want 0.10 (no doubling)", total)
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
			ID: uuid.New(), Title: "Task None",
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

// TestAggregateStats_ByFailureCategory verifies that ByFailureCategory correctly
// buckets tasks by their effective failure category.
func TestAggregateStats_ByFailureCategory(t *testing.T) {
	now := time.Now().UTC()

	tasks := []store.Task{
		{
			// Failed task with an active FailureCategory — should appear directly.
			ID:              uuid.New(),
			Status:          store.TaskStatusFailed,
			FailureCategory: store.FailureCategoryTimeout,
			CreatedAt:       now,
			Usage:           store.TaskUsage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.10},
		},
		{
			// Failed task with a different category.
			ID:              uuid.New(),
			Status:          store.TaskStatusFailed,
			FailureCategory: store.FailureCategoryContainerCrash,
			CreatedAt:       now,
			Usage:           store.TaskUsage{InputTokens: 200, OutputTokens: 80, CostUSD: 0.20},
		},
		{
			// Done task with empty FailureCategory but a RetryHistory entry — should
			// backfill from the last RetryRecord.
			ID:              uuid.New(),
			Status:          store.TaskStatusDone,
			FailureCategory: "",
			CreatedAt:       now,
			Usage:           store.TaskUsage{InputTokens: 50, OutputTokens: 20, CostUSD: 0.05},
			RetryHistory: []store.RetryRecord{
				{FailureCategory: store.FailureCategoryTimeout},
			},
		},
		{
			// Done task with no FailureCategory and no RetryHistory — not bucketed.
			ID:        uuid.New(),
			Status:    store.TaskStatusDone,
			CreatedAt: now,
			Usage:     store.TaskUsage{InputTokens: 300, OutputTokens: 100, CostUSD: 0.30},
		},
	}

	resp := aggregateStats(tasks, noSummary)

	// "timeout" bucket: task1 (0.10) + task3 backfill (0.05)
	timeoutStat, ok := resp.ByFailureCategory[store.FailureCategoryTimeout]
	if !ok {
		t.Fatal("ByFailureCategory missing 'timeout'")
	}
	if timeoutStat.Count != 2 {
		t.Errorf("ByFailureCategory[timeout].Count = %d, want 2", timeoutStat.Count)
	}
	wantTimeoutCost := 0.10 + 0.05
	if diff := timeoutStat.CostUSD - wantTimeoutCost; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByFailureCategory[timeout].CostUSD = %v, want %v", timeoutStat.CostUSD, wantTimeoutCost)
	}
	if timeoutStat.InputTokens != 100+50 {
		t.Errorf("ByFailureCategory[timeout].InputTokens = %d, want %d", timeoutStat.InputTokens, 100+50)
	}
	if timeoutStat.OutputTokens != 50+20 {
		t.Errorf("ByFailureCategory[timeout].OutputTokens = %d, want %d", timeoutStat.OutputTokens, 50+20)
	}

	// "container_crash" bucket: task2 only (0.20)
	crashStat, ok := resp.ByFailureCategory[store.FailureCategoryContainerCrash]
	if !ok {
		t.Fatal("ByFailureCategory missing 'container_crash'")
	}
	if crashStat.Count != 1 {
		t.Errorf("ByFailureCategory[container_crash].Count = %d, want 1", crashStat.Count)
	}
	if diff := crashStat.CostUSD - 0.20; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ByFailureCategory[container_crash].CostUSD = %v, want 0.20", crashStat.CostUSD)
	}

	// Done task4 (no category, no retry history) must not create a bucket.
	if len(resp.ByFailureCategory) != 2 {
		t.Errorf("ByFailureCategory len = %d, want 2 (timeout and container_crash only)", len(resp.ByFailureCategory))
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
			UsageBreakdown: map[store.SandboxActivity]store.TaskUsage{
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
			UsageBreakdown: map[store.SandboxActivity]store.TaskUsage{
				"implementation": {InputTokens: 200, OutputTokens: 100, CostUSD: 0.05},
			},
		},
	}

	// Summary for the done task with different ByActivity (e.g. after re-calc).
	summaryByActivity := map[store.SandboxActivity]store.TaskUsage{
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
