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
