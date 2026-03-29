package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// runnerWithCmd creates a minimal Runner backed by a fresh store using the
// given container command string. No workspaces are configured, which is fine
// for commit message generation tests that don't touch git worktrees.
func runnerWithCmd(t *testing.T, cmd string) *Runner {
	t.Helper()
	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
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
		WorktreesDir: worktreesDir,
	})
	t.Cleanup(func() { r.Shutdown() })
	return r
}

// validStreamJSON is a minimal well-formed stream-json result object that
// generateCommitMessage expects to receive from the container.
const validStreamJSON = `{"result":"Add authentication endpoint","session_id":"abc123","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.001}`

// ---------------------------------------------------------------------------
// generateCommitMessage unit tests
// ---------------------------------------------------------------------------

// TestGenerateCommitMessageSuccess verifies that a valid stream-json response
// from the container is parsed and its result returned as the commit message.
func TestGenerateCommitMessageSuccess(t *testing.T) {
	cmd := fakeCmdScript(t, validStreamJSON, 0)
	runner := runnerWithCmd(t, cmd)

	msg, err := runner.generateCommitMessage(context.Background(), uuid.New(), "Add authentication", "auth.go | 50 ++++", "")
	if err != nil {
		t.Fatalf("generateCommitMessage error: %v", err)
	}

	const want = "Add authentication endpoint"
	if msg != want {
		t.Fatalf("expected %q, got %q", want, msg)
	}
}

func TestGenerateCommitMessageErrorsOnInvalidOutput(t *testing.T) {
	runner := runnerWithCmd(t, "echo") // outputs its args, not valid JSON

	_, err := runner.generateCommitMessage(context.Background(), uuid.New(), "Fix the login bug", "login.go | 3 +-", "")
	if err == nil {
		t.Fatal("expected error for invalid commit message output")
	}
	if !IsCommitMessageGenerationError(err) {
		t.Fatalf("expected commit message generation error, got %v", err)
	}
}

func TestGenerateCommitMessageErrorsOnCommandError(t *testing.T) {
	cmd := fakeCmdScript(t, "", 1) // exits 1 with empty output
	runner := runnerWithCmd(t, cmd)

	_, err := runner.generateCommitMessage(context.Background(), uuid.New(), "Refactor database layer", "db/*.go | 120 ++--", "")
	if err == nil {
		t.Fatal("expected error for failed commit message command")
	}
	if !IsCommitMessageGenerationError(err) {
		t.Fatalf("expected commit message generation error, got %v", err)
	}
}

func TestGenerateCommitMessageErrorsOnBlankResult(t *testing.T) {
	blankResult := `{"result":"","session_id":"abc","stop_reason":"end_turn","is_error":false}`
	cmd := fakeCmdScript(t, blankResult, 0)
	runner := runnerWithCmd(t, cmd)

	_, err := runner.generateCommitMessage(context.Background(), uuid.New(), "Update configuration", "config.go | 5 +-", "")
	if err == nil {
		t.Fatal("expected error for blank commit message result")
	}
}

// TestGenerateCommitMessageMultiline verifies that a multiline commit message
// (subject + blank line + body) produced by the container is returned intact.
func TestGenerateCommitMessageMultiline(t *testing.T) {
	// JSON \n sequences decode to real newlines via json.Unmarshal.
	multilineResult := `{"result":"Add auth endpoint\n\nImplements OAuth2 flow.\nUpdates token validation.","session_id":"abc","stop_reason":"end_turn","is_error":false}`
	cmd := fakeCmdScript(t, multilineResult, 0)
	runner := runnerWithCmd(t, cmd)

	msg, err := runner.generateCommitMessage(context.Background(), uuid.New(), "Add auth", "auth.go | 80 ++++", "")
	if err != nil {
		t.Fatalf("generateCommitMessage error: %v", err)
	}

	if !strings.Contains(msg, "Add auth endpoint") {
		t.Fatalf("expected subject line in message, got: %q", msg)
	}
	if !strings.Contains(msg, "OAuth2") {
		t.Fatalf("expected body text in message, got: %q", msg)
	}
}

// TestGenerateCommitMessageNDJSON verifies that stream-json (NDJSON) output —
// where each turn is its own JSON line — is handled by finding the last valid
// JSON object in the stream, which contains the final result.
func TestGenerateCommitMessageNDJSON(t *testing.T) {
	ndjson := `{"type":"system","subtype":"init"}
{"type":"assistant","content":"thinking..."}
{"result":"Fix null pointer dereference","session_id":"abc","stop_reason":"end_turn","is_error":false}`
	cmd := fakeCmdScript(t, ndjson, 0)
	runner := runnerWithCmd(t, cmd)

	msg, err := runner.generateCommitMessage(context.Background(), uuid.New(), "Fix crash", "main.go | 2 +-", "")
	if err != nil {
		t.Fatalf("generateCommitMessage error: %v", err)
	}

	const want = "Fix null pointer dereference"
	if msg != want {
		t.Fatalf("expected %q from NDJSON output, got %q", want, msg)
	}
}

func TestGenerateCommitMessageFallsBackToCodexOnTokenLimit(t *testing.T) {
	tokenLimit := `{"result":"rate limit exceeded: token limit reached","session_id":"abc","stop_reason":"end_turn","is_error":true,"total_cost_usd":0.001}`
	cmd := fakeStatefulCmd(t, []string{tokenLimit, validStreamJSON})
	runner := runnerWithCmd(t, cmd)

	msg, err := runner.generateCommitMessage(context.Background(), uuid.New(), "Add authentication", "auth.go | 50 ++++", "")
	if err != nil {
		t.Fatalf("generateCommitMessage error: %v", err)
	}

	const want = "Add authentication endpoint"
	if msg != want {
		t.Fatalf("expected codex fallback message %q, got %q", want, msg)
	}
}

// ---------------------------------------------------------------------------
// hostStageAndCommit integration tests
// ---------------------------------------------------------------------------

// TestHostStageAndCommitUsesGeneratedMessage verifies end-to-end that
// hostStageAndCommit uses the message returned by generateCommitMessage when
// the container produces valid output, rather than the "wallfacer:" fallback.
func TestHostStageAndCommitUsesGeneratedMessage(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, validStreamJSON, 0)

	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	worktreesDir := filepath.Join(t.TempDir(), "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(s, RunnerConfig{
		Command:      cmd,
		SandboxImage: "test:latest",
		Workspaces:   []string{repo},
		WorktreesDir: worktreesDir,
	})
	t.Cleanup(func() { runner.Shutdown() })

	taskID := uuid.New()
	worktreePaths, branchName, err := runner.setupWorktrees(taskID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(taskID, worktreePaths, branchName) })

	wt := worktreePaths[repo]
	if err := os.WriteFile(filepath.Join(wt, "auth.go"), []byte("package auth\n"), 0644); err != nil {
		t.Fatal(err)
	}

	committed, err := runner.hostStageAndCommit(context.Background(), taskID, worktreePaths, "Add authentication")
	if err != nil {
		t.Fatalf("hostStageAndCommit error: %v", err)
	}
	if !committed {
		t.Fatal("expected a commit to be created")
	}

	// The commit subject should be the generated message, not the fallback.
	subject := gitRun(t, wt, "log", "--format=%s", "-1")
	const wantSubject = "Add authentication endpoint"
	if subject != wantSubject {
		t.Fatalf("expected commit subject %q, got %q", wantSubject, subject)
	}
}

func TestHostStageAndCommitFallsBackOnCommitMessageFailure(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, "", 1) // always fails

	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	worktreesDir := filepath.Join(t.TempDir(), "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(s, RunnerConfig{
		Command:      cmd,
		SandboxImage: "test:latest",
		Workspaces:   []string{repo},
		WorktreesDir: worktreesDir,
	})
	t.Cleanup(func() { runner.Shutdown() })

	taskID := uuid.New()
	worktreePaths, branchName, err := runner.setupWorktrees(taskID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(taskID, worktreePaths, branchName) })

	wt := worktreePaths[repo]
	if err := os.WriteFile(filepath.Join(wt, "feature.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	committed, err := runner.hostStageAndCommit(context.Background(), taskID, worktreePaths, "Add new feature")
	if err != nil {
		t.Fatalf("expected fallback commit message to succeed, got %v", err)
	}
	if committed {
		subject := gitRun(t, wt, "log", "--format=%s", "-1")
		if !strings.HasPrefix(subject, "wallfacer: ") {
			t.Fatalf("expected fallback commit subject, got %q", subject)
		}
		return
	}
	t.Fatal("expected fallback path to create a commit")
}

// TestHostStageAndCommitErrorsWhenAllWorktreesMissing verifies that
// hostStageAndCommit returns an error when every worktree path is missing,
// instead of silently succeeding with no commits.
func TestHostStageAndCommitErrorsWhenAllWorktreesMissing(t *testing.T) {
	runner := runnerWithCmd(t, "echo")
	taskID := uuid.New()

	worktreePaths := map[string]string{
		"/tmp/repo-a": "/nonexistent/worktree-a",
		"/tmp/repo-b": "/nonexistent/worktree-b",
	}
	committed, err := runner.hostStageAndCommit(context.Background(), taskID, worktreePaths, "some prompt")
	if err == nil {
		t.Fatal("expected error when all worktrees are missing, got nil")
	}
	if committed {
		t.Fatal("expected committed=false when all worktrees are missing")
	}
	if !strings.Contains(err.Error(), "all worktrees missing") {
		t.Fatalf("expected 'all worktrees missing' in error, got: %v", err)
	}
}

func TestHostStageAndCommitErrorsWhenNoWorktreesConfigured(t *testing.T) {
	runner := runnerWithCmd(t, "echo")

	committed, err := runner.hostStageAndCommit(context.Background(), uuid.New(), nil, "some prompt")
	if err == nil {
		t.Fatal("expected error when no worktrees are configured")
	}
	if committed {
		t.Fatal("expected committed=false when no worktrees are configured")
	}
	if !strings.Contains(err.Error(), "no worktrees to commit") {
		t.Fatalf("expected 'no worktrees to commit' in error, got: %v", err)
	}
}

// TestHostStageAndCommitSucceedsWhenSomeWorktreesMissing verifies that
// hostStageAndCommit still succeeds when only some worktrees are missing
// but others exist (nothing to commit is not an error when at least one
// worktree was reachable).
func TestHostStageAndCommitSucceedsWhenSomeWorktreesMissing(t *testing.T) {
	repo := setupTestRepo(t)

	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	worktreesDir := filepath.Join(t.TempDir(), "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(s, RunnerConfig{
		Command:      "echo",
		SandboxImage: "test:latest",
		Workspaces:   []string{repo},
		WorktreesDir: worktreesDir,
	})
	t.Cleanup(func() { runner.Shutdown() })

	taskID := uuid.New()
	worktreePaths, branchName, err := runner.setupWorktrees(taskID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(taskID, worktreePaths, branchName) })

	// Add a missing worktree alongside the real one.
	worktreePaths["/tmp/missing-repo"] = "/nonexistent/worktree"

	committed, err := runner.hostStageAndCommit(context.Background(), taskID, worktreePaths, "some prompt")
	if err != nil {
		t.Fatalf("unexpected error when only some worktrees are missing: %v", err)
	}
	if committed {
		t.Fatal("expected committed=false when no worktree has changes")
	}
}

// ---------------------------------------------------------------------------
// Commit pipeline span event tests
// ---------------------------------------------------------------------------

// collectSpanKeys scans a slice of events and returns two maps — one for
// span_start events and one for span_end events — keyed by phase+label.
func collectSpanKeys(events []store.TaskEvent) (started, ended map[[2]string]bool) {
	started = map[[2]string]bool{}
	ended = map[[2]string]bool{}
	for _, ev := range events {
		if ev.EventType != store.EventTypeSpanStart && ev.EventType != store.EventTypeSpanEnd {
			continue
		}
		var d store.SpanData
		if err := json.Unmarshal(ev.Data, &d); err != nil {
			continue
		}
		key := [2]string{d.Phase, d.Label}
		if ev.EventType == store.EventTypeSpanStart {
			started[key] = true
		} else {
			ended[key] = true
		}
	}
	return started, ended
}

// TestCommitPipelineEmitsStageRebaseMergeCleanupSpans verifies that the
// commit pipeline emits span_start/span_end pairs for all three phases:
// commit/stage, commit/rebase_merge, and commit/cleanup.
// It also verifies that cleanupWorktrees emits a worktree_cleanup span.
func TestCommitPipelineEmitsStageRebaseMergeCleanupSpans(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, validStreamJSON, 0) // for commit message generation

	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	worktreesDir := filepath.Join(t.TempDir(), "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(s, RunnerConfig{
		Command:      cmd,
		SandboxImage: "test:latest",
		Workspaces:   []string{repo},
		WorktreesDir: worktreesDir,
	})
	t.Cleanup(func() { runner.Shutdown() })

	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test commit spans", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}

	// Set up worktrees and persist them so Commit can read them.
	worktreePaths, branchName, err := runner.setupWorktrees(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, worktreePaths, branchName); err != nil {
		t.Fatal(err)
	}

	// Make a change in the worktree so there is something to commit.
	wt := worktreePaths[repo]
	if err := os.WriteFile(filepath.Join(wt, "feature.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runner.Commit(task.ID, "sess1"); err != nil {
		t.Fatalf("Commit error: %v", err)
	}

	events, err := s.GetEvents(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	started, ended := collectSpanKeys(events)

	// All three commit pipeline phases must have matching start/end pairs.
	for _, wantKey := range [][2]string{
		{"commit", "stage"},
		{"commit", "rebase_merge"},
		{"commit", "cleanup"},
	} {
		if !started[wantKey] {
			t.Errorf("missing span_start for phase=%q label=%q", wantKey[0], wantKey[1])
		}
		if !ended[wantKey] {
			t.Errorf("missing span_end for phase=%q label=%q", wantKey[0], wantKey[1])
		}
	}

	// cleanupWorktrees must emit its own worktree_cleanup span.
	cleanupKey := [2]string{"worktree_cleanup", "worktree_cleanup"}
	if !started[cleanupKey] {
		t.Error("missing span_start for worktree_cleanup")
	}
	if !ended[cleanupKey] {
		t.Error("missing span_end for worktree_cleanup")
	}
}

// ---------------------------------------------------------------------------
// Context cancellation regression test
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// generateCommitMessage cancellation tests
// ---------------------------------------------------------------------------

// TestGenerateCommitMessageRespectsCallerContext verifies that
// generateCommitMessage returns promptly when the caller-supplied context is
// cancelled. The commit container is intentionally not registered in
// r.taskContainers (to avoid overwriting the main execution container entry
// used by StreamLogs), so cancellation relies on exec.CommandContext.
func TestGenerateCommitMessageRespectsCallerContext(t *testing.T) {
	cmd := fakeBlockingCmd(t)
	runner := runnerWithCmd(t, cmd)

	taskID := uuid.New()
	ctx, cancel := context.WithCancel(context.Background())

	type result struct {
		msg string
		err error
	}
	done := make(chan result, 1)
	go func() {
		msg, err := runner.generateCommitMessage(ctx, taskID, "Add feature", "feature.go | 10 ++++", "")
		done <- result{msg, err}
	}()

	// Give the goroutine a moment to start.
	time.Sleep(200 * time.Millisecond)

	// Cancel the context — the blocking container should be terminated.
	cancel()

	select {
	case res := <-done:
		if res.err == nil {
			t.Fatal("expected an error after context cancellation, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("generateCommitMessage did not return within 3s after context cancellation — caller ctx not respected")
	}
}

// TestHostStageAndCommitRespectsContextCancellation verifies that when the
// context is already cancelled, hostStageAndCommit returns promptly with a
// context-related error rather than blocking on git subprocesses.
//
// This is a regression guard: the fix replaces exec.Command with
// exec.CommandContext so that git calls are interrupted by the task timeout
// or server shutdown context. Without the fix the function would hang until
// the git subprocess finished naturally even after the context expired.
func TestHostStageAndCommitRespectsContextCancellation(t *testing.T) {
	repo := setupTestRepo(t)

	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	worktreesDir := filepath.Join(t.TempDir(), "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(s, RunnerConfig{
		Command:      "echo", // dummy — not used for git operations
		SandboxImage: "test:latest",
		Workspaces:   []string{repo},
		WorktreesDir: worktreesDir,
	})
	t.Cleanup(func() { runner.Shutdown() })

	taskID := uuid.New()
	worktreePaths, branchName, err := runner.setupWorktrees(taskID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.cleanupWorktrees(taskID, worktreePaths, branchName) })

	// Write a file so that git add -A has something to stage, ensuring the
	// first git subprocess (git add) is reached before context cancellation
	// takes effect.
	wt := worktreePaths[repo]
	if err := os.WriteFile(filepath.Join(wt, "cancel_test.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a context that is already cancelled before the call.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		_, err := runner.hostStageAndCommit(ctx, taskID, worktreePaths, "cancellation regression test")
		done <- err
	}()

	select {
	case gotErr := <-done:
		// Returned promptly — verify it is a context-related error.
		if gotErr == nil {
			t.Fatal("expected a non-nil error when context is already cancelled")
		}
		if ctx.Err() == nil {
			t.Fatal("expected ctx.Err() to be non-nil")
		}
		// The error must wrap the context error or contain context information.
		if !strings.Contains(gotErr.Error(), "context") {
			t.Fatalf("expected context-related error, got: %v", gotErr)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("hostStageAndCommit did not return within 1s with a pre-cancelled context — likely missing exec.CommandContext")
	}
}

// ---------------------------------------------------------------------------
// localFallbackCommitMessage unit tests
// ---------------------------------------------------------------------------

// TestLocalFallbackCommitMessage_SimplePrompt verifies that a plain single-line
// prompt is prefixed with "wallfacer: " and returned as-is.
func TestLocalFallbackCommitMessage_SimplePrompt(t *testing.T) {
	msg := localFallbackCommitMessage("Fix login bug", "1 file changed")
	if msg != "wallfacer: Fix login bug" {
		t.Errorf("got %q", msg)
	}
}

// TestLocalFallbackCommitMessage_MultilinePrompt verifies that only the first
// line of a multi-line prompt is used.
func TestLocalFallbackCommitMessage_MultilinePrompt(t *testing.T) {
	msg := localFallbackCommitMessage("First line\nSecond line", "diff")
	if msg != "wallfacer: First line" {
		t.Errorf("got %q", msg)
	}
}

// TestLocalFallbackCommitMessage_EmptyPromptUsesDiff verifies that the first
// line of diffStat is used when prompt is empty.
func TestLocalFallbackCommitMessage_EmptyPromptUsesDiff(t *testing.T) {
	msg := localFallbackCommitMessage("", "3 files changed, 10 insertions")
	if msg != "wallfacer: 3 files changed, 10 insertions" {
		t.Errorf("got %q", msg)
	}
}

// TestLocalFallbackCommitMessage_BothEmpty verifies the fallback message when
// both prompt and diffStat are empty.
func TestLocalFallbackCommitMessage_BothEmpty(t *testing.T) {
	msg := localFallbackCommitMessage("", "")
	if msg != "wallfacer: update task changes" {
		t.Errorf("got %q", msg)
	}
}

// TestLocalFallbackCommitMessage_BackticksStripped verifies that wrapping
// backtick characters are stripped from the prompt subject.
func TestLocalFallbackCommitMessage_BackticksStripped(t *testing.T) {
	msg := localFallbackCommitMessage("`fix the thing`", "")
	if msg != "wallfacer: fix the thing" {
		t.Errorf("got %q", msg)
	}
}

// TestLocalFallbackCommitMessage_LongPromptTruncated verifies that the full
// message stays within 72 runes even when the prompt is very long.
func TestLocalFallbackCommitMessage_LongPromptTruncated(t *testing.T) {
	long := strings.Repeat("a", 100)
	msg := localFallbackCommitMessage(long, "")
	runes := []rune(msg)
	if len(runes) > 72 {
		t.Errorf("message too long: %d runes: %q", len(runes), msg)
	}
}

// TestLocalFallbackCommitMessage_DiffStatMultiline verifies that only the
// first line of diffStat is used as the fallback when prompt is empty.
func TestLocalFallbackCommitMessage_DiffStatMultiline(t *testing.T) {
	msg := localFallbackCommitMessage("", "1 file changed\n2 lines added")
	if msg != "wallfacer: 1 file changed" {
		t.Errorf("got %q", msg)
	}
}

// TestLocalFallbackCommitMessage_WhitespaceOnlyPromptUsesDiff verifies that a
// prompt consisting only of whitespace falls back to diffStat.
func TestLocalFallbackCommitMessage_WhitespaceOnlyPromptUsesDiff(t *testing.T) {
	msg := localFallbackCommitMessage("   ", "patch changes")
	if msg != "wallfacer: patch changes" {
		t.Errorf("got %q", msg)
	}
}

// ---------------------------------------------------------------------------
// maybeAutoPush unit tests
// ---------------------------------------------------------------------------

// TestMaybeAutoPush_NoEnvFile verifies that maybeAutoPush is a no-op when
// envFile is not configured.
func TestMaybeAutoPush_NoEnvFile(t *testing.T) {
	s, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	worktreesDir := filepath.Join(t.TempDir(), "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(s, RunnerConfig{EnvFile: "", WorktreesDir: worktreesDir})
	t.Cleanup(func() { r.Shutdown() })
	// Should not panic; nothing to verify since it's a no-op when envFile is empty.
	r.maybeAutoPush(context.Background(), uuid.New(), map[string]string{})
}

// TestMaybeAutoPush_AutoPushDisabled verifies maybeAutoPush is a no-op when
// WALLFACER_AUTO_PUSH is set to false.
func TestMaybeAutoPush_AutoPushDisabled(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("WALLFACER_AUTO_PUSH=false\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	worktreesDir := filepath.Join(t.TempDir(), "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(s, RunnerConfig{EnvFile: envFile, WorktreesDir: worktreesDir})
	t.Cleanup(func() { r.Shutdown() })
	// Should complete without error; auto-push is disabled.
	r.maybeAutoPush(context.Background(), uuid.New(), map[string]string{})
}

// TestMaybeAutoPush_NonGitDir verifies that maybeAutoPush skips directories
// that are not git repositories when auto-push is enabled.
func TestMaybeAutoPush_NonGitDir(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	content := "WALLFACER_AUTO_PUSH=true\nWALLFACER_AUTO_PUSH_THRESHOLD=1\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	worktreesDir := filepath.Join(t.TempDir(), "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(s, RunnerConfig{EnvFile: envFile, WorktreesDir: worktreesDir})
	t.Cleanup(func() { r.Shutdown() })

	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{Prompt: "test", Timeout: 30})
	if err != nil {
		t.Fatal(err)
	}

	nonGitDir := t.TempDir() // plain directory, not a git repo
	// Should skip the non-git dir without error.
	r.maybeAutoPush(context.Background(), task.ID, map[string]string{nonGitDir: nonGitDir})
}

// TestMaybeAutoPush_MissingEnvFile verifies that maybeAutoPush is a no-op
// when the env file path is set but the file does not exist.
func TestMaybeAutoPush_MissingEnvFile(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "nonexistent.env")
	s, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	worktreesDir := filepath.Join(t.TempDir(), "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(s, RunnerConfig{EnvFile: envFile, WorktreesDir: worktreesDir})
	t.Cleanup(func() { r.Shutdown() })
	// envconfig.Parse will fail → maybeAutoPush returns early without panic.
	r.maybeAutoPush(context.Background(), uuid.New(), map[string]string{})
}
