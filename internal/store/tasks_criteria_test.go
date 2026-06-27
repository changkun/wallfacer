package store

import "testing"

func TestCreateTaskWithCriteria_Persists(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{
		Prompt:   "build a thing",
		Criteria: "run `make test`; the /health endpoint returns 200",
		Timeout:  15,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.Criteria != "run `make test`; the /health endpoint returns 200" {
		t.Errorf("Criteria not persisted on create: %q", task.Criteria)
	}

	// Create without criteria leaves it empty.
	plain, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "no criteria", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if plain.Criteria != "" {
		t.Errorf("expected empty Criteria, got %q", plain.Criteria)
	}
}

func TestUpdateTaskCriteria_SetsAndClears(t *testing.T) {
	s := newTestStore(t)
	ctx := bg()

	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "p", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.UpdateTaskCriteria(ctx, task.ID, "verify the migration is reversible"); err != nil {
		t.Fatalf("UpdateTaskCriteria: %v", err)
	}
	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Criteria != "verify the migration is reversible" {
		t.Errorf("Criteria = %q, want the set value", got.Criteria)
	}
	// Setting to empty clears it.
	if err := s.UpdateTaskCriteria(ctx, task.ID, ""); err != nil {
		t.Fatalf("UpdateTaskCriteria clear: %v", err)
	}
	got, _ = s.GetTask(ctx, task.ID)
	if got.Criteria != "" {
		t.Errorf("Criteria not cleared: %q", got.Criteria)
	}
}
