package handler

import (
	"context"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/store"
)

// TestScheduledTaskPromotedOnTime verifies that a backlog task with a
// ScheduledAt ~150ms in the future is NOT promoted immediately, but IS promoted
// shortly after its due time via the precise timer set by ensureScheduledPromoteTrigger.
func TestScheduledTaskPromotedOnTime(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	h.autopilotMu.Lock()
	h.autopilot = true
	h.autopilotMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a backlog task scheduled 150ms from now.
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "scheduled soon", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().Add(150 * time.Millisecond)
	if err := h.store.UpdateTaskScheduledAt(ctx, task.ID, &due); err != nil {
		t.Fatalf("UpdateTaskScheduledAt: %v", err)
	}

	// First call: task is not yet due, should stay in backlog.
	// This also arms the precise timer.
	h.tryAutoPromote(ctx)

	got, err := h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TaskStatusBacklog {
		t.Fatalf("expected task to remain in backlog before due time, got %s", got.Status)
	}

	// Wait until past the due time (generous margin so CI is not flaky).
	time.Sleep(400 * time.Millisecond)

	// The timer should have fired and called tryAutoPromote by now.
	// The task must have left the backlog — it will be in_progress or further
	// along (failed/done) because the test runner has no real container.
	got, err = h.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == store.TaskStatusBacklog {
		t.Errorf("expected task to be promoted after due time via timer, but still in backlog")
	}
}

// TestScheduledTaskTimerCancelsOnContextDone verifies that cancelling the
// context prevents the timer from promoting a scheduled task.
func TestScheduledTaskTimerCancelsOnContextDone(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	h.autopilotMu.Lock()
	h.autopilot = true
	h.autopilotMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())

	// Create a backlog task scheduled 200ms from now.
	task, err := h.store.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "cancel test", Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().Add(200 * time.Millisecond)
	if err := h.store.UpdateTaskScheduledAt(ctx, task.ID, &due); err != nil {
		t.Fatalf("UpdateTaskScheduledAt: %v", err)
	}

	// Arm the timer.
	h.tryAutoPromote(ctx)

	// Cancel the context and stop the timer manually (as StartAutoPromoter would).
	cancel()
	h.scheduledPromoteMu.Lock()
	if h.scheduledPromoteTimer != nil {
		h.scheduledPromoteTimer.Stop()
		h.scheduledPromoteTimer = nil
	}
	h.scheduledPromoteMu.Unlock()

	// Wait past the due time.
	time.Sleep(350 * time.Millisecond)

	// Task must still be in backlog because the timer was stopped.
	got, err := h.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TaskStatusBacklog {
		t.Errorf("expected task to remain in backlog after timer cancellation, got %s", got.Status)
	}
}

// TestEnsureScheduledPromoteTrigger_AlreadyDue verifies that
// ensureScheduledPromoteTrigger is a no-op when the due time is in the past,
// leaving any existing timer untouched.
func TestEnsureScheduledPromoteTrigger_AlreadyDue(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	ctx := context.Background()

	// Past due time should not set a timer.
	h.ensureScheduledPromoteTrigger(ctx, time.Now().Add(-1*time.Second))

	h.scheduledPromoteMu.Lock()
	timerSet := h.scheduledPromoteTimer != nil
	h.scheduledPromoteMu.Unlock()

	if timerSet {
		t.Error("expected no timer to be set for a past due time")
	}
}

// TestEnsureScheduledPromoteTrigger_ReplacesLaterTimer verifies that setting a
// sooner due time replaces an existing timer with a later deadline.
func TestEnsureScheduledPromoteTrigger_ReplacesLaterTimer(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)
	ctx := context.Background()

	// Arm a timer 10 seconds out.
	h.ensureScheduledPromoteTrigger(ctx, time.Now().Add(10*time.Second))
	h.scheduledPromoteMu.Lock()
	first := h.scheduledPromoteTimer
	h.scheduledPromoteMu.Unlock()
	if first == nil {
		t.Fatal("expected timer to be set")
	}

	// Replace with a sooner timer (500ms).
	h.ensureScheduledPromoteTrigger(ctx, time.Now().Add(500*time.Millisecond))
	h.scheduledPromoteMu.Lock()
	second := h.scheduledPromoteTimer
	h.scheduledPromoteMu.Unlock()

	if second == nil {
		t.Fatal("expected replacement timer to be set")
	}
	if second == first {
		t.Error("expected a new timer instance after replacement")
	}

	// Clean up.
	h.scheduledPromoteMu.Lock()
	if h.scheduledPromoteTimer != nil {
		h.scheduledPromoteTimer.Stop()
	}
	h.scheduledPromoteMu.Unlock()
}
