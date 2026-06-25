package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/prompts"
)

func TestAppendPlanningUsage_RoundtripsRecord(t *testing.T) {
	root := t.TempDir()
	key := AgentSessionGroupKey([]string{"/repo/a"})

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
		SubAgent:             SandboxActivityAgentSession,
	}
	if err := AppendAgentSessionUsage(root, key, want); err != nil {
		t.Fatalf("AppendAgentSessionUsage: %v", err)
	}

	got, err := ReadAgentSessionUsage(root, key, time.Time{})
	if err != nil {
		t.Fatalf("ReadAgentSessionUsage: %v", err)
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
	key := AgentSessionGroupKey([]string{"/repo/never-written"})

	got, err := ReadAgentSessionUsage(root, key, time.Time{})
	if err != nil {
		t.Fatalf("ReadAgentSessionUsage: %v", err)
	}
	if got != nil {
		t.Errorf("want nil slice for missing file, got %v", got)
	}
}

func TestReadPlanningUsage_FiltersBySince(t *testing.T) {
	root := t.TempDir()
	key := AgentSessionGroupKey([]string{"/repo/a"})

	base := time.Now().UTC().Truncate(time.Second)
	records := []TurnUsageRecord{
		{Turn: 1, Timestamp: base.Add(-2 * time.Hour), CostUSD: 0.01},
		{Turn: 2, Timestamp: base.Add(-1 * time.Hour), CostUSD: 0.02},
		{Turn: 3, Timestamp: base, CostUSD: 0.03},
	}
	for _, rec := range records {
		if err := AppendAgentSessionUsage(root, key, rec); err != nil {
			t.Fatalf("AppendAgentSessionUsage(turn=%d): %v", rec.Turn, err)
		}
	}

	// since sits between record 2 and record 3, so only record 3 should survive.
	since := base.Add(-30 * time.Minute)
	got, err := ReadAgentSessionUsage(root, key, since)
	if err != nil {
		t.Fatalf("ReadAgentSessionUsage: %v", err)
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

	dir := AgentSessionUsageDir(root, AgentSessionGroupKey(paths))
	if !strings.HasSuffix(dir, string(filepath.Separator)+want) {
		t.Errorf("dir %q does not end with InstructionsKey %q", dir, want)
	}

	dirSwapped := AgentSessionUsageDir(root, AgentSessionGroupKey(swapped))
	if dir != dirSwapped {
		t.Errorf("key should be order-insensitive: %q vs %q", dir, dirSwapped)
	}
}

func TestAppendPlanningUsage_CreatesDir(t *testing.T) {
	root := t.TempDir()
	key := AgentSessionGroupKey([]string{"/repo/fresh"})

	dir := AgentSessionUsageDir(root, key)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("dir %q should not exist yet (err=%v)", dir, err)
	}

	rec := TurnUsageRecord{Turn: 1, Timestamp: time.Now().UTC(), CostUSD: 0.01}
	if err := AppendAgentSessionUsage(root, key, rec); err != nil {
		t.Fatalf("AppendAgentSessionUsage: %v", err)
	}

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("dir not created: %v", err)
	}
	if _, err := os.Stat(AgentSessionUsagePath(root, key)); err != nil {
		t.Errorf("usage file not created: %v", err)
	}
}

func TestMigrateAgentSessionsDir(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, legacyPlanningDirName)
	newDir := filepath.Join(root, agentSessionsDirName)

	// Seed a legacy planning/ dir with a fingerprint subdir holding state.
	if err := os.MkdirAll(filepath.Join(oldDir, "fp1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "fp1", "usage.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	moved, err := MigrateAgentSessionsDir(root)
	if err != nil {
		t.Fatalf("MigrateAgentSessionsDir: %v", err)
	}
	if !moved {
		t.Fatal("expected moved=true on first migration")
	}
	if _, err := os.Stat(filepath.Join(newDir, "fp1", "usage.jsonl")); err != nil {
		t.Errorf("seeded state should have moved to agent-sessions/: %v", err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("legacy planning/ dir should be gone (err=%v)", err)
	}

	// Idempotent: a second run moves nothing.
	moved2, err := MigrateAgentSessionsDir(root)
	if err != nil {
		t.Fatalf("second MigrateAgentSessionsDir: %v", err)
	}
	if moved2 {
		t.Error("second run should be a no-op (moved=false)")
	}
}

func TestMigrateAgentSessionsDir_NoClobber(t *testing.T) {
	root := t.TempDir()
	// Both layouts present: a stale legacy dir must not overwrite live state.
	if err := os.MkdirAll(filepath.Join(root, legacyPlanningDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	liveMarker := filepath.Join(root, agentSessionsDirName, "live.txt")
	if err := os.MkdirAll(filepath.Dir(liveMarker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(liveMarker, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	moved, err := MigrateAgentSessionsDir(root)
	if err != nil {
		t.Fatalf("MigrateAgentSessionsDir: %v", err)
	}
	if moved {
		t.Error("must not migrate when agent-sessions/ already exists")
	}
	if _, err := os.Stat(liveMarker); err != nil {
		t.Errorf("live agent-sessions/ state must be untouched: %v", err)
	}
}

func TestMigrateAgentSessionsDir_NothingToDo(t *testing.T) {
	root := t.TempDir()
	moved, err := MigrateAgentSessionsDir(root)
	if err != nil {
		t.Fatalf("MigrateAgentSessionsDir: %v", err)
	}
	if moved {
		t.Error("expected no-op when neither layout exists")
	}
}
