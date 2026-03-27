package store

import (
	"testing"

	"github.com/google/uuid"
)

func TestParseFailureCategory_KnownValues(t *testing.T) {
	known := []FailureCategory{
		FailureCategoryTimeout,
		FailureCategoryBudget,
		FailureCategoryWorktree,
		FailureCategoryContainerCrash,
		FailureCategoryAgentError,
		FailureCategorySyncError,
		FailureCategoryUnknown,
	}
	for _, cat := range known {
		got, ok := ParseFailureCategory(string(cat))
		if !ok {
			t.Errorf("ParseFailureCategory(%q): expected ok=true", cat)
		}
		if got != cat {
			t.Errorf("ParseFailureCategory(%q) = %q, want %q", cat, got, cat)
		}
	}
}

func TestParseFailureCategory_TrimsWhitespace(t *testing.T) {
	got, ok := ParseFailureCategory("  timeout  ")
	if !ok {
		t.Error("expected ok=true for whitespace-padded value")
	}
	if got != FailureCategoryTimeout {
		t.Errorf("got %q, want %q", got, FailureCategoryTimeout)
	}
}

func TestParseFailureCategory_UnknownValue(t *testing.T) {
	_, ok := ParseFailureCategory("not_a_real_category")
	if ok {
		t.Error("expected ok=false for unknown category")
	}
}

func TestParseFailureCategory_Empty(t *testing.T) {
	_, ok := ParseFailureCategory("")
	if ok {
		t.Error("expected ok=false for empty string")
	}
}

func TestNewStateChangeData_Basic(t *testing.T) {
	data := NewStateChangeData(TaskStatusBacklog, TaskStatusInProgress, TriggerUser, nil)
	if data["from"] != "backlog" {
		t.Errorf("from = %q, want backlog", data["from"])
	}
	if data["to"] != "in_progress" {
		t.Errorf("to = %q, want in_progress", data["to"])
	}
	if data["trigger"] != "user" {
		t.Errorf("trigger = %q, want user", data["trigger"])
	}
}

func TestNewStateChangeData_WithExtra(t *testing.T) {
	extra := map[string]string{"reason": "manual", "note": "test"}
	data := NewStateChangeData(TaskStatusFailed, TaskStatusBacklog, TriggerAutoRetry, extra)
	if data["from"] != "failed" {
		t.Errorf("from = %q, want failed", data["from"])
	}
	if data["reason"] != "manual" {
		t.Errorf("reason = %q, want manual", data["reason"])
	}
	if data["note"] != "test" {
		t.Errorf("note = %q, want test", data["note"])
	}
}

func TestStore_DataDir(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if got := s.DataDir(); got != dir {
		t.Errorf("DataDir() = %q, want %q", got, dir)
	}
}

func TestStore_SubscriberCount(t *testing.T) {
	s := newTestStore(t)
	if n := s.SubscriberCount(); n != 0 {
		t.Errorf("initial SubscriberCount = %d, want 0", n)
	}

	id1, _ := s.Subscribe()
	if n := s.SubscriberCount(); n != 1 {
		t.Errorf("after first subscribe, SubscriberCount = %d, want 1", n)
	}

	id2, _ := s.Subscribe()
	if n := s.SubscriberCount(); n != 2 {
		t.Errorf("after second subscribe, SubscriberCount = %d, want 2", n)
	}

	s.Unsubscribe(id1)
	if n := s.SubscriberCount(); n != 1 {
		t.Errorf("after first unsubscribe, SubscriberCount = %d, want 1", n)
	}

	s.Unsubscribe(id2)
	if n := s.SubscriberCount(); n != 0 {
		t.Errorf("after second unsubscribe, SubscriberCount = %d, want 0", n)
	}
}

func TestPurgeTask_PurgesDeletedTask(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "to purge", Timeout: 5})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := s.DeleteTask(bg(), task.ID, "test purge"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	if err := s.PurgeTask(bg(), task.ID); err != nil {
		t.Fatalf("PurgeTask: %v", err)
	}

	_, err = s.GetTask(bg(), task.ID)
	if err == nil {
		t.Error("expected error after purge, got nil")
	}
}

func TestPurgeTask_FailsForNonDeletedTask(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "alive", Timeout: 5})

	if err := s.PurgeTask(bg(), task.ID); err == nil {
		t.Error("expected error when purging non-tombstoned task, got nil")
	}
}

func TestPurgeTask_FailsForUnknownID(t *testing.T) {
	s := newTestStore(t)
	if err := s.PurgeTask(bg(), uuid.New()); err == nil {
		t.Error("expected error for unknown task ID, got nil")
	}
}

func TestListArchivedTasksPage_EmptyStore(t *testing.T) {
	s := newTestStore(t)
	tasks, total, hasBefore, hasAfter, err := s.ListArchivedTasksPage(bg(), 10, nil, nil)
	if err != nil {
		t.Fatalf("ListArchivedTasksPage: %v", err)
	}
	if total != 0 || hasBefore || hasAfter || len(tasks) != 0 {
		t.Errorf("expected empty result, got total=%d hasBefore=%v hasAfter=%v len=%d",
			total, hasBefore, hasAfter, len(tasks))
	}
}

func TestListArchivedTasksPage_FirstPage(t *testing.T) {
	s := newTestStore(t)

	for range 5 {
		task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "task", Timeout: 5})
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusDone) //nolint:errcheck
		s.SetTaskArchived(bg(), task.ID, true)                 //nolint:errcheck
	}

	tasks, total, _, _, err := s.ListArchivedTasksPage(bg(), 3, nil, nil)
	if err != nil {
		t.Fatalf("ListArchivedTasksPage: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(tasks) != 3 {
		t.Errorf("len(tasks) = %d, want 3", len(tasks))
	}
}

func TestListArchivedTasksPage_BeforeCursor(t *testing.T) {
	s := newTestStore(t)

	for range 4 {
		task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "task", Timeout: 5})
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusDone) //nolint:errcheck
		s.SetTaskArchived(bg(), task.ID, true)                 //nolint:errcheck
	}

	firstPage, total, _, _, err := s.ListArchivedTasksPage(bg(), 2, nil, nil)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if total != 4 {
		t.Fatalf("total = %d, want 4", total)
	}
	if len(firstPage) == 0 {
		t.Fatal("expected non-empty first page")
	}

	cursorID := firstPage[len(firstPage)-1].ID
	nextPage, _, _, _, err := s.ListArchivedTasksPage(bg(), 2, &cursorID, nil)
	if err != nil {
		t.Fatalf("second page (beforeID): %v", err)
	}
	if len(nextPage) == 0 {
		t.Error("expected non-empty second page")
	}
}

func TestListArchivedTasksPage_MutuallyExclusiveCursors(t *testing.T) {
	s := newTestStore(t)
	id := uuid.New()
	_, _, _, _, err := s.ListArchivedTasksPage(bg(), 10, &id, &id)
	if err == nil {
		t.Error("expected error when both before and after cursors are set, got nil")
	}
}

func TestListArchivedTasksPage_InvalidBeforeCursor(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "t", Timeout: 5})
	s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusDone) //nolint:errcheck
	s.SetTaskArchived(bg(), task.ID, true)                 //nolint:errcheck

	unknown := uuid.New()
	_, _, _, _, err := s.ListArchivedTasksPage(bg(), 10, &unknown, nil)
	if err == nil {
		t.Error("expected error for unknown beforeID cursor, got nil")
	}
}

func TestListArchivedTasksPage_InvalidAfterCursor(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "t", Timeout: 5})
	s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusDone) //nolint:errcheck
	s.SetTaskArchived(bg(), task.ID, true)                 //nolint:errcheck

	unknown := uuid.New()
	_, _, _, _, err := s.ListArchivedTasksPage(bg(), 10, nil, &unknown)
	if err == nil {
		t.Error("expected error for unknown afterID cursor, got nil")
	}
}

func TestUpdateTaskBudget_SetAndClear(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "budget task", Timeout: 5})

	cost := 10.0
	tokens := 50000
	if err := s.UpdateTaskBudget(bg(), task.ID, &cost, &tokens); err != nil {
		t.Fatalf("UpdateTaskBudget: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.MaxCostUSD != 10.0 {
		t.Errorf("MaxCostUSD = %v, want 10.0", got.MaxCostUSD)
	}
	if got.MaxInputTokens != 50000 {
		t.Errorf("MaxInputTokens = %d, want 50000", got.MaxInputTokens)
	}
}

func TestUpdateTaskBudget_ClampsNegative(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "budget task", Timeout: 5})

	neg := -5.0
	negTokens := -100
	if err := s.UpdateTaskBudget(bg(), task.ID, &neg, &negTokens); err != nil {
		t.Fatalf("UpdateTaskBudget: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.MaxCostUSD != 0 {
		t.Errorf("MaxCostUSD = %v, want 0", got.MaxCostUSD)
	}
	if got.MaxInputTokens != 0 {
		t.Errorf("MaxInputTokens = %d, want 0", got.MaxInputTokens)
	}
}

func TestUpdateTaskBudget_NilFieldsPreserved(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "budget task", Timeout: 5})

	cost := 7.5
	if err := s.UpdateTaskBudget(bg(), task.ID, &cost, nil); err != nil {
		t.Fatalf("UpdateTaskBudget (cost only): %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.MaxCostUSD != 7.5 {
		t.Errorf("MaxCostUSD = %v, want 7.5", got.MaxCostUSD)
	}
	if got.MaxInputTokens != 0 {
		t.Errorf("MaxInputTokens should remain 0 when nil passed, got %d", got.MaxInputTokens)
	}
}

func TestUpdateTaskBudget_UnknownID(t *testing.T) {
	s := newTestStore(t)
	cost := 1.0
	if err := s.UpdateTaskBudget(bg(), uuid.New(), &cost, nil); err == nil {
		t.Error("expected error for unknown task ID")
	}
}

func TestUpdateTaskModelOverride_SetAndClear(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "model task", Timeout: 5})

	if err := s.UpdateTaskModelOverride(bg(), task.ID, "claude-opus-4-5"); err != nil {
		t.Fatalf("UpdateTaskModelOverride set: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.ModelOverride == nil || *got.ModelOverride != "claude-opus-4-5" {
		t.Errorf("ModelOverride = %v, want claude-opus-4-5", got.ModelOverride)
	}

	if err := s.UpdateTaskModelOverride(bg(), task.ID, ""); err != nil {
		t.Fatalf("UpdateTaskModelOverride clear: %v", err)
	}
	got, _ = s.GetTask(bg(), task.ID)
	if got.ModelOverride != nil {
		t.Errorf("ModelOverride should be nil after clearing, got %v", *got.ModelOverride)
	}
}

func TestUpdateTaskModelOverride_TrimsWhitespace(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "model task", Timeout: 5})

	if err := s.UpdateTaskModelOverride(bg(), task.ID, "  "); err != nil {
		t.Fatalf("UpdateTaskModelOverride whitespace: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.ModelOverride != nil {
		t.Errorf("ModelOverride should be nil for whitespace-only input, got %v", *got.ModelOverride)
	}
}

func TestUpdateTaskModelOverride_UnknownID(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTaskModelOverride(bg(), uuid.New(), "model"); err == nil {
		t.Error("expected error for unknown task ID")
	}
}

func TestUpdateTaskCustomPatterns_SetAndClear(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "pattern task", Timeout: 5})

	pass := []string{"PASS", "OK"}
	fail := []string{"FAIL", "ERROR"}
	if err := s.UpdateTaskCustomPatterns(bg(), task.ID, pass, fail); err != nil {
		t.Fatalf("UpdateTaskCustomPatterns set: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if len(got.CustomPassPatterns) != 2 || got.CustomPassPatterns[0] != "PASS" {
		t.Errorf("CustomPassPatterns = %v, want [PASS OK]", got.CustomPassPatterns)
	}
	if len(got.CustomFailPatterns) != 2 || got.CustomFailPatterns[0] != "FAIL" {
		t.Errorf("CustomFailPatterns = %v, want [FAIL ERROR]", got.CustomFailPatterns)
	}

	if err := s.UpdateTaskCustomPatterns(bg(), task.ID, nil, nil); err != nil {
		t.Fatalf("UpdateTaskCustomPatterns clear: %v", err)
	}
	got, _ = s.GetTask(bg(), task.ID)
	if got.CustomPassPatterns != nil {
		t.Errorf("CustomPassPatterns should be nil after clearing, got %v", got.CustomPassPatterns)
	}
	if got.CustomFailPatterns != nil {
		t.Errorf("CustomFailPatterns should be nil after clearing, got %v", got.CustomFailPatterns)
	}
}

func TestUpdateTaskCustomPatterns_EmptySliceClearsField(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "pattern task", Timeout: 5})

	s.UpdateTaskCustomPatterns(bg(), task.ID, []string{"PASS"}, []string{"FAIL"}) //nolint:errcheck

	if err := s.UpdateTaskCustomPatterns(bg(), task.ID, []string{}, []string{}); err != nil {
		t.Fatalf("UpdateTaskCustomPatterns empty: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.CustomPassPatterns != nil {
		t.Errorf("CustomPassPatterns should be nil when empty slice provided, got %v", got.CustomPassPatterns)
	}
}

func TestUpdateTaskCustomPatterns_UnknownID(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTaskCustomPatterns(bg(), uuid.New(), nil, nil); err == nil {
		t.Error("expected error for unknown task ID")
	}
}

func TestArchiveAllDone_ArchivesDoneAndCancelled(t *testing.T) {
	s := newTestStore(t)

	done1, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "done 1", Timeout: 5})
	done2, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "done 2", Timeout: 5})
	cancelled, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "cancelled", Timeout: 5})
	backlog, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "still backlog", Timeout: 5})

	s.ForceUpdateTaskStatus(bg(), done1.ID, TaskStatusDone)          //nolint:errcheck
	s.ForceUpdateTaskStatus(bg(), done2.ID, TaskStatusDone)          //nolint:errcheck
	s.ForceUpdateTaskStatus(bg(), cancelled.ID, TaskStatusDone)      //nolint:errcheck
	s.ForceUpdateTaskStatus(bg(), cancelled.ID, TaskStatusCancelled) //nolint:errcheck

	archived, err := s.ArchiveAllDone(bg())
	if err != nil {
		t.Fatalf("ArchiveAllDone: %v", err)
	}
	if len(archived) != 3 {
		t.Errorf("expected 3 archived, got %d", len(archived))
	}

	got, _ := s.GetTask(bg(), backlog.ID)
	if got != nil && got.Archived {
		t.Error("backlog task should not be archived")
	}

	for _, id := range []uuid.UUID{done1.ID, done2.ID, cancelled.ID} {
		got, _ := s.GetTask(bg(), id)
		if got == nil || !got.Archived {
			t.Errorf("task %s should be archived", id)
		}
	}
}

func TestArchiveAllDone_SkipsAlreadyArchived(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "done", Timeout: 5})
	s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusDone) //nolint:errcheck
	s.SetTaskArchived(bg(), task.ID, true)                 //nolint:errcheck

	archived, err := s.ArchiveAllDone(bg())
	if err != nil {
		t.Fatalf("ArchiveAllDone: %v", err)
	}
	if len(archived) != 0 {
		t.Errorf("expected 0 newly archived, got %d", len(archived))
	}
}

func TestUpdateTaskPendingTestFeedback_SetAndClear(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "feedback task", Timeout: 5})

	if err := s.UpdateTaskPendingTestFeedback(bg(), task.ID, "tests failed: assert eq"); err != nil {
		t.Fatalf("UpdateTaskPendingTestFeedback set: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.PendingTestFeedback != "tests failed: assert eq" {
		t.Errorf("PendingTestFeedback = %q, want tests failed: assert eq", got.PendingTestFeedback)
	}

	if err := s.UpdateTaskPendingTestFeedback(bg(), task.ID, ""); err != nil {
		t.Fatalf("UpdateTaskPendingTestFeedback clear: %v", err)
	}
	got, _ = s.GetTask(bg(), task.ID)
	if got.PendingTestFeedback != "" {
		t.Errorf("PendingTestFeedback should be empty after clear, got %q", got.PendingTestFeedback)
	}
}

func TestUpdateTaskPendingTestFeedback_UnknownID(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTaskPendingTestFeedback(bg(), uuid.New(), "msg"); err == nil {
		t.Error("expected error for unknown task ID")
	}
}

func TestIncrementAutoRetryCount_BasicIncrement(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "retry task", Timeout: 5})

	if err := s.IncrementAutoRetryCount(bg(), task.ID, FailureCategoryTimeout); err != nil {
		t.Fatalf("IncrementAutoRetryCount: %v", err)
	}
	got, _ := s.GetTask(bg(), task.ID)
	if got.AutoRetryCount != 1 {
		t.Errorf("AutoRetryCount = %d, want 1", got.AutoRetryCount)
	}
}

func TestIncrementAutoRetryCount_DecrementsBudget(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "retry task", Timeout: 5})

	s.mu.Lock()
	s.tasks[task.ID].AutoRetryBudget = map[FailureCategory]int{
		FailureCategoryTimeout: 3,
	}
	s.mu.Unlock()

	if err := s.IncrementAutoRetryCount(bg(), task.ID, FailureCategoryTimeout); err != nil {
		t.Fatalf("IncrementAutoRetryCount: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.AutoRetryCount != 1 {
		t.Errorf("AutoRetryCount = %d, want 1", got.AutoRetryCount)
	}
	if got.AutoRetryBudget[FailureCategoryTimeout] != 2 {
		t.Errorf("AutoRetryBudget[timeout] = %d, want 2", got.AutoRetryBudget[FailureCategoryTimeout])
	}
}

func TestIncrementAutoRetryCount_BudgetFloorAtZero(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "retry task", Timeout: 5})

	s.mu.Lock()
	s.tasks[task.ID].AutoRetryBudget = map[FailureCategory]int{
		FailureCategoryTimeout: 0,
	}
	s.mu.Unlock()

	if err := s.IncrementAutoRetryCount(bg(), task.ID, FailureCategoryTimeout); err != nil {
		t.Fatalf("IncrementAutoRetryCount: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.AutoRetryBudget[FailureCategoryTimeout] != 0 {
		t.Errorf("AutoRetryBudget[timeout] = %d, want 0", got.AutoRetryBudget[FailureCategoryTimeout])
	}
}

func TestIncrementAutoRetryCount_InitializesNilBudget(t *testing.T) {
	s := newTestStore(t)
	task, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "retry task", Timeout: 5})

	if err := s.IncrementAutoRetryCount(bg(), task.ID, FailureCategoryAgentError); err != nil {
		t.Fatalf("IncrementAutoRetryCount: %v", err)
	}

	got, _ := s.GetTask(bg(), task.ID)
	if got.AutoRetryBudget == nil {
		t.Error("expected AutoRetryBudget to be initialized")
	}
}

func TestIncrementAutoRetryCount_UnknownID(t *testing.T) {
	s := newTestStore(t)
	if err := s.IncrementAutoRetryCount(bg(), uuid.New(), FailureCategoryTimeout); err == nil {
		t.Error("expected error for unknown task ID")
	}
}
