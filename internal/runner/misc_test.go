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

	"github.com/google/uuid"
	"latere.ai/x/pkg/cmdexec"
	"latere.ai/x/pkg/gitutil"

	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/store/storetest"
)

// ---------------------------------------------------------------------------
// Runner getters
// ---------------------------------------------------------------------------

// TestWorkspacesEmpty verifies that Workspaces() returns nil when no
// workspaces are configured.
func TestWorkspacesEmpty(t *testing.T) {
	dataDir := t.TempDir()
	s, err := storetest.NewFileStore(t, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRunner(s, RunnerConfig{})
	t.Cleanup(func() { r.Shutdown() })
	if r.Workspaces() != nil {
		t.Fatal("expected nil when workspaces is empty")
	}
}

// TestWorkspacesMultiple verifies that Workspaces() correctly splits a
// space-separated workspace list.
func TestWorkspacesMultiple(t *testing.T) {
	dataDir := t.TempDir()
	s, err := storetest.NewFileStore(t, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRunner(s, RunnerConfig{
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
	s, err := storetest.NewFileStore(t, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	pathWithSpaces := "/home/user/My Project/repo"
	r := NewRunner(s, RunnerConfig{
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
// the same taskID returns the same paths without error (idempotent behavior).
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

// TestResolveConflicts_PromptNamesTheWorktree verifies that the conflict
// resolver is told the host path it works in, which is also its working
// directory, rather than a path that exists only inside a container.
func TestResolveConflicts_PromptNamesTheWorktree(t *testing.T) {
	repo := setupTestRepo(t)
	mock := &MockSandboxBackend{responses: []ContainerResponse{{Stdout: []byte(endTurnOutput)}}}
	s, r := setupRunnerWithMockBackend(t, []string{repo}, mock)
	ctx := context.Background()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "conflict prompt path", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	worktreePath := t.TempDir()
	if err := r.resolveConflicts(ctx, task.ID, repo, worktreePath, "", "main", ConflictResolverTriggerCommit, 1, 3); err != nil {
		t.Fatalf("resolveConflicts: %v", err)
	}
	calls := mock.RunArgsCalls()
	if len(calls) != 1 {
		t.Fatalf("launches = %d, want 1", len(calls))
	}
	if calls[0].WorkDir != worktreePath {
		t.Errorf("WorkDir = %q, want the worktree %q", calls[0].WorkDir, worktreePath)
	}
	if prompt := strings.Join(calls[0].Args, " "); !strings.Contains(prompt, worktreePath) || strings.Contains(prompt, "/workspace/") {
		t.Errorf("prompt does not name the worktree %q: %q", worktreePath, prompt)
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

	out, stdout, stderr, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", "", "")
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

	out, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", "", "")
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

	_, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", "", "")
	if err == nil {
		t.Fatal("expected error for empty container output with non-zero exit")
	}
}

// TestRunContainerEmptyOutputZeroExit verifies that empty stdout + exit 0
// returns an "empty output" error.
func TestRunContainerEmptyOutputZeroExit(t *testing.T) {
	cmd := fakeCmdScript(t, "", 0)
	r := runnerWithCmd(t, cmd)

	_, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", "", "")
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
	out, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "sess-xyz", nil, "", "", "")
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

	out, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", "", "")
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

// TestRunContainerRegistersHandleForKill verifies that a heavyweight task
// container registers its launch handle so KillContainer can actually signal
// the running agent. Before OnLaunch was wired into runContainer the
// registration was name-only, GetHandle returned nil, and KillContainer was a
// permanent no-op — so a user cancel never stopped the running agent.
func TestRunContainerRegistersHandleForKill(t *testing.T) {
	cmd := fakeBlockingCmd(t)
	r := runnerWithCmd(t, cmd)

	taskID := uuid.New()
	done := make(chan error, 1)
	go func() {
		_, _, _, err := r.runContainer(context.Background(), taskID, "prompt", "", nil, "", "", "")
		done <- err
	}()

	// OnLaunch fires after the process starts and registers its handle.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if h, ok := r.taskProcesses.Load(taskID); ok && h != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("launch handle was never registered; KillContainer would be a no-op")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// KillContainer must now terminate the blocking agent.
	r.KillContainer(taskID)

	select {
	case <-done:
		// runContainer returned after the kill — success.
	case <-time.After(3 * time.Second):
		t.Fatal("KillContainer did not terminate the running container")
	}
}

// ---------------------------------------------------------------------------
// runContainer additional paths
// ---------------------------------------------------------------------------

// TestRunContainerParseErrorExitZero verifies that non-JSON stdout with exit 0
// returns a parse error.
func TestRunContainerParseErrorExitZero(t *testing.T) {
	cmd := fakeCmdScript(t, "this is not valid json output at all", 0)
	r := runnerWithCmd(t, cmd)

	_, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", "", "")
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

	_, _, _, err := r.runContainer(context.Background(), uuid.New(), "prompt", "", nil, "", "", "")
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

	_, _, _, err := r.runContainer(ctx, uuid.New(), "prompt", "", nil, "", "", "")
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
	s, err := storetest.NewFileStore(t, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRunner(s, RunnerConfig{EnvFile: envFile})
	t.Cleanup(func() { r.Shutdown() })
	if r.EnvFile() != envFile {
		t.Errorf("EnvFile() = %q, want %q", r.EnvFile(), envFile)
	}
}

// TestRunnerWorktreesDir verifies that WorktreesDir() returns the configured worktrees directory.
func TestRunnerWorktreesDir(t *testing.T) {
	dataDir := t.TempDir()
	s, err := storetest.NewFileStore(t, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	wtDir := filepath.Join(t.TempDir(), "worktrees")
	r := NewRunner(s, RunnerConfig{WorktreesDir: wtDir})
	t.Cleanup(func() { r.Shutdown() })
	if r.WorktreesDir() != wtDir {
		t.Errorf("WorktreesDir() = %q, want %q", r.WorktreesDir(), wtDir)
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

// ---------------------------------------------------------------------------
// sandboxForTaskActivity / modelFromEnvForSandbox
// ---------------------------------------------------------------------------

// TestSandboxForTask_NilTask_DefaultsToDefaultHarness verifies that
// sandboxForTaskActivity with a nil task falls back to the configured default harness.
// Pinning the assertion to harness.Default() (not a literal "claude") keeps the
// decoupling contract: when the native default changes, the resolver follows.
func TestSandboxForTask_NilTask_DefaultsToDefaultHarness(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	result := r.sandboxForTaskActivity(nil, activityImplementation)
	if result != harness.Default() {
		t.Errorf("sandboxForTaskActivity(nil) = %q, want %q", result, harness.Default())
	}
}

// TestSandboxForTask_TaskWithSandbox verifies that sandboxForTaskActivity returns the
// sandbox set on the task.
func TestSandboxForTask_TaskWithSandbox(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	task := &store.Task{Sandbox: "codex"}
	result := r.sandboxForTaskActivity(task, activityImplementation)
	if result != "codex" {
		t.Errorf("sandboxForTaskActivity(task{Sandbox:codex}) = %q, want %q", result, "codex")
	}
}

// TestSandboxForTask_EmptyTask_DefaultsToDefaultHarness verifies that a task
// with no sandbox configured falls back to the configured default harness.
// Asserts against harness.Default() so the decoupling holds when the default
// changes.
func TestSandboxForTask_EmptyTask_DefaultsToDefaultHarness(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	task := &store.Task{}
	result := r.sandboxForTaskActivity(task, activityImplementation)
	if result != harness.Default() {
		t.Errorf("sandboxForTaskActivity(empty task) = %q, want %q", result, harness.Default())
	}
}

// TestModelFromEnv_NoEnvFile verifies that modelFromEnvForSandbox returns an empty
// string when no env file is configured.
func TestModelFromEnv_NoEnvFile(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	// setupRunnerWithCmd leaves envFile empty.
	result := r.modelFromEnvForSandbox(harness.Claude)
	if result != "" {
		t.Errorf("modelFromEnvForSandbox with no env file = %q, want %q", result, "")
	}
}

// TestModelFromEnv_WithEnvFile verifies that modelFromEnvForSandbox reads the model
// from a populated env file.
func TestModelFromEnv_WithEnvFile(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("CLAUDE_DEFAULT_MODEL=claude-opus-4-5\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	s, err := storetest.NewFileStore(t, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRunner(s, RunnerConfig{
		EnvFile: envFile,
	})
	t.Cleanup(func() { r.Shutdown() })
	result := r.modelFromEnvForSandbox(harness.Claude)
	if result != "claude-opus-4-5" {
		t.Errorf("modelFromEnvForSandbox = %q, want %q", result, "claude-opus-4-5")
	}
}

// TestModelFromEnv_BadEnvFile verifies that modelFromEnvForSandbox returns empty string
// when the env file cannot be parsed.
func TestModelFromEnv_BadEnvFile(t *testing.T) {
	// Point to a non-existent file.
	dataDir := t.TempDir()
	s, err := storetest.NewFileStore(t, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRunner(s, RunnerConfig{
		EnvFile: "/nonexistent/path/.env",
	})
	t.Cleanup(func() { r.Shutdown() })
	result := r.modelFromEnvForSandbox(harness.Claude)
	if result != "" {
		t.Errorf("modelFromEnvForSandbox with bad env file = %q, want %q", result, "")
	}
}
