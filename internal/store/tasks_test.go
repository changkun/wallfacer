// Tests for tasks.go: all task CRUD operations and clampTimeout.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"sync"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/sandbox"
	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────────────────────
// clampTimeout
// ─────────────────────────────────────────────────────────────────────────────

func TestClampTimeout(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 60},
		{-1, 60},
		{-999, 60},
		{1, 1},
		{5, 5},
		{720, 720},
		{1440, 1440},
		{1441, 1440},
		{9999, 1440},
	}
	for _, tc := range cases {
		if got := clampTimeout(tc.in); got != tc.want {
			t.Errorf("clampTimeout(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CreateTask
// ─────────────────────────────────────────────────────────────────────────────

func TestCreateTask_Basic(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "my prompt", Timeout: 10})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.ID == (uuid.UUID{}) {
		t.Error("expected non-zero task ID")
	}
	if task.Prompt != "my prompt" {
		t.Errorf("Prompt = %q, want 'my prompt'", task.Prompt)
	}
	if task.Status != TaskStatusBacklog {
		t.Errorf("Status = %q, want 'backlog'", task.Status)
	}
	if task.Timeout != 10 {
		t.Errorf("Timeout = %d, want 10", task.Timeout)
	}
	if task.Turns != 0 {
		t.Errorf("Turns = %d, want 0", task.Turns)
	}
	if task.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestCreateTask_PositionIncrements(t *testing.T) {
	s := newTestStore(t)
	t1, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "first", Timeout: 5})
	t2, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "second", Timeout: 5})
	t3, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "third", Timeout: 5})
	// Each newer task should have a strictly lower position so it sorts to the top.
	if t2.Position >= t1.Position {
		t.Errorf("t2.Position = %d should be less than t1.Position = %d", t2.Position, t1.Position)
	}
	if t3.Position >= t2.Position {
		t.Errorf("t3.Position = %d should be less than t2.Position = %d", t3.Position, t2.Position)
	}
}

func TestCreateTask_TimeoutClampedDefault(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 0})
	if task.Timeout != 60 {
		t.Errorf("expected default timeout 60, got %d", task.Timeout)
	}
}

func TestCreateTask_TimeoutClampedMax(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 9999})
	if task.Timeout != 1440 {
		t.Errorf("expected clamped timeout 1440, got %d", task.Timeout)
	}
}

func TestCreateTask_PersistsToDisk(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "persist me", Timeout: 5})

	s2, _ := NewStore(dir)
	got, err := s2.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask after reload: %v", err)
	}
	if got.Prompt != "persist me" {
		t.Errorf("reloaded Prompt = %q, want 'persist me'", got.Prompt)
	}
}

func TestCreateTask_PositionOnlyCountsBacklog(t *testing.T) {
	s := newTestStore(t)
	t1, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "a", Timeout: 5})
	_ = s.ForceUpdateTaskStatus(bg(), t1.ID, TaskStatusDone)
	t2, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "b", Timeout: 5})
	// No backlog tasks exist, so maxPos = -1 and t2 gets position 0.
	if t2.Position != 0 {
		t.Errorf("expected position 0 when no backlog tasks exist, got %d", t2.Position)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetTask
// ─────────────────────────────────────────────────────────────────────────────

func TestGetTask_NotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.GetTask(bg(), uuid.New()); err == nil {
		t.Error("expected error for unknown task ID")
	}
}

func TestGetTask_ReturnsCopy(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "original", Timeout: 5})

	got, _ := s.GetTask(bg(), task.ID)
	got.Prompt = "mutated"

	got2, _ := s.GetTask(bg(), task.ID)
	if got2.Prompt != "original" {
		t.Errorf("GetTask returned a reference, not a copy (prompt changed to %q)", got2.Prompt)
	}
}

func TestGetTask_ReturnsDeepCloneForNestedFields(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "original", Timeout: 5})

	s.mu.Lock()
	want := setTaskCloneFixture(t, s.tasks[task.ID])
	s.mu.Unlock()

	got, err := s.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	mutateTaskCloneForIsolation(got)

	got2, err := s.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask after mutation: %v", err)
	}
	assertTaskMatchesSnapshot(t, got2, want)
}

// ─────────────────────────────────────────────────────────────────────────────
// ListTasks
// ─────────────────────────────────────────────────────────────────────────────

func TestListTasks_SortedByPosition(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "a", Timeout: 5})
	_, _ = s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "b", Timeout: 5})
	_, _ = s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "c", Timeout: 5})

	tasks, _ := s.ListTasks(bg(), false)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	for i := 0; i < len(tasks)-1; i++ {
		if tasks[i].Position > tasks[i+1].Position {
			t.Errorf("tasks not sorted at index %d: pos %d > %d", i, tasks[i].Position, tasks[i+1].Position)
		}
	}
}

func TestListTasks_SamePositionSortedByCreatedAt(t *testing.T) {
	s := newTestStore(t)
	t1, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "first", Timeout: 5})
	t2, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "second", Timeout: 5})

	// Force both to the same position.
	_ = s.UpdateTaskPosition(bg(), t1.ID, 10)
	_ = s.UpdateTaskPosition(bg(), t2.ID, 10)

	tasks, _ := s.ListTasks(bg(), false)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].ID != t1.ID {
		t.Errorf("expected t1 first (created earlier), got %s", tasks[0].ID)
	}
}

func TestListTasks_ExcludesArchivedByDefault(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "archive me", Timeout: 5})
	_ = s.SetTaskArchived(bg(), task.ID, true)

	visible, _ := s.ListTasks(bg(), false)
	if len(visible) != 0 {
		t.Errorf("expected 0 visible tasks, got %d", len(visible))
	}
}

func TestListTasks_IncludesArchivedWhenRequested(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "archive me", Timeout: 5})
	_ = s.SetTaskArchived(bg(), task.ID, true)

	all, _ := s.ListTasks(bg(), true)
	if len(all) != 1 {
		t.Errorf("expected 1 archived task, got %d", len(all))
	}
}

func TestListTasks_ReturnsDeepCloneForNestedFields(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "listed", Timeout: 5})

	s.mu.Lock()
	want := setTaskCloneFixture(t, s.tasks[task.ID])
	s.mu.Unlock()

	tasks, err := s.ListTasks(bg(), false)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	mutateTaskCloneForIsolation(&tasks[0])

	got, err := s.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask after ListTasks mutation: %v", err)
	}
	assertTaskMatchesSnapshot(t, got, want)
}

func TestListTasksAndSeq_ReturnsDeepCloneForNestedFields(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "listed", Timeout: 5})

	s.mu.Lock()
	want := setTaskCloneFixture(t, s.tasks[task.ID])
	s.mu.Unlock()

	tasks, _, err := s.ListTasksAndSeq(bg(), false)
	if err != nil {
		t.Fatalf("ListTasksAndSeq: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	mutateTaskCloneForIsolation(&tasks[0])

	got, err := s.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask after list mutation: %v", err)
	}
	assertTaskMatchesSnapshot(t, got, want)
}

func TestListDeletedTasks_ReturnsDeepCloneForNestedFields(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "deleted", Timeout: 5})

	s.mu.Lock()
	_ = setTaskCloneFixture(t, s.tasks[task.ID])
	s.mu.Unlock()

	if err := s.DeleteTask(bg(), task.ID, "test"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	s.mu.RLock()
	want := deepCloneTask(s.deleted[task.ID])
	s.mu.RUnlock()

	deleted, err := s.ListDeletedTasks(bg())
	if err != nil {
		t.Fatalf("ListDeletedTasks: %v", err)
	}
	if len(deleted) != 1 {
		t.Fatalf("expected 1 deleted task, got %d", len(deleted))
	}
	mutateTaskCloneForIsolation(&deleted[0])

	deleted2, err := s.ListDeletedTasks(bg())
	if err != nil {
		t.Fatalf("ListDeletedTasks after mutation: %v", err)
	}
	if len(deleted2) != 1 {
		t.Fatalf("expected 1 deleted task on reread, got %d", len(deleted2))
	}
	assertTaskMatchesSnapshot(t, &deleted2[0], want)
}

// ─────────────────────────────────────────────────────────────────────────────
// DeleteTask
// ─────────────────────────────────────────────────────────────────────────────

func TestDeleteTask_Basic(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "delete me", Timeout: 5})

	if err := s.DeleteTask(bg(), task.ID, ""); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	// Soft-deleted task is no longer accessible via GetTask.
	if _, err := s.GetTask(bg(), task.ID); err == nil {
		t.Error("expected task-not-found error after soft delete")
	}
}

func TestDeleteTask_RetainsDiskData(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "delete me", Timeout: 5})
	taskDir := dir + "/" + task.ID.String()

	_ = s.DeleteTask(bg(), task.ID, "")

	// Soft delete keeps the directory on disk.
	if _, err := os.Stat(taskDir); os.IsNotExist(err) {
		t.Error("task directory was removed by soft delete; expected it to be retained")
	}
	// tombstone.json must be present.
	if _, err := os.Stat(filepath.Join(taskDir, "tombstone.json")); os.IsNotExist(err) {
		t.Error("tombstone.json not created by soft delete")
	}
}

func TestDeleteTask_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.DeleteTask(bg(), uuid.New(), ""); err == nil {
		t.Error("expected error deleting unknown task")
	}
}

func TestDeleteTask_PreservesEvents(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})
	_ = s.InsertEvent(bg(), task.ID, "state_change", "test")
	_ = s.DeleteTask(bg(), task.ID, "")

	// Events must remain accessible for tombstoned tasks.
	events, _ := s.GetEvents(bg(), task.ID)
	if len(events) == 0 {
		t.Error("expected events to be preserved after soft delete")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateTaskStatus
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateTaskStatus(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	if err := s.UpdateTaskStatus(bg(), task.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.Status != TaskStatusInProgress {
		t.Errorf("Status = %q, want 'in_progress'", got.Status)
	}
}

func TestUpdateTaskStatus_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTaskStatus(bg(), uuid.New(), TaskStatusDone); err == nil {
		t.Error("expected error for unknown task")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ValidateTransition / state machine enforcement
// ─────────────────────────────────────────────────────────────────────────────

func TestValidateTransition_ValidTransitions(t *testing.T) {
	for from, tos := range allowedTransitions {
		for _, to := range tos {
			if err := ValidateTransition(from, to); err != nil {
				t.Errorf("ValidateTransition(%q, %q) = %v, want nil", from, to, err)
			}
		}
	}
}

func TestValidateTransition_InvalidTransitions(t *testing.T) {
	cases := []struct{ from, to TaskStatus }{
		{TaskStatusDone, TaskStatusInProgress},
		{TaskStatusCommitting, TaskStatusBacklog},
		{TaskStatusBacklog, TaskStatusDone},
		{TaskStatusBacklog, TaskStatusFailed},
		{TaskStatusDone, TaskStatusWaiting},
		{TaskStatusCancelled, TaskStatusInProgress},
	}
	for _, tc := range cases {
		err := ValidateTransition(tc.from, tc.to)
		if err == nil {
			t.Errorf("ValidateTransition(%q, %q) = nil, want error", tc.from, tc.to)
			continue
		}
		if !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("error = %v, want wrapping ErrInvalidTransition", err)
		}
	}
}

func TestUpdateTaskStatus_RejectsInvalidTransition(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	err := s.UpdateTaskStatus(bg(), task.ID, TaskStatusDone)
	if err == nil {
		t.Fatal("expected error for invalid transition backlog → done")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("error = %v, want ErrInvalidTransition", err)
	}
}

func TestUpdateTaskStatus_AllowsValidTransitions(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	// backlog → in_progress → waiting → in_progress → waiting → committing → done
	steps := []TaskStatus{
		TaskStatusInProgress,
		TaskStatusWaiting,
		TaskStatusInProgress,
		TaskStatusWaiting,
		TaskStatusCommitting,
		TaskStatusDone,
	}
	for _, next := range steps {
		if err := s.UpdateTaskStatus(bg(), task.ID, next); err != nil {
			t.Fatalf("UpdateTaskStatus(→%q): %v", next, err)
		}
		got, _ := s.GetTask(bg(), task.ID)
		if got.Status != next {
			t.Errorf("after transition to %q: status = %q", next, got.Status)
		}
	}
}

func TestForceUpdateTaskStatus_BypassesValidation(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	// backlog → done is invalid per the state machine
	if err := s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusDone); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.Status != TaskStatusDone {
		t.Errorf("Status = %q, want 'done'", got.Status)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateTaskTitle
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateTaskTitle(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	if err := s.UpdateTaskTitle(bg(), task.ID, "New Title"); err != nil {
		t.Fatalf("UpdateTaskTitle: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.Title != "New Title" {
		t.Errorf("Title = %q, want 'New Title'", got.Title)
	}
}

func TestUpdateTaskTitle_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTaskTitle(bg(), uuid.New(), "t"); err == nil {
		t.Error("expected error for unknown task")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateTaskResult
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateTaskResult(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	err := s.UpdateTaskResult(bg(), task.ID, "the output", "sess-xyz", "end_turn", 3)
	if err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.Result == nil || *got.Result != "the output" {
		t.Errorf("Result = %v, want 'the output'", got.Result)
	}
	if got.SessionID == nil || *got.SessionID != "sess-xyz" {
		t.Errorf("SessionID = %v, want 'sess-xyz'", got.SessionID)
	}
	if got.StopReason == nil || *got.StopReason != "end_turn" {
		t.Errorf("StopReason = %v, want 'end_turn'", got.StopReason)
	}
	if got.Turns != 3 {
		t.Errorf("Turns = %d, want 3", got.Turns)
	}
}

func TestUpdateTaskResult_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTaskResult(bg(), uuid.New(), "", "", "", 0); err == nil {
		t.Error("expected error for unknown task")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateTaskTurns
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateTaskTurns_OnlyUpdatesTurns(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	// Store an implementation result first.
	if err := s.UpdateTaskResult(bg(), task.ID, "impl output", "impl-sess", "end_turn", 3); err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}

	// UpdateTaskTurns should only change Turns.
	if err := s.UpdateTaskTurns(bg(), task.ID, 7); err != nil {
		t.Fatalf("UpdateTaskTurns: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.Turns != 7 {
		t.Errorf("Turns = %d, want 7", got.Turns)
	}
	// Result must not be overwritten.
	if got.Result == nil || *got.Result != "impl output" {
		t.Errorf("Result = %v, want 'impl output'", got.Result)
	}
	// SessionID must not be overwritten.
	if got.SessionID == nil || *got.SessionID != "impl-sess" {
		t.Errorf("SessionID = %v, want 'impl-sess'", got.SessionID)
	}
	// StopReason must not be overwritten.
	if got.StopReason == nil || *got.StopReason != "end_turn" {
		t.Errorf("StopReason = %v, want 'end_turn'", got.StopReason)
	}
}

func TestUpdateTaskTurns_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTaskTurns(bg(), uuid.New(), 0); err == nil {
		t.Error("expected error for unknown task")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AccumulateTaskUsage
// ─────────────────────────────────────────────────────────────────────────────

func TestAccumulateTaskUsage(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	delta := TaskUsage{
		InputTokens:          100,
		OutputTokens:         50,
		CacheReadInputTokens: 10,
		CacheCreationTokens:  5,
		CostUSD:              0.01,
	}
	_ = s.AccumulateTaskUsage(bg(), task.ID, delta)
	_ = s.AccumulateTaskUsage(bg(), task.ID, delta)

	got, _ := s.GetTask(bg(), task.ID)
	if got.Usage.InputTokens != 200 {
		t.Errorf("InputTokens = %d, want 200", got.Usage.InputTokens)
	}
	if got.Usage.OutputTokens != 100 {
		t.Errorf("OutputTokens = %d, want 100", got.Usage.OutputTokens)
	}
	if got.Usage.CacheReadInputTokens != 20 {
		t.Errorf("CacheReadInputTokens = %d, want 20", got.Usage.CacheReadInputTokens)
	}
	if got.Usage.CacheCreationTokens != 10 {
		t.Errorf("CacheCreationTokens = %d, want 10", got.Usage.CacheCreationTokens)
	}
	if got.Usage.CostUSD < 0.019 || got.Usage.CostUSD > 0.021 {
		t.Errorf("CostUSD = %f, want ~0.02", got.Usage.CostUSD)
	}
}

func TestAccumulateTaskUsage_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.AccumulateTaskUsage(bg(), uuid.New(), TaskUsage{}); err == nil {
		t.Error("expected error for unknown task")
	}
}

func TestUpdateTaskExecutionPrompt(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	if err := s.UpdateTaskExecutionPrompt(bg(), task.ID, "implementation prompt"); err != nil {
		t.Fatalf("UpdateTaskExecutionPrompt: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.ExecutionPrompt != "implementation prompt" {
		t.Errorf("ExecutionPrompt = %q, want %q", got.ExecutionPrompt, "implementation prompt")
	}
}

func TestUpdateTaskSandboxByActivity_NormalizesAndClears(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	updates := map[SandboxActivity]sandbox.Type{
		"implementation": sandbox.Type("CLAUDE"),
		"Testing":        sandbox.Type("Codex "),
		"invalid":        "x",
		"oversight":      "",
	}

	if err := s.UpdateTaskSandboxByActivity(bg(), task.ID, updates); err != nil {
		t.Fatalf("UpdateTaskSandboxByActivity: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.SandboxByActivity[SandboxActivityImplementation] != "claude" {
		t.Fatalf("expected implementation sandbox 'claude', got %#v", got.SandboxByActivity)
	}
	if got.SandboxByActivity[SandboxActivityTesting] != "codex" {
		t.Fatalf("expected testing sandbox 'codex', got %#v", got.SandboxByActivity)
	}
	if _, ok := got.SandboxByActivity[SandboxActivityOversight]; ok {
		t.Fatalf("expected empty oversight value to be dropped, got %#v", got.SandboxByActivity)
	}
	if _, ok := got.SandboxByActivity["invalid"]; ok {
		t.Fatalf("expected invalid activity key to be ignored, got %#v", got.SandboxByActivity)
	}

	if err := s.UpdateTaskSandboxByActivity(bg(), task.ID, map[SandboxActivity]sandbox.Type{}); err != nil {
		t.Fatalf("UpdateTaskSandboxByActivity empty: %v", err)
	}
	got, _ = s.GetTask(bg(), task.ID)
	if got.SandboxByActivity != nil {
		t.Fatalf("expected empty map to clear sandbox overrides, got %#v", got.SandboxByActivity)
	}
}

func TestUpdateTaskSandbox_TrimsWhitespace(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	if err := s.UpdateTaskSandbox(bg(), task.ID, "  codex "); err != nil {
		t.Fatalf("UpdateTaskSandbox: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.Sandbox != "codex" {
		t.Fatalf("expected sandbox to trim whitespace, got %q", got.Sandbox)
	}
}

func TestUpdateTaskTestRun(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})
	if err := s.UpdateTaskTurns(bg(), task.ID, 4); err != nil {
		t.Fatalf("seed turns: %v", err)
	}

	if err := s.UpdateTaskTestRun(bg(), task.ID, true, ""); err != nil {
		t.Fatalf("start test run: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if !got.IsTestRun {
		t.Fatal("expected IsTestRun=true while running test")
	}
	if got.TestRunStartTurn != 4 {
		t.Fatalf("expected TestRunStartTurn 4, got %d", got.TestRunStartTurn)
	}

	if err := s.UpdateTaskTestRun(bg(), task.ID, false, "pass"); err != nil {
		t.Fatalf("finish test run: %v", err)
	}
	got, _ = s.GetTask(bg(), task.ID)
	if got.IsTestRun {
		t.Fatal("expected IsTestRun=false after test completion")
	}
	if got.LastTestResult != "pass" {
		t.Fatalf("expected LastTestResult 'pass', got %q", got.LastTestResult)
	}
}

func TestUpdateRefinementJob_UpdatesAndClears(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	job := &RefinementJob{ID: "job-1", Status: "running", Result: "draft"}
	if err := s.UpdateRefinementJob(bg(), task.ID, job); err != nil {
		t.Fatalf("UpdateRefinementJob: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.CurrentRefinement == nil || got.CurrentRefinement.ID != "job-1" {
		t.Fatalf("CurrentRefinement = %#v, want running job", got.CurrentRefinement)
	}

	if err := s.UpdateRefinementJob(bg(), task.ID, nil); err != nil {
		t.Fatalf("UpdateRefinementJob clear: %v", err)
	}
	got, _ = s.GetTask(bg(), task.ID)
	if got.CurrentRefinement != nil {
		t.Fatalf("expected CurrentRefinement to clear, got %#v", got.CurrentRefinement)
	}
}

func TestStartRefinementJobIfIdle(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	if err := s.StartRefinementJobIfIdle(bg(), task.ID, &RefinementJob{ID: "job-1", Status: "running"}); err != nil {
		t.Fatalf("StartRefinementJobIfIdle first start: %v", err)
	}

	if err := s.StartRefinementJobIfIdle(bg(), task.ID, &RefinementJob{ID: "job-2", Status: "running"}); err == nil {
		t.Fatal("expected ErrRefinementAlreadyRunning when existing job is running")
	} else if !errors.Is(err, ErrRefinementAlreadyRunning) {
		t.Fatalf("expected ErrRefinementAlreadyRunning, got %v", err)
	}

	// Mark existing job as done then start a new one.
	if err := s.UpdateRefinementJob(bg(), task.ID, &RefinementJob{ID: "job-1", Status: "done"}); err != nil {
		t.Fatalf("UpdateRefinementJob: %v", err)
	}
	if err := s.StartRefinementJobIfIdle(bg(), task.ID, &RefinementJob{ID: "job-2", Status: "running"}); err != nil {
		t.Fatalf("StartRefinementJobIfIdle after done: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.CurrentRefinement == nil || got.CurrentRefinement.ID != "job-2" {
		t.Fatalf("CurrentRefinement after restart = %#v", got.CurrentRefinement)
	}
}

func TestApplyRefinement(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "old prompt", Timeout: 5})

	session := RefinementSession{ID: "session-1", Result: "suggested", StartPrompt: "old prompt"}
	if err := s.ApplyRefinement(bg(), task.ID, "new prompt", session); err != nil {
		t.Fatalf("ApplyRefinement: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.Prompt != "new prompt" {
		t.Fatalf("Prompt = %q, want %q", got.Prompt, "new prompt")
	}
	if got.CurrentRefinement != nil {
		t.Fatalf("expected CurrentRefinement cleared, got %#v", got.CurrentRefinement)
	}
	if len(got.PromptHistory) != 1 || got.PromptHistory[0] != "old prompt" {
		t.Fatalf("PromptHistory = %#v, want ['old prompt']", got.PromptHistory)
	}
	if len(got.RefineSessions) != 1 || got.RefineSessions[0].Result != "suggested" {
		t.Fatalf("RefineSessions = %#v", got.RefineSessions)
	}
}

func TestDismissRefinement(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})
	if err := s.UpdateRefinementJob(bg(), task.ID, &RefinementJob{ID: "job-1", Status: "running"}); err != nil {
		t.Fatalf("seed refinement job: %v", err)
	}

	if err := s.DismissRefinement(bg(), task.ID); err != nil {
		t.Fatalf("DismissRefinement: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.CurrentRefinement != nil {
		t.Fatalf("expected CurrentRefinement to clear, got %#v", got.CurrentRefinement)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateTaskPosition
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateTaskPosition(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	if err := s.UpdateTaskPosition(bg(), task.ID, 42); err != nil {
		t.Fatalf("UpdateTaskPosition: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.Position != 42 {
		t.Errorf("Position = %d, want 42", got.Position)
	}
}

func TestUpdateTaskPosition_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTaskPosition(bg(), uuid.New(), 0); err == nil {
		t.Error("expected error for unknown task")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateTaskBacklog
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateTaskBacklog_UpdatesPrompt(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "original", Timeout: 5})
	newPrompt := "updated prompt"

	if err := s.UpdateTaskBacklog(bg(), task.ID, &newPrompt, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("UpdateTaskBacklog: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.Prompt != "updated prompt" {
		t.Errorf("Prompt = %q, want 'updated prompt'", got.Prompt)
	}
}

func TestUpdateTaskBacklog_UpdatesTimeout(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})
	newTimeout := 30

	_ = s.UpdateTaskBacklog(bg(), task.ID, nil, &newTimeout, nil, nil, nil, nil, nil)

	got, _ := s.GetTask(bg(), task.ID)
	if got.Timeout != 30 {
		t.Errorf("Timeout = %d, want 30", got.Timeout)
	}
}

func TestUpdateTaskBacklog_ClampsTimeout(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})
	big := 9999

	_ = s.UpdateTaskBacklog(bg(), task.ID, nil, &big, nil, nil, nil, nil, nil)

	got, _ := s.GetTask(bg(), task.ID)
	if got.Timeout != 1440 {
		t.Errorf("Timeout = %d, want 1440 (clamped)", got.Timeout)
	}
}

func TestUpdateTaskBacklog_UpdatesFreshStart(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})
	fresh := true

	_ = s.UpdateTaskBacklog(bg(), task.ID, nil, nil, &fresh, nil, nil, nil, nil)

	got, _ := s.GetTask(bg(), task.ID)
	if !got.FreshStart {
		t.Error("FreshStart should be true")
	}
}

func TestUpdateTaskBacklog_NilFieldsAreNoOps(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "original", Timeout: 5})

	if err := s.UpdateTaskBacklog(bg(), task.ID, nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("UpdateTaskBacklog with all nils: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.Prompt != "original" {
		t.Errorf("Prompt changed unexpectedly to %q", got.Prompt)
	}
}

func TestUpdateTaskBacklog_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTaskBacklog(bg(), uuid.New(), nil, nil, nil, nil, nil, nil, nil); err == nil {
		t.Error("expected error for unknown task")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// MountWorktrees
// ─────────────────────────────────────────────────────────────────────────────

func TestCreateTask_MountWorktrees(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "mount test", Timeout: 5, MountWorktrees: true})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if !task.MountWorktrees {
		t.Error("expected MountWorktrees=true")
	}

	got, _ := s.GetTask(bg(), task.ID)
	if !got.MountWorktrees {
		t.Error("MountWorktrees should persist after reload")
	}
}

func TestUpdateTaskBacklog_MountWorktrees(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	// Enable mount_worktrees.
	enable := true
	_ = s.UpdateTaskBacklog(bg(), task.ID, nil, nil, nil, &enable, nil, nil, nil)

	got, _ := s.GetTask(bg(), task.ID)
	if !got.MountWorktrees {
		t.Error("expected MountWorktrees=true after update")
	}

	// Disable mount_worktrees.
	disable := false
	_ = s.UpdateTaskBacklog(bg(), task.ID, nil, nil, nil, &disable, nil, nil, nil)

	got, _ = s.GetTask(bg(), task.ID)
	if got.MountWorktrees {
		t.Error("expected MountWorktrees=false after toggle off")
	}
}

func TestResetTaskForRetry_PreservesMountWorktrees(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "mount retry", Timeout: 5, MountWorktrees: true})
	_ = s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusDone)

	if err := s.ResetTaskForRetry(bg(), task.ID, "retry prompt", true); err != nil {
		t.Fatalf("ResetTaskForRetry: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if !got.MountWorktrees {
		t.Error("MountWorktrees should be preserved across retry")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ResetTaskForRetry
// ─────────────────────────────────────────────────────────────────────────────

func TestResetTaskForRetry(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "original prompt", Timeout: 5})
	_ = s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusDone)
	_ = s.UpdateTaskResult(bg(), task.ID, "some result", "sess", "end_turn", 2)

	if err := s.ResetTaskForRetry(bg(), task.ID, "new prompt", true); err != nil {
		t.Fatalf("ResetTaskForRetry: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.Status != TaskStatusBacklog {
		t.Errorf("Status = %q, want 'backlog'", got.Status)
	}
	if got.Prompt != "new prompt" {
		t.Errorf("Prompt = %q, want 'new prompt'", got.Prompt)
	}
	if !got.FreshStart {
		t.Error("FreshStart should be true")
	}
	if got.Result != nil {
		t.Error("Result should be nil after reset")
	}
	if got.StopReason != nil {
		t.Error("StopReason should be nil after reset")
	}
	if got.Turns != 0 {
		t.Errorf("Turns = %d, want 0", got.Turns)
	}
	if got.WorktreePaths != nil {
		t.Error("WorktreePaths should be nil after reset")
	}
	if got.BranchName != "" {
		t.Errorf("BranchName = %q, want empty", got.BranchName)
	}
	if got.CommitHashes != nil {
		t.Error("CommitHashes should be nil after reset")
	}
	if len(got.PromptHistory) != 1 || got.PromptHistory[0] != "original prompt" {
		t.Errorf("PromptHistory = %v, want ['original prompt']", got.PromptHistory)
	}
}

func TestResetTaskForRetry_AccumulatesHistory(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "prompt1", Timeout: 5})
	_ = s.ResetTaskForRetry(bg(), task.ID, "prompt2", false)
	_ = s.ResetTaskForRetry(bg(), task.ID, "prompt3", false)

	got, _ := s.GetTask(bg(), task.ID)
	if len(got.PromptHistory) != 2 {
		t.Fatalf("PromptHistory length = %d, want 2", len(got.PromptHistory))
	}
	if got.PromptHistory[0] != "prompt1" || got.PromptHistory[1] != "prompt2" {
		t.Errorf("PromptHistory = %v", got.PromptHistory)
	}
}

func TestResetTaskForRetry_ClearsBaseCommitHashes(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "original", Timeout: 5})
	_ = s.UpdateTaskCommitHashes(bg(), task.ID, map[string]string{"/repo": "abc"})
	_ = s.UpdateTaskBaseCommitHashes(bg(), task.ID, map[string]string{"/repo": "def"})

	_ = s.ResetTaskForRetry(bg(), task.ID, "retry prompt", true)

	got, _ := s.GetTask(bg(), task.ID)
	if got.BaseCommitHashes != nil {
		t.Errorf("BaseCommitHashes should be nil after reset, got %v", got.BaseCommitHashes)
	}
	if got.CommitHashes != nil {
		t.Errorf("CommitHashes should be nil after reset, got %v", got.CommitHashes)
	}
}

func TestResetTaskForRetry_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.ResetTaskForRetry(bg(), uuid.New(), "", false); err == nil {
		t.Error("expected error for unknown task")
	}
}

func TestResetTaskForRetryAccumulatesHistory(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Step 1: Create task, force to failed, set result and usage.
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "first prompt", Timeout: 5})
	_ = s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusFailed)
	_ = s.UpdateTaskResult(bg(), task.ID, "first result", "sess-1", "end_turn", 3)
	_ = s.AccumulateSubAgentUsage(bg(), task.ID, SandboxActivityImplementation,
		TaskUsage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.42})

	// First retry: snapshot pre-reset state.
	if err := s.ResetTaskForRetry(bg(), task.ID, "second prompt", false); err != nil {
		t.Fatalf("ResetTaskForRetry (1st): %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if len(got.RetryHistory) != 1 {
		t.Fatalf("RetryHistory length after 1st retry = %d, want 1", len(got.RetryHistory))
	}
	rec1 := got.RetryHistory[0]
	if rec1.Prompt != "first prompt" {
		t.Errorf("RetryHistory[0].Prompt = %q, want 'first prompt'", rec1.Prompt)
	}
	if rec1.Status != TaskStatusFailed {
		t.Errorf("RetryHistory[0].Status = %q, want 'failed'", rec1.Status)
	}
	if rec1.Result != "first result" {
		t.Errorf("RetryHistory[0].Result = %q, want 'first result'", rec1.Result)
	}
	if rec1.SessionID != "sess-1" {
		t.Errorf("RetryHistory[0].SessionID = %q, want 'sess-1'", rec1.SessionID)
	}
	if rec1.Turns != 3 {
		t.Errorf("RetryHistory[0].Turns = %d, want 3", rec1.Turns)
	}
	if rec1.CostUSD != 0.42 {
		t.Errorf("RetryHistory[0].CostUSD = %f, want 0.42", rec1.CostUSD)
	}
	if rec1.RetiredAt.IsZero() {
		t.Error("RetryHistory[0].RetiredAt should not be zero")
	}

	// Step 3: Force to failed again with different values; second retry.
	// AccumulateSubAgentUsage is cumulative: adding 0.57 on top of the existing
	// 0.42 gives a running total of 0.99, which is what the RetryRecord captures.
	_ = s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusFailed)
	_ = s.UpdateTaskResult(bg(), task.ID, "second result", "sess-2", "end_turn", 7)
	_ = s.AccumulateSubAgentUsage(bg(), task.ID, SandboxActivityImplementation,
		TaskUsage{InputTokens: 200, OutputTokens: 100, CostUSD: 0.57})

	if err := s.ResetTaskForRetry(bg(), task.ID, "third prompt", true); err != nil {
		t.Fatalf("ResetTaskForRetry (2nd): %v", err)
	}

	got, _ = s.GetTask(bg(), task.ID)
	if len(got.RetryHistory) != 2 {
		t.Fatalf("RetryHistory length after 2nd retry = %d, want 2", len(got.RetryHistory))
	}
	rec2 := got.RetryHistory[1]
	if rec2.Prompt != "second prompt" {
		t.Errorf("RetryHistory[1].Prompt = %q, want 'second prompt'", rec2.Prompt)
	}
	if rec2.Result != "second result" {
		t.Errorf("RetryHistory[1].Result = %q, want 'second result'", rec2.Result)
	}
	if rec2.SessionID != "sess-2" {
		t.Errorf("RetryHistory[1].SessionID = %q, want 'sess-2'", rec2.SessionID)
	}
	if rec2.Turns != 7 {
		t.Errorf("RetryHistory[1].Turns = %d, want 7", rec2.Turns)
	}
	const wantCostUSD2 = 0.99 // 0.42 (1st run) + 0.57 (2nd run) = 0.99 accumulated total
	if rec2.CostUSD != wantCostUSD2 {
		t.Errorf("RetryHistory[1].CostUSD = %f, want %f", rec2.CostUSD, wantCostUSD2)
	}
	// FIFO order: first record is still the earliest attempt.
	if got.RetryHistory[0].Prompt != "first prompt" {
		t.Errorf("FIFO order broken: RetryHistory[0].Prompt = %q, want 'first prompt'", got.RetryHistory[0].Prompt)
	}

	// Step 4: Verify PromptHistory is still populated (backward compat).
	if len(got.PromptHistory) != 2 {
		t.Fatalf("PromptHistory length = %d, want 2 (backward compat)", len(got.PromptHistory))
	}
	if got.PromptHistory[0] != "first prompt" || got.PromptHistory[1] != "second prompt" {
		t.Errorf("PromptHistory = %v, want ['first prompt', 'second prompt']", got.PromptHistory)
	}

	// Step 5: Reload store from disk and verify RetryHistory survived round-trip.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore (reload): %v", err)
	}
	reloaded, err := s2.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask after reload: %v", err)
	}
	if len(reloaded.RetryHistory) != 2 {
		t.Fatalf("RetryHistory length after reload = %d, want 2", len(reloaded.RetryHistory))
	}
	if reloaded.RetryHistory[0].Prompt != "first prompt" {
		t.Errorf("reloaded RetryHistory[0].Prompt = %q, want 'first prompt'", reloaded.RetryHistory[0].Prompt)
	}
	if reloaded.RetryHistory[1].CostUSD != wantCostUSD2 {
		t.Errorf("reloaded RetryHistory[1].CostUSD = %f, want %f", reloaded.RetryHistory[1].CostUSD, wantCostUSD2)
	}
}

func TestResetTaskForRetry_PreservesFailureCategoryInRecord(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "crash prompt", Timeout: 5})
	_ = s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusFailed)
	_ = s.SetTaskFailureCategory(bg(), task.ID, FailureCategoryContainerCrash)

	if err := s.ResetTaskForRetry(bg(), task.ID, "retry prompt", true); err != nil {
		t.Fatalf("ResetTaskForRetry: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if len(got.RetryHistory) != 1 {
		t.Fatalf("RetryHistory length = %d, want 1", len(got.RetryHistory))
	}
	if got.RetryHistory[0].FailureCategory != FailureCategoryContainerCrash {
		t.Errorf("RetryHistory[0].FailureCategory = %q, want %q",
			got.RetryHistory[0].FailureCategory, FailureCategoryContainerCrash)
	}
	// FailureCategory on the live task should be cleared after retry.
	if got.FailureCategory != "" {
		t.Errorf("FailureCategory after reset = %q, want empty", got.FailureCategory)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SetTaskArchived
// ─────────────────────────────────────────────────────────────────────────────

func TestSetTaskArchived_TrueAndFalse(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	_ = s.SetTaskArchived(bg(), task.ID, true)
	got, _ := s.GetTask(bg(), task.ID)
	if !got.Archived {
		t.Error("expected Archived=true")
	}

	_ = s.SetTaskArchived(bg(), task.ID, false)
	got, _ = s.GetTask(bg(), task.ID)
	if got.Archived {
		t.Error("expected Archived=false")
	}
}

func TestSetTaskArchived_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetTaskArchived(bg(), uuid.New(), true); err == nil {
		t.Error("expected error for unknown task")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ResumeTask
// ─────────────────────────────────────────────────────────────────────────────

func TestResumeTask_SetsInProgress(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})
	_ = s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusFailed)

	if err := s.ResumeTask(bg(), task.ID, nil); err != nil {
		t.Fatalf("ResumeTask: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.Status != TaskStatusInProgress {
		t.Errorf("Status = %q, want 'in_progress'", got.Status)
	}
}

func TestResumeTask_WithTimeout(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})
	timeout := 60

	_ = s.ResumeTask(bg(), task.ID, &timeout)

	got, _ := s.GetTask(bg(), task.ID)
	if got.Timeout != 60 {
		t.Errorf("Timeout = %d, want 60", got.Timeout)
	}
}

func TestResumeTask_TimeoutClamped(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})
	timeout := 9999

	_ = s.ResumeTask(bg(), task.ID, &timeout)

	got, _ := s.GetTask(bg(), task.ID)
	if got.Timeout != 1440 {
		t.Errorf("Timeout = %d, want 1440 (clamped)", got.Timeout)
	}
}

func TestResumeTask_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.ResumeTask(bg(), uuid.New(), nil); err == nil {
		t.Error("expected error for unknown task")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateTaskWorktrees
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateTaskWorktrees(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})
	paths := map[string]string{"/repo/a": "/worktree/a"}

	if err := s.UpdateTaskWorktrees(bg(), task.ID, paths, "task/abc123"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.BranchName != "task/abc123" {
		t.Errorf("BranchName = %q, want 'task/abc123'", got.BranchName)
	}
	if got.WorktreePaths["/repo/a"] != "/worktree/a" {
		t.Errorf("WorktreePaths[/repo/a] = %q", got.WorktreePaths["/repo/a"])
	}
}

func TestUpdateTaskWorktrees_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTaskWorktrees(bg(), uuid.New(), nil, ""); err == nil {
		t.Error("expected error for unknown task")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateTaskCommitHashes / UpdateTaskBaseCommitHashes
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateTaskCommitHashes(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	hashes := map[string]string{"/repo/a": "abc123def456"}
	if err := s.UpdateTaskCommitHashes(bg(), task.ID, hashes); err != nil {
		t.Fatalf("UpdateTaskCommitHashes: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.CommitHashes["/repo/a"] != "abc123def456" {
		t.Errorf("CommitHashes[/repo/a] = %q", got.CommitHashes["/repo/a"])
	}
}

func TestUpdateTaskCommitHashes_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTaskCommitHashes(bg(), uuid.New(), nil); err == nil {
		t.Error("expected error for unknown task")
	}
}

func TestUpdateTaskBaseCommitHashes(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	hashes := map[string]string{"/repo/a": "base456"}
	if err := s.UpdateTaskBaseCommitHashes(bg(), task.ID, hashes); err != nil {
		t.Fatalf("UpdateTaskBaseCommitHashes: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.BaseCommitHashes["/repo/a"] != "base456" {
		t.Errorf("BaseCommitHashes[/repo/a] = %q", got.BaseCommitHashes["/repo/a"])
	}
}

func TestUpdateTaskBaseCommitHashes_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTaskBaseCommitHashes(bg(), uuid.New(), nil); err == nil {
		t.Error("expected error for unknown task")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Concurrency
// ─────────────────────────────────────────────────────────────────────────────

func TestConcurrentCreateTask(t *testing.T) {
	s := newTestStore(t)
	var wg sync.WaitGroup
	const n = 20
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "concurrent", Timeout: 5})
		}()
	}
	wg.Wait()

	tasks, _ := s.ListTasks(bg(), false)
	if len(tasks) != n {
		t.Errorf("expected %d tasks, got %d", n, len(tasks))
	}
}

func TestConcurrentUpdateStatus(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	var wg sync.WaitGroup
	for _, status := range []TaskStatus{TaskStatusInProgress, TaskStatusDone, TaskStatusFailed, TaskStatusBacklog, TaskStatusWaiting} {
		wg.Add(1)
		go func(st TaskStatus) {
			defer wg.Done()
			_ = s.ForceUpdateTaskStatus(bg(), task.ID, st)
		}(status)
	}
	wg.Wait()

	got, err := s.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask after concurrent updates: %v", err)
	}
	if got.ID != task.ID {
		t.Error("task ID changed unexpectedly")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateTaskScheduledAt
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateTaskScheduledAt_SetAndClear(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "p", Timeout: 5})

	future := time.Now().Add(2 * time.Hour)
	if err := s.UpdateTaskScheduledAt(bg(), task.ID, &future); err != nil {
		t.Fatalf("UpdateTaskScheduledAt set: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.ScheduledAt == nil {
		t.Fatal("expected ScheduledAt to be set, got nil")
	}
	if !got.ScheduledAt.Equal(future) {
		t.Errorf("ScheduledAt = %v, want %v", got.ScheduledAt, future)
	}

	// Clear it.
	if err := s.UpdateTaskScheduledAt(bg(), task.ID, nil); err != nil {
		t.Fatalf("UpdateTaskScheduledAt clear: %v", err)
	}
	got, _ = s.GetTask(bg(), task.ID)
	if got.ScheduledAt != nil {
		t.Errorf("expected ScheduledAt to be nil after clear, got %v", got.ScheduledAt)
	}
}

func TestUpdateTaskScheduledAt_PersistsAndLoads(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)

	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "persist-scheduled", Timeout: 5})
	future := time.Now().Add(3 * time.Hour).Truncate(time.Second)
	if err := s.UpdateTaskScheduledAt(bg(), task.ID, &future); err != nil {
		t.Fatalf("UpdateTaskScheduledAt: %v", err)
	}

	// Reload from disk.
	s2, _ := NewStore(dir)
	got, err := s2.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask after reload: %v", err)
	}
	if got.ScheduledAt == nil {
		t.Fatal("expected ScheduledAt to survive disk round-trip, got nil")
	}
	if !got.ScheduledAt.Equal(future) {
		t.Errorf("ScheduledAt after reload = %v, want %v", got.ScheduledAt, future)
	}
}

func TestUpdateTaskScheduledAt_NotFound(t *testing.T) {
	s := newTestStore(t)
	future := time.Now().Add(time.Hour)
	if err := s.UpdateTaskScheduledAt(bg(), uuid.New(), &future); err == nil {
		t.Error("expected error for unknown task")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Soft-delete / restore / purge
// ─────────────────────────────────────────────────────────────────────────────

func TestSoftDelete(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "soft delete me", Timeout: 5})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := s.DeleteTask(bg(), task.ID, "test reason"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	// Must not appear in ListTasks.
	tasks, _ := s.ListTasks(bg(), true)
	for _, tsk := range tasks {
		if tsk.ID == task.ID {
			t.Error("soft-deleted task still appears in ListTasks")
		}
	}

	// Must appear in ListDeletedTasks.
	deleted, err := s.ListDeletedTasks(bg())
	if err != nil {
		t.Fatalf("ListDeletedTasks: %v", err)
	}
	found := false
	for _, tsk := range deleted {
		if tsk.ID == task.ID {
			found = true
		}
	}
	if !found {
		t.Error("soft-deleted task not found in ListDeletedTasks")
	}

	// tombstone.json must exist on disk.
	tombPath := filepath.Join(s.dir, task.ID.String(), "tombstone.json")
	if _, err := os.Stat(tombPath); os.IsNotExist(err) {
		t.Error("tombstone.json not written to disk")
	}
}

func TestRestoreRoundtrip(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "restore me", Timeout: 5})

	if err := s.DeleteTask(bg(), task.ID, ""); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if err := s.RestoreTask(bg(), task.ID); err != nil {
		t.Fatalf("RestoreTask: %v", err)
	}

	// Must be back in ListTasks.
	tasks, _ := s.ListTasks(bg(), true)
	found := false
	for _, tsk := range tasks {
		if tsk.ID == task.ID {
			found = true
		}
	}
	if !found {
		t.Error("restored task not found in ListTasks")
	}

	// Must not be in ListDeletedTasks.
	deleted, _ := s.ListDeletedTasks(bg())
	for _, tsk := range deleted {
		if tsk.ID == task.ID {
			t.Error("restored task still appears in ListDeletedTasks")
		}
	}

	// tombstone.json must be gone.
	tombPath := filepath.Join(s.dir, task.ID.String(), "tombstone.json")
	if _, err := os.Stat(tombPath); !os.IsNotExist(err) {
		t.Error("tombstone.json still present after restore")
	}
}

func TestPurgeExpired(t *testing.T) {
	s := newTestStore(t)

	// Create two tasks and soft-delete them.
	old, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "old task", Timeout: 5})
	recent, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "recent task", Timeout: 5})

	if err := s.DeleteTask(bg(), old.ID, ""); err != nil {
		t.Fatalf("DeleteTask old: %v", err)
	}
	if err := s.DeleteTask(bg(), recent.ID, ""); err != nil {
		t.Fatalf("DeleteTask recent: %v", err)
	}

	// Back-date the old tombstone to 8 days ago.
	oldTombPath := filepath.Join(s.dir, old.ID.String(), "tombstone.json")
	oldTomb := Tombstone{DeletedAt: time.Now().AddDate(0, 0, -8), Reason: "old"}
	raw, _ := json.Marshal(oldTomb)
	if err := os.WriteFile(oldTombPath, raw, 0644); err != nil {
		t.Fatalf("patch old tombstone: %v", err)
	}

	// Back-date the recent tombstone to 3 days ago.
	recentTombPath := filepath.Join(s.dir, recent.ID.String(), "tombstone.json")
	recentTomb := Tombstone{DeletedAt: time.Now().AddDate(0, 0, -3), Reason: "recent"}
	raw, _ = json.Marshal(recentTomb)
	if err := os.WriteFile(recentTombPath, raw, 0644); err != nil {
		t.Fatalf("patch recent tombstone: %v", err)
	}

	s.PurgeExpiredTombstones(7)

	// Old task's directory must be gone.
	if _, err := os.Stat(filepath.Join(s.dir, old.ID.String())); !os.IsNotExist(err) {
		t.Error("old task directory not removed by PurgeExpiredTombstones")
	}

	// Recent task must still exist (not yet expired).
	if _, err := os.Stat(filepath.Join(s.dir, recent.ID.String())); os.IsNotExist(err) {
		t.Error("recent task directory was incorrectly purged")
	}

	// Recent task must still be in ListDeletedTasks.
	deleted, _ := s.ListDeletedTasks(bg())
	foundRecent := false
	for _, tsk := range deleted {
		if tsk.ID == old.ID {
			t.Error("purged task still appears in ListDeletedTasks")
		}
		if tsk.ID == recent.ID {
			foundRecent = true
		}
	}
	if !foundRecent {
		t.Error("non-expired task missing from ListDeletedTasks after purge")
	}
}

func TestStorePersistsTombstone(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "tombstone persist", Timeout: 5})
	if err := s.DeleteTask(bg(), task.ID, "persistence test"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	// Reload the store from the same directory.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}

	// Task must not be in active tasks.
	if _, err := s2.GetTask(bg(), task.ID); err == nil {
		t.Error("tombstoned task appears active after store reload")
	}

	// Task must be in deleted map.
	deleted, err := s2.ListDeletedTasks(bg())
	if err != nil {
		t.Fatalf("ListDeletedTasks after reload: %v", err)
	}
	found := false
	for _, tsk := range deleted {
		if tsk.ID == task.ID {
			found = true
		}
	}
	if !found {
		t.Error("tombstoned task not found in ListDeletedTasks after reload")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateTaskEnvironment
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateTaskEnvironment_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "env test", Timeout: 60})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	recorded := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	env := ExecutionEnvironment{
		ContainerImage:   "wallfacer-sandbox:latest",
		ContainerDigest:  "sha256:abc123def456",
		ModelName:        "claude-opus-4-6",
		APIBaseURL:       "https://api.example.com",
		InstructionsHash: "deadbeef01234567deadbeef01234567deadbeef01234567deadbeef01234567",
		RecordedAt:       recorded,
	}

	if err := s.UpdateTaskEnvironment(bg(), task.ID, env); err != nil {
		t.Fatalf("UpdateTaskEnvironment: %v", err)
	}

	got, err := s.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	if got.Environment == nil {
		t.Fatal("Environment should not be nil after UpdateTaskEnvironment")
	}
	if got.Environment.ContainerImage != env.ContainerImage {
		t.Errorf("ContainerImage = %q, want %q", got.Environment.ContainerImage, env.ContainerImage)
	}
	if got.Environment.ContainerDigest != env.ContainerDigest {
		t.Errorf("ContainerDigest = %q, want %q", got.Environment.ContainerDigest, env.ContainerDigest)
	}
	if got.Environment.ModelName != env.ModelName {
		t.Errorf("ModelName = %q, want %q", got.Environment.ModelName, env.ModelName)
	}
	if got.Environment.APIBaseURL != env.APIBaseURL {
		t.Errorf("APIBaseURL = %q, want %q", got.Environment.APIBaseURL, env.APIBaseURL)
	}
	if got.Environment.InstructionsHash != env.InstructionsHash {
		t.Errorf("InstructionsHash = %q, want %q", got.Environment.InstructionsHash, env.InstructionsHash)
	}
	if !got.Environment.RecordedAt.Equal(env.RecordedAt) {
		t.Errorf("RecordedAt = %v, want %v", got.Environment.RecordedAt, env.RecordedAt)
	}
}

func TestUpdateTaskEnvironment_PersistsAcrossLoad(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "persist test", Timeout: 60})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	env := ExecutionEnvironment{
		ContainerImage: "wallfacer:test",
		ModelName:      "claude-sonnet-4",
		RecordedAt:     time.Now().Truncate(time.Second),
	}
	if err := s.UpdateTaskEnvironment(bg(), task.ID, env); err != nil {
		t.Fatalf("UpdateTaskEnvironment: %v", err)
	}
	s.Close()

	// Reload from disk.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore (reload): %v", err)
	}
	defer s2.Close()

	got, err := s2.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask after reload: %v", err)
	}
	if got.Environment == nil {
		t.Fatal("Environment should not be nil after reload")
	}
	if got.Environment.ContainerImage != env.ContainerImage {
		t.Errorf("ContainerImage = %q, want %q after reload", got.Environment.ContainerImage, env.ContainerImage)
	}
	if got.Environment.ModelName != env.ModelName {
		t.Errorf("ModelName = %q, want %q after reload", got.Environment.ModelName, env.ModelName)
	}
}

func TestUpdateTaskEnvironment_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.UpdateTaskEnvironment(bg(), uuid.New(), ExecutionEnvironment{})
	if err == nil {
		t.Error("expected error for unknown task ID")
	}
}

