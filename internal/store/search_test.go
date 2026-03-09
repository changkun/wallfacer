package store

import (
	"fmt"
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
	// Also update the search index so SearchTasks picks up the change.
	s.mu.Lock()
	s.tasks[task.ID].Tags = []string{"frontend", "search-unique-tag"}
	s.searchIndex[task.ID] = buildIndexEntry(s.tasks[task.ID], s.searchIndex[task.ID].oversightRaw)
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

// --- Index consistency tests ---

// TestSearchIndex_UpdateTaskTitle verifies the search index is updated when a
// task's title changes via UpdateTaskTitle.
func TestSearchIndex_UpdateTaskTitle(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "some prompt", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Title is initially empty; searching for "new-title-xyz" should return nothing.
	results, err := s.SearchTasks(bg(), "new-title-xyz")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results before title update, got %d", len(results))
	}

	if err := s.UpdateTaskTitle(bg(), task.ID, "new-title-xyz"); err != nil {
		t.Fatalf("UpdateTaskTitle: %v", err)
	}

	results, err = s.SearchTasks(bg(), "new-title-xyz")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after title update, got %d", len(results))
	}
	if results[0].MatchedField != "title" {
		t.Errorf("expected matched_field=title, got %q", results[0].MatchedField)
	}
}

// TestSearchIndex_UpdateTaskBacklog verifies the search index is updated when a
// task's prompt changes via UpdateTaskBacklog.
func TestSearchIndex_UpdateTaskBacklog(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "original prompt content", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	newPrompt := "completely-different-backlog-prompt"
	if err := s.UpdateTaskBacklog(bg(), task.ID, &newPrompt, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("UpdateTaskBacklog: %v", err)
	}

	// Old prompt must not match.
	results, err := s.SearchTasks(bg(), "original prompt content")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for old prompt, got %d", len(results))
	}

	// New prompt must match.
	results, err = s.SearchTasks(bg(), "completely-different-backlog-prompt")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for new prompt, got %d", len(results))
	}
	if results[0].MatchedField != "prompt" {
		t.Errorf("expected matched_field=prompt, got %q", results[0].MatchedField)
	}
}

// TestSearchIndex_ApplyRefinement verifies the search index is updated when a
// task's prompt changes via ApplyRefinement.
func TestSearchIndex_ApplyRefinement(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "pre-refinement-prompt", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	session := RefinementSession{}
	if err := s.ApplyRefinement(bg(), task.ID, "post-refinement-prompt", session); err != nil {
		t.Fatalf("ApplyRefinement: %v", err)
	}

	// Old prompt must not match.
	results, err := s.SearchTasks(bg(), "pre-refinement-prompt")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for old prompt, got %d", len(results))
	}

	// New prompt must match.
	results, err = s.SearchTasks(bg(), "post-refinement-prompt")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for refined prompt, got %d", len(results))
	}
	if results[0].MatchedField != "prompt" {
		t.Errorf("expected matched_field=prompt, got %q", results[0].MatchedField)
	}
}

// TestSearchIndex_SaveOversight verifies the search index is updated when
// oversight is saved via SaveOversight without requiring a store restart.
func TestSearchIndex_SaveOversight(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "ordinary prompt", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Before saving oversight, the needle must not match.
	results, err := s.SearchTasks(bg(), "live-oversight-needle")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results before SaveOversight, got %d", len(results))
	}

	oversight := TaskOversight{
		Status:      OversightStatusReady,
		GeneratedAt: time.Now(),
		Phases: []OversightPhase{
			{Title: "Phase A", Summary: "live-oversight-needle present here"},
		},
	}
	if err := s.SaveOversight(task.ID, oversight); err != nil {
		t.Fatalf("SaveOversight: %v", err)
	}

	// After saving, SearchTasks must find it without a store restart.
	results, err = s.SearchTasks(bg(), "live-oversight-needle")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after SaveOversight, got %d", len(results))
	}
	if results[0].MatchedField != "oversight" {
		t.Errorf("expected matched_field=oversight, got %q", results[0].MatchedField)
	}
}

// TestSearchIndex_LoadAll verifies that the search index is populated from disk
// (including oversight) when a Store is opened against an existing data directory.
func TestSearchIndex_LoadAll(t *testing.T) {
	dir := t.TempDir()

	// Create a store, populate a task with oversight, then close it.
	s1, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	task, err := s1.CreateTask(bg(), "loadall-prompt", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	oversight := TaskOversight{
		Status:      OversightStatusReady,
		GeneratedAt: time.Now(),
		Phases: []OversightPhase{
			{Title: "Boot Phase", Summary: "loadall-oversight-needle here"},
		},
	}
	if err := s1.SaveOversight(task.ID, oversight); err != nil {
		t.Fatalf("SaveOversight: %v", err)
	}

	// Open a second Store against the same directory to simulate a server restart.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore (second): %v", err)
	}

	results, err := s2.SearchTasks(bg(), "loadall-oversight-needle")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after reload, got %d", len(results))
	}
	if results[0].MatchedField != "oversight" {
		t.Errorf("expected matched_field=oversight, got %q", results[0].MatchedField)
	}
}

// TestSearchIndex_DeleteTask verifies that deleting a task removes its entry
// from the search index.
func TestSearchIndex_DeleteTask(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "delete-me-needle", 60, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Confirm it's searchable before deletion.
	results, err := s.SearchTasks(bg(), "delete-me-needle")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result before delete, got %d", len(results))
	}

	if err := s.DeleteTask(bg(), task.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	results, err = s.SearchTasks(bg(), "delete-me-needle")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results after delete, got %d", len(results))
	}

	// Verify the index entry was removed from the map.
	s.mu.RLock()
	_, inIndex := s.searchIndex[task.ID]
	s.mu.RUnlock()
	if inIndex {
		t.Error("search index entry should have been removed on DeleteTask")
	}
}

// --- Benchmarks ---

// BenchmarkSearchTasks_Indexed measures SearchTasks using the in-memory index.
// Run with: go test -bench=BenchmarkSearchTasks -benchmem ./internal/store/
func BenchmarkSearchTasks_Indexed(b *testing.B) {
	s, err := NewStore(b.TempDir())
	if err != nil {
		b.Fatalf("NewStore: %v", err)
	}

	// Create 200 tasks, half with oversight, to simulate a loaded board.
	for i := 0; i < 200; i++ {
		task, err := s.CreateTask(bg(), fmt.Sprintf("benchmark task prompt number %d with various keywords", i), 60, false, "", TaskKindTask)
		if err != nil {
			b.Fatalf("CreateTask: %v", err)
		}
		if i%2 == 0 {
			oversight := TaskOversight{
				Status:      OversightStatusReady,
				GeneratedAt: time.Now(),
				Phases: []OversightPhase{
					{Title: fmt.Sprintf("Phase %d", i), Summary: fmt.Sprintf("oversight summary for task %d with searchable content", i)},
				},
			}
			if err := s.SaveOversight(task.ID, oversight); err != nil {
				b.Fatalf("SaveOversight: %v", err)
			}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.SearchTasks(bg(), "keywords"); err != nil {
			b.Fatalf("SearchTasks: %v", err)
		}
	}
}

// BenchmarkSearchTasks_OversightDisk measures the old search path that reads
// oversight.json from disk for every candidate on every query. It bypasses the
// index to serve as a regression baseline. Run alongside BenchmarkSearchTasks_Indexed.
func BenchmarkSearchTasks_OversightDisk(b *testing.B) {
	s, err := NewStore(b.TempDir())
	if err != nil {
		b.Fatalf("NewStore: %v", err)
	}

	// Same setup as the indexed benchmark.
	for i := 0; i < 200; i++ {
		task, err := s.CreateTask(bg(), fmt.Sprintf("benchmark task prompt number %d with various keywords", i), 60, false, "", TaskKindTask)
		if err != nil {
			b.Fatalf("CreateTask: %v", err)
		}
		if i%2 == 0 {
			oversight := TaskOversight{
				Status:      OversightStatusReady,
				GeneratedAt: time.Now(),
				Phases: []OversightPhase{
					{Title: fmt.Sprintf("Phase %d", i), Summary: fmt.Sprintf("oversight summary for task %d with searchable content", i)},
				},
			}
			if err := s.SaveOversight(task.ID, oversight); err != nil {
				b.Fatalf("SaveOversight: %v", err)
			}
		}
	}

	// oldMatchTask simulates the pre-index search path with per-query disk reads.
	oldMatchTask := func(t *Task) (field, snippet string, ok bool) {
		q := strings.ToLower("keywords")
		if idx := strings.Index(strings.ToLower(t.Title), q); idx != -1 {
			return "title", buildSnippet(t.Title, idx, len(q)), true
		}
		if idx := strings.Index(strings.ToLower(t.Prompt), q); idx != -1 {
			return "prompt", buildSnippet(t.Prompt, idx, len(q)), true
		}
		joined := strings.Join(t.Tags, " ")
		if idx := strings.Index(strings.ToLower(joined), q); idx != -1 {
			return "tags", buildSnippet(joined, idx, len(q)), true
		}
		if text, err := s.LoadOversightText(t.ID); err == nil && text != "" {
			if idx := strings.Index(strings.ToLower(text), q); idx != -1 {
				return "oversight", buildSnippet(text, idx, len(q)), true
			}
		}
		return "", "", false
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.mu.RLock()
		snapshot := make([]*Task, 0, len(s.tasks))
		for _, t := range s.tasks {
			cp := *t
			snapshot = append(snapshot, &cp)
		}
		s.mu.RUnlock()

		results := make([]TaskSearchResult, 0)
		for _, t := range snapshot {
			if len(results) >= maxSearchResults {
				break
			}
			if field, snippet, ok := oldMatchTask(t); ok {
				results = append(results, TaskSearchResult{Task: t, MatchedField: field, Snippet: snippet})
			}
		}
	}
}
