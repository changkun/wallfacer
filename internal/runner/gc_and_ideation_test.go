package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// PruneOrphanedWorktrees – git worktree remove path
// ---------------------------------------------------------------------------

// TestPruneOrphanedWorktrees_WithGitWorktree creates a real git worktree and
// verifies that PruneOrphanedWorktrees removes it via git worktree remove and
// then os.RemoveAll.
func TestPruneOrphanedWorktrees_WithGitWorktree(t *testing.T) {
	repo := setupTestRepo(t)
	_, r := setupTestRunner(t, []string{repo})

	orphanID := uuid.New()

	// Create a real git worktree inside the task directory.
	taskDir := filepath.Join(r.worktreesDir, orphanID.String())
	wtPath := filepath.Join(taskDir, filepath.Base(repo))
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a worktree branch and add the worktree.
	branchName := "task/gc-test-" + orphanID.String()[:8]
	gitRun(t, repo, "worktree", "add", "-b", branchName, wtPath)

	// Verify the worktree exists.
	if _, statErr := os.Stat(wtPath); statErr != nil {
		t.Fatalf("worktree not created: %v", statErr)
	}

	removed := r.PruneOrphanedWorktrees(context.Background(), []uuid.UUID{orphanID})
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}

	// The entire task directory should be gone.
	if _, statErr := os.Stat(taskDir); !os.IsNotExist(statErr) {
		t.Errorf("expected task dir to be removed after PruneOrphanedWorktrees, stat error: %v", statErr)
	}
}

// TestPruneOrphanedWorktrees_WithSubdirectories verifies that
// PruneOrphanedWorktrees removes a task directory that contains
// subdirectories (non-git worktree) by falling through to os.RemoveAll.
func TestPruneOrphanedWorktrees_WithSubdirectories(t *testing.T) {
	_, r := setupTestRunner(t, nil)

	orphanID := uuid.New()

	// Create the task dir with a sub-directory (no matching workspace basename).
	taskDir := filepath.Join(r.worktreesDir, orphanID.String())
	subDir := filepath.Join(taskDir, "some-sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Place a file inside so it's not trivially empty.
	if err := os.WriteFile(filepath.Join(subDir, "file.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	removed := r.PruneOrphanedWorktrees(context.Background(), []uuid.UUID{orphanID})
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}
	if _, statErr := os.Stat(taskDir); !os.IsNotExist(statErr) {
		t.Errorf("expected task dir to be removed, stat error: %v", statErr)
	}
}

// TestPruneOrphanedWorktrees_NonExistentTaskDir verifies that
// PruneOrphanedWorktrees handles a task ID whose directory doesn't exist on
// disk without panicking or counting it as removed.
func TestPruneOrphanedWorktrees_NonExistentTaskDir(t *testing.T) {
	_, r := setupTestRunner(t, nil)

	orphanID := uuid.New()
	// Do NOT create the directory — it doesn't exist.
	removed := r.PruneOrphanedWorktrees(context.Background(), []uuid.UUID{orphanID})
	if removed != 0 {
		t.Errorf("expected 0 removed for non-existent dir, got %d", removed)
	}
}

// TestPruneOrphanedWorktrees_MultipleOrphans verifies that
// PruneOrphanedWorktrees removes multiple orphan directories in one call.
func TestPruneOrphanedWorktrees_MultipleOrphans(t *testing.T) {
	_, r := setupTestRunner(t, nil)

	id1 := uuid.New()
	id2 := uuid.New()

	for _, id := range []uuid.UUID{id1, id2} {
		dir := filepath.Join(r.worktreesDir, id.String())
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	removed := r.PruneOrphanedWorktrees(context.Background(), []uuid.UUID{id1, id2})
	if removed != 2 {
		t.Errorf("expected 2 removed, got %d", removed)
	}

	for _, id := range []uuid.UUID{id1, id2} {
		dir := filepath.Join(r.worktreesDir, id.String())
		if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
			t.Errorf("expected %s to be removed", dir)
		}
	}
}

// ---------------------------------------------------------------------------
// BuildIdeationPrompt tests
// ---------------------------------------------------------------------------

// TestBuildIdeationPrompt_NoWorkspaces verifies BuildIdeationPrompt does not
// panic with no workspaces configured and returns a string (possibly empty).
func TestBuildIdeationPrompt_NoWorkspaces(t *testing.T) {
	_, r := setupTestRunner(t, nil)
	// Should not panic with no workspaces.
	result := r.BuildIdeationPrompt(nil)
	// Result can be empty or non-empty — just verify no panic.
	_ = result
}

// TestBuildIdeationPrompt_WithTasks verifies that BuildIdeationPrompt runs
// without panicking when existing tasks are provided.
func TestBuildIdeationPrompt_WithTasks(t *testing.T) {
	_, r := setupTestRunner(t, nil)

	tasks := []store.Task{
		{Prompt: "fix login bug"},
		{Prompt: "add dark mode"},
	}
	result := r.BuildIdeationPrompt(tasks)
	// Just verify it runs without panic.
	_ = result
}

// TestBuildIdeationPrompt_WithWorkspace verifies that BuildIdeationPrompt
// handles a configured workspace (even without git history) without panicking.
func TestBuildIdeationPrompt_WithWorkspace(t *testing.T) {
	repo := setupTestRepo(t)
	_, r := setupTestRunner(t, []string{repo})

	result := r.BuildIdeationPrompt(nil)
	_ = result
}
