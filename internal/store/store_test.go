// Tests for store.go: NewStore, loadAll, loadEvents, OutputsDir, Close,
// and full persistence round-trip integration tests.
package store

import (
	"math"
	"os"
	"path/filepath"
	"testing"

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
	s.DeleteTask(bg(), task.ID)

	s2, _ := NewStore(dir)
	if _, err := s2.GetTask(bg(), task.ID); err == nil {
		t.Error("expected task to be absent after delete + reload")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TaskSummary tests
// ─────────────────────────────────────────────────────────────────────────────

// transitionToDone moves a task through the valid state machine path to done:
// backlog → in_progress → committing → done.
func transitionToDone(t *testing.T, s *Store, id uuid.UUID) {
	t.Helper()
	for _, status := range []TaskStatus{TaskStatusInProgress, TaskStatusCommitting, TaskStatusDone} {
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
