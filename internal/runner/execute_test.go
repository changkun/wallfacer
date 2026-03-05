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

// TestRunCostMultiTurn verifies that session-cumulative total_cost_usd and
// usage token values are correctly converted to per-turn deltas, preventing
// double-counting. Claude Code reports cumulative totals for the session;
// on resumed sessions (--resume for max_tokens/pause_turn), each invocation
// includes prior turns' totals.
func TestRunCostMultiTurn(t *testing.T) {
	repo := setupTestRepo(t)
	// Turn 1: max_tokens with cumulative cost 0.03, tokens 100/50
	// Turn 2: end_turn with cumulative cost 0.05, tokens 180/90
	// Actual per-turn costs: 0.03 + 0.02 = 0.05
	// Actual per-turn tokens: 100+80=180 input, 50+40=90 output
	turn1 := `{"result":"partial","session_id":"s1","stop_reason":"max_tokens","is_error":false,"total_cost_usd":0.03,"usage":{"input_tokens":100,"output_tokens":50}}`
	turn2 := `{"result":"done","session_id":"s1","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.05,"usage":{"input_tokens":180,"output_tokens":90}}`
	cmd := fakeStatefulCmd(t, []string{turn1, turn2})
	s, r := setupRunnerWithCmd(t, []string{repo}, cmd)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Multi-turn cost test", 5, false, "")
	if err != nil {
		t.Fatal(err)
	}

	r.Run(task.ID, "prompt", "", false)

	updated, _ := s.GetTask(ctx, task.ID)
	// Cost should be 0.05 (delta: 0.03 + 0.02), NOT 0.08 (naive sum: 0.03 + 0.05).
	if updated.Usage.CostUSD < 0.049 || updated.Usage.CostUSD > 0.051 {
		t.Errorf("CostUSD = %f, want ~0.05 (got double-counted if ~0.08)", updated.Usage.CostUSD)
	}
	// Tokens should be 180/90 (delta: 100+80, 50+40), NOT 280/140 (naive sum).
	if updated.Usage.InputTokens != 180 {
		t.Errorf("InputTokens = %d, want 180", updated.Usage.InputTokens)
	}
	if updated.Usage.OutputTokens != 90 {
		t.Errorf("OutputTokens = %d, want 90", updated.Usage.OutputTokens)
	}
}

// TestRunCostResumedFromWaiting verifies that cost/token deltas are correct
// when a task goes waiting → in_progress (feedback resume). The second Run()
// call uses --resume, so Claude Code's cumulative totals continue from where
// the first invocation left off.
func TestRunCostResumedFromWaiting(t *testing.T) {
	repo := setupTestRepo(t)
	// First call: waiting (cumulative cost 0.03, tokens 100/50)
	// Second call: end_turn (cumulative cost 0.07, tokens 250/120)
	// Real per-turn: 0.03 + 0.04 = 0.07 cost, 100+150=250 input, 50+70=120 output
	call1 := `{"result":"need input","session_id":"s1","stop_reason":"","is_error":false,"total_cost_usd":0.03,"usage":{"input_tokens":100,"output_tokens":50}}`
	call2 := `{"result":"done","session_id":"s1","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.07,"usage":{"input_tokens":250,"output_tokens":120}}`
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

	// Cost should be 0.07 total (0.03 + 0.04 delta), NOT 0.10 (0.03 + 0.07 naive).
	if final.Usage.CostUSD < 0.069 || final.Usage.CostUSD > 0.071 {
		t.Errorf("CostUSD = %f, want ~0.07 (got double-counted if ~0.10)", final.Usage.CostUSD)
	}
	// Tokens: 250 input (100 + 150 delta), NOT 350 (100 + 250 naive).
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
