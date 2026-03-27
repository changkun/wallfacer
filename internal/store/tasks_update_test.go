package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestMutateTask_UpdatedAtPersistedAfterRefactoredMethod verifies that calling
// a refactored Update method exercises mutateTask: UpdatedAt is advanced and
// the change is visible both in-memory and after a fresh store reload from disk.
func TestMutateTask_UpdatedAtPersistedAfterRefactoredMethod(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "mutate test", Timeout: 15, Kind: TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	before := task.UpdatedAt

	// Ensure the clock advances so UpdatedAt will be strictly after before.
	time.Sleep(2 * time.Millisecond)

	if err := s.UpdateTaskPosition(bg(), task.ID, 42); err != nil {
		t.Fatalf("UpdateTaskPosition: %v", err)
	}

	got, err := s.GetTask(bg(), task.ID)
	if err != nil || got == nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !got.UpdatedAt.After(before) {
		t.Errorf("expected UpdatedAt to advance: got %v, want after %v", got.UpdatedAt, before)
	}
	if got.Position != 42 {
		t.Errorf("expected Position=42, got %d", got.Position)
	}

	// Verify persistence by loading through a fresh store backed by the same dir.
	s2, err := NewFileStore(s.dir)
	if err != nil {
		t.Fatalf("NewStore (reload): %v", err)
	}
	loaded, err := s2.GetTask(bg(), task.ID)
	if err != nil || loaded == nil {
		t.Fatalf("GetTask from reloaded store: %v", err)
	}
	if loaded.Position != 42 {
		t.Errorf("persisted Position: got %d, want 42", loaded.Position)
	}
	if !loaded.UpdatedAt.Equal(got.UpdatedAt) {
		t.Errorf("persisted UpdatedAt mismatch: got %v, want %v", loaded.UpdatedAt, got.UpdatedAt)
	}
}

// TestMutateTask_ErrorOnTaskNotFound verifies that mutateTask propagates a
// "task not found" error when the supplied ID does not exist.
func TestMutateTask_ErrorOnTaskNotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTaskPosition(bg(), uuid.New(), 1); err == nil {
		t.Fatal("expected error for non-existent task, got nil")
	}
}

// TestMutateTask_AbortOnFnError verifies that when fn returns an error the task
// is not saved and UpdatedAt is not changed.
func TestMutateTask_AbortOnFnError(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "abort test", Timeout: 15, Kind: TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	originalUpdatedAt := task.UpdatedAt

	time.Sleep(2 * time.Millisecond)

	// UpdateTaskBudget with a nil pointer → fn does nothing → succeeds.
	// We need a fn that actually returns an error.  Use mutateTask directly.
	callErr := fmt.Errorf("intentional fn error")
	err = s.mutateTask(task.ID, func(_ *Task) error { return callErr })
	if err != callErr {
		t.Fatalf("expected callErr back, got %v", err)
	}

	// UpdatedAt must not have changed.
	got, err := s.GetTask(bg(), task.ID)
	if err != nil || got == nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !got.UpdatedAt.Equal(originalUpdatedAt) {
		t.Errorf("UpdatedAt changed despite fn error: got %v, want %v", got.UpdatedAt, originalUpdatedAt)
	}
}

// TestUpdateTaskStatus_StartedAtSetOnFirstInProgress verifies that StartedAt is
// populated when a task transitions to in_progress for the first time.
func TestUpdateTaskStatus_StartedAtSetOnFirstInProgress(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "test task", Timeout: 15, Kind: TaskKindTask})
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
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "test task", Timeout: 15, Kind: TaskKindTask})
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
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "test task", Timeout: 15, Kind: TaskKindTask})
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
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "timing test", Timeout: 15, Kind: TaskKindTask})
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
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "no started_at task", Timeout: 15, Kind: TaskKindTask})
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

// TestResetTaskForRetry_ResetsAutoRetryCountAndBudget verifies that a manual
// retry (ResetTaskForRetry) fully resets AutoRetryCount and AutoRetryBudget so
// the auto-retrier is eligible to fire again after the reset.
func TestResetTaskForRetry_ResetsAutoRetryCountAndBudget(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "retry reset test", Timeout: 15, Kind: TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Confirm initial budget matches defaults.
	if task.AutoRetryCount != 0 {
		t.Fatalf("initial AutoRetryCount: got %d, want 0", task.AutoRetryCount)
	}
	if got := task.AutoRetryBudget[FailureCategoryContainerCrash]; got != defaultAutoRetryBudget[FailureCategoryContainerCrash] {
		t.Fatalf("initial AutoRetryBudget[ContainerCrash]: got %d, want %d", got, defaultAutoRetryBudget[FailureCategoryContainerCrash])
	}

	// Simulate three auto-retry increments (exhaust count cap).
	for i := 0; i < 3; i++ {
		if err := s.IncrementAutoRetryCount(ctx, task.ID, FailureCategoryContainerCrash); err != nil {
			t.Fatalf("IncrementAutoRetryCount[%d]: %v", i, err)
		}
	}

	// Transition through in_progress → failed so ResetTaskForRetry is valid.
	if err := s.UpdateTaskStatus(ctx, task.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus in_progress: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, TaskStatusFailed); err != nil {
		t.Fatalf("ForceUpdateTaskStatus failed: %v", err)
	}

	// Verify the counters are exhausted before reset.
	before, err := s.GetTask(ctx, task.ID)
	if err != nil || before == nil {
		t.Fatalf("GetTask before reset: %v", err)
	}
	if before.AutoRetryCount != 3 {
		t.Fatalf("AutoRetryCount before reset: got %d, want 3", before.AutoRetryCount)
	}
	// ContainerCrash budget decremented twice (budget was 2), now 0.
	if got := before.AutoRetryBudget[FailureCategoryContainerCrash]; got != 0 {
		t.Fatalf("AutoRetryBudget[ContainerCrash] before reset: got %d, want 0", got)
	}

	// Perform the manual retry reset.
	if err := s.ResetTaskForRetry(ctx, task.ID, task.Prompt, true); err != nil {
		t.Fatalf("ResetTaskForRetry: %v", err)
	}

	after, err := s.GetTask(ctx, task.ID)
	if err != nil || after == nil {
		t.Fatalf("GetTask after reset: %v", err)
	}

	// AutoRetryCount must be reset to 0.
	if after.AutoRetryCount != 0 {
		t.Errorf("AutoRetryCount after reset: got %d, want 0", after.AutoRetryCount)
	}

	// AutoRetryBudget must be fully restored to defaults.
	for cat, want := range defaultAutoRetryBudget {
		if got := after.AutoRetryBudget[cat]; got != want {
			t.Errorf("AutoRetryBudget[%s] after reset: got %d, want %d", cat, got, want)
		}
	}

	// After reset, the auto-retry eligibility check must pass:
	//   budget > 0 && count < maxHandlerAutoRetries(3)
	budget := after.AutoRetryBudget[FailureCategoryContainerCrash]
	if budget <= 0 || after.AutoRetryCount >= 3 {
		t.Errorf("task not eligible for auto-retry after reset: budget=%d count=%d", budget, after.AutoRetryCount)
	}
}

// TestResetTaskForRetry_ResetsAutoRetryCountAndBudget_Persisted verifies that
// the reset values survive a store reload from disk.
func TestResetTaskForRetry_ResetsAutoRetryCountAndBudget_Persisted(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "persist retry reset test", Timeout: 15, Kind: TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := s.IncrementAutoRetryCount(ctx, task.ID, FailureCategoryContainerCrash); err != nil {
		t.Fatalf("IncrementAutoRetryCount: %v", err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus in_progress: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, TaskStatusFailed); err != nil {
		t.Fatalf("ForceUpdateTaskStatus failed: %v", err)
	}
	if err := s.ResetTaskForRetry(ctx, task.ID, task.Prompt, true); err != nil {
		t.Fatalf("ResetTaskForRetry: %v", err)
	}

	// Reload from disk to verify persistence.
	s2, err := NewFileStore(s.dir)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	loaded, err := s2.GetTask(ctx, task.ID)
	if err != nil || loaded == nil {
		t.Fatalf("GetTask from reloaded store: %v", err)
	}
	if loaded.AutoRetryCount != 0 {
		t.Errorf("persisted AutoRetryCount: got %d, want 0", loaded.AutoRetryCount)
	}
	for cat, want := range defaultAutoRetryBudget {
		if got := loaded.AutoRetryBudget[cat]; got != want {
			t.Errorf("persisted AutoRetryBudget[%s]: got %d, want %d", cat, got, want)
		}
	}
}

// TestResetTaskForRetry_ClearsCurrentRefinement verifies that ResetTaskForRetry
// always clears CurrentRefinement regardless of the freshStart flag.
func TestResetTaskForRetry_ClearsCurrentRefinement(t *testing.T) {
	for _, freshStart := range []bool{true, false} {
		freshStart := freshStart
		t.Run(fmt.Sprintf("freshStart=%v", freshStart), func(t *testing.T) {
			s := newTestStore(t)
			task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "original prompt", Timeout: 15, Kind: TaskKindTask})
			if err != nil {
				t.Fatalf("CreateTask: %v", err)
			}

			// Simulate a running refinement job.
			job := &RefinementJob{
				ID:     "job-1",
				Status: RefinementJobStatusRunning,
			}
			if err := s.StartRefinementJobIfIdle(bg(), task.ID, job); err != nil {
				t.Fatalf("StartRefinementJobIfIdle: %v", err)
			}

			// Verify CurrentRefinement is set before reset.
			before, _ := s.GetTask(bg(), task.ID)
			if before.CurrentRefinement == nil {
				t.Fatal("expected CurrentRefinement to be set before reset")
			}

			if err := s.ResetTaskForRetry(bg(), task.ID, "new prompt", freshStart); err != nil {
				t.Fatalf("ResetTaskForRetry: %v", err)
			}

			got, _ := s.GetTask(bg(), task.ID)
			if got.CurrentRefinement != nil {
				t.Errorf("expected CurrentRefinement == nil after reset, got %+v", got.CurrentRefinement)
			}
		})
	}
}

// TestResetTaskForRetry_ClearsRefinementSessionsOnFreshStart verifies that
// RefineSessions is cleared when freshStart=true but preserved when freshStart=false.
func TestResetTaskForRetry_ClearsRefinementSessionsOnFreshStart(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "original prompt", Timeout: 15, Kind: TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Apply a refinement to populate RefineSessions.
	session := RefinementSession{
		ID:          "session-1",
		StartPrompt: "original prompt",
		Result:      "refined spec",
	}
	if err := s.ApplyRefinement(bg(), task.ID, "refined prompt", "", session); err != nil {
		t.Fatalf("ApplyRefinement: %v", err)
	}

	before, _ := s.GetTask(bg(), task.ID)
	if len(before.RefineSessions) == 0 {
		t.Fatal("expected RefineSessions to be non-empty after ApplyRefinement")
	}

	t.Run("freshStart=false preserves sessions", func(t *testing.T) {
		s2 := newTestStore(t)
		task2, _ := s2.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "original prompt", Timeout: 15, Kind: TaskKindTask})
		s2.ApplyRefinement(bg(), task2.ID, "refined prompt", "", session) //nolint:errcheck

		if err := s2.ResetTaskForRetry(bg(), task2.ID, "same prompt", false); err != nil {
			t.Fatalf("ResetTaskForRetry(freshStart=false): %v", err)
		}
		got, _ := s2.GetTask(bg(), task2.ID)
		if len(got.RefineSessions) == 0 {
			t.Error("expected RefineSessions to be preserved when freshStart=false")
		}
		if got.CurrentRefinement != nil {
			t.Error("expected CurrentRefinement to be nil regardless of freshStart")
		}
	})

	t.Run("freshStart=true clears sessions", func(t *testing.T) {
		if err := s.ResetTaskForRetry(bg(), task.ID, "new prompt", true); err != nil {
			t.Fatalf("ResetTaskForRetry(freshStart=true): %v", err)
		}
		got, _ := s.GetTask(bg(), task.ID)
		if len(got.RefineSessions) != 0 {
			t.Errorf("expected RefineSessions to be cleared when freshStart=true, got %d sessions", len(got.RefineSessions))
		}
		if got.CurrentRefinement != nil {
			t.Error("expected CurrentRefinement to be nil")
		}
	})
}
