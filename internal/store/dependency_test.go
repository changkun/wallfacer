package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestAreDependenciesSatisfied_NoDependencies verifies that a task with no
// dependencies is always satisfied.
func TestAreDependenciesSatisfied_NoDependencies(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "task", 15, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := s.AreDependenciesSatisfied(bg(), task.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected satisfied (no deps), got false")
	}
}

// TestAreDependenciesSatisfied_EmptySlice verifies that clearing deps to an
// empty slice is treated as no dependencies.
func TestAreDependenciesSatisfied_EmptySlice(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTask(bg(), "task", 15, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskDependsOn(bg(), task.ID, []string{}); err != nil {
		t.Fatal(err)
	}
	ok, err := s.AreDependenciesSatisfied(bg(), task.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected satisfied (empty deps), got false")
	}
}

// TestAreDependenciesSatisfied_AllDone verifies that all-done dependencies
// are reported as satisfied.
func TestAreDependenciesSatisfied_AllDone(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.CreateTask(bg(), "a", 15, false, "", "")
	b, _ := s.CreateTask(bg(), "b", 15, false, "", "")
	c, _ := s.CreateTask(bg(), "c", 15, false, "", "")
	s.ForceUpdateTaskStatus(bg(), a.ID, TaskStatusDone)
	s.ForceUpdateTaskStatus(bg(), b.ID, TaskStatusDone)
	s.UpdateTaskDependsOn(bg(), c.ID, []string{a.ID.String(), b.ID.String()})

	ok, err := s.AreDependenciesSatisfied(bg(), c.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected satisfied (all deps done), got false")
	}
}

// TestAreDependenciesSatisfied_OneNotDone verifies that one incomplete
// dependency makes the result unsatisfied.
func TestAreDependenciesSatisfied_OneNotDone(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.CreateTask(bg(), "a", 15, false, "", "")
	b, _ := s.CreateTask(bg(), "b", 15, false, "", "")
	c, _ := s.CreateTask(bg(), "c", 15, false, "", "")
	s.ForceUpdateTaskStatus(bg(), a.ID, TaskStatusDone)
	s.UpdateTaskStatus(bg(), b.ID, TaskStatusInProgress)
	s.UpdateTaskDependsOn(bg(), c.ID, []string{a.ID.String(), b.ID.String()})

	ok, err := s.AreDependenciesSatisfied(bg(), c.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected unsatisfied (one dep in_progress), got true")
	}
}

// TestAreDependenciesSatisfied_NoDone verifies that a backlog dependency is
// reported as unsatisfied.
func TestAreDependenciesSatisfied_NoDone(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.CreateTask(bg(), "a", 15, false, "", "")
	b, _ := s.CreateTask(bg(), "b", 15, false, "", "")
	s.UpdateTaskDependsOn(bg(), b.ID, []string{a.ID.String()})

	ok, err := s.AreDependenciesSatisfied(bg(), b.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected unsatisfied (dep is backlog), got true")
	}
}

// TestAreDependenciesSatisfied_DeletedDep verifies that a deleted dependency
// is treated as unsatisfied.
func TestAreDependenciesSatisfied_DeletedDep(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.CreateTask(bg(), "a", 15, false, "", "")
	b, _ := s.CreateTask(bg(), "b", 15, false, "", "")
	s.UpdateTaskDependsOn(bg(), b.ID, []string{a.ID.String()})
	s.DeleteTask(bg(), a.ID)

	ok, err := s.AreDependenciesSatisfied(bg(), b.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected unsatisfied (dep deleted), got true")
	}
}

// TestAreDependenciesSatisfied_ArchivedDone verifies that an archived done
// task is treated as satisfied.
func TestAreDependenciesSatisfied_ArchivedDone(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.CreateTask(bg(), "a", 15, false, "", "")
	b, _ := s.CreateTask(bg(), "b", 15, false, "", "")
	s.ForceUpdateTaskStatus(bg(), a.ID, TaskStatusDone)
	s.SetTaskArchived(bg(), a.ID, true)
	s.UpdateTaskDependsOn(bg(), b.ID, []string{a.ID.String()})

	ok, err := s.AreDependenciesSatisfied(bg(), b.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected satisfied (dep is archived done), got false")
	}
}

// TestAreDependenciesSatisfied_TaskNotFound verifies that a non-existent task
// returns an error.
func TestAreDependenciesSatisfied_TaskNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.AreDependenciesSatisfied(bg(), [16]byte{0x01})
	if err == nil {
		t.Error("expected error for non-existent task, got nil")
	}
}

// TestUpdateTaskDependsOn_Persists verifies that depends_on is written to disk
// and reloaded correctly.
func TestUpdateTaskDependsOn_Persists(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.CreateTask(bg(), "a", 15, false, "", "")
	b, _ := s.CreateTask(bg(), "b", 15, false, "", "")
	s.UpdateTaskDependsOn(bg(), b.ID, []string{a.ID.String()})

	// Reload from the same directory.
	s2, err := NewStore(s.dir)
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
	a, _ := s.CreateTask(bg(), "a", 15, false, "", "")
	b, _ := s.CreateTask(bg(), "b", 15, false, "", "")
	s.UpdateTaskDependsOn(bg(), b.ID, []string{a.ID.String()})
	s.UpdateTaskDependsOn(bg(), b.ID, []string{}) // clear

	// Reload from disk to check persisted JSON.
	s2, err := NewStore(s.dir)
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
