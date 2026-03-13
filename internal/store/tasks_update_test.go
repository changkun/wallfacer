package store

import (
	"testing"
	"time"
)

// TestUpdateTaskStatus_StartedAtSetOnFirstInProgress verifies that StartedAt is
// populated when a task transitions to in_progress for the first time.
func TestUpdateTaskStatus_StartedAtSetOnFirstInProgress(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "test task", 15, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.StartedAt != nil {
		t.Fatal("expected StartedAt to be nil after creation")
	}

	before := time.Now()
	if err := s.UpdateTaskStatus(bg(), task.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	after := time.Now()

	got, err := s.GetTask(bg(), task.ID)
	if err != nil || got == nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.StartedAt == nil {
		t.Fatal("expected StartedAt to be set after in_progress transition")
	}
	if got.StartedAt.Before(before) || got.StartedAt.After(after) {
		t.Errorf("StartedAt %v not in [%v, %v]", got.StartedAt, before, after)
	}
}

// TestUpdateTaskStatus_StartedAtNotOverwrittenOnSecondInProgress verifies that
// StartedAt is preserved across multiple in_progress transitions (e.g. resume cycles).
func TestUpdateTaskStatus_StartedAtNotOverwrittenOnSecondInProgress(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "test task", 15, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// First transition: backlog → in_progress sets StartedAt.
	if err := s.UpdateTaskStatus(bg(), task.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("first UpdateTaskStatus: %v", err)
	}
	first, err := s.GetTask(bg(), task.ID)
	if err != nil || first == nil || first.StartedAt == nil {
		t.Fatalf("expected StartedAt after first transition, err=%v", err)
	}
	originalStartedAt := *first.StartedAt

	// Move to waiting, then resume back to in_progress.
	if err := s.UpdateTaskStatus(bg(), task.ID, TaskStatusWaiting); err != nil {
		t.Fatalf("UpdateTaskStatus waiting: %v", err)
	}
	time.Sleep(5 * time.Millisecond) // ensure clock advances
	if err := s.UpdateTaskStatus(bg(), task.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("second UpdateTaskStatus in_progress: %v", err)
	}

	second, err := s.GetTask(bg(), task.ID)
	if err != nil || second == nil {
		t.Fatalf("GetTask: %v", err)
	}
	if second.StartedAt == nil {
		t.Fatal("StartedAt should not be nil after second in_progress transition")
	}
	if !second.StartedAt.Equal(originalStartedAt) {
		t.Errorf("StartedAt changed: got %v, want %v", second.StartedAt, originalStartedAt)
	}
}

// TestForceUpdateTaskStatus_StartedAtSetOnInProgress verifies that
// ForceUpdateTaskStatus also captures StartedAt on first in_progress.
func TestForceUpdateTaskStatus_StartedAtSetOnInProgress(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "test task", 15, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}

	got, err := s.GetTask(bg(), task.ID)
	if err != nil || got == nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.StartedAt == nil {
		t.Fatal("expected StartedAt to be set after ForceUpdateTaskStatus in_progress")
	}
}

// TestBuildAndSaveSummary_ExecutionDurationUsesStartedAt verifies that when
// StartedAt is set, ExecutionDurationSeconds reflects the active execution time
// (UpdatedAt - StartedAt) rather than wall-clock from creation.
func TestBuildAndSaveSummary_ExecutionDurationUsesStartedAt(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "timing test", 15, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Simulate idle time in backlog before execution begins.
	time.Sleep(20 * time.Millisecond)

	// Transition to in_progress (captures StartedAt).
	if err := s.UpdateTaskStatus(bg(), task.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus in_progress: %v", err)
	}

	// Simulate some execution time.
	time.Sleep(20 * time.Millisecond)

	// Force directly to done (writes summary); normal path goes via committing.
	if err := s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusDone); err != nil {
		t.Fatalf("ForceUpdateTaskStatus done: %v", err)
	}

	summary, err := s.LoadSummary(task.ID)
	if err != nil {
		t.Fatalf("LoadSummary: %v", err)
	}
	if summary == nil {
		t.Fatal("expected summary to exist after task done")
	}

	// ExecutionDurationSeconds should be shorter than DurationSeconds because
	// the task spent time in backlog before starting.
	if summary.ExecutionDurationSeconds >= summary.DurationSeconds {
		t.Errorf("expected ExecutionDurationSeconds (%v) < DurationSeconds (%v)",
			summary.ExecutionDurationSeconds, summary.DurationSeconds)
	}
	if summary.ExecutionDurationSeconds <= 0 {
		t.Errorf("expected ExecutionDurationSeconds > 0, got %v", summary.ExecutionDurationSeconds)
	}
}

// TestBuildAndSaveSummary_ExecutionDurationFallbackWithoutStartedAt verifies
// that old tasks without StartedAt fall back to DurationSeconds.
func TestBuildAndSaveSummary_ExecutionDurationFallbackWithoutStartedAt(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "no started_at task", 15, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Force directly to done without going through in_progress (simulates old task).
	if err := s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("ForceUpdateTaskStatus in_progress: %v", err)
	}

	// Manually clear StartedAt to simulate old data without it.
	s.mu.Lock()
	t2, ok := s.tasks[task.ID]
	if ok {
		t2.StartedAt = nil
		s.saveTask(task.ID, t2) //nolint:errcheck
	}
	s.mu.Unlock()

	if err := s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusDone); err != nil {
		t.Fatalf("ForceUpdateTaskStatus done: %v", err)
	}

	summary, err := s.LoadSummary(task.ID)
	if err != nil {
		t.Fatalf("LoadSummary: %v", err)
	}
	if summary == nil {
		t.Fatal("expected summary to exist")
	}

	// Without StartedAt, ExecutionDurationSeconds should equal DurationSeconds.
	if summary.ExecutionDurationSeconds != summary.DurationSeconds {
		t.Errorf("expected ExecutionDurationSeconds == DurationSeconds when StartedAt is nil, got %v vs %v",
			summary.ExecutionDurationSeconds, summary.DurationSeconds)
	}
}
