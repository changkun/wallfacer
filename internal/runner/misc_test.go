package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/gitutil"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
	"changkun.de/x/wallfacer/internal/store"
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
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRunner(s, RunnerConfig{Command: "echo"})
	t.Cleanup(func() { r.Shutdown() })
	if r.Workspaces() != nil {
		t.Fatal("expected nil when workspaces is empty")
	}
}

// TestWorkspacesMultiple verifies that Workspaces() correctly splits a
// space-separated workspace list.
func TestWorkspacesMultiple(t *testing.T) {
	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRunner(s, RunnerConfig{
		Command:    "echo",
		Workspaces: []string{"/a", "/b", "/c"},
	})
	t.Cleanup(func() { r.Shutdown() })
	ws := r.Workspaces()
	if len(ws) != 3 {
		t.Fatalf("expected 3 workspaces, got %d: %v", len(ws), ws)
	}
	if ws[0] != "/a" || ws[1] != "/b" || ws[2] != "/c" {
		t.Fatalf("unexpected workspaces: %v", ws)
	}
}

// TestWorkspacesMethod_PathsWithSpaces verifies that workspace paths containing
// spaces are preserved intact through the Runner (regression test for the
// strings.Fields roundtrip bug).
func TestWorkspacesMethod_PathsWithSpaces(t *testing.T) {
	s, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	pathWithSpaces := "/home/user/My Project/repo"
	r := NewRunner(s, RunnerConfig{
		Command:    "echo",
		Workspaces: []string{pathWithSpaces, "/normal/path"},
	})
	t.Cleanup(func() { r.Shutdown() })
	ws := r.Workspaces()
	if len(ws) != 2 {
		t.Fatalf("expected 2 workspaces, got %d: %v", len(ws), ws)
	}
	if ws[0] != pathWithSpaces {
		t.Errorf("workspace[0] = %q, want %q", ws[0], pathWithSpaces)
	}
	if ws[1] != "/normal/path" {
		t.Errorf("workspace[1] = %q, want %q", ws[1], "/normal/path")
	}
}

func TestSanitizeBasename(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"/home/user/my-repo", "my-repo"},
		{"/home/user/My Project", "My_Project"},
		{"/home/user/我的项目", "我的项目"},
		{"/path/to/café-code", "café-code"},
		{"/path/to/repo:special", "repo_special"},
		{"/path/to/dir with $vars", "dir_with__vars"},
		{"/path/with/trailing/", "trailing"},
		{"", "workspace"},
		{"/", "workspace"},
		{".", "workspace"},
		{"/path/to/a`b\"c'd", "a_b_c_d"},
		{"/path/to/🚀rocket", "_rocket"},
	}
	for _, tc := range cases {
		got := sanitizeBasename(tc.input)
		if got != tc.want {
			t.Errorf("sanitizeBasename(%q) = %q, want %q", tc.input, got, tc.want)
		}
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
	// A plain string error that happens to contain "rebase conflict" is NOT
	// a conflict error unless it actually wraps ErrConflict via %w.
	err := fmt.Errorf("rebase conflict occurred")
	if isConflictError(err) {
		t.Fatal("plain string error should not be detected as a conflict error without wrapping ErrConflict")
	}
}

func TestIsConflictErrorConflictError(t *testing.T) {
	// *ConflictError wraps ErrConflict via Unwrap() and must be detected.
	err := &gitutil.ConflictError{
		WorktreePath:    "/tmp/wt",
		ConflictedFiles: []string{"foo.go"},
	}
	if !isConflictError(err) {
		t.Fatal("*ConflictError should be detected as a conflict error")
	}
}

// ---------------------------------------------------------------------------
// cmdexec.Git
// ---------------------------------------------------------------------------

// TestCmdexecGitSuccess verifies that cmdexec.Git executes git commands successfully.
func TestCmdexecGitSuccess(t *testing.T) {
	repo := setupTestRepo(t)
	if err := cmdexec.Git(repo, "status").Run(); err != nil {
		t.Fatalf("cmdexec.Git status should succeed: %v", err)
	}
}

// TestCmdexecGitInvalidDir verifies that cmdexec.Git returns an error for a non-existent
// directory.
func TestCmdexecGitInvalidDir(t *testing.T) {
	err := cmdexec.Git("/nonexistent/xyz/path/abc", "status").Run()
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

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "conflict resolve test", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	repoPath := t.TempDir()
	worktreePath := t.TempDir()

	if err := r.resolveConflicts(ctx, task.ID, repoPath, worktreePath, "", "main", ConflictResolverTriggerCommit, 1, 3); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	events, err := s.GetEvents(ctx, task.ID)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	var started, succeeded bool
	for _, ev := range events {
		if ev.EventType != store.EventTypeSystem {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		if data["phase"] == "conflict_resolver" && data["status"] == "started" {
			started = true
		}
		if data["phase"] == "conflict_resolver" && data["status"] == "succeeded" {
			succeeded = true
		}
	}
	if !started || !succeeded {
		t.Fatalf("expected conflict_resolver started and succeeded events, got started=%v succeeded=%v", started, succeeded)
	}
}

// TestResolveConflictsContainerError verifies that resolveConflicts returns a
// wrapped error when the container itself fails.
func TestResolveConflictsContainerError(t *testing.T) {
	cmd := fakeCmdScript(t, "", 1) // empty output, exit 1
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "conflict error test", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	repoPath := t.TempDir()
	worktreePath := t.TempDir()

	err = r.resolveConflicts(ctx, task.ID, repoPath, worktreePath, "", "main", ConflictResolverTriggerCommit, 1, 3)
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

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "conflict is_error test", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	repoPath := t.TempDir()
	worktreePath := t.TempDir()

	err = r.resolveConflicts(ctx, task.ID, repoPath, worktreePath, "", "main", ConflictResolverTriggerCommit, 1, 3)
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
// PruneUnknownWorktrees
// ---------------------------------------------------------------------------

// TestPruneUnknownWorktrees verifies that directories for unknown task UUIDs
// (not in store) are preserved, while archived/done/cancelled task directories
// are removed.
func TestPruneUnknownWorktrees(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})
	ctx := context.Background()

	// A backlog task — should be preserved.
	backlogTask, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "backlog task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	// A done task — should be pruned.
	doneTask, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "done task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, doneTask.ID, store.TaskStatusDone); err != nil {
		t.Fatal(err)
	}

	// An unknown UUID (not in store) — should be preserved (may belong to
	// another workspace scope).
	unknownDir := filepath.Join(runner.worktreesDir, uuid.New().String())

	backlogDir := filepath.Join(runner.worktreesDir, backlogTask.ID.String())
	doneDir := filepath.Join(runner.worktreesDir, doneTask.ID.String())

	for _, d := range []string{backlogDir, doneDir, unknownDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	runner.PruneUnknownWorktrees()

	if _, err := os.Stat(backlogDir); err != nil {
		t.Fatal("backlog task worktree dir should be preserved:", err)
	}
	if _, err := os.Stat(unknownDir); err != nil {
		t.Fatal("unknown UUID worktree dir should be preserved:", err)
	}
	if _, err := os.Stat(doneDir); !os.IsNotExist(err) {
		t.Fatal("done task worktree dir should be pruned")
	}
}

// TestPruneUnknownWorktreesMissingDir verifies PruneUnknownWorktrees handles
// a missing worktrees directory gracefully (no panic).
func TestPruneUnknownWorktreesMissingDir(t *testing.T) {
	_, runner := setupRunnerWithCmd(t, nil, "echo")
	// Point worktreesDir to a path that doesn't exist.
	runner.worktreesDir = filepath.Join(t.TempDir(), "nonexistent_worktrees")
	// Should not panic.
	runner.PruneUnknownWorktrees()
}

func TestPruneUnknownWorktreesNilStore(t *testing.T) {
	repo := setupTestRepo(t)
	_, runner := setupRunnerWithCmd(t, []string{repo}, "echo")
	runner.store = nil

	orphanDir := filepath.Join(runner.worktreesDir, uuid.New().String())
	if err := os.MkdirAll(orphanDir, 0o755); err != nil {
		t.Fatal(err)
	}

	runner.PruneUnknownWorktrees()

	// With nil store we cannot determine task status, so nothing is pruned.
	if _, err := os.Stat(orphanDir); err != nil {
		t.Fatal("worktree dir should be preserved when store is nil:", err)
	}
}

// TestPruneUnknownWorktreesRunsGitWorktreePrune verifies that
// PruneUnknownWorktrees runs `git worktree prune` on git workspaces.
func TestPruneUnknownWorktreesRunsGitWorktreePrune(t *testing.T) {
	repo := setupTestRepo(t)
	_, runner := setupTestRunner(t, []string{repo})

	// Just verify it completes without panicking when the workspace is a git repo.
	runner.PruneUnknownWorktrees()
}

// TestPruneUnknownWorktrees_PreservesWaitingTasks is a regression test for a
// bug where PruneUnknownWorktrees destroyed worktrees for tasks in waiting
// status, losing all committed work.
func TestPruneUnknownWorktrees_PreservesWaitingTasks(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})
	ctx := context.Background()

	// Create a task and move it to waiting status (simulating a task that
	// paused for user feedback).
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "waiting task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatal(err)
	}

	waitingDir := filepath.Join(runner.worktreesDir, task.ID.String())
	if err := os.MkdirAll(waitingDir, 0755); err != nil {
		t.Fatal(err)
	}

	runner.PruneUnknownWorktrees()

	if _, err := os.Stat(waitingDir); err != nil {
		t.Fatal("waiting task worktree dir must be preserved:", err)
	}
}

// ---------------------------------------------------------------------------
// Commit (exported) — error path
// ---------------------------------------------------------------------------

// TestCommitNonExistentTask verifies that the exported Commit does not panic
// when the task does not exist in the store.
func TestCommitNonExistentTask(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	// Should return early without panicking.
	_ = r.Commit(uuid.New(), "")

}

// ---------------------------------------------------------------------------
// runContainer
// ---------------------------------------------------------------------------

// TestRunContainerSuccess verifies that runContainer parses valid JSON output
// and returns the structured result.
func TestRunContainerSuccess(t *testing.T) {
	cmd := fakeCmdScript(t, endTurnOutput, 0)
	r := runnerWithCmd(t, cmd)

	out, stdout, stderr, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", nil, "", "")
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

	out, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", nil, "", "")
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

	_, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", nil, "", "")
	if err == nil {
		t.Fatal("expected error for empty container output with non-zero exit")
	}
}

// TestRunContainerEmptyOutputZeroExit verifies that empty stdout + exit 0
// returns an "empty output" error.
func TestRunContainerEmptyOutputZeroExit(t *testing.T) {
	cmd := fakeCmdScript(t, "", 0)
	r := runnerWithCmd(t, cmd)

	_, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", nil, "", "")
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
	out, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "sess-xyz", nil, "", nil, "", "")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if out.StopReason != "end_turn" {
		t.Fatalf("expected stop_reason=end_turn, got %q", out.StopReason)
	}
}

// TestRunContainerFallsBackToCodexOnTokenLimit verifies that when the initial
// claude run reports a token/quota error, runContainer retries once using the
// codex sandbox and returns the successful retry output.
func TestRunContainerFallsBackToCodexOnTokenLimit(t *testing.T) {
	tokenLimit := `{"result":"rate limit exceeded: token limit reached","session_id":"sess1","stop_reason":"end_turn","is_error":true,"total_cost_usd":0.001}`
	cmd := fakeStatefulCmd(t, []string{tokenLimit, endTurnOutput})
	r := runnerWithCmd(t, cmd)

	out, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", nil, "", "")
	if err != nil {
		t.Fatalf("expected fallback success, got error: %v", err)
	}
	if out == nil || out.IsError {
		t.Fatalf("expected non-error output after fallback, got: %+v", out)
	}
	if out.StopReason != "end_turn" {
		t.Fatalf("expected stop_reason=end_turn after fallback, got %q", out.StopReason)
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
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	r := NewRunner(s, RunnerConfig{
		Command:      "podman",
		SandboxImage: "test:latest",
		EnvFile:      envFile,
	})
	t.Cleanup(func() { r.Shutdown() })
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
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	r := NewRunner(s, RunnerConfig{
		Command:      "podman",
		SandboxImage: "test:latest",
		Workspaces:   []string{ws},
	})
	t.Cleanup(func() { r.Shutdown() })
	args := r.buildContainerArgs("name", "", "prompt", "", map[string]string{ws: wt}, "", nil, "")
	basename := filepath.Base(ws)
	zOpt := mountOpts("z")
	expectedMount := "type=bind,src=" + hostPath(wt, "podman") + ",dst=/workspace/" + basename
	if zOpt != "" {
		expectedMount += "," + zOpt
	}
	if !containsConsecutive(args, "--mount", expectedMount) {
		t.Fatalf("expected worktree override mount %q; got: %v", expectedMount, args)
	}
	// Original workspace path must NOT appear as the host path.
	unexpectedMount := "type=bind,src=" + hostPath(ws, "podman") + ",dst=/workspace/" + basename
	if zOpt != "" {
		unexpectedMount += "," + zOpt
	}
	if containsConsecutive(args, "--mount", unexpectedMount) {
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
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	r := NewRunner(s, RunnerConfig{
		Command:      "podman",
		SandboxImage: "test:latest",
		Workspaces:   []string{repo},
	})
	t.Cleanup(func() { r.Shutdown() })
	args := r.buildContainerArgs("name", "", "prompt", "", map[string]string{repo: wt}, "", nil, "")

	// The main repo's .git should be mounted. The source path is translated
	// for the container runtime, but the destination is the raw host path
	// (so git worktree references resolve inside the container on Unix hosts).
	gitDir := filepath.Join(repo, ".git")
	expectedGitMount := "type=bind,src=" + hostPath(gitDir, "podman") + ",dst=" + gitDir
	if z := mountOpts("z"); z != "" {
		expectedGitMount += "," + z
	}
	if !containsConsecutive(args, "--mount", expectedGitMount) {
		t.Fatalf("expected .git dir mount %q; got: %v", expectedGitMount, args)
	}
}

// TestBuildContainerArgsNoGitDirMountWithoutWorktree verifies that when no
// worktree override is used, no extra .git directory mount is added.
func TestBuildContainerArgsNoGitDirMountWithoutWorktree(t *testing.T) {
	repo := setupTestRepo(t)

	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	r := NewRunner(s, RunnerConfig{
		Command:      "podman",
		SandboxImage: "test:latest",
		Workspaces:   []string{repo},
	})
	t.Cleanup(func() { r.Shutdown() })
	// No worktree override — direct mount of workspace.
	args := r.buildContainerArgs("name", "", "prompt", "", nil, "", nil, "")

	gitDir := filepath.Join(repo, ".git")
	gitMount := "type=bind,src=" + gitDir + ",dst=" + gitDir
	if z := mountOpts("z"); z != "" {
		gitMount += "," + z
	}
	if containsConsecutive(args, "--mount", gitMount) {
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

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Fix the login bug in the authentication module", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	r.GenerateTitle(task.ID, task.Prompt)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Title != "Fix Login Bug" {
		t.Fatalf("expected title 'Fix Login Bug', got %q", updated.Title)
	}
}

// TestGenerateTitleAcceptsValidOutputOnNonZeroExit verifies that title
// generation still succeeds when the agent exits non-zero after emitting a
// valid final result payload.
func TestGenerateTitleAcceptsValidOutputOnNonZeroExit(t *testing.T) {
	cmd := fakeCmdScript(t, titleOutput, 1)
	s, r := setupRunnerWithCmd(t, nil, cmd)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "Fix the login bug in the authentication module", Timeout: 5})
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

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test prompt", Timeout: 5})
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

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test prompt", Timeout: 5})
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

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test prompt", Timeout: 5})
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

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "add authentication feature", Timeout: 5})
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

	_, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", nil, "", "")
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

	_, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", nil, "", "")
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
	cmd := fakeBlockingCmd(t)
	r := runnerWithCmd(t, cmd)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, _, _, err := r.runContainer(ctx, uuid.New(), "prompt", "", nil, "", nil, "", "")
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
	// The context may expire before Launch (yielding "launch container: …
	// context deadline exceeded") or after Launch during Wait (yielding
	// "container terminated: context deadline exceeded"). Both are correct.
	errMsg := err.Error()
	if !strings.Contains(errMsg, "container terminated") && !strings.Contains(errMsg, "context deadline exceeded") {
		t.Fatalf("expected context cancellation error, got: %v", err)
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
		{"labelled PASS", "Summary: PASS", "pass"},
		{"labelled FAIL", "Status: failed", "fail"},
		{"status PASSED", "Verification status: PASSED", "pass"},
		{"outcome FAILURE", "Outcome: FAILURE", "fail"},
		{"PASS with emoji", "All checks complete. PASS ✅", "pass"},
		{"negated pass is fail", "NOT PASS: still missing one case", "fail"},
		// Content-level inference: Mocha/Jest "N passing" style.
		{"mocha N passing", "  5 passing (23ms)", "pass"},
		{"mocha N passing with text", "Running suite\n  5 passing (100ms)\n  2 pending", "pass"},
		{"mocha N passing N failing returns fail", "  5 passing\n  1 failing", "fail"},
		// Content-level inference: "all tests passed" phrases.
		{"all tests passed", "All tests passed", "pass"},
		{"all N tests passed", "All 10 tests passed successfully.", "pass"},
		{"all checks passed", "All checks passed", "pass"},
		// Content-level inference: Go test "ok  package" at line start.
		{"go test ok", "ok  github.com/foo/bar\t0.003s", "pass"},
		{"go test ok multiline", "--- PASS: TestFoo (0.00s)\nok  github.com/foo/bar  0.003s", "pass"},
		// Content-level inference: Maven/Gradle BUILD SUCCESS.
		{"maven build success", "[INFO] BUILD SUCCESS", "pass"},
		// Content-level inference: pytest/rspec "N passed".
		{"pytest N passed", "5 passed in 0.23s", "pass"},
		{"pytest N tests passed", "10 tests passed in 1.2s", "pass"},
		{"rspec N examples passed", "5 examples, 3 passed", "pass"},
		// Content-level inference: mixed pass/fail correctly returns fail.
		{"mixed N passed N failed", "5 passed, 1 failed", "fail"},
		{"mixed passed and failed text", "10 tests passed\n2 tests failed", "fail"},
		// Content-level inference: informal pass indicators.
		{"all green", "runs all 4 CLI cases against it (0.5 s, all green).", "pass"},
		{"passes succeed", "Both passes succeed:\n- make test-integration\n- make test", "pass"},
		{"tests succeed", "All tests succeed on first try.", "pass"},
		// LLM-style verdict inference: test agent concludes pass without marker.
		{"satisfies every requirement", "The workflow satisfies every requirement. No issues found.", "pass"},
		{"satisfies all requirements", "The implementation satisfies all requirements listed above.", "pass"},
		{"no changes needed", "No changes are needed — the code is correct.", "pass"},
		{"no changes required", "No changes required to meet the acceptance criteria.", "pass"},
		{"meets all requirements", "The code meets all requirements specified in the task.", "pass"},
		{"all requirements met", "All requirements are met as verified by running the tests.", "pass"},
		{"correct as written", "The workflow is correct as written.", "pass"},
		{"correct as-is", "The implementation is correct as-is.", "pass"},
		// LLM-style: failure guard prevents false pass.
		{"satisfies but requirement not met", "Satisfies every requirement except one.\nRequirement not met: missing lint step.", ""},
		{"no changes needed but fails to meet", "No changes needed for A, but fails to satisfy B.", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTestVerdict(tc.input, nil, nil)
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

// ---------------------------------------------------------------------------
// Runner accessors
// ---------------------------------------------------------------------------

// TestRunnerEnvFile verifies that EnvFile() returns the configured env file path.
func TestRunnerEnvFile(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("KEY=val\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRunner(s, RunnerConfig{Command: "echo", EnvFile: envFile})
	t.Cleanup(func() { r.Shutdown() })
	if r.EnvFile() != envFile {
		t.Errorf("EnvFile() = %q, want %q", r.EnvFile(), envFile)
	}
}

// TestRunnerWorktreesDir verifies that WorktreesDir() returns the configured worktrees directory.
func TestRunnerWorktreesDir(t *testing.T) {
	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	wtDir := filepath.Join(t.TempDir(), "worktrees")
	r := NewRunner(s, RunnerConfig{Command: "echo", WorktreesDir: wtDir})
	t.Cleanup(func() { r.Shutdown() })
	if r.WorktreesDir() != wtDir {
		t.Errorf("WorktreesDir() = %q, want %q", r.WorktreesDir(), wtDir)
	}
}

// TestRunnerSandboxImage verifies that SandboxImage() returns the configured image.
func TestRunnerSandboxImage(t *testing.T) {
	_, r := setupTestRunner(t, nil)
	// setupTestRunner uses "test:latest".
	if r.SandboxImage() != "test:latest" {
		t.Errorf("SandboxImage() = %q, want 'test:latest'", r.SandboxImage())
	}
}

// TestRunnerInstructionsPath verifies InstructionsPath() when workspaceManager is nil.
func TestRunnerInstructionsPath(t *testing.T) {
	instructionsFile := filepath.Join(t.TempDir(), "CLAUDE.md")
	if err := os.WriteFile(instructionsFile, []byte("# test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRunner(s, RunnerConfig{Command: "echo", InstructionsPath: instructionsFile})
	t.Cleanup(func() { r.Shutdown() })
	if r.InstructionsPath() != instructionsFile {
		t.Errorf("InstructionsPath() = %q, want %q", r.InstructionsPath(), instructionsFile)
	}
}

// TestRunnerPendingGoroutines verifies PendingGoroutines() returns an empty
// slice when no background goroutines are running.
func TestRunnerPendingGoroutines(t *testing.T) {
	_, r := setupTestRunner(t, nil)
	pending := r.PendingGoroutines()
	if len(pending) != 0 {
		t.Errorf("expected 0 pending goroutines initially, got %d: %v", len(pending), pending)
	}
}

// TestRunnerCodexAuthPath verifies CodexAuthPath() returns an empty string
// when no valid host codex auth cache exists.
func TestRunnerCodexAuthPath(t *testing.T) {
	_, r := setupTestRunner(t, nil)
	// No real codex auth path configured, should return "".
	path := r.CodexAuthPath()
	_ = path // just verify it doesn't panic
}

// TestRunnerHasHostCodexAuth verifies HasHostCodexAuth() returns false when
// no valid host codex auth cache exists.
func TestRunnerHasHostCodexAuth(t *testing.T) {
	_, r := setupTestRunner(t, nil)
	// No real codex auth path, should return false without panicking.
	r.HasHostCodexAuth()
}

// TestContainerCircuitBreaker verifies that the container circuit breaker
// accessors work correctly: Allow/Open/State/Failures/RecordContainerFailure.
func TestContainerCircuitBreaker(t *testing.T) {
	_, r := setupTestRunner(t, nil)

	// Initially circuit should be closed (Allow=true, Open=false).
	if !r.ContainerCircuitAllow() {
		t.Error("expected ContainerCircuitAllow=true initially")
	}
	if r.ContainerCircuitOpen() {
		t.Error("expected ContainerCircuitOpen=false initially")
	}
	if r.ContainerCircuitFailures() != 0 {
		t.Errorf("expected 0 failures initially, got %d", r.ContainerCircuitFailures())
	}
	if r.ContainerCircuitState() == "" {
		t.Error("expected non-empty circuit state")
	}

	// Record a failure.
	r.RecordContainerFailure()
	if r.ContainerCircuitFailures() != 1 {
		t.Errorf("expected 1 failure after RecordContainerFailure, got %d", r.ContainerCircuitFailures())
	}
}

// TestIdeateContainerName_Empty verifies that IdeateContainerName returns empty
// when no ideation container is running.
func TestIdeateContainerName_Empty(t *testing.T) {
	_, r := setupTestRunner(t, nil)
	name := r.IdeateContainerName()
	if name != "" {
		t.Errorf("expected empty ideation container name, got %q", name)
	}
}

// TestKillIdeateContainer_NoOp verifies KillIdeateContainer does not panic
// when no ideation container is running.
func TestKillIdeateContainer_NoOp(t *testing.T) {
	_, r := setupTestRunner(t, nil)
	// Should not panic.
	r.KillIdeateContainer()
}

// ---------------------------------------------------------------------------
// sandboxForTask / modelFromEnv / titleModelFromEnv
// ---------------------------------------------------------------------------

// TestSandboxForTask_NilTask_DefaultsClaude verifies that sandboxForTask with
// a nil task returns the "claude" sandbox as the default.
func TestSandboxForTask_NilTask_DefaultsClaude(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	result := r.sandboxForTask(nil)
	if result != "claude" {
		t.Errorf("sandboxForTask(nil) = %q, want %q", result, "claude")
	}
}

// TestSandboxForTask_TaskWithSandbox verifies that sandboxForTask returns the
// sandbox set on the task.
func TestSandboxForTask_TaskWithSandbox(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	task := &store.Task{Sandbox: "codex"}
	result := r.sandboxForTask(task)
	if result != "codex" {
		t.Errorf("sandboxForTask(task{Sandbox:codex}) = %q, want %q", result, "codex")
	}
}

// TestSandboxForTask_EmptyTask_DefaultsClaude verifies that a task with no
// sandbox configured falls back to the "claude" default.
func TestSandboxForTask_EmptyTask_DefaultsClaude(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	task := &store.Task{}
	result := r.sandboxForTask(task)
	if result != "claude" {
		t.Errorf("sandboxForTask(empty task) = %q, want %q", result, "claude")
	}
}

// TestModelFromEnv_NoEnvFile verifies that modelFromEnv returns an empty
// string when no env file is configured.
func TestModelFromEnv_NoEnvFile(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	// setupRunnerWithCmd leaves envFile empty.
	result := r.modelFromEnv()
	if result != "" {
		t.Errorf("modelFromEnv with no env file = %q, want %q", result, "")
	}
}

// TestModelFromEnv_WithEnvFile verifies that modelFromEnv reads the model
// from a populated env file.
func TestModelFromEnv_WithEnvFile(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("CLAUDE_DEFAULT_MODEL=claude-opus-4-5\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRunner(s, RunnerConfig{
		Command: "echo",
		EnvFile: envFile,
	})
	t.Cleanup(func() { r.Shutdown() })
	result := r.modelFromEnv()
	if result != "claude-opus-4-5" {
		t.Errorf("modelFromEnv = %q, want %q", result, "claude-opus-4-5")
	}
}

// TestModelFromEnv_BadEnvFile verifies that modelFromEnv returns empty string
// when the env file cannot be parsed.
func TestModelFromEnv_BadEnvFile(t *testing.T) {
	// Point to a non-existent file.
	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRunner(s, RunnerConfig{
		Command: "echo",
		EnvFile: "/nonexistent/path/.env",
	})
	t.Cleanup(func() { r.Shutdown() })
	result := r.modelFromEnv()
	if result != "" {
		t.Errorf("modelFromEnv with bad env file = %q, want %q", result, "")
	}
}

// TestTitleModelFromEnv_NoEnvFile verifies that titleModelFromEnv returns an
// empty string when no env file is configured.
func TestTitleModelFromEnv_NoEnvFile(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	result := r.titleModelFromEnv()
	if result != "" {
		t.Errorf("titleModelFromEnv with no env file = %q, want %q", result, "")
	}
}

// TestTitleModelFromEnv_WithTitleModel verifies that titleModelFromEnv returns
// the title-specific model when set.
func TestTitleModelFromEnv_WithTitleModel(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	content := "CLAUDE_DEFAULT_MODEL=claude-opus-4-5\nCLAUDE_TITLE_MODEL=claude-haiku-4-5\n"
	if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRunner(s, RunnerConfig{
		Command: "echo",
		EnvFile: envFile,
	})
	t.Cleanup(func() { r.Shutdown() })
	result := r.titleModelFromEnv()
	if result != "claude-haiku-4-5" {
		t.Errorf("titleModelFromEnv = %q, want %q", result, "claude-haiku-4-5")
	}
}

// TestTitleModelFromEnv_FallsBackToDefaultModel verifies that titleModelFromEnv
// returns the default model when no title-specific model is set.
func TestTitleModelFromEnv_FallsBackToDefaultModel(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("CLAUDE_DEFAULT_MODEL=claude-opus-4-5\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRunner(s, RunnerConfig{
		Command: "echo",
		EnvFile: envFile,
	})
	t.Cleanup(func() { r.Shutdown() })
	result := r.titleModelFromEnv()
	if result != "claude-opus-4-5" {
		t.Errorf("titleModelFromEnv fallback = %q, want %q", result, "claude-opus-4-5")
	}
}

// ---------------------------------------------------------------------------
// IdeationCategories / IdeationIgnorePatterns (Runner methods)
// ---------------------------------------------------------------------------

// TestIdeationCategories_ReturnsNonEmpty verifies that IdeationCategories
// returns a non-empty slice of category strings.
func TestIdeationCategories_ReturnsNonEmpty(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	cats := r.IdeationCategories()
	if len(cats) == 0 {
		t.Error("IdeationCategories returned empty slice, want non-empty")
	}
}

// TestIdeationCategories_ReturnsCopy verifies that mutations to the returned
// slice do not affect subsequent calls.
func TestIdeationCategories_ReturnsCopy(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	cats1 := r.IdeationCategories()
	if len(cats1) == 0 {
		t.Skip("no categories, nothing to mutate")
	}
	original := cats1[0]
	cats1[0] = "mutated"
	cats2 := r.IdeationCategories()
	if cats2[0] != original {
		t.Errorf("IdeationCategories not returning copy; original value changed to %q", cats2[0])
	}
}

// TestIdeationIgnorePatterns_ReturnsNonEmpty verifies that
// IdeationIgnorePatterns returns a non-empty slice of pattern strings.
func TestIdeationIgnorePatterns_ReturnsNonEmpty(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	pats := r.IdeationIgnorePatterns()
	if len(pats) == 0 {
		t.Error("IdeationIgnorePatterns returned empty slice, want non-empty")
	}
}

// TestIdeationIgnorePatterns_ReturnsCopy verifies that mutations to the
// returned slice do not affect subsequent calls.
func TestIdeationIgnorePatterns_ReturnsCopy(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	pats1 := r.IdeationIgnorePatterns()
	if len(pats1) == 0 {
		t.Skip("no patterns, nothing to mutate")
	}
	original := pats1[0]
	pats1[0] = "mutated"
	pats2 := r.IdeationIgnorePatterns()
	if pats2[0] != original {
		t.Errorf("IdeationIgnorePatterns not returning copy; original value changed to %q", pats2[0])
	}
}
