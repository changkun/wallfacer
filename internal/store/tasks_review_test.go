package store

import (
	"testing"
)

// waitingTaskWithWorktree creates a waiting task with one worktree, the
// minimum a review reads.
func waitingTaskWithWorktree(t *testing.T, s *Store, prompt string) *Task {
	t.Helper()
	ctx := bg()
	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: prompt, Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{"/repo": "/wt/repo"}, "task/x"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}
	return task
}

// TestListWaitingTasksForReview_ReturnsEligible: a waiting task with a
// worktree is due a review whether or not its harness reported a session.
func TestListWaitingTasksForReview_ReturnsEligible(t *testing.T) {
	s := newTestStore(t)
	task := waitingTaskWithWorktree(t, s, "no-session")

	got := s.ListWaitingTasksForReview(bg())
	if len(got) != 1 {
		t.Fatalf("expected 1 task, got %d", len(got))
	}
	if got[0].ID != task.ID {
		t.Errorf("expected task %s, got %s", task.ID, got[0].ID)
	}
}

// TestListWaitingTasksForReview_ExcludesNoWorktree: a task without a worktree
// has no diff to review.
func TestListWaitingTasksForReview_ExcludesNoWorktree(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "no-worktree", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, TaskStatusWaiting); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}

	got := s.ListWaitingTasksForReview(ctx)
	if len(got) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(got))
	}
}

func TestListWaitingTasksForReview_ExcludesAlreadyRun(t *testing.T) {
	s := newTestStore(t)
	task := waitingTaskWithWorktree(t, s, "already-run")
	if err := s.UpdateTaskReview(bg(), task.ID, 0, "", ""); err != nil {
		t.Fatalf("UpdateTaskReview: %v", err)
	}

	got := s.ListWaitingTasksForReview(bg())
	if len(got) != 0 {
		t.Errorf("expected 0 tasks after review already run, got %d", len(got))
	}
}

func TestClearReviewResult_MakesTaskReeligible(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task := waitingTaskWithWorktree(t, s, "resumed")
	// Review ran: task is excluded from the eligible set.
	if err := s.UpdateTaskReview(ctx, task.ID, 2, "boom", "/sessions/1"); err != nil {
		t.Fatalf("UpdateTaskReview: %v", err)
	}
	if got := s.ListWaitingTasksForReview(ctx); len(got) != 0 {
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
	if got := s.ListWaitingTasksForReview(ctx); len(got) != 1 {
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
