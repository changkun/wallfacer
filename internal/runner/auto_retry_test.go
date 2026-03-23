package runner

import (
	"context"
	"testing"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/store"
)

// TestTryAutoRetry_BudgetAllows verifies that the runner's tryAutoRetry returns
// true and resets the task to backlog when the per-category budget is > 0 and
// the total auto-retry count is below constants.MaxAutoRetries.
func TestTryAutoRetry_BudgetAllows(t *testing.T) {
	s, r := setupTestRunner(t, nil)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "budget allows test", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	// Default: container_crash budget=2, AutoRetryCount=0 → should return true.
	got := r.tryAutoRetry(ctx, task.ID, store.FailureCategoryContainerCrash)
	if !got {
		t.Fatal("expected tryAutoRetry to return true (budget=2, count=0)")
	}

	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != store.TaskStatusBacklog {
		t.Errorf("status = %q, want backlog after successful retry", updated.Status)
	}
	if updated.AutoRetryCount != 1 {
		t.Errorf("AutoRetryCount = %d, want 1 after one successful retry", updated.AutoRetryCount)
	}
	if updated.AutoRetryBudget[store.FailureCategoryContainerCrash] != 1 {
		t.Errorf("container_crash budget = %d, want 1 (decremented from 2)",
			updated.AutoRetryBudget[store.FailureCategoryContainerCrash])
	}
}

// TestTryAutoRetry_TotalCapPreventsRetry verifies that the runner's
// tryAutoRetry returns false when AutoRetryCount >= constants.MaxAutoRetries,
// even when the per-category budget for the failing category is non-zero.
// This isolates the count guard from the budget guard.
func TestTryAutoRetry_TotalCapPreventsRetry(t *testing.T) {
	s, r := setupTestRunner(t, nil)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "total cap test", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	// Increment count to constants.MaxAutoRetries by spending the sync_error budget
	// so that the container_crash budget (default=2) remains untouched.
	// After this loop: AutoRetryCount=3, container_crash budget=2.
	for range constants.MaxAutoRetries {
		if err := s.IncrementAutoRetryCount(ctx, task.ID, store.FailureCategorySyncError); err != nil {
			t.Fatalf("IncrementAutoRetryCount: %v", err)
		}
	}

	pre, _ := s.GetTask(ctx, task.ID)
	if pre.AutoRetryCount != constants.MaxAutoRetries {
		t.Fatalf("setup: AutoRetryCount=%d, want %d", pre.AutoRetryCount, constants.MaxAutoRetries)
	}
	if pre.AutoRetryBudget[store.FailureCategoryContainerCrash] != 2 {
		t.Fatalf("setup: container_crash budget=%d, want 2 (should be untouched)",
			pre.AutoRetryBudget[store.FailureCategoryContainerCrash])
	}

	// With count=3 >= constants.MaxAutoRetries(3), must return false even though budget=2.
	got := r.tryAutoRetry(ctx, task.ID, store.FailureCategoryContainerCrash)
	if got {
		t.Error("expected tryAutoRetry to return false (total cap hit)")
	}

	// Count must not be incremented further.
	post, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if post.AutoRetryCount != constants.MaxAutoRetries {
		t.Errorf("AutoRetryCount = %d, want %d (unchanged)", post.AutoRetryCount, constants.MaxAutoRetries)
	}
}
