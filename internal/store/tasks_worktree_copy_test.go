package store

import (
	"context"
	"testing"
)

// TestUpdateTaskWorktrees_DefensiveCopy verifies that the worktree/hash map
// setters store a copy of the caller's map, not the map by reference. The store
// hands out deep clones on read, so retaining the passed map and mutating it
// later would otherwise race a concurrent reader's maps.Clone.
func TestUpdateTaskWorktrees_DefensiveCopy(t *testing.T) {
	ctx := context.Background()
	s, err := newTestFileStore(t, t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	t.Cleanup(s.Close)

	task, err := s.CreateTaskWithOptions(ctx, TaskCreateOptions{Prompt: "wt", Timeout: 5})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	wt := map[string]string{"/repo": "/wt/a"}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, wt, "branch"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}
	// Mutate the caller's map after handing it off.
	wt["/repo"] = "/wt/HIJACKED"
	wt["/evil"] = "/wt/added"

	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.WorktreePaths["/repo"] != "/wt/a" {
		t.Errorf("stored worktree path was mutated via caller's map: %q", got.WorktreePaths["/repo"])
	}
	if _, ok := got.WorktreePaths["/evil"]; ok {
		t.Errorf("caller's later map insertion leaked into the store")
	}
}
