package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/prompts"
)

func TestAppendPlanningUsage_RoundtripsRecord(t *testing.T) {
	root := t.TempDir()
	key := PlanningGroupKey([]string{"/repo/a"})

	now := time.Now().UTC().Truncate(time.Second)
	want := TurnUsageRecord{
		Turn:                 1,
		Timestamp:            now,
		InputTokens:          123,
		OutputTokens:         45,
		CacheReadInputTokens: 67,
		CacheCreationTokens:  8,
		CostUSD:              0.0123,
		StopReason:           "end_turn",
		SubAgent:             SandboxActivityPlanning,
	}
	if err := AppendPlanningUsage(root, key, want); err != nil {
		t.Fatalf("AppendPlanningUsage: %v", err)
	}

	got, err := ReadPlanningUsage(root, key, time.Time{})
	if err != nil {
		t.Fatalf("ReadPlanningUsage: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	if got[0] != want {
		t.Errorf("record mismatch\n got: %+v\nwant: %+v", got[0], want)
	}
}

func TestReadPlanningUsage_MissingFileReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	key := PlanningGroupKey([]string{"/repo/never-written"})

	got, err := ReadPlanningUsage(root, key, time.Time{})
	if err != nil {
		t.Fatalf("ReadPlanningUsage: %v", err)
	}
	if got != nil {
		t.Errorf("want nil slice for missing file, got %v", got)
	}
}

func TestReadPlanningUsage_FiltersBySince(t *testing.T) {
	root := t.TempDir()
	key := PlanningGroupKey([]string{"/repo/a"})

	base := time.Now().UTC().Truncate(time.Second)
	records := []TurnUsageRecord{
		{Turn: 1, Timestamp: base.Add(-2 * time.Hour), CostUSD: 0.01},
		{Turn: 2, Timestamp: base.Add(-1 * time.Hour), CostUSD: 0.02},
		{Turn: 3, Timestamp: base, CostUSD: 0.03},
	}
	for _, rec := range records {
		if err := AppendPlanningUsage(root, key, rec); err != nil {
			t.Fatalf("AppendPlanningUsage(turn=%d): %v", rec.Turn, err)
		}
	}

	// since sits between record 2 and record 3, so only record 3 should survive.
	since := base.Add(-30 * time.Minute)
	got, err := ReadPlanningUsage(root, key, since)
	if err != nil {
		t.Fatalf("ReadPlanningUsage: %v", err)
	}
	if len(got) != 1 || got[0].Turn != 3 {
		t.Fatalf("want only turn 3, got %+v", got)
	}
}

func TestPlanningUsageDir_UsesInstructionsKey(t *testing.T) {
	paths := []string{"/repo/a", "/repo/b"}
	// Use an order-swapped list to confirm the key is order-insensitive
	// (matches InstructionsKey which sorts before hashing).
	swapped := []string{"/repo/b", "/repo/a"}

	root := "/tmp/wf-test"
	want := prompts.InstructionsKey(paths)

	dir := PlanningUsageDir(root, PlanningGroupKey(paths))
	if !strings.HasSuffix(dir, string(filepath.Separator)+want) {
		t.Errorf("dir %q does not end with InstructionsKey %q", dir, want)
	}

	dirSwapped := PlanningUsageDir(root, PlanningGroupKey(swapped))
	if dir != dirSwapped {
		t.Errorf("key should be order-insensitive: %q vs %q", dir, dirSwapped)
	}
}

func TestAppendPlanningUsage_CreatesDir(t *testing.T) {
	root := t.TempDir()
	key := PlanningGroupKey([]string{"/repo/fresh"})

	dir := PlanningUsageDir(root, key)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("dir %q should not exist yet (err=%v)", dir, err)
	}

	rec := TurnUsageRecord{Turn: 1, Timestamp: time.Now().UTC(), CostUSD: 0.01}
	if err := AppendPlanningUsage(root, key, rec); err != nil {
		t.Fatalf("AppendPlanningUsage: %v", err)
	}

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("dir not created: %v", err)
	}
	if _, err := os.Stat(PlanningUsagePath(root, key)); err != nil {
		t.Errorf("usage file not created: %v", err)
	}
}
