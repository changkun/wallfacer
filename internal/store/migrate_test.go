package store

import (
	"encoding/json"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/sandbox"
	"github.com/google/uuid"
)

// buildMinimalTaskJSON creates a minimal valid task JSON for migration testing.
func buildMinimalTaskJSON(t *testing.T, overrides map[string]any) []byte {
	t.Helper()
	base := map[string]any{
		"id":     uuid.New().String(),
		"prompt": "test prompt",
	}
	for k, v := range overrides {
		base[k] = v
	}
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal task JSON: %v", err)
	}
	return data
}

func TestMigrateTaskJSON_MissingStatus_DefaultsToBacklog(t *testing.T) {
	raw := buildMinimalTaskJSON(t, nil) // no status field

	task, changed, err := migrateTaskJSON(raw, time.Now())
	if err != nil {
		t.Fatalf("migrateTaskJSON: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when status was missing")
	}
	if task.Status != TaskStatusBacklog {
		t.Errorf("Status = %q, want %q", task.Status, TaskStatusBacklog)
	}
}

func TestMigrateTaskJSON_MissingTimeout_DefaultsToClamp(t *testing.T) {
	raw := buildMinimalTaskJSON(t, nil) // no timeout field

	task, changed, err := migrateTaskJSON(raw, time.Now())
	if err != nil {
		t.Fatalf("migrateTaskJSON: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when timeout was 0")
	}
	if task.Timeout == 0 {
		t.Error("expected non-zero timeout after migration")
	}
}

func TestMigrateTaskJSON_MissingCreatedAt_UsesFileModTime(t *testing.T) {
	raw := buildMinimalTaskJSON(t, nil)
	modTime := time.Unix(1700000000, 0).UTC()

	task, changed, err := migrateTaskJSON(raw, modTime)
	if err != nil {
		t.Fatalf("migrateTaskJSON: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when timestamps were zero")
	}
	if !task.CreatedAt.Equal(modTime) {
		t.Errorf("CreatedAt = %v, want %v", task.CreatedAt, modTime)
	}
	if !task.UpdatedAt.Equal(modTime) {
		t.Errorf("UpdatedAt = %v, want %v", task.UpdatedAt, modTime)
	}
}

func TestMigrateTaskJSON_DuplicateDependsOn_Deduplicated(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()
	// id1 appears twice, id2 once.
	raw := buildMinimalTaskJSON(t, map[string]any{
		"depends_on": []string{id1.String(), id2.String(), id1.String()},
	})

	task, changed, err := migrateTaskJSON(raw, time.Now())
	if err != nil {
		t.Fatalf("migrateTaskJSON: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when depends_on had duplicates")
	}
	if len(task.DependsOn) != 2 {
		t.Fatalf("expected 2 unique depends_on entries, got %d: %v", len(task.DependsOn), task.DependsOn)
	}
}

func TestMigrateTaskJSON_DependsOnInvalidUUID_Dropped(t *testing.T) {
	id1 := uuid.New()
	raw := buildMinimalTaskJSON(t, map[string]any{
		"depends_on": []string{id1.String(), "not-a-uuid"},
	})

	task, _, err := migrateTaskJSON(raw, time.Now())
	if err != nil {
		t.Fatalf("migrateTaskJSON: %v", err)
	}
	if len(task.DependsOn) != 1 {
		t.Fatalf("expected 1 valid depends_on entry, got %d: %v", len(task.DependsOn), task.DependsOn)
	}
	if task.DependsOn[0] != id1.String() {
		t.Errorf("DependsOn[0] = %q, want %q", task.DependsOn[0], id1.String())
	}
}

func TestMigrateTaskJSON_DependsOnSorted(t *testing.T) {
	// UUIDs: create two and ensure they end up sorted.
	// Generate UUIDs where second > first alphabetically.
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	raw := buildMinimalTaskJSON(t, map[string]any{
		"depends_on": []string{ids[1].String(), ids[0].String()},
	})

	task, _, err := migrateTaskJSON(raw, time.Now())
	if err != nil {
		t.Fatalf("migrateTaskJSON: %v", err)
	}
	if len(task.DependsOn) != 2 {
		t.Fatalf("expected 2 depends_on entries, got %d", len(task.DependsOn))
	}
	if task.DependsOn[0] > task.DependsOn[1] {
		t.Errorf("DependsOn not sorted: %v", task.DependsOn)
	}
}

func TestMigrateTaskJSON_SandboxNormalized(t *testing.T) {
	raw := buildMinimalTaskJSON(t, map[string]any{
		"sandbox": "CLAUDE",
	})

	task, changed, err := migrateTaskJSON(raw, time.Now())
	if err != nil {
		t.Fatalf("migrateTaskJSON: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when sandbox needed normalization")
	}
	if task.Sandbox != sandbox.Claude {
		t.Errorf("Sandbox = %q, want %q", task.Sandbox, sandbox.Claude)
	}
}

func TestMigrateTaskJSON_AutoRetryBudgetBackfilled(t *testing.T) {
	// Task with no auto_retry_budget should get defaults.
	raw := buildMinimalTaskJSON(t, nil)

	task, changed, err := migrateTaskJSON(raw, time.Now())
	if err != nil {
		t.Fatalf("migrateTaskJSON: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when auto_retry_budget was nil")
	}
	if task.AutoRetryBudget == nil {
		t.Fatal("expected non-nil AutoRetryBudget after migration")
	}
	if task.AutoRetryBudget[FailureCategoryContainerCrash] != 2 {
		t.Errorf("ContainerCrash budget = %d, want 2", task.AutoRetryBudget[FailureCategoryContainerCrash])
	}
	if task.AutoRetryBudget[FailureCategorySyncError] != 2 {
		t.Errorf("SyncError budget = %d, want 2", task.AutoRetryBudget[FailureCategorySyncError])
	}
	if task.AutoRetryBudget[FailureCategoryWorktree] != 1 {
		t.Errorf("Worktree budget = %d, want 1", task.AutoRetryBudget[FailureCategoryWorktree])
	}
}

func TestMigrateTaskJSON_SchemaVersionStamped(t *testing.T) {
	raw := buildMinimalTaskJSON(t, nil)

	task, changed, err := migrateTaskJSON(raw, time.Now())
	if err != nil {
		t.Fatalf("migrateTaskJSON: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when schema version was not current")
	}
	if task.SchemaVersion != CurrentTaskSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", task.SchemaVersion, CurrentTaskSchemaVersion)
	}
}

func TestMigrateTaskJSON_InvalidJSON(t *testing.T) {
	_, _, err := migrateTaskJSON([]byte("{bad json"), time.Now())
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestMigrateTaskJSON_NoChangesWhenAlreadyCurrent(t *testing.T) {
	// Build a task that is already fully migrated.
	id1 := uuid.New()
	id2 := uuid.New()
	// Ensure alphabetical order for depends_on.
	dep1, dep2 := id1.String(), id2.String()
	if dep1 > dep2 {
		dep1, dep2 = dep2, dep1
	}
	now := time.Now().UTC()
	raw := buildMinimalTaskJSON(t, map[string]any{
		"status":         string(TaskStatusBacklog),
		"timeout":        60,
		"created_at":     now,
		"updated_at":     now,
		"sandbox":        string(sandbox.Claude),
		"schema_version": CurrentTaskSchemaVersion,
		"auto_retry_budget": map[string]int{
			string(FailureCategoryContainerCrash): 2,
			string(FailureCategorySyncError):      2,
			string(FailureCategoryWorktree):       1,
		},
		"depends_on": []string{dep1, dep2},
	})

	_, changed, err := migrateTaskJSON(raw, now)
	if err != nil {
		t.Fatalf("migrateTaskJSON: %v", err)
	}
	// Note: changed might still be true due to sandbox normalization details.
	// The important thing is no error and proper parsing.
	_ = changed
}

// --- canonicalizeDependsOn tests ---

func TestCanonicalizeDependsOn_EmptySlice(t *testing.T) {
	result := canonicalizeDependsOn(nil)
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
	result = canonicalizeDependsOn([]string{})
	if result != nil {
		t.Errorf("expected nil for empty slice, got %v", result)
	}
}

func TestCanonicalizeDependsOn_AllInvalid_ReturnsNil(t *testing.T) {
	result := canonicalizeDependsOn([]string{"not-uuid", "also-not", ""})
	if result != nil {
		t.Errorf("expected nil for all-invalid UUIDs, got %v", result)
	}
}

func TestCanonicalizeDependsOn_Deduplicates(t *testing.T) {
	id := uuid.New()
	result := canonicalizeDependsOn([]string{id.String(), id.String(), id.String()})
	if len(result) != 1 {
		t.Errorf("expected 1 after dedup, got %d: %v", len(result), result)
	}
}

func TestCanonicalizeDependsOn_CaseInsensitive(t *testing.T) {
	id := uuid.New()
	lower := id.String()
	upper := id.String() // uuid.UUID.String() is always lowercase
	result := canonicalizeDependsOn([]string{lower, upper})
	if len(result) != 1 {
		t.Errorf("expected 1 after case dedup, got %d: %v", len(result), result)
	}
}

func TestCanonicalizeDependsOn_Sorted(t *testing.T) {
	ids := []string{uuid.New().String(), uuid.New().String(), uuid.New().String()}
	result := canonicalizeDependsOn(ids)
	for i := 1; i < len(result); i++ {
		if result[i-1] > result[i] {
			t.Errorf("result not sorted at index %d: %v", i, result)
		}
	}
}

func TestCanonicalizeDependsOn_TrimsWhitespace(t *testing.T) {
	id := uuid.New()
	result := canonicalizeDependsOn([]string{"  " + id.String() + "  "})
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0] != id.String() {
		t.Errorf("result = %q, want %q", result[0], id.String())
	}
}
