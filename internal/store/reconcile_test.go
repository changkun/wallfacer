package store

import (
	"testing"
	"time"
)

// TestRebuildSearchIndex verifies RebuildSearchIndex restores missing entries
// and is idempotent when the index is already up to date.
func TestRebuildSearchIndex(t *testing.T) {
	s := newTestStore(t)

	// Create a task with oversight so both prompt and oversight are indexed.
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "rebuild-index-prompt-needle", Timeout: 60, Kind: TaskKindTask})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	oversight := TaskOversight{
		Status:      OversightStatusReady,
		GeneratedAt: time.Now(),
		Phases: []OversightPhase{
			{Title: "Rebuild Phase", Summary: "rebuild-index-oversight-needle"},
		},
	}
	if err := s.SaveOversight(task.ID, oversight); err != nil {
		t.Fatalf("SaveOversight: %v", err)
	}

	// Step 1: task must be findable before we corrupt the index.
	results, err := s.SearchTasks(bg(), "rebuild-index-prompt-needle")
	if err != nil {
		t.Fatalf("SearchTasks before corruption: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result before corruption, got %d", len(results))
	}

	// Step 2: directly remove the entry from the search index to simulate corruption.
	s.mu.Lock()
	delete(s.searchIndex, task.ID)
	s.mu.Unlock()

	// Confirm the task is no longer findable.
	results, err = s.SearchTasks(bg(), "rebuild-index-prompt-needle")
	if err != nil {
		t.Fatalf("SearchTasks after corruption: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results after index corruption, got %d", len(results))
	}

	// Step 3: rebuild the index and check that at least 1 entry was repaired.
	repaired, err := s.RebuildSearchIndex(bg())
	if err != nil {
		t.Fatalf("RebuildSearchIndex: %v", err)
	}
	if repaired < 1 {
		t.Errorf("expected repaired >= 1, got %d", repaired)
	}

	// Step 4: the task must now be findable again.
	results, err = s.SearchTasks(bg(), "rebuild-index-prompt-needle")
	if err != nil {
		t.Fatalf("SearchTasks after rebuild: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after rebuild, got %d", len(results))
	}
	if results[0].ID != task.ID {
		t.Errorf("expected task %s, got %s", task.ID, results[0].ID)
	}

	// Oversight must also be searchable.
	results, err = s.SearchTasks(bg(), "rebuild-index-oversight-needle")
	if err != nil {
		t.Fatalf("SearchTasks oversight after rebuild: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 oversight result after rebuild, got %d", len(results))
	}

	// Step 5: call again immediately — repaired count must be 0 (idempotent).
	repaired2, err := s.RebuildSearchIndex(bg())
	if err != nil {
		t.Fatalf("RebuildSearchIndex (second call): %v", err)
	}
	if repaired2 != 0 {
		t.Errorf("expected 0 repaired on second call (idempotent), got %d", repaired2)
	}
}
