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
		cat := idea.Category
		if cat == "" {
			cat = "code quality / refactoring"
		}
		items = append(items, fmt.Sprintf(`{"title":%q,"category":%q,"prompt":%q}`, idea.Title, cat, idea.Prompt))
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

// TestIdeationTaskTagsChildTasksWithCategory verifies that each child task
// created by the brainstorm agent is tagged with the idea's category so the
// category is visible on the task card in the UI.
func TestIdeationTaskTagsChildTasksWithCategory(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "Add tests", Category: "test coverage", Prompt: "Write unit tests for all handlers."},
		{Title: "Improve docs", Category: "developer experience", Prompt: "Update the README with usage examples."},
		{Title: "Refactor auth", Category: "code quality / refactoring", Prompt: "Move auth logic to a dedicated package."},
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

	// Build a map from child task title → tags for assertions.
	childByTitle := make(map[string][]string)
	for _, tsk := range allTasks {
		if tsk.ID == task.ID {
			continue
		}
		if tsk.HasTag("idea-agent") {
			childByTitle[tsk.Title] = tsk.Tags
		}
	}

	for _, idea := range ideas {
		tags, ok := childByTitle[idea.Title]
		if !ok {
			t.Errorf("child task %q not found", idea.Title)
			continue
		}
		found := false
		for _, tag := range tags {
			if tag == idea.Category {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("child task %q missing category tag %q; got tags: %v", idea.Title, idea.Category, tags)
		}
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

// TestIdeationTaskStoresActualPrompt verifies that the dynamically-generated
// ideation prompt (with domain categories) is persisted to task.Prompt so that
// the UI shows what was actually sent to the sandbox rather than the static
// placeholder used when the task card is created.
func TestIdeationTaskStoresActualPrompt(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "Add tests", Prompt: "Write unit tests for all handlers."},
		{Title: "Improve docs", Prompt: "Update the README with usage examples."},
		{Title: "Refactor auth", Prompt: "Move auth logic to a dedicated package."},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	const staticPlaceholder = "Analyzes the workspace and proposes 3 actionable improvements."
	task, err := s.CreateTask(ctx, staticPlaceholder, 5, false, "", store.TaskKindIdeaAgent)
	if err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "", "", false)

	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Prompt == staticPlaceholder {
		t.Fatal("task.Prompt was not updated: still contains the static placeholder instead of the actual ideation prompt")
	}
	if updated.Prompt == "" {
		t.Fatal("task.Prompt is empty after ideation run")
	}
	// The actual prompt must reference domain categories from the pool.
	if !strings.Contains(updated.Prompt, "domain:") {
		t.Fatalf("task.Prompt does not look like a generated ideation prompt: %q", updated.Prompt[:min(len(updated.Prompt), 200)])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestBuildIdeationPromptNoExistingTasks verifies that when there are no active
// tasks the prompt does not include the "Existing active tasks" section.
func TestBuildIdeationPromptNoExistingTasks(t *testing.T) {
	prompt := buildIdeationPrompt(nil)
	if strings.Contains(prompt, "Existing active tasks") {
		t.Fatal("prompt should not mention existing tasks when none are provided")
	}
	if !strings.Contains(prompt, "domain:") {
		t.Fatal("prompt must still include domain assignments")
	}
}

// TestBuildIdeationPromptIncludesActiveTasks verifies that task titles, statuses,
// and prompt excerpts are injected into the prompt when active tasks are provided.
func TestBuildIdeationPromptIncludesActiveTasks(t *testing.T) {
	tasks := []store.Task{
		{Title: "Add dark mode", Status: store.TaskStatusBacklog, Prompt: "Implement a dark mode toggle for the UI."},
		{Title: "Fix login bug", Status: store.TaskStatusInProgress, Prompt: "Resolve the authentication error on the login page."},
		{Title: "Write API docs", Status: store.TaskStatusWaiting, Prompt: "Document all REST endpoints."},
	}
	prompt := buildIdeationPrompt(tasks)

	if !strings.Contains(prompt, "Existing active tasks") {
		t.Fatal("prompt must include the 'Existing active tasks' section")
	}
	if !strings.Contains(prompt, "Add dark mode") {
		t.Fatal("prompt must include the title 'Add dark mode'")
	}
	if !strings.Contains(prompt, "status: backlog") {
		t.Fatal("prompt must include backlog status")
	}
	if !strings.Contains(prompt, "Fix login bug") {
		t.Fatal("prompt must include the title 'Fix login bug'")
	}
	if !strings.Contains(prompt, "status: in_progress") {
		t.Fatal("prompt must include in_progress status")
	}
	if !strings.Contains(prompt, "Write API docs") {
		t.Fatal("prompt must include the title 'Write API docs'")
	}
	if !strings.Contains(prompt, "status: waiting") {
		t.Fatal("prompt must include waiting status")
	}
	if !strings.Contains(prompt, "Non-duplicating") {
		t.Fatal("prompt must include the Non-duplicating requirement")
	}
}

// TestBuildIdeationPromptTruncatesLongPrompts verifies that task prompts longer
// than 120 characters are truncated with "..." to keep the context concise.
func TestBuildIdeationPromptTruncatesLongPrompts(t *testing.T) {
	longPrompt := strings.Repeat("x", 200)
	tasks := []store.Task{
		{Title: "Long task", Status: store.TaskStatusBacklog, Prompt: longPrompt},
	}
	prompt := buildIdeationPrompt(tasks)
	if strings.Contains(prompt, longPrompt) {
		t.Fatal("long prompt should be truncated in ideation context")
	}
	if !strings.Contains(prompt, "...") {
		t.Fatal("truncated prompt should end with '...'")
	}
}

// TestBuildIdeationPromptUntitledTask verifies that tasks without a title show
// "(untitled)" as a fallback so the agent still has context.
func TestBuildIdeationPromptUntitledTask(t *testing.T) {
	tasks := []store.Task{
		{Title: "", Status: store.TaskStatusBacklog, Prompt: "Some work."},
	}
	prompt := buildIdeationPrompt(tasks)
	if !strings.Contains(prompt, "(untitled)") {
		t.Fatal("prompt must show '(untitled)' for tasks without a title")
	}
}

// TestIdeationTaskPromptIncludesExistingTasks verifies that when sibling tasks
// in backlog/in_progress/waiting exist, the stored ideation prompt references them.
func TestIdeationTaskPromptIncludesExistingTasks(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "Add tests", Prompt: "Write unit tests for all handlers."},
		{Title: "Improve docs", Prompt: "Update the README with usage examples."},
		{Title: "Refactor auth", Prompt: "Move auth logic to a dedicated package."},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	// Pre-create sibling tasks in different active states.
	backlogTask, err := s.CreateTask(ctx, "Add dark mode toggle", 10, false, "", store.TaskKindTask)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskTitle(ctx, backlogTask.ID, "Add dark mode"); err != nil {
		t.Fatal(err)
	}

	inProgressTask, err := s.CreateTask(ctx, "Fix login authentication bug", 10, false, "", store.TaskKindTask)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskTitle(ctx, inProgressTask.ID, "Fix login bug"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, inProgressTask.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}

	// Create and run the brainstorm task.
	brainstormTask, err := s.CreateTask(ctx, "brainstorm", 5, false, "", store.TaskKindIdeaAgent)
	if err != nil {
		t.Fatal(err)
	}

	r.Run(brainstormTask.ID, "", "", false)

	updated, err := s.GetTask(ctx, brainstormTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated.Prompt, "Existing active tasks") {
		t.Fatal("ideation prompt must include 'Existing active tasks' when sibling tasks exist")
	}
	if !strings.Contains(updated.Prompt, "Add dark mode") {
		t.Fatalf("ideation prompt must reference the backlog task title; got: %q", updated.Prompt[:min(len(updated.Prompt), 400)])
	}
	if !strings.Contains(updated.Prompt, "Fix login bug") {
		t.Fatalf("ideation prompt must reference the in_progress task title; got: %q", updated.Prompt[:min(len(updated.Prompt), 400)])
	}
}

// TestIdeationTaskExcludesDoneAndFailedFromContext verifies that tasks in done,
// failed, or cancelled states are NOT included in the brainstorm context — only
// backlog, in_progress, and waiting tasks are relevant.
func TestIdeationTaskExcludesDoneAndFailedFromContext(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "Add tests", Prompt: "Write unit tests."},
		{Title: "Improve docs", Prompt: "Update docs."},
		{Title: "Refactor auth", Prompt: "Refactor auth."},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	// Create tasks in terminal states — these should NOT appear in the prompt.
	doneTask, err := s.CreateTask(ctx, "Completed feature prompt", 10, false, "", store.TaskKindTask)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskTitle(ctx, doneTask.ID, "Completed feature"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, doneTask.ID, store.TaskStatusDone); err != nil {
		t.Fatal(err)
	}

	failedTask, err := s.CreateTask(ctx, "Failed feature prompt", 10, false, "", store.TaskKindTask)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskTitle(ctx, failedTask.ID, "Failed feature"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, failedTask.ID, store.TaskStatusFailed); err != nil {
		t.Fatal(err)
	}

	brainstormTask, err := s.CreateTask(ctx, "brainstorm", 5, false, "", store.TaskKindIdeaAgent)
	if err != nil {
		t.Fatal(err)
	}

	r.Run(brainstormTask.ID, "", "", false)

	updated, err := s.GetTask(ctx, brainstormTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Neither done nor failed task titles should appear in the prompt.
	if strings.Contains(updated.Prompt, "Completed feature") {
		t.Fatal("done task should NOT appear in ideation context")
	}
	if strings.Contains(updated.Prompt, "Failed feature") {
		t.Fatal("failed task should NOT appear in ideation context")
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
