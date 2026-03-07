package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/wallfacer/internal/store"
)

// ideaOutput returns a stream-json result line whose "result" field contains
// a JSON array of ideas. The brainstorm agent must output this exact format.
func ideaOutput(ideas []IdeateResult) string {
	var items []string
	for _, idea := range ideas {
		items = append(items, fmt.Sprintf(`{"title":%q,"prompt":%q}`, idea.Title, idea.Prompt))
	}
	jsonArray := "[" + strings.Join(items, ",") + "]"
	// Escape the JSON array so it can be embedded inside the result field.
	escaped := strings.ReplaceAll(jsonArray, `"`, `\"`)
	return fmt.Sprintf(`{"result":"%s","session_id":"ideate-sess","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.002}`, escaped)
}

// ---------------------------------------------------------------------------
// runIdeationTask — state transitions
// ---------------------------------------------------------------------------

// TestIdeationTaskTransitionsToDone verifies that Run moves an idea-agent task
// to "done" when the brainstorm container exits successfully.
func TestIdeationTaskTransitionsToDone(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "Add tests", Prompt: "Write unit tests for all handlers."},
		{Title: "Improve docs", Prompt: "Update the README with usage examples."},
		{Title: "Refactor auth", Prompt: "Move auth logic to a dedicated package."},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "brainstorm", 5, false, "", store.TaskKindIdeaAgent)
	if err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "", "", false)

	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != store.TaskStatusDone {
		t.Fatalf("expected status=done, got %q", updated.Status)
	}
}

// TestIdeationTaskCreatesChildTasks verifies that Run creates backlog child
// tasks from the brainstorm results.
func TestIdeationTaskCreatesChildTasks(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "Add tests", Prompt: "Write unit tests for all handlers."},
		{Title: "Improve docs", Prompt: "Update the README with usage examples."},
		{Title: "Refactor auth", Prompt: "Move auth logic to a dedicated package."},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "brainstorm", 5, false, "", store.TaskKindIdeaAgent)
	if err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "", "", false)

	allTasks, err := s.ListTasks(ctx, false)
	if err != nil {
		t.Fatal(err)
	}

	// Count backlog tasks tagged "idea-agent" (the child tasks).
	var childTasks []store.Task
	for _, tsk := range allTasks {
		if tsk.ID == task.ID {
			continue
		}
		for _, tag := range tsk.Tags {
			if tag == "idea-agent" {
				childTasks = append(childTasks, tsk)
				break
			}
		}
	}

	if len(childTasks) != len(ideas) {
		t.Fatalf("expected %d child tasks, got %d", len(ideas), len(childTasks))
	}
}

// TestIdeationTaskSavesTurnOutput verifies that the raw container output is
// persisted as turn-0001.json so it can be inspected and used for oversight.
func TestIdeationTaskSavesTurnOutput(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "Add tests", Prompt: "Write unit tests."},
		{Title: "Fix bugs", Prompt: "Fix known bugs."},
		{Title: "Improve perf", Prompt: "Optimise hot paths."},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "brainstorm", 5, false, "", store.TaskKindIdeaAgent)
	if err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "", "", false)

	// The turn output file must exist after the task completes.
	outputsDir := s.OutputsDir(task.ID)
	turnFile := filepath.Join(outputsDir, "turn-0001.json")
	if _, statErr := os.Stat(turnFile); statErr != nil {
		t.Fatalf("turn-0001.json should exist after idea-agent run: %v", statErr)
	}

	content, err := os.ReadFile(turnFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) == 0 {
		t.Fatal("turn-0001.json should be non-empty")
	}
}

// TestIdeationTaskRecordsTurns verifies that the task's Turns counter is set
// to 1 after the brainstorm agent completes. This is required for oversight
// generation (which skips tasks with Turns==0).
func TestIdeationTaskRecordsTurns(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "A", Prompt: "Do A."},
		{Title: "B", Prompt: "Do B."},
		{Title: "C", Prompt: "Do C."},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "brainstorm", 5, false, "", store.TaskKindIdeaAgent)
	if err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "", "", false)

	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Turns != 1 {
		t.Fatalf("expected Turns=1 after idea-agent run, got %d", updated.Turns)
	}
}

// TestIdeationTaskEmitsOutputEvent verifies that an EventTypeOutput event is
// recorded after the brainstorm container finishes. This mirrors the behaviour
// of regular implementation tasks and enables the event timeline to work.
func TestIdeationTaskEmitsOutputEvent(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "A", Prompt: "Do A."},
		{Title: "B", Prompt: "Do B."},
		{Title: "C", Prompt: "Do C."},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "brainstorm", 5, false, "", store.TaskKindIdeaAgent)
	if err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "", "", false)

	events, err := s.GetEvents(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range events {
		if ev.EventType == store.EventTypeOutput {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected at least one EventTypeOutput event after idea-agent run")
	}
}

// TestIdeationTaskOversightGeneratedAfterDone verifies that oversight is
// triggered (in background) when the idea-agent task transitions to done,
// so that the Oversight tab shows content instead of "no data".
func TestIdeationTaskOversightGeneratedAfterDone(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "A", Prompt: "Do A."},
		{Title: "B", Prompt: "Do B."},
		{Title: "C", Prompt: "Do C."},
	}
	// Use a stateful command: first call is the brainstorm container (ideas),
	// second call is the oversight agent.
	brainstormOut := ideaOutput(ideas)
	oversightOut := `{"result":"{\"phases\":[{\"title\":\"Brainstorm\",\"summary\":\"Agent proposed ideas.\",\"tools_used\":[],\"actions\":[\"Proposed 3 ideas\"]}]}","session_id":"ov","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001}`
	cmd := fakeStatefulCmd(t, []string{brainstormOut, oversightOut})
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "brainstorm", 5, false, "", store.TaskKindIdeaAgent)
	if err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "", "", false)
	// Wait for the background oversight goroutine to finish.
	r.WaitBackground()

	oversight, err := s.GetOversight(task.ID)
	if err != nil {
		t.Fatalf("unexpected error reading oversight: %v", err)
	}
	// Oversight must be in a terminal state (ready or failed), NOT pending or generating.
	if oversight.Status == store.OversightStatusPending || oversight.Status == store.OversightStatusGenerating {
		t.Fatalf("oversight should be in terminal state after idea-agent done, got %q", oversight.Status)
	}
}

// TestIdeationTaskContainerErrorTransitionsToFailed verifies that when the
// brainstorm container fails (empty output, non-zero exit), the idea-agent
// task transitions to "failed".
func TestIdeationTaskContainerErrorTransitionsToFailed(t *testing.T) {
	cmd := fakeCmdScript(t, "", 1) // empty output, exit 1
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "brainstorm", 5, false, "", store.TaskKindIdeaAgent)
	if err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "", "", false)

	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != store.TaskStatusFailed {
		t.Fatalf("expected status=failed on container error, got %q", updated.Status)
	}
}
