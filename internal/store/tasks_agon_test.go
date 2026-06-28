package store

import (
	"testing"
)

func TestListWaitingTasksWithSession_ReturnsEligible(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	// Create a waiting task with a SessionID.
	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "task-with-session", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	sid := "session-abc"
	if err := s.UpdateTaskResult(ctx, task.ID, "done", sid, "end_turn", 1); err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}

	got := s.ListWaitingTasksWithSession(ctx)
	if len(got) != 1 {
		t.Fatalf("expected 1 task, got %d", len(got))
	}
	if got[0].ID != task.ID {
		t.Errorf("expected task %s, got %s", task.ID, got[0].ID)
	}
}

func TestListWaitingTasksWithSession_ExcludesNoSession(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	// Task without session ID.
	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "no-session", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}

	got := s.ListWaitingTasksWithSession(ctx)
	if len(got) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(got))
	}
}

func TestListWaitingTasksWithSession_ExcludesAlreadyRun(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	// Task with session ID but AgonUnresolved already set.
	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "already-run", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	if err := s.UpdateTaskResult(ctx, task.ID, "done", "session-xyz", "end_turn", 1); err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}
	if err := s.UpdateTaskAgon(ctx, task.ID, 0, "", ""); err != nil {
		t.Fatalf("UpdateTaskAgon: %v", err)
	}

	got := s.ListWaitingTasksWithSession(ctx)
	if len(got) != 0 {
		t.Errorf("expected 0 tasks after agon already run, got %d", len(got))
	}
}

func TestClearAgonResult_MakesTaskReeligible(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "resumed", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	if err := s.UpdateTaskResult(ctx, task.ID, "done", "session-xyz", "end_turn", 1); err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}
	// Agon ran: task is excluded from the eligible set.
	if err := s.UpdateTaskAgon(ctx, task.ID, 2, "boom", "/sessions/1"); err != nil {
		t.Fatalf("UpdateTaskAgon: %v", err)
	}
	if got := s.ListWaitingTasksWithSession(ctx); len(got) != 0 {
		t.Fatalf("expected 0 eligible while verdict set, got %d", len(got))
	}

	// On resume the verdict is cleared, making the task eligible again.
	if err := s.ClearAgonResult(ctx, task.ID); err != nil {
		t.Fatalf("ClearAgonResult: %v", err)
	}
	fresh, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if fresh.AgonUnresolved != nil || fresh.AgonHeadline != "" || fresh.AgonSessionDir != "" {
		t.Errorf("agon fields not cleared: unresolved=%v headline=%q dir=%q",
			fresh.AgonUnresolved, fresh.AgonHeadline, fresh.AgonSessionDir)
	}
	if got := s.ListWaitingTasksWithSession(ctx); len(got) != 1 {
		t.Errorf("expected task re-eligible after clear, got %d", len(got))
	}
}

func TestAgonFeedback_SetClearReset(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "fb", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// First unresolved verdict: set feedback, count -> 1.
	if err := s.SetAgonFeedback(ctx, task.ID, "attack: nil deref"); err != nil {
		t.Fatalf("SetAgonFeedback: %v", err)
	}
	got, _ := s.GetTask(ctx, task.ID)
	if got.PendingAgonFeedback != "attack: nil deref" || got.AgonRetryCount != 1 {
		t.Fatalf("after set: feedback=%q count=%d", got.PendingAgonFeedback, got.AgonRetryCount)
	}

	// Second unresolved verdict: count -> 2, feedback replaced.
	if err := s.SetAgonFeedback(ctx, task.ID, "attack: race"); err != nil {
		t.Fatalf("SetAgonFeedback: %v", err)
	}
	got, _ = s.GetTask(ctx, task.ID)
	if got.AgonRetryCount != 2 {
		t.Errorf("count = %d, want 2", got.AgonRetryCount)
	}

	// Resume consumes the feedback but preserves the retry count.
	if err := s.ClearAgonResult(ctx, task.ID); err != nil {
		t.Fatalf("ClearAgonResult: %v", err)
	}
	got, _ = s.GetTask(ctx, task.ID)
	if got.PendingAgonFeedback != "" || got.AgonRetryCount != 2 {
		t.Errorf("after resume: feedback=%q count=%d, want empty + 2", got.PendingAgonFeedback, got.AgonRetryCount)
	}

	// A clean verdict resets the cycle.
	if err := s.ResetAgonRetry(ctx, task.ID); err != nil {
		t.Fatalf("ResetAgonRetry: %v", err)
	}
	got, _ = s.GetTask(ctx, task.ID)
	if got.PendingAgonFeedback != "" || got.AgonRetryCount != 0 {
		t.Errorf("after reset: feedback=%q count=%d, want empty + 0", got.PendingAgonFeedback, got.AgonRetryCount)
	}
}

func TestUpdateTaskAgon_PersistsAllFields(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "agon-test", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := s.UpdateTaskAgon(ctx, task.ID, 2, "Some attack claim", "/tmp/agon/sessions/abc"); err != nil {
		t.Fatalf("UpdateTaskAgon: %v", err)
	}

	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.AgonUnresolved == nil {
		t.Fatal("AgonUnresolved is nil after UpdateTaskAgon")
	}
	if *got.AgonUnresolved != 2 {
		t.Errorf("AgonUnresolved = %d, want 2", *got.AgonUnresolved)
	}
	if got.AgonHeadline != "Some attack claim" {
		t.Errorf("AgonHeadline = %q, want %q", got.AgonHeadline, "Some attack claim")
	}
	if got.AgonSessionDir != "/tmp/agon/sessions/abc" {
		t.Errorf("AgonSessionDir = %q, want %q", got.AgonSessionDir, "/tmp/agon/sessions/abc")
	}
}

func TestUpdateTaskAgon_ZeroUnresolved_IsClean(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "agon-clean", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.UpdateTaskAgon(ctx, task.ID, 0, "", "/sess"); err != nil {
		t.Fatalf("UpdateTaskAgon: %v", err)
	}
	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.AgonUnresolved == nil || *got.AgonUnresolved != 0 {
		t.Errorf("expected AgonUnresolved=0 (clean), got %v", got.AgonUnresolved)
	}
}
