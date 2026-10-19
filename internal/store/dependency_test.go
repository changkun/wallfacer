package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestUpdateTaskDependsOn_Persists verifies that depends_on is written to disk
// and reloaded correctly.
func TestUpdateTaskDependsOn_Persists(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "a", Timeout: 15})
	b, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "b", Timeout: 15})
	_ = s.UpdateTaskDependsOn(bg(), b.ID, []string{a.ID.String()})

	// Reload from the same directory.
	s2, err := newTestFileStore(t, s.dir)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	task, err := s2.GetTask(bg(), b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(task.DependsOn) != 1 || task.DependsOn[0] != a.ID.String() {
		t.Errorf("expected DependsOn=[%s], got %v", a.ID, task.DependsOn)
	}
}

// TestUpdateTaskDependsOn_ClearsToNil verifies that setting an empty slice
// stores nil (omitempty) so the JSON field is absent.
func TestUpdateTaskDependsOn_ClearsToNil(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "a", Timeout: 15})
	b, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "b", Timeout: 15})
	_ = s.UpdateTaskDependsOn(bg(), b.ID, []string{a.ID.String()})
	_ = s.UpdateTaskDependsOn(bg(), b.ID, []string{}) // clear

	// Reload from disk to check persisted JSON.
	s2, err := newTestFileStore(t, s.dir)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	task, err := s2.GetTask(bg(), b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(task.DependsOn) != 0 {
		t.Errorf("expected DependsOn nil after clear, got %v", task.DependsOn)
	}

	// Also verify the raw JSON does not contain the key (omitempty).
	taskFile := filepath.Join(s.dir, b.ID.String(), "task.json")
	raw, err := os.ReadFile(taskFile)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if _, present := m["depends_on"]; present {
		t.Error("expected depends_on absent from JSON after clear (omitempty), but key was found")
	}
}
