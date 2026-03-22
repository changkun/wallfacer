package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// TestScanOrphanedWorktrees_UnknownTask verifies that a worktree directory
// whose UUID is not present in the store is NOT returned as an orphan,
// because it may belong to a different workspace scope.
func TestScanOrphanedWorktrees_UnknownTask(t *testing.T) {
	_, r := setupTestRunner(t, nil)

	// Create a directory with a UUID that does not correspond to any task.
	unknownID := uuid.New()
	unknownDir := filepath.Join(r.worktreesDir, unknownID.String())
	if err := os.MkdirAll(unknownDir, 0755); err != nil {
		t.Fatal(err)
	}

	orphans, err := r.ScanOrphanedWorktrees(context.Background())
	if err != nil {
		t.Fatalf("ScanOrphanedWorktrees: %v", err)
	}

	for _, id := range orphans {
		if id == unknownID {
			t.Errorf("unknown task %s should NOT be in orphans (may belong to another workspace)", unknownID)
		}
	}
}

// TestPruneOrphanedWorktrees_RemovesOrphanDir verifies that
// PruneOrphanedWorktrees removes the on-disk directory for a given orphan ID.
func TestPruneOrphanedWorktrees_RemovesOrphanDir(t *testing.T) {
	_, r := setupTestRunner(t, nil)

	orphanID := uuid.New()
	orphanDir := filepath.Join(r.worktreesDir, orphanID.String())
	if err := os.MkdirAll(orphanDir, 0755); err != nil {
		t.Fatal(err)
	}

	removed := r.PruneOrphanedWorktrees(context.Background(), []uuid.UUID{orphanID})
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}

	if _, err := os.Stat(orphanDir); !os.IsNotExist(err) {
		t.Error("expected orphan dir to be removed after PruneOrphanedWorktrees")
	}
}

// TestScanOrphanedWorktrees_SkipsInProgressTask verifies that a worktree
// directory whose task is still in_progress is NOT returned as an orphan,
// and that unknown UUIDs (not in store) are also skipped.
func TestScanOrphanedWorktrees_SkipsInProgressTask(t *testing.T) {
	s, r := setupTestRunner(t, nil)
	ctx := context.Background()

	// Create a directory with a UUID not in store (may belong to another workspace).
	unknownID := uuid.New()
	if err := os.MkdirAll(filepath.Join(r.worktreesDir, unknownID.String()), 0755); err != nil {
		t.Fatal(err)
	}

	// Create an in_progress task in the store and a matching worktree directory.
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "active task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(r.worktreesDir, task.ID.String()), 0755); err != nil {
		t.Fatal(err)
	}

	orphans, err := r.ScanOrphanedWorktrees(ctx)
	if err != nil {
		t.Fatalf("ScanOrphanedWorktrees: %v", err)
	}

	for _, id := range orphans {
		if id == unknownID {
			t.Errorf("unknown UUID %s should NOT be in orphans", unknownID)
		}
		if id == task.ID {
			t.Errorf("in_progress task %s should NOT be in orphans", task.ID)
		}
	}
}

// TestScanOrphanedWorktrees_TerminalStatesAreOrphans verifies that worktree
// directories for done, cancelled, and archived tasks are treated as orphans.
func TestScanOrphanedWorktrees_TerminalStatesAreOrphans(t *testing.T) {
	s, r := setupTestRunner(t, nil)
	ctx := context.Background()

	createTaskInStatus := func(status store.TaskStatus) uuid.UUID {
		t.Helper()
		task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task for " + string(status), Timeout: 5})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ForceUpdateTaskStatus(ctx, task.ID, status); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(r.worktreesDir, task.ID.String()), 0755); err != nil {
			t.Fatal(err)
		}
		return task.ID
	}

	doneID := createTaskInStatus(store.TaskStatusDone)
	cancelledID := createTaskInStatus(store.TaskStatusCancelled)

	orphans, err := r.ScanOrphanedWorktrees(ctx)
	if err != nil {
		t.Fatalf("ScanOrphanedWorktrees: %v", err)
	}

	inOrphans := make(map[uuid.UUID]bool, len(orphans))
	for _, id := range orphans {
		inOrphans[id] = true
	}

	if !inOrphans[doneID] {
		t.Errorf("done task %s should be in orphans", doneID)
	}
	if !inOrphans[cancelledID] {
		t.Errorf("cancelled task %s should be in orphans", cancelledID)
	}
}

// TestScanOrphanedWorktrees_SkipsNonTerminalStates verifies that worktree
// directories for backlog, waiting, committing, and failed tasks are not orphans.
func TestScanOrphanedWorktrees_SkipsNonTerminalStates(t *testing.T) {
	s, r := setupTestRunner(t, nil)
	ctx := context.Background()

	createTaskInStatus := func(status store.TaskStatus) uuid.UUID {
		t.Helper()
		task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "task for " + string(status), Timeout: 5})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ForceUpdateTaskStatus(ctx, task.ID, status); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(r.worktreesDir, task.ID.String()), 0755); err != nil {
			t.Fatal(err)
		}
		return task.ID
	}

	backlogID := createTaskInStatus(store.TaskStatusBacklog)
	waitingID := createTaskInStatus(store.TaskStatusWaiting)
	failedID := createTaskInStatus(store.TaskStatusFailed)

	orphans, err := r.ScanOrphanedWorktrees(ctx)
	if err != nil {
		t.Fatalf("ScanOrphanedWorktrees: %v", err)
	}

	inOrphans := make(map[uuid.UUID]bool, len(orphans))
	for _, id := range orphans {
		inOrphans[id] = true
	}

	for _, id := range []uuid.UUID{backlogID, waitingID, failedID} {
		if inOrphans[id] {
			t.Errorf("task %s in non-terminal state should NOT be in orphans", id)
		}
	}
}

// TestScanOrphanedWorktrees_MissingDir verifies that ScanOrphanedWorktrees
// returns nil (not an error) when worktreesDir does not exist.
func TestScanOrphanedWorktrees_MissingDir(t *testing.T) {
	_, r := setupTestRunner(t, nil)
	r.worktreesDir = filepath.Join(t.TempDir(), "nonexistent")

	orphans, err := r.ScanOrphanedWorktrees(context.Background())
	if err != nil {
		t.Fatalf("expected nil error for missing dir, got: %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("expected no orphans for missing dir, got %v", orphans)
	}
}

// TestPruneOrphanedWorktrees_EmptyList verifies that PruneOrphanedWorktrees
// is a no-op and returns 0 when given an empty orphan list.
func TestPruneOrphanedWorktrees_EmptyList(t *testing.T) {
	_, r := setupTestRunner(t, nil)

	removed := r.PruneOrphanedWorktrees(context.Background(), nil)
	if removed != 0 {
		t.Errorf("expected 0 removed for empty list, got %d", removed)
	}
}

// TestScanMissingTaskWorktrees_DetectsMissing verifies that an in_progress task
// whose WorktreePaths map points to a non-existent directory is returned by
// ScanMissingTaskWorktrees.
func TestScanMissingTaskWorktrees_DetectsMissing(t *testing.T) {
	s, r := setupTestRunner(t, nil)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "missing worktree task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	nonexistent := filepath.Join(t.TempDir(), "does-not-exist", "repo")
	if err := s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{"/fake/repo": nonexistent}, "task/test1234"); err != nil {
		t.Fatal(err)
	}

	missing, err := r.ScanMissingTaskWorktrees(ctx)
	if err != nil {
		t.Fatalf("ScanMissingTaskWorktrees: %v", err)
	}

	found := false
	for _, m := range missing {
		if m.ID == task.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected task %s with missing worktree to be returned, got %v", task.ID, missing)
	}
}

// TestScanMissingTaskWorktrees_SkipsTerminalTasks verifies that done and
// cancelled tasks are NOT returned by ScanMissingTaskWorktrees even when their
// WorktreePaths point to non-existent directories.
func TestScanMissingTaskWorktrees_SkipsTerminalTasks(t *testing.T) {
	s, r := setupTestRunner(t, nil)
	ctx := context.Background()

	createTerminal := func(status store.TaskStatus) *store.Task {
		t.Helper()
		task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "terminal task " + string(status), Timeout: 5})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ForceUpdateTaskStatus(ctx, task.ID, status); err != nil {
			t.Fatal(err)
		}
		nonexistent := filepath.Join(t.TempDir(), "does-not-exist", "repo")
		if err := s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{"/fake/repo": nonexistent}, "task/test5678"); err != nil {
			t.Fatal(err)
		}
		return task
	}

	doneTask := createTerminal(store.TaskStatusDone)
	cancelledTask := createTerminal(store.TaskStatusCancelled)

	missing, err := r.ScanMissingTaskWorktrees(ctx)
	if err != nil {
		t.Fatalf("ScanMissingTaskWorktrees: %v", err)
	}

	inMissing := make(map[uuid.UUID]bool, len(missing))
	for _, m := range missing {
		inMissing[m.ID] = true
	}

	if inMissing[doneTask.ID] {
		t.Errorf("done task %s should NOT be in missing list", doneTask.ID)
	}
	if inMissing[cancelledTask.ID] {
		t.Errorf("cancelled task %s should NOT be in missing list", cancelledTask.ID)
	}
}
