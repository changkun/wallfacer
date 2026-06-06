package store

import (
	"context"
	"testing"
)

// TestLoadAll_CorruptTombstoneKeepsTaskDeleted verifies that a soft-deleted task
// whose tombstone.json is present but unparseable stays soft-deleted across a
// restart. Before the fix, loadAll only routed the task to s.deleted when the
// tombstone parsed; a corrupt marker fell through and the task was loaded as a
// live task (visible on the board, eligible for the auto-promoter).
func TestLoadAll_CorruptTombstoneKeepsTaskDeleted(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	s, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "doomed", Timeout: 5})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.DeleteTask(ctx, task.ID, "test"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Corrupt the tombstone marker on disk.
	if err := s.backend.SaveBlob(task.ID, "tombstone.json", []byte("not json")); err != nil {
		t.Fatalf("corrupt tombstone: %v", err)
	}
	s.Close()

	// Reopen: loadAll runs against the on-disk state.
	s2, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen NewFileStore: %v", err)
	}
	t.Cleanup(s2.Close)

	live, err := s2.ListTasks(ctx, true)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	for _, lt := range live {
		if lt.ID == task.ID {
			t.Fatalf("soft-deleted task with corrupt tombstone resurrected as live")
		}
	}

	s2.mu.RLock()
	_, deleted := s2.deleted[task.ID]
	s2.mu.RUnlock()
	if !deleted {
		t.Fatalf("task with corrupt tombstone should remain in s.deleted")
	}
}
