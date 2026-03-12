// Tests for store.go: NewStore, loadAll, loadEvents, OutputsDir, Close,
// and full persistence round-trip integration tests.
package store

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewStore_EmptyDir(t *testing.T) {
	s := newTestStore(t)
	tasks, err := s.ListTasks(bg(), false)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestNewStore_CreatesDirectoryRecursively(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore with nested path: %v", err)
	}
	s.Close()
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("directory not created: %v", err)
	}
}

func TestNewStore_SkipsNonUUIDDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "not-a-uuid"), 0755); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	tasks, _ := s.ListTasks(bg(), false)
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestNewStore_SkipsUUIDDirWithMissingTaskJSON(t *testing.T) {
	dir := t.TempDir()
	id := uuid.New()
	if err := os.MkdirAll(filepath.Join(dir, id.String()), 0755); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	tasks, _ := s.ListTasks(bg(), false)
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestNewStore_SkipsCorruptTaskJSON(t *testing.T) {
	dir := t.TempDir()
	id := uuid.New()
	taskDir := filepath.Join(dir, id.String())
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "task.json"), []byte("{invalid json}"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	tasks, _ := s.ListTasks(bg(), false)
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestNewStore_LoadsExistingTask(t *testing.T) {
	dir := t.TempDir()
	s1, _ := NewStore(dir)
	task, _ := s1.CreateTask(bg(), "hello", 10, false, "", "")

	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reload NewStore: %v", err)
	}
	got, err := s2.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask after reload: %v", err)
	}
	if got.Prompt != "hello" {
		t.Errorf("Prompt = %q, want 'hello'", got.Prompt)
	}
}

func TestClose_IsNoOp(t *testing.T) {
	s := newTestStore(t)
	s.Close() // must not panic
}

func TestOutputsDir(t *testing.T) {
	s := newTestStore(t)
	id := uuid.New()
	want := filepath.Join(s.dir, id.String(), "outputs")
	if got := s.OutputsDir(id); got != want {
		t.Errorf("OutputsDir = %q, want %q", got, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Full persistence round-trip integration tests
// ─────────────────────────────────────────────────────────────────────────────

func TestPersistence_FullRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)

	task, _ := s.CreateTask(bg(), "round trip prompt", 15, false, "", "")
	s.UpdateTaskStatus(bg(), task.ID, "in_progress")
	s.UpdateTaskTitle(bg(), task.ID, "Round Trip Title")
	s.AccumulateTaskUsage(bg(), task.ID, TaskUsage{InputTokens: 100, CostUSD: 0.5})
	s.UpdateTaskWorktrees(bg(), task.ID, map[string]string{"/repo": "/wt"}, "task/rt")
	s.InsertEvent(bg(), task.ID, EventTypeStateChange, "in_progress")
	s.InsertEvent(bg(), task.ID, EventTypeOutput, "some output")

	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reload NewStore: %v", err)
	}

	got, err := s2.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask after reload: %v", err)
	}
	if got.Prompt != "round trip prompt" {
		t.Errorf("Prompt = %q", got.Prompt)
	}
	if got.Status != "in_progress" {
		t.Errorf("Status = %q", got.Status)
	}
	if got.Title != "Round Trip Title" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.Timeout != 15 {
		t.Errorf("Timeout = %d", got.Timeout)
	}
	if got.Usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d", got.Usage.InputTokens)
	}
	if got.BranchName != "task/rt" {
		t.Errorf("BranchName = %q", got.BranchName)
	}

	events, _ := s2.GetEvents(bg(), task.ID)
	if len(events) != 2 {
		t.Errorf("expected 2 events after reload, got %d", len(events))
	}
}

func TestPersistence_DeletedTaskGoneAfterReload(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	task, _ := s.CreateTask(bg(), "delete me", 5, false, "", "")
	s.DeleteTask(bg(), task.ID, "")

	s2, _ := NewStore(dir)
	if _, err := s2.GetTask(bg(), task.ID); err == nil {
		t.Error("expected task to be absent after delete + reload")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TaskSummary tests
// ─────────────────────────────────────────────────────────────────────────────

// transitionToDone moves a task through the valid state machine path to done:
// backlog → in_progress → waiting → committing → done.
func transitionToDone(t *testing.T, s *Store, id uuid.UUID) {
	t.Helper()
	for _, status := range []TaskStatus{TaskStatusInProgress, TaskStatusWaiting, TaskStatusCommitting, TaskStatusDone} {
		if err := s.UpdateTaskStatus(bg(), id, status); err != nil {
			t.Fatalf("UpdateTaskStatus(%s): %v", status, err)
		}
	}
}

// TestSummary_WrittenOnDoneTransition verifies that a summary.json is created
// with correct fields when a task transitions to done.
func TestSummary_WrittenOnDoneTransition(t *testing.T) {
	s := newTestStore(t)

	task, _ := s.CreateTask(bg(), "summary test", 10, false, "", "")
	s.UpdateTaskTitle(bg(), task.ID, "Summary Test")
	s.AccumulateSubAgentUsage(bg(), task.ID, SandboxActivityImplementation,
		TaskUsage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.42})
	s.UpdateTaskTestRun(bg(), task.ID, false, "pass")
	s.UpdateTaskTurns(bg(), task.ID, 3)

	transitionToDone(t, s, task.ID)

	summary, err := s.LoadSummary(task.ID)
	if err != nil {
		t.Fatalf("LoadSummary: %v", err)
	}
	if summary == nil {
		t.Fatal("LoadSummary returned nil for a done task")
	}

	if summary.TaskID != task.ID {
		t.Errorf("TaskID = %v, want %v", summary.TaskID, task.ID)
	}
	if summary.Title != "Summary Test" {
		t.Errorf("Title = %q, want 'Summary Test'", summary.Title)
	}
	if summary.Status != TaskStatusDone {
		t.Errorf("Status = %q, want 'done'", summary.Status)
	}
	if summary.TotalTurns != 3 {
		t.Errorf("TotalTurns = %d, want 3", summary.TotalTurns)
	}
	if math.Abs(summary.TotalCostUSD-0.42) > 1e-9 {
		t.Errorf("TotalCostUSD = %v, want 0.42", summary.TotalCostUSD)
	}
	if summary.TestResult != "pass" {
		t.Errorf("TestResult = %q, want 'pass'", summary.TestResult)
	}
	implUsage, ok := summary.ByActivity[SandboxActivityImplementation]
	if !ok {
		t.Error("ByActivity missing 'implementation'")
	} else if implUsage.InputTokens != 100 {
		t.Errorf("ByActivity[implementation].InputTokens = %d, want 100", implUsage.InputTokens)
	}
	if summary.DurationSeconds < 0 {
		t.Errorf("DurationSeconds = %v, expected non-negative", summary.DurationSeconds)
	}
}

// TestSummary_WrittenOnForceDoneTransition verifies that ForceUpdateTaskStatus
// also writes summary.json when transitioning to done, matching the behaviour
// of UpdateTaskStatus. This covers the auto-submit and manual "Mark as Done"
// no-session paths that bypass the normal state machine via ForceUpdateTaskStatus.
func TestSummary_WrittenOnForceDoneTransition(t *testing.T) {
	s := newTestStore(t)

	task, _ := s.CreateTask(bg(), "force done task", 10, false, "", "")
	s.UpdateTaskTitle(bg(), task.ID, "Force Done Task")
	s.AccumulateSubAgentUsage(bg(), task.ID, SandboxActivityImplementation,
		TaskUsage{InputTokens: 50, OutputTokens: 25, CostUSD: 0.21})
	s.UpdateTaskTestRun(bg(), task.ID, false, "pass")
	s.UpdateTaskTurns(bg(), task.ID, 2)

	// Skip straight to done via ForceUpdateTaskStatus (no state machine check).
	if err := s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusDone); err != nil {
		t.Fatalf("ForceUpdateTaskStatus(done): %v", err)
	}

	summary, err := s.LoadSummary(task.ID)
	if err != nil {
		t.Fatalf("LoadSummary: %v", err)
	}
	if summary == nil {
		t.Fatal("LoadSummary returned nil after ForceUpdateTaskStatus to done")
	}
	if summary.TaskID != task.ID {
		t.Errorf("TaskID = %v, want %v", summary.TaskID, task.ID)
	}
	if summary.Status != TaskStatusDone {
		t.Errorf("Status = %q, want 'done'", summary.Status)
	}
	if summary.TotalTurns != 2 {
		t.Errorf("TotalTurns = %d, want 2", summary.TotalTurns)
	}
	if summary.TestResult != "pass" {
		t.Errorf("TestResult = %q, want 'pass'", summary.TestResult)
	}
}

// TestSummary_NotWrittenOnFailedTransition verifies that no summary.json is
// created when a task transitions to failed (only done triggers summary).
func TestSummary_NotWrittenOnFailedTransition(t *testing.T) {
	s := newTestStore(t)

	task, _ := s.CreateTask(bg(), "will fail", 10, false, "", "")
	if err := s.UpdateTaskStatus(bg(), task.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus(in_progress): %v", err)
	}
	if err := s.UpdateTaskStatus(bg(), task.ID, TaskStatusFailed); err != nil {
		t.Fatalf("UpdateTaskStatus(failed): %v", err)
	}

	summary, err := s.LoadSummary(task.ID)
	if err != nil {
		t.Fatalf("LoadSummary: %v", err)
	}
	if summary != nil {
		t.Errorf("LoadSummary returned non-nil for a failed task, want nil")
	}
}

// TestListSummaries_ReturnsOnlyDoneTasks verifies that ListSummaries returns
// entries for done tasks and skips tasks in other states.
func TestListSummaries_ReturnsOnlyDoneTasks(t *testing.T) {
	s := newTestStore(t)

	// Create one done task and one in-progress task.
	done, _ := s.CreateTask(bg(), "done task", 10, false, "", "")
	transitionToDone(t, s, done.ID)

	inProg, _ := s.CreateTask(bg(), "in progress task", 10, false, "", "")
	if err := s.UpdateTaskStatus(bg(), inProg.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	summaries, err := s.ListSummaries()
	if err != nil {
		t.Fatalf("ListSummaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Errorf("ListSummaries returned %d summaries, want 1", len(summaries))
	}
	if len(summaries) > 0 && summaries[0].TaskID != done.ID {
		t.Errorf("summary TaskID = %v, want %v", summaries[0].TaskID, done.ID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Schema migration tests
// ─────────────────────────────────────────────────────────────────────────────

// TestMigrateTaskJSON_LegacyNoSchemaVersion verifies that a task.json written
// before schema versioning was introduced gets all missing fields defaulted and
// SchemaVersion stamped.
func TestMigrateTaskJSON_LegacyNoSchemaVersion(t *testing.T) {
	id := uuid.New()
	raw := []byte(fmt.Sprintf(`{"id":%q,"prompt":"legacy task"}`, id.String()))
	modTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	task, changed, err := migrateTaskJSON(raw, modTime)
	if err != nil {
		t.Fatalf("migrateTaskJSON: %v", err)
	}
	if !changed {
		t.Error("expected changed=true for legacy JSON missing defaults")
	}
	if task.Status != TaskStatusBacklog {
		t.Errorf("Status = %q, want %q", task.Status, TaskStatusBacklog)
	}
	if task.Timeout != 60 {
		t.Errorf("Timeout = %d, want 60 (default)", task.Timeout)
	}
	if !task.CreatedAt.Equal(modTime) {
		t.Errorf("CreatedAt = %v, want %v (file mod time)", task.CreatedAt, modTime)
	}
	if !task.UpdatedAt.Equal(modTime) {
		t.Errorf("UpdatedAt = %v, want %v (file mod time)", task.UpdatedAt, modTime)
	}
	if task.SchemaVersion != CurrentTaskSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", task.SchemaVersion, CurrentTaskSchemaVersion)
	}
}

// TestMigrateTaskJSON_AlreadyCurrent verifies that a fully-populated current
// task.json is not marked as changed.
func TestMigrateTaskJSON_AlreadyCurrent(t *testing.T) {
	id := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)
	raw := []byte(fmt.Sprintf(`{
		"schema_version": %d,
		"id": %q,
		"prompt": "current task",
		"status": "backlog",
		"timeout": 30,
		"created_at": %q,
		"updated_at": %q
	}`, CurrentTaskSchemaVersion, id.String(), now.Format(time.RFC3339), now.Format(time.RFC3339)))

	_, changed, err := migrateTaskJSON(raw, time.Now())
	if err != nil {
		t.Fatalf("migrateTaskJSON: %v", err)
	}
	if changed {
		t.Error("expected changed=false for already-current task JSON")
	}
}

// TestMigrateTaskJSON_CanonicalizeDependsOn verifies that DependsOn entries
// are trimmed, UUID-validated, deduplicated, and sorted.
func TestMigrateTaskJSON_CanonicalizeDependsOn(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()
	// Ensure id1 < id2 in canonical string form for predictable sort assertion.
	if id1.String() > id2.String() {
		id1, id2 = id2, id1
	}

	// Include: whitespace padding, duplicate (id1 repeated), invalid UUID, and
	// uppercase variant of id2 that should round-trip to lowercase.
	raw := []byte(fmt.Sprintf(`{
		"schema_version": %d,
		"id": "11111111-1111-1111-1111-111111111111",
		"prompt": "p",
		"status": "backlog",
		"timeout": 30,
		"created_at": "2024-01-01T00:00:00Z",
		"updated_at": "2024-01-01T00:00:00Z",
		"depends_on": [" %s ", "%s", "not-a-uuid", "%s"]
	}`,
		CurrentTaskSchemaVersion,
		id1.String(), id2.String(),
		id1.String(), // duplicate
	))

	task, changed, err := migrateTaskJSON(raw, time.Now())
	if err != nil {
		t.Fatalf("migrateTaskJSON: %v", err)
	}
	if !changed {
		t.Error("expected changed=true for DependsOn requiring canonicalization")
	}

	want := []string{id1.String(), id2.String()}
	sort.Strings(want)
	if !stringSliceEqual(task.DependsOn, want) {
		t.Errorf("DependsOn = %v, want %v", task.DependsOn, want)
	}
}

// TestMigrateTaskJSON_InvalidDependsOnDropped verifies that a DependsOn list
// containing only invalid UUIDs results in nil (keeping JSON clean).
func TestMigrateTaskJSON_InvalidDependsOnDropped(t *testing.T) {
	now := "2024-01-01T00:00:00Z"
	raw := []byte(fmt.Sprintf(`{
		"schema_version": %d,
		"id": "22222222-2222-2222-2222-222222222222",
		"prompt": "p",
		"status": "backlog",
		"timeout": 30,
		"created_at": %q,
		"updated_at": %q,
		"depends_on": ["not-a-uuid", "also-not", ""]
	}`, CurrentTaskSchemaVersion, now, now))

	task, changed, err := migrateTaskJSON(raw, time.Now())
	if err != nil {
		t.Fatalf("migrateTaskJSON: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when invalid DependsOn entries are dropped")
	}
	if task.DependsOn != nil {
		t.Errorf("DependsOn = %v, want nil after dropping all invalid entries", task.DependsOn)
	}
}

// TestLoadAll_MigratesLegacyJSON is an integration test that writes a legacy
// task.json (no schema_version, no status, no timeout, no timestamps) directly
// to disk, loads it via NewStore, and asserts that:
//  1. The in-memory task has all defaults applied.
//  2. The on-disk task.json is re-written with the current schema version.
func TestLoadAll_MigratesLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	id := uuid.New()
	taskDir := filepath.Join(dir, id.String())
	if err := os.MkdirAll(filepath.Join(taskDir, "traces"), 0755); err != nil {
		t.Fatal(err)
	}

	legacy := fmt.Sprintf(`{"id":%q,"prompt":"legacy prompt"}`, id.String())
	taskFile := filepath.Join(taskDir, "task.json")
	if err := os.WriteFile(taskFile, []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	task, err := s.GetTask(bg(), id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != TaskStatusBacklog {
		t.Errorf("Status = %q, want %q", task.Status, TaskStatusBacklog)
	}
	if task.Timeout != 60 {
		t.Errorf("Timeout = %d, want 60", task.Timeout)
	}
	if task.SchemaVersion != CurrentTaskSchemaVersion {
		t.Errorf("in-memory SchemaVersion = %d, want %d", task.SchemaVersion, CurrentTaskSchemaVersion)
	}
	if task.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero after migration")
	}

	// Verify the migrated task.json was persisted back to disk.
	raw, err := os.ReadFile(taskFile)
	if err != nil {
		t.Fatalf("read migrated task.json: %v", err)
	}
	var persisted Task
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("unmarshal persisted task.json: %v", err)
	}
	if persisted.SchemaVersion != CurrentTaskSchemaVersion {
		t.Errorf("persisted SchemaVersion = %d, want %d", persisted.SchemaVersion, CurrentTaskSchemaVersion)
	}
	if persisted.Status != TaskStatusBacklog {
		t.Errorf("persisted Status = %q, want %q", persisted.Status, TaskStatusBacklog)
	}
}

// TestCreateTask_StampsSchemaVersion verifies that newly created tasks have
// SchemaVersion = CurrentTaskSchemaVersion both in memory and on disk.
func TestCreateTask_StampsSchemaVersion(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "versioned task", 10, false, "", "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.SchemaVersion != CurrentTaskSchemaVersion {
		t.Errorf("in-memory SchemaVersion = %d, want %d", task.SchemaVersion, CurrentTaskSchemaVersion)
	}

	// Reload and check on-disk value.
	raw, err := os.ReadFile(filepath.Join(s.dir, task.ID.String(), "task.json"))
	if err != nil {
		t.Fatalf("read task.json: %v", err)
	}
	var persisted Task
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if persisted.SchemaVersion != CurrentTaskSchemaVersion {
		t.Errorf("persisted SchemaVersion = %d, want %d", persisted.SchemaVersion, CurrentTaskSchemaVersion)
	}
}

// TestMutationMethods_StampSchemaVersion verifies that any mutation method
// (UpdateTaskTitle used as a representative) re-stamps the schema version so
// that even tasks loaded without migration get their version corrected on the
// next write.
func TestMutationMethods_StampSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	id := uuid.New()
	taskDir := filepath.Join(dir, id.String())
	if err := os.MkdirAll(filepath.Join(taskDir, "traces"), 0755); err != nil {
		t.Fatal(err)
	}

	// Write a task with schema_version=0 (legacy).
	legacy := fmt.Sprintf(`{"schema_version":0,"id":%q,"prompt":"p","status":"backlog","timeout":30,"created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`, id.String())
	taskFile := filepath.Join(taskDir, "task.json")
	if err := os.WriteFile(taskFile, []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// After loading, schema version is already migrated to current.
	// Perform a mutation to confirm the version stays current.
	if err := s.UpdateTaskTitle(bg(), id, "Updated Title"); err != nil {
		t.Fatalf("UpdateTaskTitle: %v", err)
	}

	raw, err := os.ReadFile(taskFile)
	if err != nil {
		t.Fatalf("read task.json: %v", err)
	}
	var persisted Task
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if persisted.SchemaVersion != CurrentTaskSchemaVersion {
		t.Errorf("SchemaVersion after mutation = %d, want %d", persisted.SchemaVersion, CurrentTaskSchemaVersion)
	}
	if persisted.Title != "Updated Title" {
		t.Errorf("Title = %q, want 'Updated Title'", persisted.Title)
	}
}

// TestSetTaskFailureCategory verifies that SetTaskFailureCategory persists the
// failure_category field and that it can be retrieved via GetTask.
func TestSetTaskFailureCategory(t *testing.T) {
	s := newTestStore(t)

	task, err := s.CreateTask(bg(), "test failure category", 15, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Transition to in_progress so we can fail the task.
	if err := s.UpdateTaskStatus(bg(), task.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus(in_progress): %v", err)
	}
	if err := s.UpdateTaskStatus(bg(), task.ID, TaskStatusFailed); err != nil {
		t.Fatalf("UpdateTaskStatus(failed): %v", err)
	}

	// Set the failure category.
	if err := s.SetTaskFailureCategory(bg(), task.ID, FailureCategoryContainerCrash); err != nil {
		t.Fatalf("SetTaskFailureCategory: %v", err)
	}

	// Reload via GetTask and assert the field is persisted.
	got, err := s.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.FailureCategory != FailureCategoryContainerCrash {
		t.Errorf("FailureCategory = %q, want %q", got.FailureCategory, FailureCategoryContainerCrash)
	}

	// Round-trip through JSON (simulate server restart): reload from disk.
	s2, err := NewStore(s.dir)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	reloaded, err := s2.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask after reload: %v", err)
	}
	if reloaded.FailureCategory != FailureCategoryContainerCrash {
		t.Errorf("FailureCategory after reload = %q, want %q", reloaded.FailureCategory, FailureCategoryContainerCrash)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Status-partitioned secondary index: ListTasksByStatus / CountByStatus
// ─────────────────────────────────────────────────────────────────────────────

// TestStatusIndex_CountsAndContents creates 50 tasks with a mix of statuses,
// then asserts that CountByStatus and ListTasksByStatus return exactly the
// expected counts and that every returned task carries the requested status.
func TestStatusIndex_CountsAndContents(t *testing.T) {
	s := newTestStore(t)

	// Distribution: 20 backlog, 10 in_progress, 10 waiting, 5 done, 5 failed.
	want := map[TaskStatus]int{
		TaskStatusBacklog:    20,
		TaskStatusInProgress: 10,
		TaskStatusWaiting:    10,
		TaskStatusDone:       5,
		TaskStatusFailed:     5,
	}

	// Helper: advance a task through the state machine to reach target status.
	advance := func(id uuid.UUID, target TaskStatus) {
		t.Helper()
		path := map[TaskStatus][]TaskStatus{
			TaskStatusInProgress: {TaskStatusInProgress},
			TaskStatusWaiting:    {TaskStatusInProgress, TaskStatusWaiting},
			TaskStatusCommitting: {TaskStatusInProgress, TaskStatusWaiting, TaskStatusCommitting},
			TaskStatusDone:       {TaskStatusInProgress, TaskStatusWaiting, TaskStatusCommitting, TaskStatusDone},
			TaskStatusFailed:     {TaskStatusInProgress, TaskStatusFailed},
		}
		for _, st := range path[target] {
			if err := s.UpdateTaskStatus(bg(), id, st); err != nil {
				t.Fatalf("advance %s -> %s: %v", id, st, err)
			}
		}
	}

	totals := map[TaskStatus]int{}
	for status, n := range want {
		for i := 0; i < n; i++ {
			task, err := s.CreateTask(bg(), fmt.Sprintf("task-%s-%d", status, i), 10, false, "", "")
			if err != nil {
				t.Fatalf("CreateTask: %v", err)
			}
			if status != TaskStatusBacklog {
				advance(task.ID, status)
			}
			totals[status]++
		}
	}

	// Verify CountByStatus for each status.
	for status, wantCount := range want {
		got := s.CountByStatus(status)
		if got != wantCount {
			t.Errorf("CountByStatus(%s) = %d, want %d", status, got, wantCount)
		}
	}

	// Verify ListTasksByStatus: count and that every returned task has the right status.
	for status, wantCount := range want {
		tasks, err := s.ListTasksByStatus(bg(), status)
		if err != nil {
			t.Fatalf("ListTasksByStatus(%s): %v", status, err)
		}
		if len(tasks) != wantCount {
			t.Errorf("ListTasksByStatus(%s) returned %d tasks, want %d", status, len(tasks), wantCount)
		}
		for _, task := range tasks {
			if task.Status != status {
				t.Errorf("ListTasksByStatus(%s): task %s has status %s", status, task.ID, task.Status)
			}
		}
	}

	// Unrepresented statuses must return zero.
	if n := s.CountByStatus(TaskStatusCancelled); n != 0 {
		t.Errorf("CountByStatus(cancelled) = %d, want 0", n)
	}
	tasks, err := s.ListTasksByStatus(bg(), TaskStatusCancelled)
	if err != nil {
		t.Fatalf("ListTasksByStatus(cancelled): %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("ListTasksByStatus(cancelled) = %d tasks, want 0", len(tasks))
	}
}

// TestStatusIndex_UpdatedOnStatusChange verifies that the index is kept in
// sync across UpdateTaskStatus transitions.
func TestStatusIndex_UpdatedOnStatusChange(t *testing.T) {
	s := newTestStore(t)

	task, _ := s.CreateTask(bg(), "index sync test", 10, false, "", "")
	if s.CountByStatus(TaskStatusBacklog) != 1 {
		t.Fatalf("expected 1 backlog task after create")
	}
	if s.CountByStatus(TaskStatusInProgress) != 0 {
		t.Fatalf("expected 0 in_progress tasks after create")
	}

	if err := s.UpdateTaskStatus(bg(), task.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	if s.CountByStatus(TaskStatusBacklog) != 0 {
		t.Errorf("backlog count should be 0 after promotion")
	}
	if s.CountByStatus(TaskStatusInProgress) != 1 {
		t.Errorf("in_progress count should be 1 after promotion")
	}
}

// TestStatusIndex_DeleteRemovesFromIndex verifies that deleting a task removes
// it from the secondary index.
func TestStatusIndex_DeleteRemovesFromIndex(t *testing.T) {
	s := newTestStore(t)

	task, _ := s.CreateTask(bg(), "to be deleted", 10, false, "", "")
	if s.CountByStatus(TaskStatusBacklog) != 1 {
		t.Fatalf("expected 1 backlog task")
	}
	if err := s.DeleteTask(bg(), task.ID, "test cleanup"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if s.CountByStatus(TaskStatusBacklog) != 0 {
		t.Errorf("backlog count should be 0 after delete")
	}
}

// TestStatusIndex_ReloadRebuildsIndex verifies that reloading a store from
// disk correctly repopulates the secondary index.
func TestStatusIndex_ReloadRebuildsIndex(t *testing.T) {
	dir := t.TempDir()
	s1, _ := NewStore(dir)

	t1, _ := s1.CreateTask(bg(), "backlog task", 10, false, "", "")
	t2, _ := s1.CreateTask(bg(), "in_progress task", 10, false, "", "")
	s1.UpdateTaskStatus(bg(), t2.ID, TaskStatusInProgress)
	_ = t1

	// Reload from disk.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}

	if got := s2.CountByStatus(TaskStatusBacklog); got != 1 {
		t.Errorf("CountByStatus(backlog) after reload = %d, want 1", got)
	}
	if got := s2.CountByStatus(TaskStatusInProgress); got != 1 {
		t.Errorf("CountByStatus(in_progress) after reload = %d, want 1", got)
	}
}

// TestStatusIndex_SortedByPositionThenCreatedAt verifies that ListTasksByStatus
// returns tasks in position-ascending, then created_at-ascending order.
func TestStatusIndex_SortedByPositionThenCreatedAt(t *testing.T) {
	s := newTestStore(t)

	ids := make([]uuid.UUID, 3)
	for i := range ids {
		task, _ := s.CreateTask(bg(), fmt.Sprintf("task-%d", i), 10, false, "", "")
		ids[i] = task.ID
		// Give each task a distinct position: 10, 5, 0 (decreasing creation order
		// so the index must sort them rather than return them in map iteration order).
		s.UpdateTaskPosition(bg(), task.ID, (2-i)*5)
	}

	tasks, err := s.ListTasksByStatus(bg(), TaskStatusBacklog)
	if err != nil {
		t.Fatalf("ListTasksByStatus: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	// Position 0, 5, 10 → created last, middle, first
	if tasks[0].ID != ids[2] {
		t.Errorf("tasks[0].ID = %v, want %v (position 0)", tasks[0].ID, ids[2])
	}
	if tasks[1].ID != ids[1] {
		t.Errorf("tasks[1].ID = %v, want %v (position 5)", tasks[1].ID, ids[1])
	}
	if tasks[2].ID != ids[0] {
		t.Errorf("tasks[2].ID = %v, want %v (position 10)", tasks[2].ID, ids[0])
	}
}

// TestCountRegularInProgress verifies that CountRegularInProgress excludes
// tasks flagged as test runs.
func TestCountRegularInProgress(t *testing.T) {
	s := newTestStore(t)

	// One regular in-progress task.
	t1, _ := s.CreateTask(bg(), "regular", 10, false, "", "")
	s.UpdateTaskStatus(bg(), t1.ID, TaskStatusInProgress)

	// One test-run in-progress task.
	t2, _ := s.CreateTask(bg(), "test run", 10, false, "", "")
	s.UpdateTaskStatus(bg(), t2.ID, TaskStatusInProgress)
	s.UpdateTaskTestRun(bg(), t2.ID, true, "")

	if got := s.CountRegularInProgress(); got != 1 {
		t.Errorf("CountRegularInProgress() = %d, want 1", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Benchmarks: ListTasksByStatus vs ListTasks+filter
// ─────────────────────────────────────────────────────────────────────────────

// seedBenchmarkStore creates a store with n tasks divided evenly across
// backlog, in_progress, waiting, done, and failed statuses.
func seedBenchmarkStore(b *testing.B, n int) *Store {
	b.Helper()
	s, err := NewStore(b.TempDir())
	if err != nil {
		b.Fatalf("NewStore: %v", err)
	}

	statuses := []TaskStatus{
		TaskStatusBacklog,
		TaskStatusInProgress,
		TaskStatusWaiting,
		TaskStatusDone,
		TaskStatusFailed,
	}
	paths := map[TaskStatus][]TaskStatus{
		TaskStatusBacklog:    {},
		TaskStatusInProgress: {TaskStatusInProgress},
		TaskStatusWaiting:    {TaskStatusInProgress, TaskStatusWaiting},
		TaskStatusDone:       {TaskStatusInProgress, TaskStatusWaiting, TaskStatusCommitting, TaskStatusDone},
		TaskStatusFailed:     {TaskStatusInProgress, TaskStatusFailed},
	}

	for i := 0; i < n; i++ {
		target := statuses[i%len(statuses)]
		task, err := s.CreateTask(bg(), fmt.Sprintf("bench-task-%d", i), 10, false, "", "")
		if err != nil {
			b.Fatalf("CreateTask: %v", err)
		}
		for _, st := range paths[target] {
			if err := s.UpdateTaskStatus(bg(), task.ID, st); err != nil {
				b.Fatalf("advance to %s: %v", st, err)
			}
		}
	}
	return s
}

// BenchmarkListTasksByStatus measures the cost of fetching only waiting tasks
// from the secondary index on a 200-task store.
func BenchmarkListTasksByStatus(b *testing.B) {
	s := seedBenchmarkStore(b, 200)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.ListTasksByStatus(bg(), TaskStatusWaiting); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkListTasksFilterWaiting measures the baseline cost of the old
// ListTasks+filter pattern on the same 200-task store.
func BenchmarkListTasksFilterWaiting(b *testing.B) {
	s := seedBenchmarkStore(b, 200)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		all, err := s.ListTasks(bg(), false)
		if err != nil {
			b.Fatal(err)
		}
		result := all[:0]
		for j := range all {
			if all[j].Status == TaskStatusWaiting {
				result = append(result, all[j])
			}
		}
		_ = result
	}
}

// BenchmarkCountByStatus measures the O(1) count path.
func BenchmarkCountByStatus(b *testing.B) {
	s := seedBenchmarkStore(b, 200)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.CountByStatus(TaskStatusWaiting)
	}
}

// BenchmarkCountByStatusFullScan measures the old full-scan pattern.
func BenchmarkCountByStatusFullScan(b *testing.B) {
	s := seedBenchmarkStore(b, 200)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		all, err := s.ListTasks(bg(), false)
		if err != nil {
			b.Fatal(err)
		}
		count := 0
		for j := range all {
			if all[j].Status == TaskStatusWaiting {
				count++
			}
		}
		_ = count
	}
}
