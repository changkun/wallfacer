package runner

import (
	"context"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// TestRunRefinement_NoopsAfterTaskReset verifies that a RunRefinement goroutine
// completing after ResetTaskForRetry correctly no-ops instead of writing stale
// results back to the task. This is the regression test for the bug where a
// background refinement could write the result from an old prompt onto a newly
// retried task because the guard `cur.CurrentRefinement == nil` was never true
// (ResetTaskForRetry did not clear it).
func TestRunRefinement_NoopsAfterTaskReset(t *testing.T) {
	refinementOutput := `{"result":"# Old spec\nsome old implementation plan","session_id":"s1","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001,"usage":{"input_tokens":10,"output_tokens":5}}`
	cmd := fakeCmdScript(t, refinementOutput, 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)

	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "original prompt", Timeout: 5})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Start a refinement job as the auto-refiner would.
	job := &store.RefinementJob{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
		Status:    store.RefinementJobStatusRunning,
		Source:    "auto",
	}
	if err := s.StartRefinementJobIfIdle(ctx, task.ID, job); err != nil {
		t.Fatalf("StartRefinementJobIfIdle: %v", err)
	}

	// Simulate the user retrying the task before the refinement goroutine
	// completes. This clears CurrentRefinement.
	if err := s.ResetTaskForRetry(ctx, task.ID, "new prompt", true); err != nil {
		t.Fatalf("ResetTaskForRetry: %v", err)
	}

	// Confirm CurrentRefinement is nil after reset.
	afterReset, _ := s.GetTask(ctx, task.ID)
	if afterReset.CurrentRefinement != nil {
		t.Fatal("pre-condition: expected CurrentRefinement == nil after ResetTaskForRetry")
	}

	// Now run RunRefinement synchronously (as if the goroutine just completed).
	// It should detect CurrentRefinement == nil and no-op.
	r.RunRefinement(task.ID, "")

	// The task must still have CurrentRefinement == nil — RunRefinement must
	// not have written back a stale result.
	got, _ := s.GetTask(ctx, task.ID)
	if got.CurrentRefinement != nil {
		t.Errorf("RunRefinement wrote stale result after task reset: CurrentRefinement=%+v", got.CurrentRefinement)
	}
}

// TestRunRefinementFallsBackToCodexOnTokenLimit drives the public
// RunRefinement flow with two fake responses — the first is a Claude
// token-limit signal, the second is a clean refinement from codex.
// After migration to runAgent the fallback still runs and the
// persisted usage record is attributed to codex + refinement.
func TestRunRefinementFallsBackToCodexOnTokenLimit(t *testing.T) {
	tokenLimit := `{"result":"rate limit exceeded: token limit reached","session_id":"s1","stop_reason":"end_turn","is_error":true,"total_cost_usd":0.001}`
	refinementOutput := `{"result":"# Goal\nShip it\n## Objective\nDetailed plan","session_id":"s2","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001,"usage":{"input_tokens":111,"output_tokens":22}}`
	cmd := fakeStatefulCmd(t, []string{tokenLimit, refinementOutput})
	s, r := setupRunnerWithCmd(t, nil, cmd)

	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "Refine this task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	// RunRefinement expects CurrentRefinement to be initialised so it
	// has somewhere to persist the result.
	if err := s.UpdateRefinementJob(context.Background(), task.ID, &store.RefinementJob{
		Status: store.RefinementJobStatusRunning,
	}); err != nil {
		t.Fatalf("UpdateRefinementJob: %v", err)
	}

	r.RunRefinement(task.ID, "")

	got, err := s.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.CurrentRefinement == nil {
		t.Fatal("expected CurrentRefinement set after refinement")
	}
	if got.CurrentRefinement.Status != store.RefinementJobStatusDone {
		t.Fatalf("status = %q, want done", got.CurrentRefinement.Status)
	}

	usages, err := s.GetTurnUsages(task.ID)
	if err != nil {
		t.Fatalf("GetTurnUsages: %v", err)
	}
	if len(usages) == 0 {
		t.Fatal("expected refinement usage record after fallback")
	}
	last := usages[len(usages)-1]
	if last.Sandbox != "codex" {
		t.Fatalf("expected refinement usage sandbox codex, got %q", last.Sandbox)
	}
	if last.SubAgent != "refinement" {
		t.Fatalf("expected refinement sub-agent, got %q", last.SubAgent)
	}
}
