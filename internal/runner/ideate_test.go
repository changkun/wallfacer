package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/planner"
	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// testRunnerForPrompts returns a minimal Runner suitable for prompt-rendering
// tests that do not need a real store or container command.
func testRunnerForPrompts() *Runner {
	return &Runner{promptsMgr: prompts.Default}
}

// ideaOutput returns a stream-json result line whose "result" field contains
// a JSON array of ideas. The brainstorm agent must output this exact format.
func ideaOutput(ideas []IdeateResult) string {
	var items []string
	for _, idea := range ideas {
		cat := idea.Category
		if cat == "" {
			cat = "code quality / refactoring"
		}
		score := idea.ImpactScore
		if score == 0 {
			score = 80
		}
		items = append(items, fmt.Sprintf(`{"title":%q,"category":%q,"prompt":%q,"impact_score":%d}`, idea.Title, cat, idea.Prompt, score))
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
		{Title: "Add tests", Prompt: "Write unit tests for all handlers.", ImpactScore: 80},
		{Title: "Improve docs", Prompt: "Update the README with usage examples.", ImpactScore: 78},
		{Title: "Refactor auth", Prompt: "Move auth logic to a dedicated package.", ImpactScore: 85},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm", Timeout: 5, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
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
		{Title: "Add tests", Prompt: "Write unit tests for all handlers.", ImpactScore: 80},
		{Title: "Improve docs", Prompt: "Update the README with usage examples.", ImpactScore: 78},
		{Title: "Refactor auth", Prompt: "Move auth logic to a dedicated package.", ImpactScore: 85},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm", Timeout: 5, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
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
		{Title: "Add tests", Category: "test coverage", Prompt: "Write unit tests for all handlers.", ImpactScore: 80},
		{Title: "Improve docs", Category: "developer experience", Prompt: "Update the README with usage examples.", ImpactScore: 78},
		{Title: "Refactor auth", Category: "code quality / refactoring", Prompt: "Move auth logic to a dedicated package.", ImpactScore: 85},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm", Timeout: 5, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
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
		{Title: "Add tests", Prompt: "Write unit tests.", ImpactScore: 80},
		{Title: "Fix bugs", Prompt: "Fix known bugs.", ImpactScore: 85},
		{Title: "Improve perf", Prompt: "Optimise hot paths.", ImpactScore: 78},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm", Timeout: 5, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "", "", false)

	// The turn output file must exist after the task completes.
	outputsDir := filepath.Join(s.DataDir(), task.ID.String(), "outputs")
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
		{Title: "A", Prompt: "Do A.", ImpactScore: 80},
		{Title: "B", Prompt: "Do B.", ImpactScore: 78},
		{Title: "C", Prompt: "Do C.", ImpactScore: 75},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm", Timeout: 5, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
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
		{Title: "A", Prompt: "Do A.", ImpactScore: 80},
		{Title: "B", Prompt: "Do B.", ImpactScore: 78},
		{Title: "C", Prompt: "Do C.", ImpactScore: 75},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm", Timeout: 5, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
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
		{Title: "A", Prompt: "Do A.", ImpactScore: 80},
		{Title: "B", Prompt: "Do B.", ImpactScore: 78},
		{Title: "C", Prompt: "Do C.", ImpactScore: 75},
	}
	// Use a stateful command: first call is the brainstorm container (ideas),
	// second call is the oversight agent.
	brainstormOut := ideaOutput(ideas)
	oversightOut := `{"result":"{\"phases\":[{\"title\":\"Brainstorm\",\"summary\":\"Agent proposed ideas.\",\"tools_used\":[],\"actions\":[\"Proposed 3 ideas\"]}]}","session_id":"ov","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001}`
	cmd := fakeStatefulCmd(t, []string{brainstormOut, oversightOut})
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm", Timeout: 5, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
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

// TestIdeationTaskStoresActualPrompt verifies that the brainstorm task stores
// the full generated ideation prompt in ExecutionPrompt while keeping Prompt
// unchanged, and idea result tasks store their full implementation text in
// ExecutionPrompt.
func TestIdeationTaskStoresActualPrompt(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "Add tests", Prompt: "Write unit tests for all handlers.", ImpactScore: 80},
		{Title: "Improve docs", Prompt: "Update the README with usage examples.", ImpactScore: 78},
		{Title: "Refactor auth", Prompt: "Move auth logic to a dedicated package.", ImpactScore: 85},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	const staticPlaceholder = "Analyzes the workspace and proposes 3 actionable improvements."
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: staticPlaceholder, Timeout: 5, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "", "", false)

	// The brainstorm agent card keeps Prompt unchanged, but the full runtime
	// prompt must be stored in ExecutionPrompt for accurate display/debugging.
	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Prompt != staticPlaceholder {
		t.Fatalf("brainstorm card Prompt should remain as short placeholder, got: %q", updated.Prompt)
	}
	if updated.ExecutionPrompt == "" {
		t.Fatal("brainstorm card ExecutionPrompt should store the full runtime ideation prompt")
	}
	if !strings.Contains(updated.ExecutionPrompt, "Output ONLY a JSON array with exactly 3 objects") {
		t.Fatalf("brainstorm card ExecutionPrompt does not look like ideation prompt: %q", updated.ExecutionPrompt[:min(len(updated.ExecutionPrompt), 200)])
	}

	// Each created idea task must have its full implementation text in
	// ExecutionPrompt and only a short title in Prompt.
	allTasks, err := s.ListTasks(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var ideaTasks []store.Task
	for _, tt := range allTasks {
		if tt.ID != task.ID && tt.Kind != store.TaskKindIdeaAgent {
			for _, tag := range tt.Tags {
				if tag == "idea-agent" {
					ideaTasks = append(ideaTasks, tt)
					break
				}
			}
		}
	}
	if len(ideaTasks) == 0 {
		t.Fatal("no idea tasks were created")
	}
	for _, tt := range ideaTasks {
		if tt.ExecutionPrompt == "" {
			t.Errorf("idea task %q has empty ExecutionPrompt; full implementation text must be stored there", tt.Title)
		}
		if strings.Contains(tt.Prompt, "Suggested focus areas") {
			t.Errorf("idea task %q Prompt should not contain full ideation text; got: %q", tt.Title, tt.Prompt[:min(len(tt.Prompt), 200)])
		}
	}
}

// TestBuildIdeationPromptNoExistingTasks verifies that when there are no active
// tasks the prompt does not include the "Existing active tasks" section, and
// that it still contains suggested focus areas for the agent.
func TestBuildIdeationPromptNoExistingTasks(t *testing.T) {
	prompt := testRunnerForPrompts().buildIdeationPrompt(nil, "")
	if strings.Contains(prompt, "Existing active tasks") {
		t.Fatal("prompt should not mention existing tasks when none are provided")
	}
	if !strings.Contains(prompt, "Example focus areas") {
		t.Fatal("prompt must still include suggested focus areas")
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
	prompt := testRunnerForPrompts().buildIdeationPrompt(tasks, "")

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
	prompt := testRunnerForPrompts().buildIdeationPrompt(tasks, "")
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
	prompt := testRunnerForPrompts().buildIdeationPrompt(tasks, "")
	if !strings.Contains(prompt, "(untitled)") {
		t.Fatal("prompt must show '(untitled)' for tasks without a title")
	}
}

// TestIdeationTaskPromptIncludesExistingTasks verifies that when sibling tasks
// in backlog/in_progress/waiting exist, the brainstorm task's ExecutionPrompt
// includes the existing-task context and idea result tasks still store their
// full implementation text in ExecutionPrompt.
func TestIdeationTaskPromptIncludesExistingTasks(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "Add tests", Prompt: "Write unit tests for all handlers.", ImpactScore: 80},
		{Title: "Improve docs", Prompt: "Update the README with usage examples.", ImpactScore: 78},
		{Title: "Refactor auth", Prompt: "Move auth logic to a dedicated package.", ImpactScore: 85},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	// Pre-create sibling tasks in different active states.
	backlogTask, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Add dark mode toggle", Timeout: 10, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskTitle(ctx, backlogTask.ID, "Add dark mode"); err != nil {
		t.Fatal(err)
	}

	inProgressTask, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Fix login authentication bug", Timeout: 10, Kind: store.TaskKindTask})
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
	brainstormTask, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm", Timeout: 5, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, brainstormTask.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(brainstormTask.ID, "", "", false)

	// Prompt stays concise, but ExecutionPrompt should include sibling-task context.
	updated, err := s.GetTask(ctx, brainstormTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(updated.Prompt, "Existing active tasks") {
		t.Fatal("brainstorm card Prompt should stay concise")
	}
	if !strings.Contains(updated.ExecutionPrompt, "Existing active tasks") {
		t.Fatal("brainstorm card ExecutionPrompt should include full ideation context")
	}
	if !strings.Contains(updated.ExecutionPrompt, "Add dark mode") || !strings.Contains(updated.ExecutionPrompt, "Fix login bug") {
		t.Fatal("brainstorm card ExecutionPrompt missing existing task details")
	}

	// Verify that the buildIdeationPrompt function would include existing tasks
	// context (covered by TestBuildIdeationPromptIncludesActiveTasks unit test).
	// Here verify that created idea tasks store their full text in ExecutionPrompt.
	allTasks, err := s.ListTasks(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var ideaTasks []store.Task
	for _, tt := range allTasks {
		if tt.ID == brainstormTask.ID || tt.Kind == store.TaskKindIdeaAgent {
			continue
		}
		for _, tag := range tt.Tags {
			if tag == "idea-agent" {
				ideaTasks = append(ideaTasks, tt)
				break
			}
		}
	}
	if len(ideaTasks) == 0 {
		t.Fatal("no idea tasks were created")
	}
	for _, tt := range ideaTasks {
		if tt.ExecutionPrompt == "" {
			t.Errorf("idea task %q has empty ExecutionPrompt; full implementation text must be stored there", tt.Title)
		}
	}
}

// TestIdeationTaskExcludesDoneAndFailedFromContext verifies that tasks in done,
// failed, or cancelled states are NOT included in the brainstorm context — only
// backlog, in_progress, and waiting tasks are relevant.
func TestIdeationTaskExcludesDoneAndFailedFromContext(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "Add tests", Prompt: "Write unit tests.", ImpactScore: 80},
		{Title: "Improve docs", Prompt: "Update docs.", ImpactScore: 78},
		{Title: "Refactor auth", Prompt: "Refactor auth.", ImpactScore: 85},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	// Create tasks in terminal states — these should NOT appear in the prompt.
	doneTask, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Completed feature prompt", Timeout: 10, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskTitle(ctx, doneTask.ID, "Completed feature"); err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, doneTask.ID, store.TaskStatusDone); err != nil {
		t.Fatal(err)
	}

	failedTask, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Failed feature prompt", Timeout: 10, Kind: store.TaskKindTask})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskTitle(ctx, failedTask.ID, "Failed feature"); err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, failedTask.ID, store.TaskStatusFailed); err != nil {
		t.Fatal(err)
	}

	brainstormTask, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm", Timeout: 5, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, brainstormTask.ID, store.TaskStatusInProgress); err != nil {
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

// TestExtractIdeasAcceptsPromptEqualsTitle verifies that ideas where the prompt
// is identical to the title are accepted. With goal-focused ideation, a concise
// goal may legitimately match the title.
func TestExtractIdeasAcceptsPromptEqualsTitle(t *testing.T) {
	raw := `[
		{"title": "Batch Task Creation API",        "category": "backend / API",           "prompt": "Batch Task Creation API"},
		{"title": "Execution Environment Provenance","category": "observability / debugging","prompt": "Execution Environment Provenance"},
		{"title": "Scheduled Task Auto-Promotion",  "category": "product feature",          "prompt": "Scheduled Task Auto-Promotion"}
	]`
	ideas, rejections, err := extractIdeas(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ideas) != 3 {
		t.Fatalf("expected 3 ideas (prompt==title is accepted), got %d", len(ideas))
	}
	if len(rejections) != 0 {
		t.Errorf("expected 0 rejections, got %d", len(rejections))
	}
}

func TestExtractIdeasReturnsRejectionReasonsAndScores(t *testing.T) {
	raw := `[
		{"title": "Low impact", "category": "code quality", "prompt": "Improve lint rules", "impact_score": 40},
		{"title": "", "category": "test coverage", "prompt": "Write missing tests"},
		{"title": "Duplicate", "category": "backend / API", "prompt": "Refactor service", "impact_score": 90},
		{"title": "Duplicate", "category": "backend / API", "prompt": "Rework request validation", "impact_score": 95}
	]`
	ideas, rejections, err := extractIdeas(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "Low impact" is now accepted (impact score filtering removed — the agent
	// already self-critiques and ranks). Only empty-fields and duplicate are rejected.
	if len(ideas) != 2 {
		t.Fatalf("expected 2 valid ideas, got %d", len(ideas))
	}
	if len(rejections) != 2 {
		t.Fatalf("expected 2 rejections, got %d", len(rejections))
	}

	seen := map[ideaRejectReason]int{}
	for _, rej := range rejections {
		seen[rej.Reason]++
	}
	if seen[ideaRejectEmptyFields] != 1 {
		t.Fatalf("expected 1 empty-field rejection, got %d", seen[ideaRejectEmptyFields])
	}
	if seen[ideaRejectDuplicateTitle] != 1 {
		t.Fatalf("expected 1 duplicate-title rejection, got %d", seen[ideaRejectDuplicateTitle])
	}
}

func TestExtractIdeasFromRunOutputFallsBackToPreviousNDJSONResult(t *testing.T) {
	stream := strings.Join([]string{
		`{"result":"[{\"title\":\"Add tests\",\"category\":\"test quality\",\"prompt\":\"Write unit tests for all handlers.\",\"impact_score\":80}]","session_id":"ideate-sess","stop_reason":"","is_error":false,"total_cost_usd":0.002}`,
		`{"result":"The background exploration is complete and confirms all three findings. The output JSON was already delivered above.","session_id":"ideate-sess","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.002}`,
	}, "\n")

	ideas, _, err := extractIdeasFromRunOutput("", []byte(stream), nil)
	if err != nil {
		t.Fatalf("expected fallback to parse ideas from NDJSON stream, got: %v", err)
	}
	if len(ideas) != 1 {
		t.Fatalf("expected 1 idea from fallback output, got %d", len(ideas))
	}
	if ideas[0].Title != "Add tests" {
		t.Fatalf("expected fallback idea to be 'Add tests', got %q", ideas[0].Title)
	}
}

func TestExtractIdeasFromRunOutputReturnsErrorWhenNoArrayFound(t *testing.T) {
	stream := `{"result":"The background exploration is complete and no actions are needed.","session_id":"ideate-sess","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.002}`
	_, _, err := extractIdeasFromRunOutput("The background exploration is complete and no actions are needed.", []byte(stream), nil)
	if err == nil {
		t.Fatal("expected parse error when output contains no JSON array")
	}
}

// TestIdeationTaskSucceedsWhenPromptsEqualTitles verifies that when the
// brainstorm agent returns JSON where every prompt equals its title, the
// ideas are accepted and the task succeeds. Goal-focused prompts may
// legitimately be very concise, matching the title.
func TestIdeationTaskSucceedsWhenPromptsEqualTitles(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "Batch Task Creation API", Prompt: "Batch Task Creation API"},
		{Title: "Execution Environment Provenance", Prompt: "Execution Environment Provenance"},
		{Title: "Scheduled Task Auto-Promotion", Prompt: "Scheduled Task Auto-Promotion"},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm", Timeout: 5, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "", "", false)

	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	// With autosubmit enabled (the default), the task transitions through
	// waiting → committing → done. Without autosubmit it stops at waiting.
	// Either way, it must NOT be failed.
	if updated.Status == store.TaskStatusFailed {
		t.Fatalf("expected ideas to be accepted (prompt==title is allowed), but task failed")
	}
}

// TestIdeationTaskContainerErrorTransitionsToFailed verifies that when the
// brainstorm container fails (empty output, non-zero exit), the idea-agent
// task transitions to "failed".
func TestIdeationTaskContainerErrorTransitionsToFailed(t *testing.T) {
	cmd := fakeCmdScript(t, "", 1) // empty output, exit 1
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "brainstorm", Timeout: 5, Kind: store.TaskKindIdeaAgent})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
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

// ---------------------------------------------------------------------------
// repairTruncatedJSONArray
// ---------------------------------------------------------------------------

func TestRepairTruncatedJSONArray(t *testing.T) {
	// A minimal valid JSON object (field names match IdeateResult for clarity,
	// but the repair function is purely string-based and does not parse).
	objA := `{"title":"Add tests","category":"quality","prompt":"Write unit tests for all handlers.","impact_score":70}`

	tests := []struct {
		name  string
		text  string
		start int
		want  string // empty string means "no result expected"
	}{
		{
			name:  "complete valid array returns equivalent array",
			text:  "[" + objA + "]",
			start: 0,
			want:  "[" + objA + "]",
		},
		{
			name:  "truncated after first complete object returns single-element array",
			text:  "[" + objA + ",",
			start: 0,
			want:  "[" + objA + "]",
		},
		{
			name:  "truncated mid-second object returns first object only",
			text:  "[" + objA + `,{"title":"Fix bug","prompt":"Fix the nil-pointer`,
			start: 0,
			want:  "[" + objA + "]",
		},
		{
			name:  "string field containing braces tracks depth correctly",
			text:  `[{"title":"use {braces}","category":"quality","prompt":"Implement {feature} properly.","impact_score":70},{"title":"B"`,
			start: 0,
			want:  `[{"title":"use {braces}","category":"quality","prompt":"Implement {feature} properly.","impact_score":70}]`,
		},
		{
			name:  "no complete objects returns empty string",
			text:  `[{"title":"A","prompt":"Do`,
			start: 0,
			want:  "",
		},
		{
			name:  "object with nested sub-object tracks depth correctly",
			text:  `[{"title":"Foo","details":{"key":"val"},"category":"quality","prompt":"Implementation details.","impact_score":70}]`,
			start: 0,
			want:  `[{"title":"Foo","details":{"key":"val"},"category":"quality","prompt":"Implementation details.","impact_score":70}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repairTruncatedJSONArray(tt.text, tt.start)
			if got != tt.want {
				t.Errorf("repairTruncatedJSONArray(%q, %d)\n got  %q\n want %q",
					tt.text, tt.start, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseIdeaJSONArray
// ---------------------------------------------------------------------------

func TestParseIdeaJSONArray(t *testing.T) {
	// A valid JSON object whose fields pass all normalization filters.
	validObj := `{"title":"Add tests","category":"quality","prompt":"Write unit tests for all handlers.","impact_score":80}`
	validArray := "[" + validObj + "]"

	t.Run("input wrapped in json fence parsed correctly", func(t *testing.T) {
		fenced := "```json\n" + validArray + "\n```"
		results, _, err := parseIdeaJSONArray(fenced)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Title != "Add tests" {
			t.Errorf("expected title %q, got %q", "Add tests", results[0].Title)
		}
	})

	t.Run("input with text before and after array", func(t *testing.T) {
		wrapped := "Here are the ideas: " + validArray + " That's all."
		results, _, err := parseIdeaJSONArray(wrapped)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Title != "Add tests" {
			t.Errorf("expected title %q, got %q", "Add tests", results[0].Title)
		}
	})

	t.Run("truncated input triggers partial recovery and returns non-empty slice", func(t *testing.T) {
		truncated := "[" + validObj // no closing ]
		results, _, err := parseIdeaJSONArray(truncated)
		if err != nil {
			t.Fatalf("unexpected error from partial recovery: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected non-empty results from partial recovery")
		}
		if results[0].Title != "Add tests" {
			t.Errorf("expected title %q, got %q", "Add tests", results[0].Title)
		}
	})

	t.Run("empty string returns error", func(t *testing.T) {
		_, _, err := parseIdeaJSONArray("")
		if err == nil {
			t.Fatal("expected error for empty string input")
		}
	})
}

// ---------------------------------------------------------------------------
// IdeationHistory
// ---------------------------------------------------------------------------

// TestIdeationHistoryPersistence verifies that rejected entries written to a
// temp dir are returned by RejectedTitles after reloading via LoadHistory.
func TestIdeationHistoryPersistence(t *testing.T) {
	dir := t.TempDir()
	h, err := LoadHistory(dir)
	if err != nil {
		t.Fatal(err)
	}

	e1 := HistoryEntry{Title: "Alpha idea", Reason: "rejected_below_threshold"}
	e2 := HistoryEntry{Title: "Beta idea", Reason: "rejected_duplicate_title"}
	if err := h.Append(e1); err != nil {
		t.Fatal(err)
	}
	if err := h.Append(e2); err != nil {
		t.Fatal(err)
	}

	// Reload from disk.
	h2, err := LoadHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	titles := h2.RejectedTitles()
	if len(titles) != 2 {
		t.Fatalf("expected 2 rejected titles, got %d: %v", len(titles), titles)
	}
	found := map[string]bool{"Alpha idea": false, "Beta idea": false}
	for _, title := range titles {
		found[title] = true
	}
	for title, ok := range found {
		if !ok {
			t.Errorf("expected title %q in RejectedTitles", title)
		}
	}
}

// TestIdeationHistoryTTLExpiry verifies that entries older than the TTL are
// excluded from RejectedTitles after reloading.
func TestIdeationHistoryTTLExpiry(t *testing.T) {
	dir := t.TempDir()

	// Write an expired entry directly to the JSONL file.
	expired := HistoryEntry{
		Title:      "Old idea",
		Reason:     "rejected_below_threshold",
		RecordedAt: time.Now().Add(-(ideationHistoryTTL + time.Hour)),
	}
	fresh := HistoryEntry{
		Title:      "Fresh idea",
		Reason:     "rejected_duplicate_title",
		RecordedAt: time.Now().Add(-time.Hour),
	}

	h, err := LoadHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Append(expired); err != nil {
		t.Fatal(err)
	}
	if err := h.Append(fresh); err != nil {
		t.Fatal(err)
	}

	h2, err := LoadHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	titles := h2.RejectedTitles()
	for _, title := range titles {
		if title == "Old idea" {
			t.Error("expired entry should be excluded from RejectedTitles")
		}
	}
	found := false
	for _, title := range titles {
		if title == "Fresh idea" {
			found = true
		}
	}
	if !found {
		t.Error("fresh entry should be present in RejectedTitles")
	}
}

// TestIdeationHistoryTruncatedFile verifies that a partial (truncated) JSON
// line at the end of the file does not prevent LoadHistory from returning the
// valid preceding entries.
func TestIdeationHistoryTruncatedFile(t *testing.T) {
	dir := t.TempDir()

	h, err := LoadHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	e1 := HistoryEntry{Title: "Good entry", Reason: "rejected_below_threshold"}
	if err := h.Append(e1); err != nil {
		t.Fatal(err)
	}

	// Append a truncated (malformed) JSON line directly to the file.
	histPath := ideationHistoryPath(dir)
	f, err := os.OpenFile(histPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"title":"truncated`); err != nil {
		_ = f.Close()

		t.Fatal(err)
	}
	_ = f.Close()

	h2, err := LoadHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	titles := h2.RejectedTitles()
	if len(titles) != 1 {
		t.Fatalf("expected 1 valid entry after truncated line, got %d: %v", len(titles), titles)
	}
	if titles[0] != "Good entry" {
		t.Errorf("expected 'Good entry', got %q", titles[0])
	}
}

// TestBuildIdeationPromptIncludesRejectedTitles verifies that rejected titles
// loaded from history appear verbatim in the rendered ideation prompt string.
func TestBuildIdeationPromptIncludesRejectedTitles(t *testing.T) {
	dir := t.TempDir()

	// Pre-populate history with two rejected entries.
	h, err := LoadHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Append(HistoryEntry{Title: "Rejected Alpha", Reason: "rejected_below_threshold"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Append(HistoryEntry{Title: "Rejected Beta", Reason: "rejected_duplicate_title"}); err != nil {
		t.Fatal(err)
	}

	s, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	r := &Runner{store: s, promptsMgr: prompts.Default}
	prompt := r.buildIdeationPrompt(nil, "")

	if !strings.Contains(prompt, "Rejected Alpha") {
		t.Errorf("prompt should contain rejected title 'Rejected Alpha'; prompt excerpt: %q",
			prompt[:min(len(prompt), 300)])
	}
	if !strings.Contains(prompt, "Rejected Beta") {
		t.Errorf("prompt should contain rejected title 'Rejected Beta'; prompt excerpt: %q",
			prompt[:min(len(prompt), 300)])
	}
}

// ---------------------------------------------------------------------------
// Exploration/Exploitation ratio
// ---------------------------------------------------------------------------

func TestExploreScoreRange(t *testing.T) {
	// Verify exploreScore always returns values in [0, 1] across many rounds.
	for round := 0; round < 100; round++ {
		score := exploreScore(round)
		if score < 0 || score > 1 {
			t.Errorf("exploreScore(%d) = %f; want [0, 1]", round, score)
		}
	}
}

func TestModulateExploitRatio(t *testing.T) {
	// Verify the modulated ratio is always in [0, 1] and varies across rounds.
	seen := map[float64]struct{}{}
	for round := 0; round < 30; round++ {
		r := modulateExploitRatio(0.8, round)
		if r < 0 || r > 1 {
			t.Errorf("modulateExploitRatio(0.8, %d) = %f; want [0, 1]", round, r)
		}
		seen[r] = struct{}{}
	}
	// With jitter, we expect multiple distinct values across 30 rounds.
	if len(seen) < 3 {
		t.Errorf("expected variation across rounds; got only %d distinct values", len(seen))
	}
}

func TestBuildIdeationPromptIncludesExploitRatio(t *testing.T) {
	r := testRunnerForPrompts()
	r.ideationExploitRatioFn = func() float64 { return 0.7 }
	prompt := r.buildIdeationPrompt(nil, "")
	if !strings.Contains(prompt, "exploitation") {
		t.Error("prompt should contain exploitation/exploration guidance")
	}
}

// TestBuildIdeationPromptSurfacesUserFocus verifies that when a user
// types text into the composer's prompt field, that text is visible
// in the resulting ideation prompt so the agent biases its brainstorm
// toward the stated direction instead of silently ignoring it.
// Regression test for: "typing something then moving the brainstorm
// task to in_progress replaced my prompt with the agent's own" — the
// replacement was correct (the card stores the user hint, the agent
// receives the wrapped brainstorm template) but the user's text was
// being dropped on the floor.
func TestBuildIdeationPromptSurfacesUserFocus(t *testing.T) {
	r := testRunnerForPrompts()
	prompt := r.buildIdeationPrompt(nil, "focus on performance regressions")

	if !strings.Contains(prompt, "User focus") {
		t.Error("prompt must include a 'User focus' section when a hint is supplied")
	}
	if !strings.Contains(prompt, "focus on performance regressions") {
		t.Errorf("prompt must surface the user's literal hint; got: %q",
			prompt[:min(len(prompt), 400)])
	}
}

// TestBuildIdeationPromptOmitsUserFocusWhenEmpty verifies the focus
// section is absent when the user left the composer's prompt blank,
// so the agent scans the full workspace unbiased.
func TestBuildIdeationPromptOmitsUserFocusWhenEmpty(t *testing.T) {
	r := testRunnerForPrompts()
	prompt := r.buildIdeationPrompt(nil, "")

	if strings.Contains(prompt, "User focus") {
		t.Error("prompt must not include a 'User focus' section when the hint is empty")
	}
}

func TestIdeationHistoryRound(t *testing.T) {
	dir := t.TempDir()
	hist, err := LoadHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hist.Round() != 0 {
		t.Errorf("expected 0 rounds for empty history, got %d", hist.Round())
	}
	_ = hist.Append(HistoryEntry{Title: "A", Reason: "accepted", RecordedAt: time.Now()})
	_ = hist.Append(HistoryEntry{Title: "B", Reason: "rejected_threshold", RecordedAt: time.Now()})
	_ = hist.Append(HistoryEntry{Title: "C", Reason: "accepted", RecordedAt: time.Now()})

	hist2, err := LoadHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hist2.Round() != 2 {
		t.Errorf("expected 2 rounds, got %d", hist2.Round())
	}
}

// ---------------------------------------------------------------------------
// Planner-based ideation
// ---------------------------------------------------------------------------

// TestIdeationViaPlanner verifies that when a planner is set on the runner,
// RunIdeation routes through runIdeationViaPlanner and the planner is
// auto-started.
func TestIdeationViaPlanner(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "Add tests", Prompt: "Write unit tests.", ImpactScore: 80},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	// Create a planner backed by the same sandbox backend the runner uses.
	p := planner.New(planner.Config{
		Backend:     r.backend,
		Command:     r.command,
		Image:       r.sandboxImage,
		Workspaces:  r.workspaces,
		Fingerprint: "test-fp",
	})
	r.SetPlanner(p)

	// Planner is not started — auto-start should happen inside RunIdeation.
	if p.IsRunning() {
		t.Fatal("planner should not be running before RunIdeation")
	}

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt:  "brainstorm",
		Timeout: 5,
		Kind:    store.TaskKindIdeaAgent,
	})
	if err != nil {
		t.Fatal(err)
	}

	resultIdeas, _, _, _, _, err := r.RunIdeation(ctx, task.ID, "brainstorm prompt")
	if err != nil {
		t.Fatalf("RunIdeation via planner: %v", err)
	}

	if !p.IsRunning() {
		t.Error("planner should have been auto-started by RunIdeation")
	}

	if len(resultIdeas) != 1 {
		t.Fatalf("expected 1 idea, got %d", len(resultIdeas))
	}
	if resultIdeas[0].Title != "Add tests" {
		t.Errorf("idea title = %q, want %q", resultIdeas[0].Title, "Add tests")
	}
}

// TestIdeationViaPlannerCodexFallbackSkipped verifies that when running
// through the planner, Codex fallback is skipped (logged, not retried).
func TestIdeationViaPlannerCodexFallbackSkipped(t *testing.T) {
	// Simulate a token limit error output.
	tokenLimitOutput := `{"result":"Error: token limit exceeded","session_id":"ideate-sess","stop_reason":"end_turn","is_error":true,"subtype":"token_limit","total_cost_usd":0.001}`
	cmd := fakeCmdScript(t, tokenLimitOutput, 0)
	_, r := setupRunnerWithCmd(t, nil, cmd)

	p := planner.New(planner.Config{
		Backend:     r.backend,
		Command:     r.command,
		Image:       r.sandboxImage,
		Workspaces:  r.workspaces,
		Fingerprint: "test-fp",
	})
	r.SetPlanner(p)

	ctx := context.Background()
	// RunIdeation should not panic or retry with Codex — it should return
	// the error output directly. The token limit error makes extractIdeas
	// fail, which is expected.
	_, _, output, _, _, _ := r.RunIdeation(ctx, uuid.Nil, "brainstorm prompt")

	// If we got output, the planner path was used (no codex retry).
	if output != nil && !output.IsError {
		t.Error("expected error output from token-limited planner run")
	}
}

// TestIdeationFallsBackToEphemeralWithoutPlanner verifies that RunIdeation
// uses the ephemeral container path when no planner is set.
func TestIdeationFallsBackToEphemeralWithoutPlanner(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "Improve docs", Prompt: "Update README.", ImpactScore: 75},
	}
	cmd := fakeCmdScript(t, ideaOutput(ideas), 0)
	_, r := setupRunnerWithCmd(t, nil, cmd)
	// No planner set — should use ephemeral path.

	ctx := context.Background()
	resultIdeas, _, _, _, _, err := r.RunIdeation(ctx, uuid.Nil, "brainstorm prompt")
	if err != nil {
		t.Fatalf("RunIdeation ephemeral: %v", err)
	}
	if len(resultIdeas) != 1 {
		t.Fatalf("expected 1 idea, got %d", len(resultIdeas))
	}
	if resultIdeas[0].Title != "Improve docs" {
		t.Errorf("idea title = %q, want %q", resultIdeas[0].Title, "Improve docs")
	}
}
