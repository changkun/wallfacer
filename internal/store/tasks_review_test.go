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

	// Task with session ID but ReviewUnresolved already set.
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
	if err := s.UpdateTaskReview(ctx, task.ID, 0, "", ""); err != nil {
		t.Fatalf("UpdateTaskReview: %v", err)
	}

	got := s.ListWaitingTasksWithSession(ctx)
	if len(got) != 0 {
		t.Errorf("expected 0 tasks after review already run, got %d", len(got))
	}
}

func TestClearReviewResult_MakesTaskReeligible(t *testing.T) {
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
	// Review ran: task is excluded from the eligible set.
	if err := s.UpdateTaskReview(ctx, task.ID, 2, "boom", "/sessions/1"); err != nil {
		t.Fatalf("UpdateTaskReview: %v", err)
	}
	if got := s.ListWaitingTasksWithSession(ctx); len(got) != 0 {
		t.Fatalf("expected 0 eligible while verdict set, got %d", len(got))
	}

	// On resume the verdict is cleared, making the task eligible again.
	if err := s.ClearReviewResult(ctx, task.ID); err != nil {
		t.Fatalf("ClearReviewResult: %v", err)
	}
	fresh, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if fresh.ReviewUnresolved != nil || fresh.ReviewHeadline != "" || fresh.ReviewSessionDir != "" {
		t.Errorf("review fields not cleared: unresolved=%v headline=%q dir=%q",
			fresh.ReviewUnresolved, fresh.ReviewHeadline, fresh.ReviewSessionDir)
	}
	if got := s.ListWaitingTasksWithSession(ctx); len(got) != 1 {
		t.Errorf("expected task re-eligible after clear, got %d", len(got))
	}
}

func TestUpdateTaskReview_PersistsAllFields(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "review-test", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := s.UpdateTaskReview(ctx, task.ID, 2, "Some attack claim", "/tmp/review/sessions/abc"); err != nil {
		t.Fatalf("UpdateTaskReview: %v", err)
	}

	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ReviewUnresolved == nil {
		t.Fatal("ReviewUnresolved is nil after UpdateTaskReview")
	}
	if *got.ReviewUnresolved != 2 {
		t.Errorf("ReviewUnresolved = %d, want 2", *got.ReviewUnresolved)
	}
	if got.ReviewHeadline != "Some attack claim" {
		t.Errorf("ReviewHeadline = %q, want %q", got.ReviewHeadline, "Some attack claim")
	}
	if got.ReviewSessionDir != "/tmp/review/sessions/abc" {
		t.Errorf("ReviewSessionDir = %q, want %q", got.ReviewSessionDir, "/tmp/review/sessions/abc")
	}
}

func TestUpdateTaskReview_ZeroUnresolved_IsClean(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "review-clean", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.UpdateTaskReview(ctx, task.ID, 0, "", "/sess"); err != nil {
		t.Fatalf("UpdateTaskReview: %v", err)
	}
	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ReviewUnresolved == nil || *got.ReviewUnresolved != 0 {
		t.Errorf("expected ReviewUnresolved=0 (clean), got %v", got.ReviewUnresolved)
	}
}
