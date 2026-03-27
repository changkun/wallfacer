package store

import (
	"testing"

	"github.com/google/uuid"
)

func TestUpdateTaskTags_Persists(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "task", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := s.UpdateTaskTags(bg(), task.ID, []string{"foo", "bar"}); err != nil {
		t.Fatalf("UpdateTaskTags: %v", err)
	}

	s2, err := NewFileStore(s.dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	got, err := s2.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "foo" || got.Tags[1] != "bar" {
		t.Fatalf("tags = %#v, want [foo bar]", got.Tags)
	}
}

func TestUpdateTaskTags_ClearsToNil(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "task", Timeout: 15})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.UpdateTaskTags(bg(), task.ID, []string{"foo"}); err != nil {
		t.Fatalf("UpdateTaskTags set: %v", err)
	}
	if err := s.UpdateTaskTags(bg(), task.ID, []string{}); err != nil {
		t.Fatalf("UpdateTaskTags clear: %v", err)
	}

	got, err := s.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Tags != nil {
		t.Fatalf("tags = %#v, want nil", got.Tags)
	}
}

func TestUpdateTaskTags_UnknownTask(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTaskTags(bg(), uuid.New(), []string{"foo"}); err == nil {
		t.Fatal("expected error for unknown task")
	}
}
