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

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// fakeStatefulCmd creates an executable shell script that returns different
// JSON outputs on successive invocations. Container lifecycle calls ("rm",
// "kill") are silently skipped without advancing the counter, so only the
// real "run ..." calls consume an output slot.
func fakeStatefulCmd(t *testing.T, outputs []string) string {
	t.Helper()
	dir := t.TempDir()

	counterFile := filepath.Join(dir, "counter")
	if err := os.WriteFile(counterFile, []byte("0"), 0644); err != nil {
		t.Fatal(err)
	}

	for i, o := range outputs {
		p := filepath.Join(dir, fmt.Sprintf("out%d.txt", i))
		if err := os.WriteFile(p, []byte(o), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// last.txt is the fallback when the counter exceeds the number of outputs.
	last := outputs[len(outputs)-1]
	if err := os.WriteFile(filepath.Join(dir, "last.txt"), []byte(last), 0644); err != nil {
		t.Fatal(err)
	}

	// The script skips "rm", "kill", and "inspect" subcommands and uses a counter
	// to select the output file on each real "run" invocation.
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
  rm|kill|inspect|ps) exit 0 ;;
esac
count=$(cat %s 2>/dev/null || echo 0)
outfile=%s/out${count}.txt
if [ ! -f "$outfile" ]; then outfile=%s/last.txt; fi
cat "$outfile"
echo $((count+1)) > %s
`, counterFile, dir, dir, counterFile)

	scriptPath := filepath.Join(dir, "fake-stateful")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return scriptPath
}

// setupRunnerWithCmd creates a Store and Runner for testing with a custom
// container command. Useful when tests need to control container output.
// Accepts testing.TB so it can be used from both *testing.T and *testing.B.
func setupRunnerWithCmd(t testing.TB, workspaces []string, cmd string) (*store.Store, *Runner) {
	t.Helper()
	// Use /dev/shm (tmpfs) when available to avoid ENOTEMPTY from overlayfs in
	// container sandboxes. Heavy create/rename activity on overlayfs can cause
	// unlinkat(AT_REMOVEDIR) to fail even on apparently-empty directories.
	// Falling back to t.TempDir() on platforms where /dev/shm is absent (macOS).
	var dataDir string
	if dir, err := os.MkdirTemp("/dev/shm", "wallfacer-test-*"); err == nil {
		dataDir = dir
		t.Cleanup(func() { os.RemoveAll(dataDir) })
	} else {
		dataDir = t.TempDir()
	}
	s, err := store.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	worktreesDir := filepath.Join(t.TempDir(), "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(s, RunnerConfig{
		Command:      cmd,
		SandboxImage: "test:latest",
		Workspaces:   strings.Join(workspaces, " "),
		WorktreesDir: worktreesDir,
	})
	// Cleanups are called in LIFO order. Register WaitBackground first so it
	// runs second; register the subscription shutdown second so it runs first,
	// ensuring the board-cache-invalidator goroutine has exited cleanly (and
	// unsubscribed from the store) before the store is closed.
	t.Cleanup(r.WaitBackground)
	t.Cleanup(func() {
		close(r.shutdownCh)
		r.boardSubscriptionWg.Wait()
	})
	return s, r
}

// JSON fixtures for container output tests.
const (
	endTurnOutput   = `{"result":"task complete","session_id":"sess1","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001}`
	waitingOutput   = `{"result":"need feedback","session_id":"sess1","stop_reason":"","is_error":false,"total_cost_usd":0.001}`
	isErrorOutput   = `{"result":"claude error","session_id":"sess1","stop_reason":"end_turn","is_error":true,"total_cost_usd":0.001}`
	maxTokensOutput = `{"result":"partial result","session_id":"sess1","stop_reason":"max_tokens","is_error":false,"total_cost_usd":0.001}`
)

// ---------------------------------------------------------------------------
// Run — state transitions
// ---------------------------------------------------------------------------

// TestRunEndTurnTransitionsToDone verifies that Run moves the task to "done"
// when the container exits with stop_reason=end_turn.
func TestRunEndTurnTransitionsToDone(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, endTurnOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Test end_turn", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "do the task", "", false)

	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "waiting" {
		t.Fatalf("expected status=waiting (pending review), got %q", updated.Status)
	}
}

// TestRunWaitingTransitionsToWaiting verifies that an empty stop_reason
// moves the task to "waiting" (awaiting user feedback).
func TestRunWaitingTransitionsToWaiting(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, waitingOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Test waiting", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "some prompt", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "waiting" {
		t.Fatalf("expected status=waiting, got %q", updated.Status)
	}
}

// TestRunIsErrorTransitionsToFailed verifies that IsError=true moves the
// task to "failed".
func TestRunIsErrorTransitionsToFailed(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, isErrorOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Test is_error", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "do something", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "failed" {
		t.Fatalf("expected status=failed, got %q", updated.Status)
	}
}

// TestRunContainerErrorTransitionsToFailed verifies that a container error
// (empty output + non-zero exit) moves the task to "failed".
func TestRunContainerErrorTransitionsToFailed(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, "", 1) // empty output, exit 1
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Test container error", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "prompt", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "failed" {
		t.Fatalf("expected status=failed on container error, got %q", updated.Status)
	}
}

// TestRunMaxTokensAutoContinues verifies that max_tokens triggers an
// auto-continue turn and the task eventually reaches the terminal state.
func TestRunMaxTokensAutoContinues(t *testing.T) {
	repo := setupTestRepo(t)
	// First real call returns max_tokens; second returns end_turn.
	cmd := fakeStatefulCmd(t, []string{maxTokensOutput, endTurnOutput})
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Test max_tokens auto-continue", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "prompt", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "waiting" {
		t.Fatalf("expected status=waiting after max_tokens+end_turn, got %q", updated.Status)
	}
	if updated.Turns < 2 {
		t.Fatalf("expected at least 2 turns after auto-continue, got %d", updated.Turns)
	}
}

func TestRunMaxTokensTriggersStopReasonHandler(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeStatefulCmd(t, []string{maxTokensOutput, endTurnOutput})
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Stop reason handler test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}

	seen := false
	r.SetStopReasonHandler(func(_ uuid.UUID, reason string) {
		if reason == "max_tokens" {
			seen = true
		}
	})
	r.Run(task.ID, "prompt", "", false)

	if !seen {
		t.Fatal("expected max_tokens stop-reason handler to be called")
	}
}

// TestRunUnknownTaskDoesNotPanic verifies that Run handles a missing task
// gracefully (returns without panicking; deferred status update is a no-op).
func TestRunUnknownTaskDoesNotPanic(t *testing.T) {
	_, r := setupRunnerWithCmd(t, nil, "echo")
	// UUID does not exist in the store — should not panic.
	r.Run(uuid.New(), "prompt", "", false)
}

// TestRunWorktreeSetupFailureTransitionsToFailed verifies that a worktree
// setup failure (e.g. a non-existent workspace path) moves the task to
// "failed" rather than leaving it stuck.
func TestRunWorktreeSetupFailureTransitionsToFailed(t *testing.T) {
	// Use a workspace path that doesn't exist so CreateWorktree will fail.
	nonExistent := filepath.Join(t.TempDir(), "does_not_exist_repo")
	cmd := fakeCmdScript(t, endTurnOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{nonExistent}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Worktree fail task", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "prompt", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "failed" {
		t.Fatalf("expected status=failed when worktree setup fails, got %q", updated.Status)
	}
}

// TestRunEndTurnRecordsResult verifies that the task result and session ID
// are stored after a successful run.
func TestRunEndTurnRecordsResult(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, endTurnOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Result recording test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "do the task", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Result == nil || *updated.Result == "" {
		t.Fatal("expected non-empty result after Run")
	}
	if updated.SessionID == nil || *updated.SessionID == "" {
		t.Fatal("expected session ID to be recorded")
	}
}

// ---------------------------------------------------------------------------
// SyncWorktrees
// ---------------------------------------------------------------------------

// TestSyncWorktreesAlreadyUpToDate verifies that a worktree already at HEAD
// causes a skip (n=0 commits behind) and the task status is restored.
func TestSyncWorktreesAlreadyUpToDate(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "sync up-to-date test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	wt, br, err := runner.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(task.ID, wt, br) })

	if err := s.UpdateTaskWorktrees(ctx, task.ID, wt, br); err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatal(err)
	}

	// Worktree was just created from HEAD — 0 commits behind main.
	runner.SyncWorktrees(task.ID, "", "waiting")

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "waiting" {
		t.Fatalf("expected status=waiting after up-to-date sync, got %q", updated.Status)
	}
}

// TestSyncWorktreesBehindMain verifies that a worktree behind the default
// branch is rebased and the task status is restored to prevStatus.
func TestSyncWorktreesBehindMain(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "sync behind test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	wt, br, err := runner.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(task.ID, wt, br) })

	if err := s.UpdateTaskWorktrees(ctx, task.ID, wt, br); err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatal(err)
	}

	// Advance main with a new commit so the worktree is 1 commit behind.
	if err := os.WriteFile(filepath.Join(repo, "advance.txt"), []byte("advance\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "advance main branch")

	runner.SyncWorktrees(task.ID, "", "waiting")

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "waiting" {
		t.Fatalf("expected status=waiting after sync, got %q", updated.Status)
	}

	// The rebase should have brought advance.txt into the worktree.
	if _, err := os.Stat(filepath.Join(wt[repo], "advance.txt")); err != nil {
		t.Fatal("advance.txt should be in worktree after sync rebase:", err)
	}
}

// TestSyncWorktreesNonGitWorkspaceSkipped verifies that non-git workspaces
// are skipped during sync (logged as informational, not an error).
func TestSyncWorktreesNonGitWorkspaceSkipped(t *testing.T) {
	nonGitDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(nonGitDir, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	s, runner := setupTestRunner(t, []string{nonGitDir})
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "non-git sync test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	wt, br, err := runner.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(task.ID, wt, br) })

	if err := s.UpdateTaskWorktrees(ctx, task.ID, wt, br); err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatal(err)
	}

	// Non-git workspace is skipped, sync completes, status is restored.
	runner.SyncWorktrees(task.ID, "", "waiting")

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "waiting" {
		t.Fatalf("expected status=waiting after non-git sync, got %q", updated.Status)
	}
}

// TestSyncWorktreesNoWorktreePaths verifies SyncWorktrees on a task that has
// no WorktreePaths (e.g. a task that never started) — should complete without
// error and restore the status.
func TestSyncWorktreesNoWorktreePaths(t *testing.T) {
	s, runner := setupTestRunner(t, nil)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "no worktrees sync test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatal(err)
	}

	// No WorktreePaths set — the sync loop is a no-op.
	runner.SyncWorktrees(task.ID, "", "waiting")

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "waiting" {
		t.Fatalf("expected status=waiting, got %q", updated.Status)
	}
}

// ---------------------------------------------------------------------------
// failSync
// ---------------------------------------------------------------------------

// TestFailSync verifies that failSync sets the task status to "failed" and
// records the error message in the task result.
func TestFailSync(t *testing.T) {
	s, runner := setupTestRunner(t, nil)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "fail sync test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, "in_progress"); err != nil {
		t.Fatal(err)
	}

	runner.failSync(ctx, task.ID, "", 0, "simulated sync failure")

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "failed" {
		t.Fatalf("expected status=failed after failSync, got %q", updated.Status)
	}
	if updated.Result == nil || !strings.Contains(*updated.Result, "Sync failed") {
		t.Fatalf("expected result to contain 'Sync failed', got %v", updated.Result)
	}
	if updated.StopReason == nil || *updated.StopReason != "sync_failed" {
		t.Fatalf("expected stop_reason=sync_failed, got %v", updated.StopReason)
	}
}

// TestFailSyncRecordsErrorEvent verifies that failSync inserts an error event
// into the task's event trace.
func TestFailSyncRecordsErrorEvent(t *testing.T) {
	s, runner := setupTestRunner(t, nil)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "failSync event test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	runner.failSync(ctx, task.ID, "", 0, "disk full")

	events, _ := s.GetEvents(ctx, task.ID)
	foundError := false
	for _, ev := range events {
		if ev.EventType == "error" {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Fatal("expected an error event to be recorded by failSync")
	}
}

// TestRunWithPreexistingWorktrees verifies that Run reuses existing worktrees
// if they are already on disk (idempotent path).
func TestRunWithPreexistingWorktrees(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, waitingOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "preexisting worktrees test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Pre-create worktrees and persist them in the store (simulates a task
	// that already started and has existing worktrees).
	wt, br, err := r.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, wt, br); err != nil {
		t.Fatal(err)
	}

	// Run should detect existing worktrees and skip re-creation.
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "continue task", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	// With waitingOutput, task ends in waiting.
	if updated.Status != "waiting" {
		t.Fatalf("expected status=waiting, got %q", updated.Status)
	}

	// Cleanup (worktrees still exist since Run didn't commit).
	r.cleanupWorktrees(task.ID, wt, br)
}

// TestSyncWorktreesUnknownTask verifies that SyncWorktrees on a non-existent
// task does not panic (deferred status restore is a no-op).
func TestSyncWorktreesUnknownTask(t *testing.T) {
	_, runner := setupRunnerWithCmd(t, nil, "echo")
	// Should not panic.
	runner.SyncWorktrees(uuid.New(), "", "waiting")
}

// TestRunUsageAccumulation verifies that token usage returned by the container
// is accumulated in the task store.
func TestRunUsageAccumulation(t *testing.T) {
	repo := setupTestRepo(t)
	usageOutput := `{"result":"done","session_id":"s1","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.05,"usage":{"input_tokens":100,"output_tokens":50}}`
	cmd := fakeCmdScript(t, usageOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Usage test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "task prompt", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Usage.InputTokens == 0 {
		t.Fatal("expected input tokens to be accumulated")
	}
	if updated.Usage.CostUSD == 0 {
		t.Fatal("expected cost to be accumulated")
	}
}

// TestRunCostMultiTurn verifies that per-invocation cost and token values from
// each container invocation are accumulated correctly. The agent's -p mode
// reports per-invocation totals (not session-cumulative), so each turn's values
// represent only that turn's consumption and should be summed directly.
func TestRunCostMultiTurn(t *testing.T) {
	repo := setupTestRepo(t)
	// Turn 1: max_tokens, per-invocation cost 0.03, tokens 100/50
	// Turn 2: end_turn, per-invocation cost 0.02, tokens 80/40
	// Total: 0.03 + 0.02 = 0.05 cost, 100+80=180 input, 50+40=90 output
	turn1 := `{"result":"partial","session_id":"s1","stop_reason":"max_tokens","is_error":false,"total_cost_usd":0.03,"usage":{"input_tokens":100,"output_tokens":50}}`
	turn2 := `{"result":"done","session_id":"s1","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.02,"usage":{"input_tokens":80,"output_tokens":40}}`
	cmd := fakeStatefulCmd(t, []string{turn1, turn2})
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Multi-turn cost test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "prompt", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	// Cost should be 0.05 (sum of per-invocation: 0.03 + 0.02).
	if updated.Usage.CostUSD < 0.049 || updated.Usage.CostUSD > 0.051 {
		t.Errorf("CostUSD = %f, want ~0.05", updated.Usage.CostUSD)
	}
	// Tokens should be 180/90 (sum of per-invocation: 100+80, 50+40).
	if updated.Usage.InputTokens != 180 {
		t.Errorf("InputTokens = %d, want 180", updated.Usage.InputTokens)
	}
	if updated.Usage.OutputTokens != 90 {
		t.Errorf("OutputTokens = %d, want 90", updated.Usage.OutputTokens)
	}
}

// TestRunCostResumedFromWaiting verifies that cost/token values are summed
// correctly when a task goes waiting → in_progress (feedback resume). Each
// container invocation reports per-invocation values that are accumulated,
// including oversight sub-agent calls that are now also tracked.
func TestRunCostResumedFromWaiting(t *testing.T) {
	repo := setupTestRepo(t)
	// call1: waiting (stop_reason=""), per-invocation cost 0.03, tokens 100/50.
	// call2: end_turn,               per-invocation cost 0.04, tokens 150/70.
	//
	// Invocation sequence (fakeStatefulCmd counter):
	//   0 → call1: implementation turn 1 (→ waiting)         +0.03, +100in, +50out
	//   1 → call2: oversight for waiting  (synchronous)       +0.04, +150in, +70out
	//   2 → call2: implementation turn 2  (→ done)            +0.04, +150in, +70out
	//   3 → call2: commit message gen     (not accumulated)
	//   4 → call2: oversight for done     (background goroutine, after WaitBackground)
	//                                                          +0.04, +150in, +70out
	// Grand total: 0.15, 550 input, 260 output.
	call1 := `{"result":"need input","session_id":"s1","stop_reason":"","is_error":false,"total_cost_usd":0.03,"usage":{"input_tokens":100,"output_tokens":50}}`
	call2 := `{"result":"done","session_id":"s1","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.04,"usage":{"input_tokens":150,"output_tokens":70}}`
	cmd := fakeStatefulCmd(t, []string{call1, call2})
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Waiting resume cost test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	// First Run: goes to waiting.
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "prompt", "", false)
	waiting, _ := s.GetTask(ctx, task.ID)
	if waiting.Status != "waiting" {
		t.Fatalf("expected waiting, got %q", waiting.Status)
	}

	// Second Run (feedback resume): goes to waiting (pending review).
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "continue", *waiting.SessionID, false)
	// Wait for all background goroutines (oversight) to finish so the
	// cost totals are deterministic before we read the final values.
	r.WaitBackground()
	final, _ := s.GetTask(ctx, task.ID)
	if final.Status != "waiting" {
		t.Fatalf("expected waiting, got %q", final.Status)
	}

	// Total cost: 0.03 (impl1) + 0.04 (oversight-waiting) + 0.04 (impl2) + 0.04 (oversight-waiting2) = 0.15.
	if final.Usage.CostUSD < 0.149 || final.Usage.CostUSD > 0.151 {
		t.Errorf("CostUSD = %f, want ~0.15", final.Usage.CostUSD)
	}
	// Tokens: 100+150+150+150=550 input, 50+70+70+70=260 output.
	if final.Usage.InputTokens != 550 {
		t.Errorf("InputTokens = %d, want 550", final.Usage.InputTokens)
	}
	if final.Usage.OutputTokens != 260 {
		t.Errorf("OutputTokens = %d, want 260", final.Usage.OutputTokens)
	}
	// UsageBreakdown should separate implementation and oversight costs.
	bd := final.UsageBreakdown
	if bd == nil {
		t.Fatal("UsageBreakdown is nil")
	}
	if impl, ok := bd["implementation"]; !ok {
		t.Error("missing implementation breakdown")
	} else {
		// Turn 1 (0.03) + Turn 2 (0.04) = 0.07.
		if impl.CostUSD < 0.069 || impl.CostUSD > 0.071 {
			t.Errorf("implementation CostUSD = %f, want ~0.07", impl.CostUSD)
		}
	}
	if ov, ok := bd["oversight"]; !ok {
		t.Error("missing oversight breakdown")
	} else {
		// Waiting oversight (0.04) + waiting oversight2 (0.04) = 0.08.
		if ov.CostUSD < 0.079 || ov.CostUSD > 0.081 {
			t.Errorf("oversight CostUSD = %f, want ~0.08", ov.CostUSD)
		}
	}
}

// TestSyncWorktreesPrevStatusRestored verifies that SyncWorktrees restores
// a failed task to waiting (not back to failed, which would cause a retry
// loop), while non-failed tasks are restored to their original prevStatus.
func TestSyncWorktreesPrevStatusRestored(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "status restore test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	wt, br, err := runner.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(task.ID, wt, br) })

	if err := s.UpdateTaskWorktrees(ctx, task.ID, wt, br); err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed); err != nil {
		t.Fatal(err)
	}

	// A failed task should be restored to "waiting" after a successful sync,
	// not back to "failed" (which would cause a retry loop).
	runner.SyncWorktrees(task.ID, "", "failed")

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusWaiting {
		t.Fatalf("expected status=waiting after syncing a failed task, got %q", updated.Status)
	}
}

// TestRunWaitingFeedbackDonePreservesChanges is the critical end-to-end test
// for the exact bug scenario: in_progress → waiting → (feedback) → in_progress → done.
// It verifies that all changes from both runs are preserved on the default branch.
func TestRunWaitingFeedbackDonePreservesChanges(t *testing.T) {
	repo := setupTestRepo(t)

	// First call returns waiting (empty stop_reason), second returns end_turn.
	cmd := fakeStatefulCmd(t, []string{waitingOutput, endTurnOutput})
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Waiting→Done test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	// First Run: produces waitingOutput → task goes to "waiting".
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "do the task", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "waiting" {
		t.Fatalf("expected status=waiting after first run, got %q", updated.Status)
	}
	if len(updated.WorktreePaths) == 0 {
		t.Fatal("WorktreePaths should be populated after first run")
	}

	wt := updated.WorktreePaths[repo]

	// Simulate Claude writing a file during execution (between runs).
	if err := os.WriteFile(filepath.Join(wt, "task-output.txt"), []byte("task result\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Second Run (feedback resume): produces endTurnOutput → task goes to waiting
	// (pending review) instead of directly committing. Changes remain in the worktree.
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "continue", *updated.SessionID, false)

	final, _ := s.GetTask(ctx, task.ID)
	if final.Status != "waiting" {
		t.Fatalf("expected status=waiting after second run, got %q", final.Status)
	}

	// Verify the file still exists in the worktree (changes preserved for review).
	if _, err := os.Stat(filepath.Join(wt, "task-output.txt")); err != nil {
		t.Fatal("task-output.txt should still exist in worktree:", err)
	}
	content, _ := os.ReadFile(filepath.Join(wt, "task-output.txt"))
	if string(content) != "task result\n" {
		t.Fatalf("unexpected content: %q", content)
	}
}

// TestRunTestRunPreservesImplementationResult verifies that a test run (IsTestRun=true)
// does not overwrite the implementation agent's Result or SessionID. The test
// verdict is recorded in LastTestResult but the implementation output is left intact
// so the user can still see what was implemented and resume the same session.
func TestRunTestRunPreservesImplementationResult(t *testing.T) {
	repo := setupTestRepo(t)

	// Implementation agent: pauses at "waiting" (empty stop_reason).
	implOutput := `{"result":"implementation complete","session_id":"impl-sess","stop_reason":"","is_error":false,"total_cost_usd":0.001}`
	// Test agent: concludes with PASS verdict.
	testOutput := `{"result":"All checks passed. **PASS**","session_id":"test-sess","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001}`

	cmd := fakeStatefulCmd(t, []string{implOutput, testOutput})
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Preserve impl result test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Phase 1: implementation run → task goes to "waiting".
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "implement the feature", "", false)

	afterImpl, _ := s.GetTask(ctx, task.ID)
	if afterImpl.Status != "waiting" {
		t.Fatalf("expected status=waiting after implementation run, got %q", afterImpl.Status)
	}
	if afterImpl.Result == nil || *afterImpl.Result != "implementation complete" {
		t.Fatalf("expected implementation result, got %v", afterImpl.Result)
	}
	if afterImpl.SessionID == nil || *afterImpl.SessionID != "impl-sess" {
		t.Fatalf("expected impl-sess session ID, got %v", afterImpl.SessionID)
	}

	// Phase 2: mark as test run and run the test agent (fresh session "").
	if err := s.UpdateTaskTestRun(ctx, task.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, "in_progress"); err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "verify the implementation", "", false)

	afterTest, _ := s.GetTask(ctx, task.ID)
	if afterTest.Status != "waiting" {
		t.Fatalf("expected status=waiting after test run, got %q", afterTest.Status)
	}

	// Implementation result must NOT be overwritten by the test agent.
	if afterTest.Result == nil || *afterTest.Result != "implementation complete" {
		t.Fatalf("test run overwrote implementation result; got %v, want 'implementation complete'", afterTest.Result)
	}
	// Implementation session ID must NOT be overwritten by the test agent's session.
	if afterTest.SessionID == nil || *afterTest.SessionID != "impl-sess" {
		t.Fatalf("test run overwrote implementation session ID; got %v, want 'impl-sess'", afterTest.SessionID)
	}
	// Test verdict must be recorded.
	if afterTest.LastTestResult != "pass" {
		t.Fatalf("expected last_test_result=pass, got %q", afterTest.LastTestResult)
	}
	if afterTest.PendingTestFeedback != "" {
		t.Fatalf("expected no pending test feedback on pass, got %q", afterTest.PendingTestFeedback)
	}
	// IsTestRun must be cleared after the test completes.
	if afterTest.IsTestRun {
		t.Fatal("IsTestRun should be false after test completion")
	}
}

func TestRunTestRunFailStoresPendingFeedback(t *testing.T) {
	repo := setupTestRepo(t)

	implOutput := `{"result":"implementation done","session_id":"impl-sess","stop_reason":"","is_error":false,"total_cost_usd":0.001}`
	testOutput := `{"result":"pytest failed\n\n- TestFoo: expected 200, got 500\n\n**FAIL**","session_id":"test-sess","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001}`

	cmd := fakeStatefulCmd(t, []string{implOutput, testOutput})
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Store test failure feedback", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "implement the feature", "", false)

	if err := s.UpdateTaskTestRun(ctx, task.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "verify the implementation", "", false)

	afterTest, _ := s.GetTask(ctx, task.ID)
	if afterTest.Status != "waiting" {
		t.Fatalf("expected status=waiting after failed test run, got %q", afterTest.Status)
	}
	if afterTest.LastTestResult != "fail" {
		t.Fatalf("expected last_test_result=fail, got %q", afterTest.LastTestResult)
	}
	if !strings.Contains(afterTest.PendingTestFeedback, "Automated test verification failed.") {
		t.Fatalf("expected pending feedback header, got %q", afterTest.PendingTestFeedback)
	}
	if !strings.Contains(afterTest.PendingTestFeedback, "expected 200, got 500") {
		t.Fatalf("expected failing test details in pending feedback, got %q", afterTest.PendingTestFeedback)
	}
}

// TestRunTestRunFailVerdictWhenNoMarker verifies that when the test agent's
// output does not contain a recognizable PASS/FAIL marker, the verdict is stored
// as "fail" (not "") so the task is not auto-submitted without explicit confirmation.
func TestRunTestRunFailVerdictWhenNoMarker(t *testing.T) {
	repo := setupTestRepo(t)

	// Implementation agent: pauses at "waiting".
	implOutput := `{"result":"implementation done","session_id":"impl-sess","stop_reason":"","is_error":false,"total_cost_usd":0.001}`
	// Test agent: outputs a result without any explicit PASS/FAIL marker.
	testOutput := `{"result":"I reviewed the code and everything looks correct.","session_id":"test-sess","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001}`

	cmd := fakeStatefulCmd(t, []string{implOutput, testOutput})
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "No marker verdict test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "implement the feature", "", false)

	// Mark as test run and run the test agent.
	if err := s.UpdateTaskTestRun(ctx, task.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, "in_progress"); err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "verify the implementation", "", false)

	afterTest, _ := s.GetTask(ctx, task.ID)
	if afterTest.Status != "waiting" {
		t.Fatalf("expected status=waiting after test run, got %q", afterTest.Status)
	}
	// No clear verdict → should be "fail", not "".
	if afterTest.LastTestResult != "fail" {
		t.Fatalf("expected last_test_result=fail for ambiguous output, got %q", afterTest.LastTestResult)
	}
	if afterTest.IsTestRun {
		t.Fatal("IsTestRun should be false after test completion")
	}
}

// TestRunTestRunDefaultStopReasonSetsFail verifies that when the test
// agent's container produces an empty stop_reason (the "default" case), the
// task is still correctly transitioned to "waiting" with last_test_result set
// to "fail" — NOT left as "" ("unverified"). This covers the scenario where
// the agent's --verbose flag appends extra JSON after the result message and
// parseOutput ends up returning the wrong line.
func TestRunTestRunDefaultStopReasonSetsFail(t *testing.T) {
	repo := setupTestRepo(t)

	// Implementation agent: pauses at "waiting" (empty stop_reason).
	implOutput := `{"result":"impl done","session_id":"impl-sess","stop_reason":"","is_error":false,"total_cost_usd":0.001}`
	// Test agent: returns output with empty stop_reason (simulates parseOutput
	// picking up a verbose/debug line instead of the real result message).
	testOutput := `{"result":"","session_id":"test-sess","stop_reason":"","is_error":false,"total_cost_usd":0.001}`

	cmd := fakeStatefulCmd(t, []string{implOutput, testOutput})
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Default stop_reason test run", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "implement the feature", "", false)

	// Mark as test run and run the test agent.
	if err := s.UpdateTaskTestRun(ctx, task.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, "in_progress"); err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "verify the implementation", "", false)

	afterTest, _ := s.GetTask(ctx, task.ID)
	if afterTest.Status != "waiting" {
		t.Fatalf("expected status=waiting after test run, got %q", afterTest.Status)
	}
	// Must NOT be "" (unverified) — must be "fail" so the task is not auto-submitted.
	if afterTest.LastTestResult != "fail" {
		t.Fatalf("expected last_test_result=fail for empty stop_reason, got %q", afterTest.LastTestResult)
	}
	if afterTest.IsTestRun {
		t.Fatal("IsTestRun should be false after test completion")
	}
}

// TestRunTestRunMultiTurnContinuesWithTestSession verifies that when a test
// agent hits max_tokens and the loop auto-continues, it resumes the test
// agent's own session (not the implementation session) and correctly records
// the verdict when the second turn produces end_turn.
func TestRunTestRunMultiTurnContinuesWithTestSession(t *testing.T) {
	repo := setupTestRepo(t)

	// Implementation agent: pauses at "waiting" (empty stop_reason).
	implOutput := `{"result":"impl done","session_id":"impl-sess","stop_reason":"","is_error":false,"total_cost_usd":0.001}`
	// Test agent turn 1: hits max_tokens.
	testTurn1 := `{"result":"partial verification","session_id":"test-sess","stop_reason":"max_tokens","is_error":false,"total_cost_usd":0.001}`
	// Test agent turn 2: completes with PASS verdict.
	testTurn2 := `{"result":"All checks passed.\n\n**PASS**","session_id":"test-sess","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001}`

	cmd := fakeStatefulCmd(t, []string{implOutput, testTurn1, testTurn2})
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Multi-turn test run", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "implement the feature", "", false)

	// Mark as test run and run the test agent.
	if err := s.UpdateTaskTestRun(ctx, task.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, "in_progress"); err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "verify the implementation", "", false)

	afterTest, _ := s.GetTask(ctx, task.ID)
	if afterTest.Status != "waiting" {
		t.Fatalf("expected status=waiting after multi-turn test run, got %q", afterTest.Status)
	}
	if afterTest.LastTestResult != "pass" {
		t.Fatalf("expected last_test_result=pass, got %q", afterTest.LastTestResult)
	}
	// Implementation result and session must be preserved.
	if afterTest.Result == nil || *afterTest.Result != "impl done" {
		t.Fatalf("test run overwrote implementation result; got %v", afterTest.Result)
	}
	if afterTest.SessionID == nil || *afterTest.SessionID != "impl-sess" {
		t.Fatalf("test run overwrote implementation session; got %v", afterTest.SessionID)
	}
	if afterTest.IsTestRun {
		t.Fatal("IsTestRun should be false after test completion")
	}
}

// TestRunOversightTerminalBeforeWaiting verifies that when a task transitions
// to "waiting", the oversight has already reached a terminal state (ready or
// failed — never pending or generating). Oversight is generated synchronously
// before the status update so it is always viewable when the task is waiting.
func TestRunOversightTerminalBeforeWaiting(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, waitingOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Oversight before waiting test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "some prompt", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "waiting" {
		t.Fatalf("expected status=waiting, got %q", updated.Status)
	}

	// Oversight must be in a terminal state (ready or failed) — NOT pending or
	// generating — because it is generated synchronously before the status update.
	oversight, err := s.GetOversight(task.ID)
	if err != nil {
		t.Fatalf("unexpected error reading oversight: %v", err)
	}
	if oversight.Status == store.OversightStatusPending || oversight.Status == store.OversightStatusGenerating {
		t.Fatalf("oversight should be in terminal state when task is waiting, got %q", oversight.Status)
	}
}

// TestRunTestRunOversightTerminalBeforeWaiting verifies that when a test run
// completes (end_turn path), the impl oversight is preserved in terminal state
// and the test-agent oversight is also in terminal state before the task
// becomes visible as "waiting".
func TestRunTestRunOversightTerminalBeforeWaiting(t *testing.T) {
	repo := setupTestRepo(t)

	implOutput := `{"result":"impl done","session_id":"impl-sess","stop_reason":"","is_error":false,"total_cost_usd":0.001}`
	testOutput := `{"result":"All checks passed. **PASS**","session_id":"test-sess","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001}`

	cmd := fakeStatefulCmd(t, []string{implOutput, testOutput})
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Test run oversight before waiting", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "implement feature", "", false)

	if err := s.UpdateTaskTestRun(ctx, task.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, "in_progress"); err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "verify implementation", "", false)

	afterTest, _ := s.GetTask(ctx, task.ID)
	if afterTest.Status != "waiting" {
		t.Fatalf("expected status=waiting after test run, got %q", afterTest.Status)
	}

	// Implementation oversight must still be in terminal state (not overwritten).
	oversight, err := s.GetOversight(task.ID)
	if err != nil {
		t.Fatalf("unexpected error reading impl oversight: %v", err)
	}
	if oversight.Status == store.OversightStatusPending || oversight.Status == store.OversightStatusGenerating {
		t.Fatalf("impl oversight should be in terminal state when task is waiting, got %q", oversight.Status)
	}

	// Test-agent oversight must also be in terminal state.
	testOversight, err := s.GetTestOversight(task.ID)
	if err != nil {
		t.Fatalf("unexpected error reading test oversight: %v", err)
	}
	if testOversight.Status == store.OversightStatusPending || testOversight.Status == store.OversightStatusGenerating {
		t.Fatalf("test oversight should be in terminal state when task is waiting, got %q", testOversight.Status)
	}
}

// TestRunTestRunDefaultStopReasonTestOversightTerminal verifies that when a
// test run ends without an explicit stop_reason (default case), the
// test-agent oversight is also in terminal state before the task becomes
// visible as "waiting".
func TestRunTestRunDefaultStopReasonTestOversightTerminal(t *testing.T) {
	repo := setupTestRepo(t)

	implOutput := `{"result":"impl done","session_id":"impl-sess","stop_reason":"","is_error":false,"total_cost_usd":0.001}`
	testOutput := `{"result":"","session_id":"test-sess","stop_reason":"","is_error":false,"total_cost_usd":0.001}`

	cmd := fakeStatefulCmd(t, []string{implOutput, testOutput})
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Test run default stop_reason test oversight", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "implement feature", "", false)

	if err := s.UpdateTaskTestRun(ctx, task.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, "in_progress"); err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "verify implementation", "", false)

	afterTest, _ := s.GetTask(ctx, task.ID)
	if afterTest.Status != "waiting" {
		t.Fatalf("expected status=waiting after test run, got %q", afterTest.Status)
	}

	// Test-agent oversight must be in terminal state.
	testOversight, err := s.GetTestOversight(task.ID)
	if err != nil {
		t.Fatalf("unexpected error reading test oversight: %v", err)
	}
	if testOversight.Status == store.OversightStatusPending || testOversight.Status == store.OversightStatusGenerating {
		t.Fatalf("test oversight should be in terminal state when task is waiting, got %q", testOversight.Status)
	}
}

// Ensure time is imported to avoid unused import warnings.
var _ = time.Second

// TestSyncWorktreesBehindMainDirtyWorktree verifies that uncommitted changes in
// a worktree are stashed before the rebase and restored afterward (stash pop).
func TestSyncWorktreesBehindMainDirtyWorktree(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "dirty stash sync test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	wt, br, err := runner.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(task.ID, wt, br) })

	if err := s.UpdateTaskWorktrees(ctx, task.ID, wt, br); err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatal(err)
	}

	// Create an uncommitted change in the worktree (makes it "dirty").
	dirtyFile := filepath.Join(wt[repo], "dirty.txt")
	if err := os.WriteFile(dirtyFile, []byte("uncommitted\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Advance main so the worktree is 1 commit behind.
	if err := os.WriteFile(filepath.Join(repo, "advance2.txt"), []byte("advance\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "advance main for stash test")

	// SyncWorktrees should: stash dirty change -> rebase -> restore (stash pop).
	runner.SyncWorktrees(task.ID, "", "waiting")

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "waiting" {
		t.Fatalf("expected status=waiting after dirty sync, got %q", updated.Status)
	}

	// The advanced commit should be in the worktree after rebase.
	if _, err := os.Stat(filepath.Join(wt[repo], "advance2.txt")); err != nil {
		t.Fatal("advance2.txt should be in worktree after sync:", err)
	}
}

// TestSyncWorktreesConflictHandedOffToAgent verifies that when auto-resolution
// of a rebase conflict fails, the task is kept in_progress and Run() is
// invoked so the agent can resolve the conflict interactively. On completion
// of Run() the task transitions to "waiting" (not "failed").
func TestSyncWorktreesConflictHandedOffToAgent(t *testing.T) {
	repo := setupTestRepo(t)

	// First container invocation: resolver call — empty output causes an
	// "empty output" error, simulating a resolver that cannot fix conflicts.
	// Second invocation: Run() — returns waitingOutput so the task ends up
	// in "waiting" (agent reviewed the conflict and needs user feedback).
	cmd := fakeStatefulCmd(t, []string{"", waitingOutput})
	s, runner := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "conflict handoff test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	wt, br, err := runner.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(task.ID, wt, br) })

	if err := s.UpdateTaskWorktrees(ctx, task.ID, wt, br); err != nil {
		t.Fatal(err)
	}

	worktreePath := wt[repo]

	// Commit a conflicting change on the task branch.
	if err := os.WriteFile(filepath.Join(worktreePath, "README.md"), []byte("# Task version\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, worktreePath, "add", ".")
	gitRun(t, worktreePath, "commit", "-m", "task: modify README")

	// Commit a conflicting change on main (same file, different content).
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Main version\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "main: modify README")

	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatal(err)
	}

	// SyncWorktrees detects the conflict, the resolver fails (empty container
	// output), and the new code hands off to Run() with a conflict prompt.
	// Run() returns waitingOutput (stop_reason="") → task ends up in "waiting".
	runner.SyncWorktrees(task.ID, "", "waiting")

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "waiting" {
		t.Fatalf("expected status=waiting after conflict handoff to agent, got %q", updated.Status)
	}
}

// TestRunBudgetCostExceededTransitionsToWaiting verifies that when a task's
// accumulated cost exceeds MaxCostUSD after a turn, the runner transitions it
// to "waiting" and inserts a system event with budget_exceeded:true.
func TestRunBudgetCostExceededTransitionsToWaiting(t *testing.T) {
	repo := setupTestRepo(t)
	// endTurnOutput reports total_cost_usd=0.001; set MaxCostUSD=0.0005 so
	// a single turn already exceeds the budget.
	cmd := fakeCmdScript(t, endTurnOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Budget cost exceeded test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Set a tiny cost budget that will be exceeded by the first turn.
	maxCost := 0.0005
	if err := s.UpdateTaskBudget(ctx, task.ID, &maxCost, nil); err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "do the task", "", false)

	updated2, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated2.Status != "waiting" {
		t.Fatalf("expected status=waiting when cost budget exceeded, got %q", updated2.Status)
	}

	// Verify a system event with budget_exceeded:true was inserted.
	events, err := s.GetEvents(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundBudgetEvent := false
	for _, ev := range events {
		if ev.EventType == "system" {
			var data map[string]any
			if jsonErr := json.Unmarshal(ev.Data, &data); jsonErr == nil {
				if exceeded, ok := data["budget_exceeded"]; ok && exceeded == true {
					foundBudgetEvent = true
					break
				}
			}
		}
	}
	if !foundBudgetEvent {
		t.Fatal("expected a system event with budget_exceeded:true when cost budget is exceeded")
	}
}

// TestRunBudgetTokensExceededTransitionsToWaiting verifies that when a task's
// accumulated input tokens exceed MaxInputTokens after a turn, the runner
// transitions it to "waiting" with the correct system event.
func TestRunBudgetTokensExceededTransitionsToWaiting(t *testing.T) {
	repo := setupTestRepo(t)
	// Use a usage output that reports non-zero input tokens.
	usageOutput := `{"result":"done","session_id":"s1","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001,"usage":{"input_tokens":100,"output_tokens":50}}`
	cmd := fakeCmdScript(t, usageOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Budget tokens exceeded test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Set a tiny token budget (1 token) that will be exceeded by any real turn.
	maxTokens := 1
	if err := s.UpdateTaskBudget(ctx, task.ID, nil, &maxTokens); err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "do the task", "", false)

	updated2, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated2.Status != "waiting" {
		t.Fatalf("expected status=waiting when token budget exceeded, got %q", updated2.Status)
	}

	// Verify a system event with budget_exceeded:true was inserted.
	events, err := s.GetEvents(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundBudgetEvent := false
	for _, ev := range events {
		if ev.EventType == "system" {
			var data map[string]any
			if jsonErr := json.Unmarshal(ev.Data, &data); jsonErr == nil {
				if exceeded, ok := data["budget_exceeded"]; ok && exceeded == true {
					foundBudgetEvent = true
					break
				}
			}
		}
	}
	if !foundBudgetEvent {
		t.Fatal("expected a system event with budget_exceeded:true when token budget is exceeded")
	}
}

func TestSyncWorktrees_PreservesTestVerdictAfterCleanSync(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, "", 0)
	s, runner := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Sync preserves test verdict on clean rebase", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-b", "task-branch", wt, "HEAD")
	if err := s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repo: wt}, "task-branch"); err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusWaiting); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskTestRun(ctx, task.ID, false, "pass"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repo, "upstream.txt"), []byte("upstream\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "main: upstream change")

	runner.SyncWorktrees(task.ID, "", "waiting")

	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "waiting" {
		t.Fatalf("expected status=waiting after sync, got %q", updated.Status)
	}
	if updated.LastTestResult != "pass" {
		t.Fatalf("expected clean sync to preserve test verdict, got %q", updated.LastTestResult)
	}
}

// ---------------------------------------------------------------------------
// Mock executor tests
// ---------------------------------------------------------------------------

// setupRunnerWithMockExecutor creates a Store and Runner whose container calls
// are routed through the provided MockContainerExecutor instead of a real
// container runtime. The runner still needs a real git workspace for worktree
// setup; pass a repo returned by setupTestRepo(t) (or nil for idea-agent-only
// tasks that skip worktree setup).
func setupRunnerWithMockExecutor(t testing.TB, workspaces []string, mock *MockContainerExecutor) (*store.Store, *Runner) {
	t.Helper()
	s, r := setupRunnerWithCmd(t, workspaces, "true") // "true" for rm/kill calls, not used
	r.executor = mock
	return s, r
}

// TestMockMaxTokensAutoContinue verifies that stop_reason="max_tokens" on turn 1
// triggers an auto-continue and the task completes after stop_reason="end_turn"
// on turn 2. Two RunArgs calls must be recorded.
func TestMockMaxTokensAutoContinue(t *testing.T) {
	repo := setupTestRepo(t)
	mock := &MockContainerExecutor{
		responses: []ContainerResponse{
			{Stdout: []byte(`{"result":"partial","session_id":"sess1","stop_reason":"max_tokens","is_error":false,"total_cost_usd":0.001}`)},
			{Stdout: []byte(`{"result":"done","session_id":"sess1","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001}`)},
		},
	}
	s, r := setupRunnerWithMockExecutor(t, []string{repo}, mock)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "mock max_tokens test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "do the task", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusWaiting {
		t.Fatalf("expected status=waiting after max_tokens+end_turn, got %q", updated.Status)
	}
	if updated.Turns < 2 {
		t.Fatalf("expected at least 2 turns, got %d", updated.Turns)
	}
	calls := mock.RunArgsCalls()
	if len(calls) != 2 {
		t.Fatalf("expected exactly 2 RunArgs calls, got %d", len(calls))
	}
}

// TestMockPauseTurnAutoContinue verifies that stop_reason="pause_turn" on turn 1
// triggers an auto-continue, and the task completes after stop_reason="end_turn"
// on turn 2.
func TestMockPauseTurnAutoContinue(t *testing.T) {
	repo := setupTestRepo(t)
	mock := &MockContainerExecutor{
		responses: []ContainerResponse{
			{Stdout: []byte(`{"result":"paused","session_id":"sess2","stop_reason":"pause_turn","is_error":false,"total_cost_usd":0.001}`)},
			{Stdout: []byte(`{"result":"done","session_id":"sess2","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001}`)},
		},
	}
	s, r := setupRunnerWithMockExecutor(t, []string{repo}, mock)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "mock pause_turn test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "do the task", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusWaiting {
		t.Fatalf("expected status=waiting after pause_turn+end_turn, got %q", updated.Status)
	}
	if updated.Turns < 2 {
		t.Fatalf("expected at least 2 turns after pause_turn auto-continue, got %d", updated.Turns)
	}
	calls := mock.RunArgsCalls()
	if len(calls) != 2 {
		t.Fatalf("expected exactly 2 RunArgs calls, got %d", len(calls))
	}
}

// TestMockIdeaAgentJSONParsing verifies that a valid JSON idea array returned
// by the mock causes the idea-agent task to create child backlog tasks and
// transition to waiting.
func TestMockIdeaAgentJSONParsing(t *testing.T) {
	ideas := []IdeateResult{
		{Title: "Improve search", Prompt: "Add full-text search indexing to the tasks store."},
		{Title: "Add metrics", Prompt: "Expose Prometheus metrics for task state transitions."},
		{Title: "Refactor UI", Prompt: "Replace the inline styles with a Tailwind component library."},
	}
	ideasJSON := ideaOutput(ideas)

	mock := &MockContainerExecutor{
		responses: []ContainerResponse{
			{Stdout: []byte(ideasJSON)},
		},
	}
	s, r := setupRunnerWithMockExecutor(t, nil, mock)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "brainstorm via mock", 5, false, "", store.TaskKindIdeaAgent)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusWaiting {
		t.Fatalf("expected status=waiting after idea-agent run, got %q", updated.Status)
	}

	allTasks, _ := s.ListTasks(ctx, false)
	var childTasks []store.Task
	for _, tsk := range allTasks {
		if tsk.ID == task.ID {
			continue
		}
		if tsk.HasTag("idea-agent") {
			childTasks = append(childTasks, tsk)
		}
	}
	if len(childTasks) != len(ideas) {
		t.Fatalf("expected %d child tasks, got %d", len(ideas), len(childTasks))
	}
}

// TestMockDeferredPanicRecovery verifies that a panic inside RunArgs is caught
// by Run()'s deferred recover and the task transitions to failed with
// FailureCategoryUnknown.
func TestMockDeferredPanicRecovery(t *testing.T) {
	repo := setupTestRepo(t)
	mock := &MockContainerExecutor{
		responses: []ContainerResponse{
			{Panic: true},
		},
	}
	s, r := setupRunnerWithMockExecutor(t, []string{repo}, mock)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "panic recovery test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "do the task", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusFailed {
		t.Fatalf("expected status=failed after panic, got %q", updated.Status)
	}
	if updated.FailureCategory != store.FailureCategoryUnknown {
		t.Fatalf("expected failure_category=unknown after panic, got %q", updated.FailureCategory)
	}
}

// TestMockFailureCategoryContainerCrash verifies that empty stdout combined
// with a non-zero exit error causes classifyFailure to return
// FailureCategoryContainerCrash and the task to transition to failed.
func TestMockFailureCategoryContainerCrash(t *testing.T) {
	repo := setupTestRepo(t)
	// Provide 3 crash responses: 2 consumed by auto-retries, 1 for final failure.
	mock := &MockContainerExecutor{
		responses: []ContainerResponse{
			{Stdout: nil, Stderr: nil, Err: fmt.Errorf("exit status 1")},
			{Stdout: nil, Stderr: nil, Err: fmt.Errorf("exit status 1")},
			{Stdout: nil, Stderr: nil, Err: fmt.Errorf("exit status 1")},
		},
	}
	s, r := setupRunnerWithMockExecutor(t, []string{repo}, mock)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "container crash test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Run 3 times: 2 auto-retries then permanent failure.
	for i := range 3 {
		if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
			t.Fatalf("run %d: UpdateTaskStatus: %v", i+1, err)
		}
		r.Run(task.ID, "do the task", "", false)
	}

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != store.TaskStatusFailed {
		t.Fatalf("expected status=failed after budget exhausted, got %q", updated.Status)
	}
	if updated.FailureCategory != store.FailureCategoryContainerCrash {
		t.Fatalf("expected failure_category=container_crash, got %q", updated.FailureCategory)
	}
}

// TestMockSessionIDPassedToResume verifies that when turn 1 returns a
// session_id the value is forwarded as --resume in the args of turn 2.
func TestMockSessionIDPassedToResume(t *testing.T) {
	repo := setupTestRepo(t)
	const wantSession = "session-abc-123"
	mock := &MockContainerExecutor{
		responses: []ContainerResponse{
			// Turn 1: max_tokens so the loop continues; session_id is captured.
			{Stdout: []byte(`{"result":"partial","session_id":"` + wantSession + `","stop_reason":"max_tokens","is_error":false,"total_cost_usd":0.001}`)},
			// Turn 2: end_turn to terminate the loop.
			{Stdout: []byte(`{"result":"done","session_id":"` + wantSession + `","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001}`)},
		},
	}
	s, r := setupRunnerWithMockExecutor(t, []string{repo}, mock)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "session id resume test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "do the task", "", false)

	calls := mock.RunArgsCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 RunArgs calls, got %d", len(calls))
	}

	// Turn 2 args must contain "--resume" followed by the session ID.
	args2 := calls[1].Args
	foundResume := false
	for i, a := range args2 {
		if a == "--resume" && i+1 < len(args2) && args2[i+1] == wantSession {
			foundResume = true
			break
		}
	}
	if !foundResume {
		t.Fatalf("expected --resume %s in turn-2 args, got: %v", wantSession, args2)
	}
}


// ---------------------------------------------------------------------------
// Auto-retry tests
// ---------------------------------------------------------------------------

func TestAutoRetry_ContainerCrash(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, "", 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "crash retry test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	for i := range 3 {
		if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
			t.Fatalf("run %d: UpdateTaskStatus in_progress: %v", i+1, err)
		}
		r.Run(task.ID, "do the task", "", false)

		got, err := s.GetTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("run %d: GetTask: %v", i+1, err)
		}
		if i < 2 {
			if got.Status != store.TaskStatusBacklog {
				t.Errorf("run %d: expected backlog after retry, got %s", i+1, got.Status)
			}
		} else {
			if got.Status != store.TaskStatusFailed {
				t.Errorf("run %d: expected failed after budget exhausted, got %s", i+1, got.Status)
			}
			if got.FailureCategory != store.FailureCategoryContainerCrash {
				t.Errorf("run %d: expected failure_category=container_crash, got %s", i+1, got.FailureCategory)
			}
			if got.AutoRetryCount != 2 {
				t.Errorf("run %d: expected AutoRetryCount=2, got %d", i+1, got.AutoRetryCount)
			}
		}
	}
}

func TestAutoRetry_BudgetCategoryDoesNotRetry(t *testing.T) {
	repo := setupTestRepo(t)
	budgetErrOutput := `{"result":"budget exceeded","session_id":"s1","stop_reason":"end_turn","is_error":true,"total_cost_usd":0.001}`
	cmd := fakeCmdScript(t, budgetErrOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "budget exceeded test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "do the task", "", false)

	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TaskStatusFailed {
		t.Errorf("expected failed (no retry budget for budget_exceeded), got %s", got.Status)
	}
	if got.AutoRetryCount != 0 {
		t.Errorf("expected AutoRetryCount=0 (no retry attempted), got %d", got.AutoRetryCount)
	}
}

func TestAutoRetry_MaxTotalCap(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, "", 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "total cap test", 5, false, "", "")
	if err != nil {
		t.Fatal(err)
	}

	for range maxTotalAutoRetries {
		if err := s.IncrementAutoRetryCount(ctx, task.ID, store.FailureCategorySyncError); err != nil {
			t.Fatalf("IncrementAutoRetryCount: %v", err)
		}
	}

	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	r.Run(task.ID, "do the task", "", false)

	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TaskStatusFailed {
		t.Errorf("expected failed (total cap hit), got %s", got.Status)
	}
	if got.AutoRetryCount != maxTotalAutoRetries {
		t.Errorf("expected AutoRetryCount=%d (unchanged), got %d", maxTotalAutoRetries, got.AutoRetryCount)
	}
}

// TestParseTestVerdictCustomPatterns verifies that user-supplied regex patterns
// are evaluated before the built-in heuristics.
func TestParseTestVerdictCustomPatterns(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		customPass  []string
		customFail  []string
		expected    string
	}{
		{
			name:       "custom fail pattern matches",
			input:      "something went BOOM in the pipeline",
			customFail: []string{"BOOM"},
			expected:   "fail",
		},
		{
			name:       "custom pass pattern matches",
			input:      "compilation finished: BUILD OK",
			customPass: []string{"BUILD OK"},
			expected:   "pass",
		},
		{
			name:       "custom fail takes precedence over built-in pass heuristic",
			input:      "ok  github.com/foo/bar\t0.003s\nCUSTOM_FAIL_MARKER",
			customFail: []string{"CUSTOM_FAIL_MARKER"},
			expected:   "fail",
		},
		{
			name:       "invalid custom regex is silently skipped, falls through to built-in",
			input:      "ok  github.com/foo/bar\t0.003s",
			customFail: []string{"[invalid"},
			expected:   "pass", // built-in goTestOKPattern fires
		},
		{
			name:     "empty custom slices reproduce existing behaviour",
			input:    "5 passing (23ms)",
			expected: "pass",
		},
		{
			name:     "empty custom slices, no match",
			input:    "no verdict here",
			expected: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTestVerdict(tc.input, tc.customPass, tc.customFail)
			if got != tc.expected {
				t.Errorf("parseTestVerdict(%q, pass=%v, fail=%v) = %q, want %q",
					tc.input, tc.customPass, tc.customFail, got, tc.expected)
			}
		})
	}
}
