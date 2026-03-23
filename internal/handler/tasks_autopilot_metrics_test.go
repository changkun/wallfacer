package handler

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/metrics"
	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
)

// newTestHandlerWithRegistry creates a Handler backed by a temp-dir store,
// a minimal runner, and a real metrics registry for counter assertions.
func newTestHandlerWithRegistry(t *testing.T) (*Handler, *metrics.Registry) {
	t.Helper()
	// Use os.MkdirTemp instead of t.TempDir for the store directory so that
	// late trace-file writes from background goroutines don't cause TempDir
	// cleanup failures.
	storeDir, err := os.MkdirTemp("", "wallfacer-handler-test-*")
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.NewStore(storeDir)
	if err != nil {
		_ = os.RemoveAll(storeDir)

		t.Fatal(err)
	}
	r := runner.NewRunner(s, runner.RunnerConfig{})
	// Cleanups run LIFO: remove store dir last, after compaction and background work finish.
	t.Cleanup(func() { _ = os.RemoveAll(storeDir) })
	t.Cleanup(s.WaitCompaction)
	t.Cleanup(r.WaitBackground)
	t.Cleanup(r.Shutdown)

	reg := metrics.NewRegistry()
	// Pre-register the counter so it appears in exposition even before any
	// increments occur.
	reg.Counter(
		"wallfacer_autopilot_actions_total",
		"Total number of autonomous actions taken by autopilot watchers, by watcher and outcome.",
	)
	h := NewHandler(s, r, t.TempDir(), nil, reg)
	return h, reg
}

// autopilotCounterValue returns the current value of the
// wallfacer_autopilot_actions_total counter for the given watcher/outcome pair
// by parsing the Prometheus text exposition from the registry.
func autopilotCounterValue(t *testing.T, reg *metrics.Registry, watcher, outcome string) float64 {
	t.Helper()
	var sb strings.Builder
	reg.WritePrometheus(&sb)
	body := sb.String()

	// Prometheus text format labels are sorted alphabetically, so outcome comes
	// before watcher in the label set.
	target := fmt.Sprintf(`outcome="%s",watcher="%s"`, outcome, watcher)
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "wallfacer_autopilot_actions_total{") {
			continue
		}
		if !strings.Contains(line, target) {
			continue
		}
		// Line format: metric_name{labels} value
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		var v float64
		if _, err := fmt.Sscanf(parts[len(parts)-1], "%f", &v); err == nil {
			return v
		}
	}
	return 0
}

// TestAutoRetrySuppressedBudget verifies that when the per-category budget is
// zero the auto_retrier emits a suppressed_budget counter and does not promote.
func TestAutoRetrySuppressedBudget(t *testing.T) {
	h, reg := newTestHandlerWithRegistry(t)
	h.SetAutopilot(true)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "budget test", Timeout: 15, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus in_progress: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed); err != nil {
		t.Fatalf("ForceUpdateTaskStatus failed: %v", err)
	}
	if err := h.store.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatalf("SetTaskFailureCategory: %v", err)
	}

	// Drain the category budget by incrementing until it hits zero.
	for {
		snap, _ := h.store.GetTask(ctx, task.ID)
		if snap.AutoRetryBudget[store.FailureCategoryContainerCrash] <= 0 {
			break
		}
		if err := h.store.IncrementAutoRetryCount(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
			t.Fatalf("IncrementAutoRetryCount: %v", err)
		}
	}

	snap, _ := h.store.GetTask(ctx, task.ID)
	h.tryAutoRetry(ctx, *snap)

	if got := autopilotCounterValue(t, reg, "auto_retrier", "suppressed_budget"); got != 1 {
		t.Errorf("expected suppressed_budget=1, got %v", got)
	}
	after, _ := h.store.GetTask(ctx, task.ID)
	if after.Status != store.TaskStatusFailed {
		t.Errorf("expected task still failed, got %s", after.Status)
	}
}

// TestAutoRetrySuppressedMaxCount verifies that when the global retry count cap
// is reached the auto_retrier emits a suppressed_max_count counter and does not
// promote, even when the per-category budget is non-zero.
func TestAutoRetrySuppressedMaxCount(t *testing.T) {
	h, reg := newTestHandlerWithRegistry(t)
	h.SetAutopilot(true)
	ctx := context.Background()

	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "max count test", Timeout: 15, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := h.store.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateTaskStatus in_progress: %v", err)
	}
	if err := h.store.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed); err != nil {
		t.Fatalf("ForceUpdateTaskStatus failed: %v", err)
	}
	if err := h.store.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryContainerCrash); err != nil {
		t.Fatalf("SetTaskFailureCategory: %v", err)
	}

	// Increment AutoRetryCount to the global cap using a different category so
	// that the ContainerCrash budget (set as FailureCategory) remains non-zero.
	// This isolates the count-cap path from the budget-exhausted path.
	for i := 0; i < constants.MaxAutoRetries; i++ {
		if err := h.store.IncrementAutoRetryCount(ctx, task.ID, store.FailureCategorySyncError); err != nil {
			t.Fatalf("IncrementAutoRetryCount[%d]: %v", i, err)
		}
	}

	snap, _ := h.store.GetTask(ctx, task.ID)
	h.tryAutoRetry(ctx, *snap)

	if got := autopilotCounterValue(t, reg, "auto_retrier", "suppressed_max_count"); got != 1 {
		t.Errorf("expected suppressed_max_count=1, got %v", got)
	}
	after, _ := h.store.GetTask(ctx, task.ID)
	if after.Status != store.TaskStatusFailed {
		t.Errorf("expected task still failed, got %s", after.Status)
	}
}

// TestTryAutoPromote_PromotedCounterIncrements verifies that successfully
// promoting a backlog task increments the
// wallfacer_autopilot_actions_total{watcher="auto_promoter",outcome="promoted"}
// counter by exactly 1.
func TestTryAutoPromote_PromotedCounterIncrements(t *testing.T) {
	h, reg := newTestHandlerWithRegistry(t)
	h.SetAutopilot(true)
	ctx := context.Background()

	// Create a single backlog task.
	if _, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 30, Kind: store.TaskKindTask}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	h.tryAutoPromote(ctx)

	got := autopilotCounterValue(t, reg, "auto_promoter", "promoted")
	if got != 1 {
		t.Errorf("expected promoted counter = 1, got %v", got)
	}
}
