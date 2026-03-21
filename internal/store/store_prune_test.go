// Tests for payload pruning: pruneTaskPayload, saveTask disk-only truncation,
// and the loadAll load-time migration path.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestPruneTaskPayload verifies the full pruning contract:
//  1. After saveTask the on-disk slices are ≤ the configured limits.
//  2. The most-recent (tail) entries are retained on disk.
//  3. The in-memory task returned by GetTask still holds the full slices.
//  4. Reloading the store (NewStore on the same dir) yields pruned counts
//     because it reads the already-pruned on-disk file.
func TestPruneTaskPayload(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Create a task so the on-disk directory exists.
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "prune test", Timeout: 10})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// ── Step 1: populate slices beyond the default limits ──────────────────
	now := time.Now()

	s.mu.Lock()
	tp := s.tasks[task.ID]

	// 15 RetryHistory entries (limit = DefaultRetryHistoryLimit = 10).
	for i := 0; i < 15; i++ {
		tp.RetryHistory = append(tp.RetryHistory, RetryRecord{
			RetiredAt: now.Add(time.Duration(i) * time.Second),
			Prompt:    fmt.Sprintf("retry-%d", i),
			Status:    TaskStatusFailed,
		})
	}

	// 8 RefineSessions entries (limit = DefaultRefineSessionsLimit = 5).
	for i := 0; i < 8; i++ {
		tp.RefineSessions = append(tp.RefineSessions, RefinementSession{
			ID:        fmt.Sprintf("session-%d", i),
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		})
	}

	// 25 PromptHistory entries (limit = DefaultPromptHistoryLimit = 20).
	for i := 0; i < 25; i++ {
		tp.PromptHistory = append(tp.PromptHistory, fmt.Sprintf("prompt-%d", i))
	}

	// ── Step 2: saveTask – only disk is pruned ──────────────────────────────
	if err := s.saveTask(task.ID, tp); err != nil {
		s.mu.Unlock()
		t.Fatalf("saveTask: %v", err)
	}
	s.mu.Unlock()

	// ── Step 3: verify on-disk counts ≤ limits ─────────────────────────────
	taskPath := filepath.Join(s.dir, task.ID.String(), "task.json")
	raw, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatalf("ReadFile task.json: %v", err)
	}
	var diskTask Task
	if err := json.Unmarshal(raw, &diskTask); err != nil {
		t.Fatalf("Unmarshal task.json: %v", err)
	}

	if got := len(diskTask.RetryHistory); got > DefaultRetryHistoryLimit {
		t.Errorf("disk RetryHistory len = %d, want ≤ %d", got, DefaultRetryHistoryLimit)
	}
	if got := len(diskTask.RefineSessions); got > DefaultRefineSessionsLimit {
		t.Errorf("disk RefineSessions len = %d, want ≤ %d", got, DefaultRefineSessionsLimit)
	}
	if got := len(diskTask.PromptHistory); got > DefaultPromptHistoryLimit {
		t.Errorf("disk PromptHistory len = %d, want ≤ %d", got, DefaultPromptHistoryLimit)
	}

	// ── Step 4: verify most-recent entries are retained (tail of each slice) ─
	if len(diskTask.RetryHistory) == DefaultRetryHistoryLimit {
		// The oldest kept entry should be index (15-10)=5, the newest index 14.
		first := diskTask.RetryHistory[0].Prompt
		last := diskTask.RetryHistory[DefaultRetryHistoryLimit-1].Prompt
		wantFirst := fmt.Sprintf("retry-%d", 15-DefaultRetryHistoryLimit)
		wantLast := "retry-14"
		if first != wantFirst {
			t.Errorf("disk RetryHistory[0].Prompt = %q, want %q (oldest retained)", first, wantFirst)
		}
		if last != wantLast {
			t.Errorf("disk RetryHistory[last].Prompt = %q, want %q (most recent)", last, wantLast)
		}
	}

	if len(diskTask.PromptHistory) == DefaultPromptHistoryLimit {
		last := diskTask.PromptHistory[DefaultPromptHistoryLimit-1]
		wantLast := "prompt-24"
		if last != wantLast {
			t.Errorf("disk PromptHistory[last] = %q, want %q (most recent)", last, wantLast)
		}
	}

	// ── Step 5: GetTask returns in-memory task with full 15/8/25 entries ────
	got, err := s.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if n := len(got.RetryHistory); n != 15 {
		t.Errorf("in-memory RetryHistory len = %d, want 15 (disk-only pruning)", n)
	}
	if n := len(got.RefineSessions); n != 8 {
		t.Errorf("in-memory RefineSessions len = %d, want 8 (disk-only pruning)", n)
	}
	if n := len(got.PromptHistory); n != 25 {
		t.Errorf("in-memory PromptHistory len = %d, want 25 (disk-only pruning)", n)
	}

	// ── Step 6: reload from same dir – loaded task has pruned counts ─────────
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	reloaded, err := s2.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask after reload: %v", err)
	}
	if n := len(reloaded.RetryHistory); n > DefaultRetryHistoryLimit {
		t.Errorf("reloaded RetryHistory len = %d, want ≤ %d", n, DefaultRetryHistoryLimit)
	}
	if n := len(reloaded.RefineSessions); n > DefaultRefineSessionsLimit {
		t.Errorf("reloaded RefineSessions len = %d, want ≤ %d", n, DefaultRefineSessionsLimit)
	}
	if n := len(reloaded.PromptHistory); n > DefaultPromptHistoryLimit {
		t.Errorf("reloaded PromptHistory len = %d, want ≤ %d", n, DefaultPromptHistoryLimit)
	}
}

// TestPruneTaskPayload_LoadTimeMigration verifies that when a task.json on disk
// already has oversized slices (written before the limits were introduced),
// loadAll prunes the in-memory task immediately on startup.
func TestPruneTaskPayload_LoadTimeMigration(t *testing.T) {
	dir := t.TempDir()
	id := uuid.New()
	taskDir := filepath.Join(dir, id.String())
	if err := os.MkdirAll(filepath.Join(taskDir, "traces"), 0755); err != nil {
		t.Fatal(err)
	}

	// Write a task.json with slices that exceed all three default limits.
	overLimit := Task{
		SchemaVersion: CurrentTaskSchemaVersion,
		ID:            id,
		Prompt:        "migration test",
		Status:        TaskStatusBacklog,
		Timeout:       60,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	now := time.Now()
	for i := 0; i < 15; i++ {
		overLimit.RetryHistory = append(overLimit.RetryHistory, RetryRecord{
			RetiredAt: now.Add(time.Duration(i) * time.Second),
			Prompt:    fmt.Sprintf("retry-%d", i),
			Status:    TaskStatusFailed,
		})
	}
	for i := 0; i < 8; i++ {
		overLimit.RefineSessions = append(overLimit.RefineSessions, RefinementSession{
			ID:        fmt.Sprintf("session-%d", i),
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		})
	}
	for i := 0; i < 25; i++ {
		overLimit.PromptHistory = append(overLimit.PromptHistory, fmt.Sprintf("prompt-%d", i))
	}

	raw, err := json.MarshalIndent(overLimit, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "task.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}

	// Load the store – loadAll should prune the in-memory task.
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	loaded, err := s.GetTask(bg(), id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if n := len(loaded.RetryHistory); n > DefaultRetryHistoryLimit {
		t.Errorf("load-time RetryHistory len = %d, want ≤ %d", n, DefaultRetryHistoryLimit)
	}
	if n := len(loaded.RefineSessions); n > DefaultRefineSessionsLimit {
		t.Errorf("load-time RefineSessions len = %d, want ≤ %d", n, DefaultRefineSessionsLimit)
	}
	if n := len(loaded.PromptHistory); n > DefaultPromptHistoryLimit {
		t.Errorf("load-time PromptHistory len = %d, want ≤ %d", n, DefaultPromptHistoryLimit)
	}
}

// TestPruneTaskPayload_ZeroLimitDisablesPruning verifies that setting a limit
// to 0 via WALLFACER_*_LIMIT leaves the slice untouched on disk.
func TestPruneTaskPayload_ZeroLimitDisablesPruning(t *testing.T) {
	t.Setenv("WALLFACER_RETRY_HISTORY_LIMIT", "0")

	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "no-prune test", Timeout: 10})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	now := time.Now()
	s.mu.Lock()
	tp := s.tasks[task.ID]
	for i := 0; i < 15; i++ {
		tp.RetryHistory = append(tp.RetryHistory, RetryRecord{
			RetiredAt: now.Add(time.Duration(i) * time.Second),
			Prompt:    fmt.Sprintf("retry-%d", i),
			Status:    TaskStatusFailed,
		})
	}
	if err := s.saveTask(task.ID, tp); err != nil {
		s.mu.Unlock()
		t.Fatalf("saveTask: %v", err)
	}
	s.mu.Unlock()

	// Disk should still have all 15 entries since limit=0 disables pruning.
	raw, err := os.ReadFile(filepath.Join(s.dir, task.ID.String(), "task.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var diskTask Task
	if err := json.Unmarshal(raw, &diskTask); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if n := len(diskTask.RetryHistory); n != 15 {
		t.Errorf("disk RetryHistory len = %d, want 15 (no pruning when limit=0)", n)
	}
}

// TestGetPayloadLimits verifies that NewStore reads the default limits when no
// environment overrides are set.
func TestGetPayloadLimits(t *testing.T) {
	s := newTestStore(t)
	limits := s.GetPayloadLimits()
	if limits.RetryHistory != DefaultRetryHistoryLimit {
		t.Errorf("RetryHistory limit = %d, want %d", limits.RetryHistory, DefaultRetryHistoryLimit)
	}
	if limits.RefineSessions != DefaultRefineSessionsLimit {
		t.Errorf("RefineSessions limit = %d, want %d", limits.RefineSessions, DefaultRefineSessionsLimit)
	}
	if limits.PromptHistory != DefaultPromptHistoryLimit {
		t.Errorf("PromptHistory limit = %d, want %d", limits.PromptHistory, DefaultPromptHistoryLimit)
	}
}

// TestGetPayloadLimits_EnvOverride verifies that WALLFACER_*_LIMIT env vars
// are picked up when creating a new store.
func TestGetPayloadLimits_EnvOverride(t *testing.T) {
	t.Setenv("WALLFACER_RETRY_HISTORY_LIMIT", "3")
	t.Setenv("WALLFACER_REFINE_SESSIONS_LIMIT", "2")
	t.Setenv("WALLFACER_PROMPT_HISTORY_LIMIT", "7")

	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	limits := s.GetPayloadLimits()
	if limits.RetryHistory != 3 {
		t.Errorf("RetryHistory limit = %d, want 3", limits.RetryHistory)
	}
	if limits.RefineSessions != 2 {
		t.Errorf("RefineSessions limit = %d, want 2", limits.RefineSessions)
	}
	if limits.PromptHistory != 7 {
		t.Errorf("PromptHistory limit = %d, want 7", limits.PromptHistory)
	}
}

// TestGetPayloadLimits_InvalidEnvFallsBackToDefault verifies that a non-integer
// env value is silently ignored and the default is used instead.
func TestGetPayloadLimits_InvalidEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("WALLFACER_RETRY_HISTORY_LIMIT", "not-a-number")

	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	limits := s.GetPayloadLimits()
	if limits.RetryHistory != DefaultRetryHistoryLimit {
		t.Errorf("RetryHistory limit = %d, want default %d for invalid env value", limits.RetryHistory, DefaultRetryHistoryLimit)
	}
}

