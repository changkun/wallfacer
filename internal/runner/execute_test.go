package runner

import (
	"context"
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

	// The script skips "rm" and "kill" subcommands and uses a counter to
	// select the output file on each real invocation.
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
  rm|kill) exit 0 ;;
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
func setupRunnerWithCmd(t *testing.T, workspaces []string, cmd string) (*store.Store, *Runner) {
	t.Helper()
	dataDir := t.TempDir()
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

	task, err := s.CreateTask(ctx, "Test end_turn", 5, false, "")
	if err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "do the task", "", false)

	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "done" {
		t.Fatalf("expected status=done, got %q", updated.Status)
	}
}

// TestRunWaitingTransitionsToWaiting verifies that an empty stop_reason
// moves the task to "waiting" (awaiting user feedback).
func TestRunWaitingTransitionsToWaiting(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, waitingOutput, 0)
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Test waiting", 5, false, "")
	if err != nil {
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

	task, err := s.CreateTask(ctx, "Test is_error", 5, false, "")
	if err != nil {
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

	task, err := s.CreateTask(ctx, "Test container error", 5, false, "")
	if err != nil {
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

	task, err := s.CreateTask(ctx, "Test max_tokens auto-continue", 5, false, "")
	if err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "prompt", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "done" {
		t.Fatalf("expected status=done after max_tokens+end_turn, got %q", updated.Status)
	}
	if updated.Turns < 2 {
		t.Fatalf("expected at least 2 turns after auto-continue, got %d", updated.Turns)
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

	task, err := s.CreateTask(ctx, "Worktree fail task", 5, false, "")
	if err != nil {
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

	task, err := s.CreateTask(ctx, "Result recording test", 5, false, "")
	if err != nil {
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

	task, err := s.CreateTask(ctx, "sync up-to-date test", 5, false, "")
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
	if err := s.UpdateTaskStatus(ctx, task.ID, "waiting"); err != nil {
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

	task, err := s.CreateTask(ctx, "sync behind test", 5, false, "")
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
	if err := s.UpdateTaskStatus(ctx, task.ID, "waiting"); err != nil {
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

	task, err := s.CreateTask(ctx, "non-git sync test", 5, false, "")
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
	if err := s.UpdateTaskStatus(ctx, task.ID, "waiting"); err != nil {
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

	task, err := s.CreateTask(ctx, "no worktrees sync test", 5, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskStatus(ctx, task.ID, "waiting"); err != nil {
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

	task, err := s.CreateTask(ctx, "fail sync test", 5, false, "")
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

	task, err := s.CreateTask(ctx, "failSync event test", 5, false, "")
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

	task, err := s.CreateTask(ctx, "preexisting worktrees test", 5, false, "")
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

	task, err := s.CreateTask(ctx, "Usage test", 5, false, "")
	if err != nil {
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
// each container invocation are accumulated correctly. Claude Code's -p mode
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

	task, err := s.CreateTask(ctx, "Multi-turn cost test", 5, false, "")
	if err != nil {
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
// container invocation reports per-invocation values that are accumulated.
func TestRunCostResumedFromWaiting(t *testing.T) {
	repo := setupTestRepo(t)
	// First call: waiting, per-invocation cost 0.03, tokens 100/50
	// Second call: end_turn, per-invocation cost 0.04, tokens 150/70
	// Total: 0.03 + 0.04 = 0.07 cost, 100+150=250 input, 50+70=120 output
	call1 := `{"result":"need input","session_id":"s1","stop_reason":"","is_error":false,"total_cost_usd":0.03,"usage":{"input_tokens":100,"output_tokens":50}}`
	call2 := `{"result":"done","session_id":"s1","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.04,"usage":{"input_tokens":150,"output_tokens":70}}`
	cmd := fakeStatefulCmd(t, []string{call1, call2})
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Waiting resume cost test", 5, false, "")
	if err != nil {
		t.Fatal(err)
	}

	// First Run: goes to waiting.
	r.Run(task.ID, "prompt", "", false)
	waiting, _ := s.GetTask(ctx, task.ID)
	if waiting.Status != "waiting" {
		t.Fatalf("expected waiting, got %q", waiting.Status)
	}

	// Second Run (feedback resume): goes to done.
	r.Run(task.ID, "continue", *waiting.SessionID, false)
	final, _ := s.GetTask(ctx, task.ID)
	if final.Status != "done" {
		t.Fatalf("expected done, got %q", final.Status)
	}

	// Cost should be 0.07 total (sum: 0.03 + 0.04).
	if final.Usage.CostUSD < 0.069 || final.Usage.CostUSD > 0.071 {
		t.Errorf("CostUSD = %f, want ~0.07", final.Usage.CostUSD)
	}
	// Tokens: 250 input (100 + 150), 120 output (50 + 70).
	if final.Usage.InputTokens != 250 {
		t.Errorf("InputTokens = %d, want 250", final.Usage.InputTokens)
	}
	if final.Usage.OutputTokens != 120 {
		t.Errorf("OutputTokens = %d, want 120", final.Usage.OutputTokens)
	}
}

// TestSyncWorktreesPrevStatusRestored verifies that SyncWorktrees restores
// the task to the exact prevStatus provided, not a hardcoded value.
func TestSyncWorktreesPrevStatusRestored(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "status restore test", 5, false, "")
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
	if err := s.UpdateTaskStatus(ctx, task.ID, "failed"); err != nil {
		t.Fatal(err)
	}

	// Restore to "failed" (a different prevStatus from "waiting").
	runner.SyncWorktrees(task.ID, "", "failed")

	updated, _ := s.GetTask(ctx, task.ID)
	if updated.Status != "failed" {
		t.Fatalf("expected status=failed (prevStatus), got %q", updated.Status)
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

	task, err := s.CreateTask(ctx, "Waiting→Done test", 5, false, "")
	if err != nil {
		t.Fatal(err)
	}

	// First Run: produces waitingOutput → task goes to "waiting".
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

	// Second Run (feedback resume): produces endTurnOutput → commit pipeline → done.
	r.Run(task.ID, "continue", *updated.SessionID, false)

	final, _ := s.GetTask(ctx, task.ID)
	if final.Status != "done" {
		t.Fatalf("expected status=done after second run, got %q", final.Status)
	}

	// Verify the file exists on the default branch after merge.
	if _, err := os.Stat(filepath.Join(repo, "task-output.txt")); err != nil {
		t.Fatal("task-output.txt should exist on default branch after commit pipeline:", err)
	}
	content, _ := os.ReadFile(filepath.Join(repo, "task-output.txt"))
	if string(content) != "task result\n" {
		t.Fatalf("unexpected content: %q", content)
	}

	// Verify CommitHashes and BaseCommitHashes are stored.
	if len(final.CommitHashes) == 0 {
		t.Error("CommitHashes should be populated after commit pipeline")
	}
	if len(final.BaseCommitHashes) == 0 {
		t.Error("BaseCommitHashes should be populated after commit pipeline")
	}
	if final.BaseCommitHashes[repo] == "" {
		t.Error("BaseCommitHashes should contain a hash for the repo")
	}
	if final.CommitHashes[repo] == "" {
		t.Error("CommitHashes should contain a hash for the repo")
	}
	// Base and commit hashes should differ (task added a commit).
	if final.BaseCommitHashes[repo] == final.CommitHashes[repo] {
		t.Error("BaseCommitHashes and CommitHashes should differ (task made changes)")
	}

	// Verify worktrees are cleaned up.
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("worktree should have been cleaned up after commit pipeline")
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

	task, err := s.CreateTask(ctx, "Preserve impl result test", 5, false, "")
	if err != nil {
		t.Fatal(err)
	}

	// Phase 1: implementation run → task goes to "waiting".
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
	// IsTestRun must be cleared after the test completes.
	if afterTest.IsTestRun {
		t.Fatal("IsTestRun should be false after test completion")
	}
}

// TestRunTestRunUnknownVerdictWhenNoMarker verifies that when the test agent's
// output does not contain a recognizable PASS/FAIL marker, the verdict is stored
// as "unknown" (not "") so the UI can distinguish "never tested" from "tested
// but no clear verdict".
func TestRunTestRunUnknownVerdictWhenNoMarker(t *testing.T) {
	repo := setupTestRepo(t)

	// Implementation agent: pauses at "waiting".
	implOutput := `{"result":"implementation done","session_id":"impl-sess","stop_reason":"","is_error":false,"total_cost_usd":0.001}`
	// Test agent: outputs a result without any explicit PASS/FAIL marker.
	testOutput := `{"result":"I reviewed the code and everything looks correct.","session_id":"test-sess","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001}`

	cmd := fakeStatefulCmd(t, []string{implOutput, testOutput})
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Unknown verdict test", 5, false, "")
	if err != nil {
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
	// No clear verdict → should be "unknown", not "".
	if afterTest.LastTestResult != "unknown" {
		t.Fatalf("expected last_test_result=unknown for ambiguous output, got %q", afterTest.LastTestResult)
	}
	if afterTest.IsTestRun {
		t.Fatal("IsTestRun should be false after test completion")
	}
}

// TestRunTestRunDefaultStopReasonSetsUnknown verifies that when the test
// agent's container produces an empty stop_reason (the "default" case), the
// task is still correctly transitioned to "waiting" with last_test_result set
// to "unknown" — NOT left as "" ("unverified"). This covers the scenario where
// Claude Code's --verbose flag appends extra JSON after the result message and
// parseOutput ends up returning the wrong line.
func TestRunTestRunDefaultStopReasonSetsUnknown(t *testing.T) {
	repo := setupTestRepo(t)

	// Implementation agent: pauses at "waiting" (empty stop_reason).
	implOutput := `{"result":"impl done","session_id":"impl-sess","stop_reason":"","is_error":false,"total_cost_usd":0.001}`
	// Test agent: returns output with empty stop_reason (simulates parseOutput
	// picking up a verbose/debug line instead of the real result message).
	testOutput := `{"result":"","session_id":"test-sess","stop_reason":"","is_error":false,"total_cost_usd":0.001}`

	cmd := fakeStatefulCmd(t, []string{implOutput, testOutput})
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Default stop_reason test run", 5, false, "")
	if err != nil {
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
	// Must NOT be "" (unverified) — must be "unknown" to show "no verdict".
	if afterTest.LastTestResult != "unknown" {
		t.Fatalf("expected last_test_result=unknown for empty stop_reason, got %q", afterTest.LastTestResult)
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

	task, err := s.CreateTask(ctx, "Multi-turn test run", 5, false, "")
	if err != nil {
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

// Ensure time is imported to avoid unused import warnings.
var _ = time.Second

// TestSyncWorktreesBehindMainDirtyWorktree verifies that uncommitted changes in
// a worktree are stashed before the rebase and restored afterward (stash pop).
func TestSyncWorktreesBehindMainDirtyWorktree(t *testing.T) {
	repo := setupTestRepo(t)
	s, runner := setupTestRunner(t, []string{repo})
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "dirty stash sync test", 5, false, "")
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
	if err := s.UpdateTaskStatus(ctx, task.ID, "waiting"); err != nil {
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

	task, err := s.CreateTask(ctx, "conflict handoff test", 5, false, "")
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

	if err := s.UpdateTaskStatus(ctx, task.ID, "waiting"); err != nil {
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
