package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// createTaskWithTitle is a helper that creates a task and sets its title.
func createTaskWithTitle(t *testing.T, s *Store, prompt, title string) *Task {
	t.Helper()
	task, err := s.CreateTask(bg(), prompt, 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.UpdateTaskTitle(bg(), task.ID, title); err != nil {
		t.Fatalf("UpdateTaskTitle: %v", err)
	}
	task.Title = title
	return task
}

func TestSearchTasks_MatchTitle(t *testing.T) {
	s := newTestStore(t)
	task := createTaskWithTitle(t, s, "some prompt text", "unique-title-xyz")

	results, err := s.SearchTasks(bg(), "unique-title-xyz")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != task.ID {
		t.Errorf("expected task %s, got %s", task.ID, results[0].ID)
	}
	if results[0].MatchedField != "title" {
		t.Errorf("expected matched_field=title, got %q", results[0].MatchedField)
	}
}

func TestSearchTasks_MatchPrompt(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "find-me-in-prompt unique content", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	results, err := s.SearchTasks(bg(), "find-me-in-prompt")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != task.ID {
		t.Errorf("expected task %s, got %s", task.ID, results[0].ID)
	}
	if results[0].MatchedField != "prompt" {
		t.Errorf("expected matched_field=prompt, got %q", results[0].MatchedField)
	}
}

func TestSearchTasks_MatchTags(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "irrelevant prompt", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Inject tags directly since CreateTask varargs are not exposed through handler.
	s.mu.Lock()
	s.tasks[task.ID].Tags = []string{"frontend", "search-unique-tag"}
	s.mu.Unlock()

	results, err := s.SearchTasks(bg(), "search-unique-tag")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != task.ID {
		t.Errorf("expected task %s, got %s", task.ID, results[0].ID)
	}
	if results[0].MatchedField != "tags" {
		t.Errorf("expected matched_field=tags, got %q", results[0].MatchedField)
	}
}

func TestSearchTasks_MatchOversight(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "ordinary prompt", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	oversight := TaskOversight{
		Status:      OversightStatusReady,
		GeneratedAt: time.Now(),
		Phases: []OversightPhase{
			{Title: "Setup Phase", Summary: "Configured the environment with oversight-needle-xyz settings"},
		},
	}
	if err := s.SaveOversight(task.ID, oversight); err != nil {
		t.Fatalf("SaveOversight: %v", err)
	}

	results, err := s.SearchTasks(bg(), "oversight-needle-xyz")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != task.ID {
		t.Errorf("expected task %s, got %s", task.ID, results[0].ID)
	}
	if results[0].MatchedField != "oversight" {
		t.Errorf("expected matched_field=oversight, got %q", results[0].MatchedField)
	}
}

func TestSearchTasks_NoMatch(t *testing.T) {
	s := newTestStore(t)
	_, err := s.CreateTask(bg(), "completely different content", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	results, err := s.SearchTasks(bg(), "zzznomatch999")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if results == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchTasks_MissingOversight(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "matchable-prompt-text", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// No oversight.json written — should fall back to prompt match, no error.
	results, err := s.SearchTasks(bg(), "matchable-prompt-text")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != task.ID {
		t.Errorf("expected task %s, got %s", task.ID, results[0].ID)
	}
	if results[0].MatchedField != "prompt" {
		t.Errorf("expected matched_field=prompt, got %q", results[0].MatchedField)
	}
}

func TestSearchTasks_SnippetTruncation(t *testing.T) {
	s := newTestStore(t)
	// Build a very long prompt with the match in the middle.
	left := strings.Repeat("a", 200)
	right := strings.Repeat("b", 200)
	longPrompt := left + "NEEDLE" + right
	_, err := s.CreateTask(bg(), longPrompt, 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	results, err := s.SearchTasks(bg(), "NEEDLE")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	snippet := results[0].Snippet
	// Snippet must be shorter than the full prompt (which is 406 chars).
	if len(snippet) >= len(longPrompt) {
		t.Errorf("expected snippet shorter than full prompt; got len=%d", len(snippet))
	}
	// Must still contain the match text (HTML-safe, no special chars here).
	if !strings.Contains(snippet, "NEEDLE") {
		t.Errorf("snippet does not contain match text: %q", snippet)
	}
	// Should have ellipsis on both sides.
	if !strings.Contains(snippet, "…") {
		t.Errorf("expected ellipsis in snippet: %q", snippet)
	}
}

func TestSearchTasks_CaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	_, err := s.CreateTask(bg(), "lowercase-needle content", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	results, err := s.SearchTasks(bg(), "LOWERCASE-NEEDLE")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].MatchedField != "prompt" {
		t.Errorf("expected matched_field=prompt, got %q", results[0].MatchedField)
	}
}

func TestSearchTasks_Cap(t *testing.T) {
	s := newTestStore(t)
	// Create 60 tasks that all match the query.
	for i := 0; i < 60; i++ {
		if _, err := s.CreateTask(bg(), "captest-match content", 60, false, "", TaskKindTask); err != nil {
			t.Fatalf("CreateTask %d: %v", i, err)
		}
	}

	results, err := s.SearchTasks(bg(), "captest-match")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) > maxSearchResults {
		t.Errorf("expected at most %d results, got %d", maxSearchResults, len(results))
	}
}

func TestLoadOversightText_Missing(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "some prompt", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	text, err := s.LoadOversightText(task.ID)
	if err != nil {
		t.Fatalf("LoadOversightText: unexpected error: %v", err)
	}
	if text != "" {
		t.Errorf("expected empty string for missing oversight, got %q", text)
	}
}

func TestLoadOversightText_Content(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "some prompt", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	oversight := TaskOversight{
		Status:      OversightStatusReady,
		GeneratedAt: time.Now(),
		Phases: []OversightPhase{
			{Title: "Phase One Title", Summary: "Phase one summary text"},
			{Title: "Phase Two Title", Summary: "Phase two summary text"},
		},
	}
	if err := s.SaveOversight(task.ID, oversight); err != nil {
		t.Fatalf("SaveOversight: %v", err)
	}

	text, err := s.LoadOversightText(task.ID)
	if err != nil {
		t.Fatalf("LoadOversightText: %v", err)
	}
	for _, want := range []string{"Phase One Title", "Phase one summary text", "Phase Two Title", "Phase two summary text"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in oversight text, got: %q", want, text)
		}
	}
}

func TestLoadOversightText_InvalidJSON(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "some prompt", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Write corrupt JSON directly to the oversight path.
	p := filepath.Join(s.dir, task.ID.String(), "oversight.json")
	if err := os.WriteFile(p, []byte("not-json{{{"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err = s.LoadOversightText(task.ID)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestSearchTasks_TitleBeatsPrompt(t *testing.T) {
	// When the query matches both title and prompt, title is reported.
	s := newTestStore(t)
	task := createTaskWithTitle(t, s, "shared-token in prompt too", "contains shared-token")

	results, err := s.SearchTasks(bg(), "shared-token")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != task.ID {
		t.Errorf("wrong task returned")
	}
	if results[0].MatchedField != "title" {
		t.Errorf("expected matched_field=title (cheapest wins), got %q", results[0].MatchedField)
	}
}

func TestSearchTasks_MatchOversightPhaseTitle(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "ordinary prompt", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	oversight := TaskOversight{
		Status:      OversightStatusReady,
		GeneratedAt: time.Now(),
		Phases: []OversightPhase{
			{Title: "phase-title-needle", Summary: "unrelated summary"},
		},
	}
	if err := s.SaveOversight(task.ID, oversight); err != nil {
		t.Fatalf("SaveOversight: %v", err)
	}

	results, err := s.SearchTasks(bg(), "phase-title-needle")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].MatchedField != "oversight" {
		t.Errorf("expected matched_field=oversight, got %q", results[0].MatchedField)
	}
}

func TestSearchTasks_SnippetHTMLEscaping(t *testing.T) {
	s := newTestStore(t)
	_, err := s.CreateTask(bg(), `prompt with <script>alert("xss")</script> content`, 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	results, err := s.SearchTasks(bg(), "script")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	snippet := results[0].Snippet
	// Raw < and > must not appear in the snippet.
	if strings.Contains(snippet, "<script>") {
		t.Errorf("snippet contains unescaped HTML: %q", snippet)
	}
	if !strings.Contains(snippet, "&lt;script&gt;") {
		t.Errorf("snippet missing HTML-escaped tag: %q", snippet)
	}
}

// TestSearchTasks_ArchiveIncluded verifies that archived tasks are returned.
func TestSearchTasks_ArchiveIncluded(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "archived-task-needle", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Force task to done so we can archive it.
	if err := s.ForceUpdateTaskStatus(bg(), task.ID, TaskStatusDone); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	if err := s.SetTaskArchived(bg(), task.ID, true); err != nil {
		t.Fatalf("SetTaskArchived: %v", err)
	}

	results, err := s.SearchTasks(bg(), "archived-task-needle")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (archived included), got %d", len(results))
	}
	if results[0].ID != task.ID {
		t.Errorf("expected task %s, got %s", task.ID, results[0].ID)
	}
}

// TestBuildSnippet_NoEllipsis verifies short source strings have no ellipsis.
func TestBuildSnippet_NoEllipsis(t *testing.T) {
	src := "hello needle world"
	idx := strings.Index(src, "needle")
	snippet := buildSnippet(src, idx, len("needle"))
	if strings.Contains(snippet, "…") {
		t.Errorf("short source should have no ellipsis, got: %q", snippet)
	}
	if !strings.Contains(snippet, "needle") {
		t.Errorf("snippet must contain the match: %q", snippet)
	}
}

