package store

import (
	"testing"
)

// TestUpdateTaskCommitMessage verifies that a commit message is persisted and
// can be retrieved via GetTask.
func TestUpdateTaskCommitMessage(t *testing.T) {
	s := newTestStore(t)

	task, err := s.CreateTask(bg(), "test prompt", 15, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	msg := "feat: implement new feature\n\nDetailed description."
	if err := s.UpdateTaskCommitMessage(bg(), task.ID, msg); err != nil {
		t.Fatalf("UpdateTaskCommitMessage: %v", err)
	}

	updated, err := s.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if updated.CommitMessage != msg {
		t.Errorf("expected CommitMessage %q, got %q", msg, updated.CommitMessage)
	}
}

// TestUpdateTaskCommitMessage_Overwrite verifies that a second call replaces
// the previously stored commit message.
func TestUpdateTaskCommitMessage_Overwrite(t *testing.T) {
	s := newTestStore(t)

	task, err := s.CreateTask(bg(), "overwrite test", 15, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	first := "initial commit message"
	second := "updated commit message"

	if err := s.UpdateTaskCommitMessage(bg(), task.ID, first); err != nil {
		t.Fatalf("UpdateTaskCommitMessage (first): %v", err)
	}
	if err := s.UpdateTaskCommitMessage(bg(), task.ID, second); err != nil {
		t.Fatalf("UpdateTaskCommitMessage (second): %v", err)
	}

	got, err := s.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.CommitMessage != second {
		t.Errorf("expected %q, got %q", second, got.CommitMessage)
	}
}

// TestUpdateTaskCommitMessage_Empty verifies that an empty commit message can
// be stored (clearing a previously set value).
func TestUpdateTaskCommitMessage_Empty(t *testing.T) {
	s := newTestStore(t)

	task, err := s.CreateTask(bg(), "empty msg test", 15, false, "", TaskKindTask)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := s.UpdateTaskCommitMessage(bg(), task.ID, "initial"); err != nil {
		t.Fatalf("UpdateTaskCommitMessage (set): %v", err)
	}
	if err := s.UpdateTaskCommitMessage(bg(), task.ID, ""); err != nil {
		t.Fatalf("UpdateTaskCommitMessage (clear): %v", err)
	}

	got, err := s.GetTask(bg(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.CommitMessage != "" {
		t.Errorf("expected empty CommitMessage, got %q", got.CommitMessage)
	}
}
