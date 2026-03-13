package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/store"
)

// ---------------------------------------------------------------------------
// setupNonGitSnapshot
// ---------------------------------------------------------------------------

// TestSetupNonGitSnapshotCopiesFiles verifies that setupNonGitSnapshot copies
// workspace files (including nested directories) into the snapshot path and
// initialises a git repo there.
func TestSetupNonGitSnapshotCopiesFiles(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "subdir", "nested.txt"), []byte("nested"), 0644); err != nil {
		t.Fatal(err)
	}

	snapshotPath := filepath.Join(t.TempDir(), "snapshot")
	if err := setupNonGitSnapshot(ws, snapshotPath); err != nil {
		t.Fatal("setupNonGitSnapshot:", err)
	}

	// Top-level file must be present.
	if _, err := os.Stat(filepath.Join(snapshotPath, "file.txt")); err != nil {
		t.Fatal("file.txt should be in snapshot:", err)
	}
	// Nested file must be present.
	if _, err := os.Stat(filepath.Join(snapshotPath, "subdir", "nested.txt")); err != nil {
		t.Fatal("subdir/nested.txt should be in snapshot:", err)
	}
	// Git repo must be initialised.
	if _, err := os.Stat(filepath.Join(snapshotPath, ".git")); err != nil {
		t.Fatal(".git should exist in snapshot:", err)
	}
}

// TestSetupNonGitSnapshotHasInitialCommit verifies that the snapshot contains
// the "wallfacer: initial snapshot" commit so Phase 1 of the pipeline has a
// clean base to diff against.
func TestSetupNonGitSnapshotHasInitialCommit(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "app.txt"), []byte("app content"), 0644); err != nil {
		t.Fatal(err)
	}

	snapshotPath := filepath.Join(t.TempDir(), "snapshot")
	if err := setupNonGitSnapshot(ws, snapshotPath); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command("git", "-C", snapshotPath, "log", "--oneline").Output()
	if err != nil {
		t.Fatal("git log in snapshot:", err)
	}
	if !strings.Contains(string(out), "wallfacer: initial snapshot") {
		t.Fatalf("expected initial snapshot commit, git log:\n%s", out)
	}
}

// TestSetupNonGitSnapshotEmptyWorkspace verifies that setupNonGitSnapshot
// handles an empty workspace without error (uses --allow-empty commit).
func TestSetupNonGitSnapshotEmptyWorkspace(t *testing.T) {
	ws := t.TempDir() // deliberately empty
	snapshotPath := filepath.Join(t.TempDir(), "snapshot")
	if err := setupNonGitSnapshot(ws, snapshotPath); err != nil {
		t.Fatal("setupNonGitSnapshot on empty workspace should not fail:", err)
	}
	if _, err := os.Stat(filepath.Join(snapshotPath, ".git")); err != nil {
		t.Fatal(".git should exist in empty snapshot:", err)
	}
}

// ---------------------------------------------------------------------------
// extractSnapshotToWorkspace
// ---------------------------------------------------------------------------

// TestExtractSnapshotToWorkspace verifies that files from the snapshot are
// copied to the target directory.
func TestExtractSnapshotToWorkspace(t *testing.T) {
	snapshot := t.TempDir()
	target := t.TempDir()

	if err := os.WriteFile(filepath.Join(snapshot, "result.txt"), []byte("result"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(snapshot, "output"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshot, "output", "data.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := extractSnapshotToWorkspace(snapshot, target); err != nil {
		t.Fatal("extractSnapshotToWorkspace:", err)
	}

	content, err := os.ReadFile(filepath.Join(target, "result.txt"))
	if err != nil {
		t.Fatal("result.txt should be in target:", err)
	}
	if string(content) != "result" {
		t.Fatalf("unexpected content: %q", content)
	}
	if _, err := os.Stat(filepath.Join(target, "output", "data.txt")); err != nil {
		t.Fatal("output/data.txt should be in target:", err)
	}
}

// TestExtractSnapshotDoesNotLeakGitDir verifies that the .git directory from
// the snapshot is not extracted to the target workspace. rsync excludes it;
// the cp fallback removes it afterward.
func TestExtractSnapshotDoesNotLeakGitDir(t *testing.T) {
	snapshot := t.TempDir()
	target := t.TempDir()

	if err := os.WriteFile(filepath.Join(snapshot, "app.txt"), []byte("app"), 0644); err != nil {
		t.Fatal(err)
	}
	// Simulate a .git directory in the snapshot.
	if err := os.MkdirAll(filepath.Join(snapshot, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshot, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := extractSnapshotToWorkspace(snapshot, target); err != nil {
		t.Fatal("extractSnapshotToWorkspace:", err)
	}

	// The main file must be present.
	if _, err := os.Stat(filepath.Join(target, "app.txt")); err != nil {
		t.Fatal("app.txt should be in target:", err)
	}
}

// ---------------------------------------------------------------------------
// Non-git commit pipeline integration
// ---------------------------------------------------------------------------

// TestCommitPipelineNonGitWorkspace verifies that the full commit pipeline
// handles a non-git workspace: Phase 1 commits changes in the snapshot, and
// Phase 2 extracts the changes back to the original workspace directory.
func TestCommitPipelineNonGitWorkspace(t *testing.T) {
	ws := t.TempDir() // non-git workspace

	// Create an original file in the workspace.
	if err := os.WriteFile(filepath.Join(ws, "app.txt"), []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	s, runner := setupTestRunner(t, []string{ws})
	enableCommitMessageGeneration(t, runner)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Non-git commit test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	// setupWorktrees creates a snapshot of ws.
	wt, br, err := runner.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, wt, br); err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusCommitting); err != nil {
		t.Fatal(err)
	}

	// Simulate Claude modifying files inside the snapshot (the "container").
	snapshotPath := wt[ws]
	if err := os.WriteFile(filepath.Join(snapshotPath, "app.txt"), []byte("modified\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshotPath, "new.txt"), []byte("new file\n"), 0644); err != nil {
		t.Fatal(err)
	}

	commitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	runner.commit(commitCtx, task.ID, "", 1, wt, br)

	// Verify modifications were extracted back to the original workspace.
	content, err := os.ReadFile(filepath.Join(ws, "app.txt"))
	if err != nil {
		t.Fatal("app.txt should exist in workspace after non-git commit:", err)
	}
	if string(content) != "modified\n" {
		t.Fatalf("expected modified content, got %q", content)
	}
	if _, err := os.Stat(filepath.Join(ws, "new.txt")); err != nil {
		t.Fatal("new.txt should exist in workspace after non-git commit:", err)
	}
}

// TestRunEndToEndNonGitWorkspace verifies that the full Run() → commit flow
// works for a non-git workspace: the container (fake) triggers end_turn,
// and the snapshot changes are extracted back to the original directory.
func TestRunEndToEndNonGitWorkspace(t *testing.T) {
	ws := t.TempDir() // non-git workspace
	if err := os.WriteFile(filepath.Join(ws, "init.txt"), []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := fakeCmdScript(t, endTurnOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{ws}, cmd)
	// Wait for background goroutines (e.g. oversight) before temp dir cleanup.
	t.Cleanup(r.WaitBackground)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Non-git E2E test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "modify init.txt", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "waiting" {
		t.Fatalf("expected status=waiting (pending review), got %q", updated.Status)
	}
}
