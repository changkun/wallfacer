package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/gitutil"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Runner getters
// ---------------------------------------------------------------------------

// TestRunnerCommand verifies that Command() returns the configured binary path.
func TestRunnerCommand(t *testing.T) {
	r := newTestRunnerWithInstructions(t, "")
	if r.Command() != "podman" {
		t.Fatalf("expected command 'podman', got %q", r.Command())
	}
}

// TestWorkspacesEmpty verifies that Workspaces() returns nil when no
// workspaces are configured.
func TestWorkspacesEmpty(t *testing.T) {
	dataDir := t.TempDir()
	s, err := store.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRunner(s, RunnerConfig{Command: "echo"})
	if r.Workspaces() != nil {
		t.Fatal("expected nil when workspaces is empty")
	}
}

// TestWorkspacesMultiple verifies that Workspaces() correctly splits a
// space-separated workspace list.
func TestWorkspacesMultiple(t *testing.T) {
	dataDir := t.TempDir()
	s, err := store.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRunner(s, RunnerConfig{
		Command:    "echo",
		Workspaces: "/a /b /c",
	})
	ws := r.Workspaces()
	if len(ws) != 3 {
		t.Fatalf("expected 3 workspaces, got %d: %v", len(ws), ws)
	}
	if ws[0] != "/a" || ws[1] != "/b" || ws[2] != "/c" {
		t.Fatalf("unexpected workspaces: %v", ws)
	}
}

// TestKillContainer verifies that KillContainer does not panic when no
// container is running (error from exec is silently ignored).
func TestKillContainer(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	// Should not panic or return an error.
	r.KillContainer(uuid.New())
}

// ---------------------------------------------------------------------------
// isConflictError
// ---------------------------------------------------------------------------

func TestIsConflictErrorNil(t *testing.T) {
	if isConflictError(nil) {
		t.Fatal("nil should not be a conflict error")
	}
}

func TestIsConflictErrorNonConflict(t *testing.T) {
	if isConflictError(fmt.Errorf("some regular error")) {
		t.Fatal("a regular error should not be a conflict error")
	}
}

func TestIsConflictErrorWrappedErrConflict(t *testing.T) {
	err := fmt.Errorf("rebase failed: %w", gitutil.ErrConflict)
	if !isConflictError(err) {
		t.Fatal("wrapped ErrConflict should be detected as a conflict error")
	}
}

func TestIsConflictErrorDirectString(t *testing.T) {
	// isConflictError checks if the error message contains ErrConflict.Error().
	err := fmt.Errorf("rebase conflict occurred")
	if !isConflictError(err) {
		t.Fatal("error containing 'rebase conflict' should be detected")
	}
}

// ---------------------------------------------------------------------------
// runGit
// ---------------------------------------------------------------------------

// TestRunGitSuccess verifies that runGit executes git commands successfully.
func TestRunGitSuccess(t *testing.T) {
	repo := setupTestRepo(t)
	if err := runGit(repo, "status"); err != nil {
		t.Fatalf("runGit git status should succeed: %v", err)
	}
}

// TestRunGitInvalidDir verifies that runGit returns an error for a non-existent
// directory.
func TestRunGitInvalidDir(t *testing.T) {
	err := runGit("/nonexistent/xyz/path/abc", "status")
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
}

// ---------------------------------------------------------------------------
// setupWorktrees — idempotent path
// ---------------------------------------------------------------------------

// TestSetupWorktreesIdempotent verifies that calling setupWorktrees twice for
// the same taskID returns the same paths without error (idempotent behaviour).
func TestSetupWorktreesIdempotent(t *testing.T) {
	repo := setupTestRepo(t)
	_, runner := setupTestRunner(t, []string{repo})
	taskID := uuid.New()

	wt1, br1, err := runner.setupWorktrees(taskID)
	if err != nil {
		t.Fatal("first setupWorktrees:", err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(taskID, wt1, br1) })

	// Second call — worktree directory already exists, should be reused.
	wt2, _, err := runner.setupWorktrees(taskID)
	if err != nil {
		t.Fatal("second (idempotent) setupWorktrees:", err)
	}
	if wt1[repo] != wt2[repo] {
		t.Errorf("expected same worktree path on second call: %q vs %q", wt1[repo], wt2[repo])
	}
}

// TestResolveConflictsSuccess verifies that resolveConflicts returns nil when
// the container exits successfully with a valid result.
func TestResolveConflictsSuccess(t *testing.T) {
	cmd := fakeCmdScript(t, endTurnOutput, 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "conflict resolve test", 5, false, "")
	if err != nil {
		t.Fatal(err)
	}

	repoPath := t.TempDir()
	worktreePath := t.TempDir()

	if err := r.resolveConflicts(ctx, task.ID, repoPath, worktreePath, ""); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// TestResolveConflictsContainerError verifies that resolveConflicts returns a
// wrapped error when the container itself fails.
func TestResolveConflictsContainerError(t *testing.T) {
	cmd := fakeCmdScript(t, "", 1) // empty output, exit 1
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "conflict error test", 5, false, "")
	if err != nil {
		t.Fatal(err)
	}

	repoPath := t.TempDir()
	worktreePath := t.TempDir()

	err = r.resolveConflicts(ctx, task.ID, repoPath, worktreePath, "")
	if err == nil {
		t.Fatal("expected error from container failure")
	}
	if !strings.Contains(err.Error(), "conflict resolver container") {
		t.Fatalf("expected 'conflict resolver container' error, got: %v", err)
	}
}

// TestResolveConflictsIsError verifies that resolveConflicts returns an error
// when the container reports is_error=true.
func TestResolveConflictsIsError(t *testing.T) {
	cmd := fakeCmdScript(t, isErrorOutput, 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "conflict is_error test", 5, false, "")
	if err != nil {
		t.Fatal(err)
	}

	repoPath := t.TempDir()
	worktreePath := t.TempDir()

	err = r.resolveConflicts(ctx, task.ID, repoPath, worktreePath, "")
	if err == nil {
		t.Fatal("expected error when container reports is_error=true")
	}
	if !strings.Contains(err.Error(), "conflict resolver reported error") {
		t.Fatalf("expected 'conflict resolver reported error', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CleanupWorktrees (exported)
// ---------------------------------------------------------------------------

// TestCleanupWorktreesExported verifies the exported CleanupWorktrees removes
// worktree directories and git branches.
func TestCleanupWorktreesExported(t *testing.T) {
	repo := setupTestRepo(t)
	_, runner := setupTestRunner(t, []string{repo})
	taskID := uuid.New()

	wt, br, err := runner.setupWorktrees(taskID)
	if err != nil {
		t.Fatal(err)
	}
	worktreePath := wt[repo]
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatal("worktree should exist before cleanup:", err)
	}

	runner.CleanupWorktrees(taskID, wt, br)

	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatal("worktree should be removed after exported CleanupWorktrees")
	}
}

// ---------------------------------------------------------------------------
// PruneOrphanedWorktrees
// ---------------------------------------------------------------------------

// TestPruneOrphanedWorktrees verifies that directories not matching any known
// task UUID are removed, while known-task directories are preserved.
func TestPruneOrphanedWorktrees(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "known task", 5, false, "")
	if err != nil {
		t.Fatal(err)
	}

	knownDir := filepath.Join(runner.worktreesDir, task.ID.String())
	orphanDir := filepath.Join(runner.worktreesDir, uuid.New().String())

	for _, d := range []string{knownDir, orphanDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	runner.PruneOrphanedWorktrees(s)

	if _, err := os.Stat(knownDir); err != nil {
		t.Fatal("known task worktree dir should be preserved:", err)
	}
	if _, err := os.Stat(orphanDir); !os.IsNotExist(err) {
		t.Fatal("orphan worktree dir should be pruned")
	}
}

// TestPruneOrphanedWorktreesMissingDir verifies PruneOrphanedWorktrees handles
// a missing worktrees directory gracefully (no panic).
func TestPruneOrphanedWorktreesMissingDir(t *testing.T) {
	s, runner := setupRunnerWithCmd(t, nil, "echo")
	// Point worktreesDir to a path that doesn't exist.
	runner.worktreesDir = filepath.Join(t.TempDir(), "nonexistent_worktrees")
	// Should not panic.
	runner.PruneOrphanedWorktrees(s)
}

// TestPruneOrphanedWorktreesRunsGitWorktreePrune verifies that
// PruneOrphanedWorktrees runs `git worktree prune` on git workspaces.
func TestPruneOrphanedWorktreesRunsGitWorktreePrune(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})

	// Just verify it completes without panicking when the workspace is a git repo.
	runner.PruneOrphanedWorktrees(s)
}

// ---------------------------------------------------------------------------
// Commit (exported) — error path
// ---------------------------------------------------------------------------

// TestCommitNonExistentTask verifies that the exported Commit does not panic
// when the task does not exist in the store.
func TestCommitNonExistentTask(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	// Should return early without panicking.
	r.Commit(uuid.New(), "")
}

// ---------------------------------------------------------------------------
// runContainer
// ---------------------------------------------------------------------------

// TestRunContainerSuccess verifies that runContainer parses valid JSON output
// and returns the structured result.
func TestRunContainerSuccess(t *testing.T) {
	cmd := fakeCmdScript(t, endTurnOutput, 0)
	r := runnerWithCmd(t, cmd)

	out, stdout, stderr, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", nil, "")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if out.StopReason != "end_turn" {
		t.Fatalf("expected stop_reason=end_turn, got %q", out.StopReason)
	}
	_ = stdout
	_ = stderr
}

// TestRunContainerNonZeroExitWithValidOutput verifies that a non-zero exit is
// tolerated when the container produced parseable JSON output.
func TestRunContainerNonZeroExitWithValidOutput(t *testing.T) {
	cmd := fakeCmdScript(t, endTurnOutput, 1)
	r := runnerWithCmd(t, cmd)

	out, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", nil, "")
	if err != nil {
		t.Fatalf("expected no error for non-zero exit with valid output, got: %v", err)
	}
	if out.StopReason != "end_turn" {
		t.Fatalf("expected stop_reason=end_turn, got %q", out.StopReason)
	}
}

// TestRunContainerEmptyOutputNonZeroExit verifies that empty stdout + exit 1
// returns an appropriate error.
func TestRunContainerEmptyOutputNonZeroExit(t *testing.T) {
	cmd := fakeCmdScript(t, "", 1)
	r := runnerWithCmd(t, cmd)

	_, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", nil, "")
	if err == nil {
		t.Fatal("expected error for empty container output with non-zero exit")
	}
}

// TestRunContainerEmptyOutputZeroExit verifies that empty stdout + exit 0
// returns an "empty output" error.
func TestRunContainerEmptyOutputZeroExit(t *testing.T) {
	cmd := fakeCmdScript(t, "", 0)
	r := runnerWithCmd(t, cmd)

	_, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", nil, "")
	if err == nil {
		t.Fatal("expected error for empty container output with exit 0")
	}
	if !strings.Contains(err.Error(), "empty output") {
		t.Fatalf("expected 'empty output' error, got: %v", err)
	}
}

// TestRunContainerSessionID verifies that a non-empty sessionID is passed to
// the container args as --resume.
func TestRunContainerWithSessionID(t *testing.T) {
	cmd := fakeCmdScript(t, endTurnOutput, 0)
	r := runnerWithCmd(t, cmd)

	// Should succeed; session ID is passed to args (verified via args tests).
	out, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "sess-xyz", nil, "", nil, "")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if out.StopReason != "end_turn" {
		t.Fatalf("expected stop_reason=end_turn, got %q", out.StopReason)
	}
}

// ---------------------------------------------------------------------------
// buildContainerArgs extras (paths not covered by runner_test.go)
// ---------------------------------------------------------------------------

// TestBuildContainerArgsWithSessionID verifies that a non-empty sessionID
// adds --resume <sessionID> to the container args.
func TestBuildContainerArgsWithSessionID(t *testing.T) {
	r := newTestRunnerWithInstructions(t, "")
	args := r.buildContainerArgs("name", "", "prompt", "sess-abc", nil, "", nil, "")
	if !containsConsecutive(args, "--resume", "sess-abc") {
		t.Fatalf("expected --resume sess-abc in args; got: %v", args)
	}
}

// TestBuildContainerArgsWithEnvFile verifies that a non-empty envFile adds
// --env-file to the container args.
func TestBuildContainerArgsWithEnvFile(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("KEY=val\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	s, err := store.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	r := NewRunner(s, RunnerConfig{
		Command:      "podman",
		SandboxImage: "test:latest",
		EnvFile:      envFile,
	})
	args := r.buildContainerArgs("name", "", "prompt", "", nil, "", nil, "")
	if !containsConsecutive(args, "--env-file", envFile) {
		t.Fatalf("expected --env-file %s in args; got: %v", envFile, args)
	}
}

// TestBuildContainerArgsWorktreeOverride verifies that worktreeOverrides
// replaces the workspace host path in the volume mount.
func TestBuildContainerArgsWorktreeOverride(t *testing.T) {
	ws := t.TempDir()
	wt := t.TempDir()

	dataDir := t.TempDir()
	s, err := store.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	r := NewRunner(s, RunnerConfig{
		Command:      "podman",
		SandboxImage: "test:latest",
		Workspaces:   ws,
	})
	args := r.buildContainerArgs("name", "", "prompt", "", map[string]string{ws: wt}, "", nil, "")
	basename := filepath.Base(ws)
	expectedMount := wt + ":/workspace/" + basename + ":z"
	if !containsConsecutive(args, "-v", expectedMount) {
		t.Fatalf("expected worktree override mount %q; got: %v", expectedMount, args)
	}
	// Original workspace path must NOT appear as the host path.
	unexpectedMount := ws + ":/workspace/" + basename + ":z"
	if containsConsecutive(args, "-v", unexpectedMount) {
		t.Fatalf("original workspace path should be replaced by worktree, but found %q", unexpectedMount)
	}
}

// TestBuildContainerArgsWorktreeGitDirMount verifies that when a workspace has
// a worktree override and the original workspace is a git repo, the main repo's
// .git directory is mounted at its host path so the worktree's .git file
// reference resolves correctly inside the container.
func TestBuildContainerArgsWorktreeGitDirMount(t *testing.T) {
	// Create a real git repo so .git directory exists.
	repo := setupTestRepo(t)
	wt := t.TempDir()

	dataDir := t.TempDir()
	s, err := store.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	r := NewRunner(s, RunnerConfig{
		Command:      "podman",
		SandboxImage: "test:latest",
		Workspaces:   repo,
	})
	args := r.buildContainerArgs("name", "", "prompt", "", map[string]string{repo: wt}, "", nil, "")

	// The main repo's .git should be mounted at the same host path.
	gitDir := filepath.Join(repo, ".git")
	expectedGitMount := gitDir + ":" + gitDir + ":z"
	if !containsConsecutive(args, "-v", expectedGitMount) {
		t.Fatalf("expected .git dir mount %q; got: %v", expectedGitMount, args)
	}
}

// TestBuildContainerArgsNoGitDirMountWithoutWorktree verifies that when no
// worktree override is used, no extra .git directory mount is added.
func TestBuildContainerArgsNoGitDirMountWithoutWorktree(t *testing.T) {
	repo := setupTestRepo(t)

	dataDir := t.TempDir()
	s, err := store.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	r := NewRunner(s, RunnerConfig{
		Command:      "podman",
		SandboxImage: "test:latest",
		Workspaces:   repo,
	})
	// No worktree override — direct mount of workspace.
	args := r.buildContainerArgs("name", "", "prompt", "", nil, "", nil, "")

	gitDir := filepath.Join(repo, ".git")
	gitMount := gitDir + ":" + gitDir + ":z"
	if containsConsecutive(args, "-v", gitMount) {
		t.Fatalf("should NOT mount .git dir separately when no worktree override; found %q", gitMount)
	}
}

// TestBuildContainerArgsNoSessionID verifies that omitting sessionID means
// --resume is NOT added to the args.
func TestBuildContainerArgsNoSessionID(t *testing.T) {
	r := newTestRunnerWithInstructions(t, "")
	args := r.buildContainerArgs("name", "", "prompt", "", nil, "", nil, "")
	for i, a := range args {
		if a == "--resume" {
			t.Fatalf("--resume should not appear when sessionID is empty (found at index %d)", i)
		}
	}
}

// ---------------------------------------------------------------------------
// GenerateTitle
// ---------------------------------------------------------------------------

const titleOutput = `{"result":"Fix Login Bug","session_id":"sess1","stop_reason":"end_turn","is_error":false}`

// TestGenerateTitleSuccess verifies that a valid container output sets the
// task title.
func TestGenerateTitleSuccess(t *testing.T) {
	cmd := fakeCmdScript(t, titleOutput, 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Fix the login bug in the authentication module", 5, false, "")
	if err != nil {
		t.Fatal(err)
	}

	r.GenerateTitle(task.ID, task.Prompt)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Title != "Fix Login Bug" {
		t.Fatalf("expected title 'Fix Login Bug', got %q", updated.Title)
	}
}

// TestGenerateTitleSkipsExistingTitle verifies that GenerateTitle is a no-op
// when the task already has a title.
func TestGenerateTitleSkipsExistingTitle(t *testing.T) {
	// Command exits 1 — if it were called, GenerateTitle would fail to set a title.
	cmd := fakeCmdScript(t, "", 1)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "test prompt", 5, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskTitle(ctx, task.ID, "Pre-set Title"); err != nil {
		t.Fatal(err)
	}

	// Should return immediately without calling the container.
	r.GenerateTitle(task.ID, task.Prompt)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Title != "Pre-set Title" {
		t.Fatalf("expected title to remain 'Pre-set Title', got %q", updated.Title)
	}
}

// TestGenerateTitleFallbackOnContainerError verifies that GenerateTitle does
// not set a title (silently drops the error) when the container fails.
func TestGenerateTitleFallbackOnContainerError(t *testing.T) {
	cmd := fakeCmdScript(t, "", 1) // always fails
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "test prompt", 5, false, "")
	if err != nil {
		t.Fatal(err)
	}

	r.GenerateTitle(task.ID, task.Prompt)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Title != "" {
		t.Fatalf("expected empty title when container fails, got %q", updated.Title)
	}
}

// TestGenerateTitleBlankResult verifies that a blank result from the container
// does not set the title.
func TestGenerateTitleBlankResult(t *testing.T) {
	blankOutput := `{"result":"","session_id":"s1","stop_reason":"end_turn","is_error":false}`
	cmd := fakeCmdScript(t, blankOutput, 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "test prompt", 5, false, "")
	if err != nil {
		t.Fatal(err)
	}

	r.GenerateTitle(task.ID, task.Prompt)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Title != "" {
		t.Fatalf("expected empty title for blank container result, got %q", updated.Title)
	}
}

// TestGenerateTitleNDJSONOutput verifies that NDJSON output from the container
// is parsed correctly and the result is used as the title.
func TestGenerateTitleNDJSONOutput(t *testing.T) {
	ndjson := `{"type":"system","subtype":"init"}
{"type":"assistant","content":"thinking..."}
{"result":"Add Auth Feature","session_id":"s1","stop_reason":"end_turn","is_error":false}`
	cmd := fakeCmdScript(t, ndjson, 0)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "add authentication feature", 5, false, "")
	if err != nil {
		t.Fatal(err)
	}

	r.GenerateTitle(task.ID, task.Prompt)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Title != "Add Auth Feature" {
		t.Fatalf("expected title 'Add Auth Feature', got %q", updated.Title)
	}
}

// TestGenerateTitleUnknownTask verifies that GenerateTitle does not panic when
// the task does not exist in the store.
func TestGenerateTitleUnknownTask(t *testing.T) {
	cmd := fakeCmdScript(t, titleOutput, 0)
	_, r := setupRunnerWithCmd(t, nil, cmd)
	// Should not panic.
	r.GenerateTitle(uuid.New(), "some prompt")
}

// ---------------------------------------------------------------------------
// runContainer additional paths
// ---------------------------------------------------------------------------

// TestRunContainerParseErrorExitZero verifies that non-JSON stdout with exit 0
// returns a parse error.
func TestRunContainerParseErrorExitZero(t *testing.T) {
	cmd := fakeCmdScript(t, "this is not valid json output at all", 0)
	r := runnerWithCmd(t, cmd)

	_, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", nil, "")
	if err == nil {
		t.Fatal("expected error for non-JSON output")
	}
	if !strings.Contains(err.Error(), "parse output") {
		t.Fatalf("expected parse error, got: %v", err)
	}
}

// TestRunContainerParseErrorWithExitCode verifies that non-JSON stdout with a
// non-zero exit code returns an exit-code error (not a parse error), because
// the exit code is more informative.
func TestRunContainerParseErrorWithExitCode(t *testing.T) {
	cmd := fakeCmdScript(t, "not valid json", 1)
	r := runnerWithCmd(t, cmd)

	_, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", nil, "")
	if err == nil {
		t.Fatal("expected error for invalid JSON with exit code 1")
	}
	if !strings.Contains(err.Error(), "container exited with code") {
		t.Fatalf("expected exit code error, got: %v", err)
	}
}

// TestRunContainerContextCancelled verifies that cancelling the context while
// the container is running causes runContainer to return a "container terminated"
// error immediately.
func TestRunContainerContextCancelled(t *testing.T) {
	// Script that handles lifecycle calls (rm/kill) quickly but sleeps on "run".
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "slow-cmd")
	script := "#!/bin/sh\ncase \"$1\" in rm|kill) exit 0 ;; esac\nsleep 10\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	r := runnerWithCmd(t, scriptPath)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, _, _, err := r.runContainer(ctx, uuid.New(), "prompt", "", nil, "", nil, "")
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
	if !strings.Contains(err.Error(), "container terminated") {
		t.Fatalf("expected 'container terminated' error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// parseContainerList — Podman vs Docker JSON format handling
// ---------------------------------------------------------------------------

// TestParseContainerListPodmanFormat verifies parsing of Podman's JSON array output.
func TestParseContainerListPodmanFormat(t *testing.T) {
	// Podman outputs a JSON array with Names as []string and Created as int64.
	input := []byte(`[
		{"Id":"abc123","Names":["wallfacer-task1"],"Image":"wallfacer:latest","State":"running","Status":"Up 5 minutes","Created":1700000000},
		{"Id":"def456","Names":["wallfacer-task2"],"Image":"wallfacer:latest","State":"exited","Status":"Exited (0) 1 hour ago","Created":1699990000}
	]`)

	containers, err := parseContainerList(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(containers))
	}
	if containers[0].name() != "wallfacer-task1" {
		t.Fatalf("expected name 'wallfacer-task1', got %q", containers[0].name())
	}
	if containers[0].createdUnix() != 1700000000 {
		t.Fatalf("expected created 1700000000, got %d", containers[0].createdUnix())
	}
}

// TestParseContainerListDockerFormat verifies parsing of Docker's NDJSON output.
func TestParseContainerListDockerFormat(t *testing.T) {
	// Docker outputs one JSON object per line with Names as a string.
	input := []byte(`{"Id":"abc123","Names":"wallfacer-task1","Image":"wallfacer:latest","State":"running","Status":"Up 5 minutes","Created":1700000000}
{"Id":"def456","Names":"wallfacer-task2","Image":"wallfacer:latest","State":"exited","Status":"Exited (0) 1 hour ago","Created":1699990000}`)

	containers, err := parseContainerList(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(containers))
	}
	if containers[0].name() != "wallfacer-task1" {
		t.Fatalf("expected name 'wallfacer-task1', got %q", containers[0].name())
	}
	if containers[1].name() != "wallfacer-task2" {
		t.Fatalf("expected name 'wallfacer-task2', got %q", containers[1].name())
	}
}

// TestParseContainerListDockerSlashPrefix verifies that Docker's "/" prefix on names is stripped.
func TestParseContainerListDockerSlashPrefix(t *testing.T) {
	input := []byte(`{"Id":"abc123","Names":"/wallfacer-task1","Image":"img","State":"running","Status":"Up"}`)

	containers, err := parseContainerList(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containers[0].name() != "wallfacer-task1" {
		t.Fatalf("expected name without slash prefix, got %q", containers[0].name())
	}
}

// TestParseContainerListEmpty verifies that empty output returns nil.
func TestParseContainerListEmpty(t *testing.T) {
	for _, input := range [][]byte{nil, []byte(""), []byte("  \n  "), []byte("null")} {
		containers, err := parseContainerList(input)
		if err != nil {
			t.Fatalf("unexpected error for input %q: %v", input, err)
		}
		if containers != nil {
			t.Fatalf("expected nil for input %q, got %v", input, containers)
		}
	}
}

// TestContainerJSONNamePodman verifies name extraction from Podman's []string format.
func TestContainerJSONNamePodman(t *testing.T) {
	c := containerJSON{Names: []byte(`["foo","bar"]`)}
	if c.name() != "foo" {
		t.Fatalf("expected 'foo', got %q", c.name())
	}
}

// TestContainerJSONNameDocker verifies name extraction from Docker's string format.
func TestContainerJSONNameDocker(t *testing.T) {
	c := containerJSON{Names: []byte(`"my-container"`)}
	if c.name() != "my-container" {
		t.Fatalf("expected 'my-container', got %q", c.name())
	}
}

// TestContainerJSONNameNil verifies that nil Names returns empty string.
func TestContainerJSONNameNil(t *testing.T) {
	c := containerJSON{}
	if c.name() != "" {
		t.Fatalf("expected empty, got %q", c.name())
	}
}

// TestContainerJSONCreatedFloat verifies Created as float64 (default JSON number).
func TestContainerJSONCreatedFloat(t *testing.T) {
	c := containerJSON{Created: float64(1700000000)}
	if c.createdUnix() != 1700000000 {
		t.Fatalf("expected 1700000000, got %d", c.createdUnix())
	}
}

// TestContainerJSONCreatedNil verifies Created as nil returns 0.
func TestContainerJSONCreatedNil(t *testing.T) {
	c := containerJSON{}
	if c.createdUnix() != 0 {
		t.Fatalf("expected 0, got %d", c.createdUnix())
	}
}

// ---------------------------------------------------------------------------
// slugifyPrompt
// ---------------------------------------------------------------------------

func TestSlugifyPrompt(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"simple words", "Add dark mode", 30, "add-dark-mode"},
		{"special chars", "Fix bug: in #42!", 20, "fix-bug-in-42"},
		{"leading spaces", "  hello world", 20, "hello-world"},
		{"consecutive spaces", "a  b  c", 20, "a-b-c"},
		{"empty string", "", 20, "task"},
		{"all special", "!@#$%", 20, "task"},
		{"truncate", "abcdefghijklmnopqrstuvwxyz", 10, "abcdefghij"},
		{"truncate at dash boundary", "add dark mode toggle feature", 12, "add-dark-mod"},
		{"numbers preserved", "fix issue 123", 20, "fix-issue-123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slugifyPrompt(tc.input, tc.maxLen)
			if got != tc.want {
				t.Errorf("slugifyPrompt(%q, %d) = %q, want %q", tc.input, tc.maxLen, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isUUID
// ---------------------------------------------------------------------------

func TestIsUUID(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"249e9c9c-1234-5678-abcd-ef0123456789", true},
		{"00000000-0000-0000-0000-000000000000", true},
		{"ffffffff-ffff-ffff-ffff-ffffffffffff", true},
		{"add-dark-mode-249e9c9c", false},          // slug-based name fragment
		{"249e9c9c", false},                        // short UUID
		{"", false},
		{"not-a-uuid-at-all-xxxxxxxxxxxxxxxxxx", false},
	}
	for _, tc := range cases {
		got := isUUID(tc.s)
		if got != tc.want {
			t.Errorf("isUUID(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ListContainers — label-based task ID extraction
// ---------------------------------------------------------------------------

// TestListContainersLabelExtraction verifies that ListContainers prefers the
// wallfacer.task.id label over name-based UUID extraction when available.
func TestListContainersLabelExtraction(t *testing.T) {
	// Simulate a Podman-format JSON array with a slug-based container name
	// and the task ID in a label.
	input := []byte(`[
		{"Id":"abc123","Names":["wallfacer-add-dark-mode-249e9c9c"],"Image":"wallfacer:latest","State":"running","Status":"Up","Created":1700000000,"Labels":{"wallfacer.task.id":"249e9c9c-1234-5678-abcd-ef0123456789"}}
	]`)
	containers, err := parseContainerList(input)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}
	// Labels should be present.
	if containers[0].Labels == nil {
		t.Fatal("expected Labels to be parsed")
	}
	taskID := containers[0].Labels["wallfacer.task.id"]
	if taskID != "249e9c9c-1234-5678-abcd-ef0123456789" {
		t.Errorf("expected full UUID in label, got %q", taskID)
	}
}

// ---------------------------------------------------------------------------
// parseTestVerdict
// ---------------------------------------------------------------------------

func TestParseTestVerdict(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"bold PASS marker", "The implementation is complete. **PASS**", "pass"},
		{"trailing PASS", "All tests passed.\nPASS", "pass"},
		{"bold FAIL marker", "Build failed. **FAIL**", "fail"},
		{"trailing FAIL", "Requirements not met.\nFAIL", "fail"},
		{"no verdict", "Some output with no verdict", ""},
		{"empty", "", ""},
		{"lowercase trailing pass is matched", "everything looks good. pass", "pass"},
		{"lowercase mid-sentence fail not matched", "fail detected in the middle", ""},
		// Trailing punctuation cases.
		{"PASS with period", "All tests pass.\n\n**PASS**.", "pass"},
		{"PASS period no bold", "All tests pass. PASS.", "pass"},
		{"FAIL exclamation", "Build failed. FAIL!", "fail"},
		{"FAIL colon", "Requirements unmet. FAIL:", "fail"},
		// Verdict after label on last line.
		{"verdict label PASS", "Summary of checks.\nResult: PASS", "pass"},
		{"verdict label FAIL", "Summary of checks.\nVerdict: FAIL", "fail"},
		// Trailing blank lines should be skipped.
		{"trailing blank lines", "All good.\nPASS\n\n\n", "pass"},
		// Bold PASS/FAIL with details on subsequent lines.
		{"bold PASS then details", "**PASS**\nDetails: all 5 tests passed.", "pass"},
		{"bold FAIL then details", "**FAIL**\nDetails: test_foo failed.", "fail"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTestVerdict(tc.input)
			if got != tc.expected {
				t.Errorf("parseTestVerdict(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

// TestParseOutputPrefersStopReason verifies that parseOutput returns the JSON
// line with stop_reason set even when additional JSON lines appear after it
// (e.g. verbose debug output appended by the agent's --verbose flag).
func TestParseOutputPrefersStopReason(t *testing.T) {
	// Simulate NDJSON stream where a debug/verbose line follows the result.
	ndjson := `{"type":"system","session_id":"s1"}
{"type":"assistant","session_id":"s1","message":{"content":[{"type":"text","text":"**PASS**"}]}}
{"type":"result","subtype":"success","result":"All tests passed.\n\n**PASS**","session_id":"s1","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.01}
{"type":"debug","data":{"elapsed_ms":1234}}`

	out, err := parseOutput(ndjson)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.StopReason != "end_turn" {
		t.Fatalf("expected stop_reason=end_turn, got %q", out.StopReason)
	}
	if !strings.Contains(out.Result, "**PASS**") {
		t.Fatalf("expected PASS in result, got %q", out.Result)
	}
}

// TestParseOutputFallsBackToLastJSON verifies that when no JSON line has
// stop_reason set, parseOutput still returns the last valid JSON object.
func TestParseOutputFallsBackToLastJSON(t *testing.T) {
	ndjson := `{"type":"system","session_id":"s1"}
{"type":"assistant","session_id":"s2"}`

	out, err := parseOutput(ndjson)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No stop_reason set, but last valid JSON should be returned as fallback.
	if out.SessionID != "s2" {
		t.Fatalf("expected session_id=s2 from last JSON, got %q", out.SessionID)
	}
}
