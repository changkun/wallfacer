// Tests for tasks.go: all task CRUD operations and clampTimeout.
package store

import (
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────────────────────
// clampTimeout
// ─────────────────────────────────────────────────────────────────────────────

func TestClampTimeout(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 5},
		{-1, 5},
		{-999, 5},
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
	task, err := s.CreateTask(bg(), "my prompt", 10)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.ID == (uuid.UUID{}) {
		t.Error("expected non-zero task ID")
	}
	if task.Prompt != "my prompt" {
		t.Errorf("Prompt = %q, want 'my prompt'", task.Prompt)
	}
	if task.Status != "backlog" {
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
	t1, _ := s.CreateTask(bg(), "first", 5)
	t2, _ := s.CreateTask(bg(), "second", 5)
	t3, _ := s.CreateTask(bg(), "third", 5)
	if t2.Position != t1.Position+1 {
		t.Errorf("t2.Position = %d, want %d", t2.Position, t1.Position+1)
	}
	if t3.Position != t2.Position+1 {
		t.Errorf("t3.Position = %d, want %d", t3.Position, t2.Position+1)
	}
}

func TestCreateTask_TimeoutClampedDefault(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 0)
	if task.Timeout != 5 {
		t.Errorf("expected default timeout 5, got %d", task.Timeout)
	}
}

func TestCreateTask_TimeoutClampedMax(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 9999)
	if task.Timeout != 1440 {
		t.Errorf("expected clamped timeout 1440, got %d", task.Timeout)
	}
}

func TestCreateTask_PersistsToDisk(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	task, _ := s.CreateTask(bg(), "persist me", 5)

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
	t1, _ := s.CreateTask(bg(), "a", 5)
	s.UpdateTaskStatus(bg(), t1.ID, "done")
	t2, _ := s.CreateTask(bg(), "b", 5)
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
	task, _ := s.CreateTask(bg(), "original", 5)

	got, _ := s.GetTask(bg(), task.ID)
	got.Prompt = "mutated"

	got2, _ := s.GetTask(bg(), task.ID)
	if got2.Prompt != "original" {
		t.Errorf("GetTask returned a reference, not a copy (prompt changed to %q)", got2.Prompt)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ListTasks
// ─────────────────────────────────────────────────────────────────────────────

func TestListTasks_SortedByPosition(t *testing.T) {
	s := newTestStore(t)
	s.CreateTask(bg(), "a", 5)
	s.CreateTask(bg(), "b", 5)
	s.CreateTask(bg(), "c", 5)

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
	t1, _ := s.CreateTask(bg(), "first", 5)
	t2, _ := s.CreateTask(bg(), "second", 5)

	// Force both to the same position.
	s.UpdateTaskPosition(bg(), t1.ID, 10)
	s.UpdateTaskPosition(bg(), t2.ID, 10)

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
	task, _ := s.CreateTask(bg(), "archive me", 5)
	s.SetTaskArchived(bg(), task.ID, true)

	visible, _ := s.ListTasks(bg(), false)
	if len(visible) != 0 {
		t.Errorf("expected 0 visible tasks, got %d", len(visible))
	}
}

func TestListTasks_IncludesArchivedWhenRequested(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "archive me", 5)
	s.SetTaskArchived(bg(), task.ID, true)

	all, _ := s.ListTasks(bg(), true)
	if len(all) != 1 {
		t.Errorf("expected 1 archived task, got %d", len(all))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DeleteTask
// ─────────────────────────────────────────────────────────────────────────────

func TestDeleteTask_Basic(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "delete me", 5)

	if err := s.DeleteTask(bg(), task.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if _, err := s.GetTask(bg(), task.ID); err == nil {
		t.Error("expected task-not-found error after delete")
	}
}

func TestDeleteTask_RemovesDiskData(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	task, _ := s.CreateTask(bg(), "delete me", 5)
	taskDir := dir + "/" + task.ID.String()

	s.DeleteTask(bg(), task.ID)

	if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
		t.Errorf("task directory still exists after delete, stat err: %v", err)
	}
}

func TestDeleteTask_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.DeleteTask(bg(), uuid.New()); err == nil {
		t.Error("expected error deleting unknown task")
	}
}

func TestDeleteTask_RemovesFromEvents(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5)
	s.InsertEvent(bg(), task.ID, "state_change", "test")
	s.DeleteTask(bg(), task.ID)

	events, _ := s.GetEvents(bg(), task.ID)
	if len(events) != 0 {
		t.Errorf("expected 0 events after delete, got %d", len(events))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateTaskStatus
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateTaskStatus(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5)

	if err := s.UpdateTaskStatus(bg(), task.ID, "in_progress"); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.Status != "in_progress" {
		t.Errorf("Status = %q, want 'in_progress'", got.Status)
	}
}

func TestUpdateTaskStatus_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTaskStatus(bg(), uuid.New(), "done"); err == nil {
		t.Error("expected error for unknown task")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateTaskTitle
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateTaskTitle(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5)

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
	task, _ := s.CreateTask(bg(), "p", 5)

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
// AccumulateTaskUsage
// ─────────────────────────────────────────────────────────────────────────────

func TestAccumulateTaskUsage(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5)

	delta := TaskUsage{
		InputTokens:          100,
		OutputTokens:         50,
		CacheReadInputTokens: 10,
		CacheCreationTokens:  5,
		CostUSD:              0.01,
	}
	s.AccumulateTaskUsage(bg(), task.ID, delta)
	s.AccumulateTaskUsage(bg(), task.ID, delta)

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

// ─────────────────────────────────────────────────────────────────────────────
// UpdateTaskPosition
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdateTaskPosition(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5)

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
	task, _ := s.CreateTask(bg(), "original", 5)
	newPrompt := "updated prompt"

	if err := s.UpdateTaskBacklog(bg(), task.ID, &newPrompt, nil, nil); err != nil {
		t.Fatalf("UpdateTaskBacklog: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.Prompt != "updated prompt" {
		t.Errorf("Prompt = %q, want 'updated prompt'", got.Prompt)
	}
}

func TestUpdateTaskBacklog_UpdatesTimeout(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5)
	newTimeout := 30

	s.UpdateTaskBacklog(bg(), task.ID, nil, &newTimeout, nil)

	got, _ := s.GetTask(bg(), task.ID)
	if got.Timeout != 30 {
		t.Errorf("Timeout = %d, want 30", got.Timeout)
	}
}

func TestUpdateTaskBacklog_ClampsTimeout(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5)
	big := 9999

	s.UpdateTaskBacklog(bg(), task.ID, nil, &big, nil)

	got, _ := s.GetTask(bg(), task.ID)
	if got.Timeout != 1440 {
		t.Errorf("Timeout = %d, want 1440 (clamped)", got.Timeout)
	}
}

func TestUpdateTaskBacklog_UpdatesFreshStart(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5)
	fresh := true

	s.UpdateTaskBacklog(bg(), task.ID, nil, nil, &fresh)

	got, _ := s.GetTask(bg(), task.ID)
	if !got.FreshStart {
		t.Error("FreshStart should be true")
	}
}

func TestUpdateTaskBacklog_NilFieldsAreNoOps(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "original", 5)

	if err := s.UpdateTaskBacklog(bg(), task.ID, nil, nil, nil); err != nil {
		t.Fatalf("UpdateTaskBacklog with all nils: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.Prompt != "original" {
		t.Errorf("Prompt changed unexpectedly to %q", got.Prompt)
	}
}

func TestUpdateTaskBacklog_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTaskBacklog(bg(), uuid.New(), nil, nil, nil); err == nil {
		t.Error("expected error for unknown task")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ResetTaskForRetry
// ─────────────────────────────────────────────────────────────────────────────

func TestResetTaskForRetry(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "original prompt", 5)
	s.UpdateTaskStatus(bg(), task.ID, "done")
	s.UpdateTaskResult(bg(), task.ID, "some result", "sess", "end_turn", 2)

	if err := s.ResetTaskForRetry(bg(), task.ID, "new prompt", true); err != nil {
		t.Fatalf("ResetTaskForRetry: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.Status != "backlog" {
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
	task, _ := s.CreateTask(bg(), "prompt1", 5)
	s.ResetTaskForRetry(bg(), task.ID, "prompt2", false)
	s.ResetTaskForRetry(bg(), task.ID, "prompt3", false)

	got, _ := s.GetTask(bg(), task.ID)
	if len(got.PromptHistory) != 2 {
		t.Fatalf("PromptHistory length = %d, want 2", len(got.PromptHistory))
	}
	if got.PromptHistory[0] != "prompt1" || got.PromptHistory[1] != "prompt2" {
		t.Errorf("PromptHistory = %v", got.PromptHistory)
	}
}

func TestResetTaskForRetry_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.ResetTaskForRetry(bg(), uuid.New(), "", false); err == nil {
		t.Error("expected error for unknown task")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SetTaskArchived
// ─────────────────────────────────────────────────────────────────────────────

func TestSetTaskArchived_TrueAndFalse(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5)

	s.SetTaskArchived(bg(), task.ID, true)
	got, _ := s.GetTask(bg(), task.ID)
	if !got.Archived {
		t.Error("expected Archived=true")
	}

	s.SetTaskArchived(bg(), task.ID, false)
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
	task, _ := s.CreateTask(bg(), "p", 5)
	s.UpdateTaskStatus(bg(), task.ID, "failed")

	if err := s.ResumeTask(bg(), task.ID, nil); err != nil {
		t.Fatalf("ResumeTask: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.Status != "in_progress" {
		t.Errorf("Status = %q, want 'in_progress'", got.Status)
	}
}

func TestResumeTask_WithTimeout(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5)
	timeout := 60

	s.ResumeTask(bg(), task.ID, &timeout)

	got, _ := s.GetTask(bg(), task.ID)
	if got.Timeout != 60 {
		t.Errorf("Timeout = %d, want 60", got.Timeout)
	}
}

func TestResumeTask_TimeoutClamped(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTask(bg(), "p", 5)
	timeout := 9999

	s.ResumeTask(bg(), task.ID, &timeout)

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
	task, _ := s.CreateTask(bg(), "p", 5)
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
	task, _ := s.CreateTask(bg(), "p", 5)

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
	task, _ := s.CreateTask(bg(), "p", 5)

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
			s.CreateTask(bg(), "concurrent", 5)
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
	task, _ := s.CreateTask(bg(), "p", 5)

	var wg sync.WaitGroup
	for _, status := range []string{"in_progress", "done", "failed", "backlog", "waiting"} {
		wg.Add(1)
		go func(st string) {
			defer wg.Done()
			s.UpdateTaskStatus(bg(), task.ID, st)
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
