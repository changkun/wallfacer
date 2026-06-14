package handler

import (
	"context"
	"testing"

	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
)

// TestSubmitAutoSubmitCandidate_SkipsStaleNonWaiting verifies that a candidate
// captured as waiting in Phase 1 but no longer waiting at submit time is not
// acted on. Before the fix the no-session branch used the stale Phase-1 snapshot
// and force-marked the task done via ForceUpdateTaskStatus, bypassing the state
// machine even though the task had already left waiting (e.g. user resumed it).
func TestSubmitAutoSubmitCandidate_SkipsStaleNonWaiting(t *testing.T) {
	h, s := newTestHandlerWithMockRunner(t, &runner.MockRunner{})
	ctx := context.Background()

	task, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "stale", Timeout: 15})
	_ = s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	repo := setupRepo(t)
	_ = s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: repo}, "task")

	// Snapshot the task while waiting (what Phase 1 would capture), no session.
	waiting, _ := s.GetTask(ctx, task.ID)
	cand := autoSubmitCandidate{task: *waiting, store: s, naturallyComplete: true}

	// The task leaves waiting before Phase 2 acts (e.g. user resume).
	_ = s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress)

	if h.submitAutoSubmitCandidate(ctx, cand) {
		t.Fatal("stale non-waiting candidate should not be submitted")
	}
	got, _ := s.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusInProgress {
		t.Fatalf("task should stay in_progress, got %s", got.Status)
	}
}

// TestSubmitAutoSubmitCandidate_NoSessionGoesToDone verifies the happy path: a
// still-waiting no-session candidate with present worktrees is submitted to done.
func TestSubmitAutoSubmitCandidate_NoSessionGoesToDone(t *testing.T) {
	h, s := newTestHandlerWithMockRunner(t, &runner.MockRunner{})
	ctx := context.Background()

	task, _ := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "done", Timeout: 15})
	_ = s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting)
	repo := setupRepo(t)
	_ = s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: repo}, "task")

	waiting, _ := s.GetTask(ctx, task.ID)
	cand := autoSubmitCandidate{task: *waiting, store: s, naturallyComplete: true}

	if !h.submitAutoSubmitCandidate(ctx, cand) {
		t.Fatal("eligible waiting candidate should be submitted")
	}
	got, _ := s.GetTask(ctx, task.ID)
	if got.Status != store.TaskStatusDone {
		t.Fatalf("task should be done, got %s", got.Status)
	}
}
