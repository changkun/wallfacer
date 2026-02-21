package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// fakeCmdScript creates a temporary executable shell script. When called with
// any arguments the script writes output to stdout and exits with exitCode.
// Using a file avoids shell-quoting issues with arbitrary output strings.
func fakeCmdScript(t *testing.T, output string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()

	dataPath := filepath.Join(dir, "output.txt")
	if err := os.WriteFile(dataPath, []byte(output), 0644); err != nil {
		t.Fatal(err)
	}

	scriptPath := filepath.Join(dir, "fake-cmd")
	script := fmt.Sprintf("#!/bin/sh\ncat %s\nexit %d\n", dataPath, exitCode)
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return scriptPath
}

// runnerWithCmd creates a minimal Runner backed by a fresh store using the
// given container command string. No workspaces are configured, which is fine
// for commit message generation tests that don't touch git worktrees.
func runnerWithCmd(t *testing.T, cmd string) *Runner {
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
	return NewRunner(s, RunnerConfig{
		Command:      cmd,
		SandboxImage: "test:latest",
		WorktreesDir: worktreesDir,
	})
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

	msg := runner.generateCommitMessage(uuid.New(), "Add authentication", "auth.go | 50 ++++")

	const want = "Add authentication endpoint"
	if msg != want {
		t.Fatalf("expected %q, got %q", want, msg)
	}
}

// TestGenerateCommitMessageFallbackOnInvalidOutput verifies that when the
// container outputs non-JSON (e.g. the "echo" dummy), generateCommitMessage
// returns the "wallfacer: <first line of prompt>" fallback.
func TestGenerateCommitMessageFallbackOnInvalidOutput(t *testing.T) {
	runner := runnerWithCmd(t, "echo") // outputs its args, not valid JSON

	prompt := "Fix the login bug\nwith more detail on a second line"
	msg := runner.generateCommitMessage(uuid.New(), prompt, "login.go | 3 +-")

	if !strings.HasPrefix(msg, "wallfacer: ") {
		t.Fatalf("expected fallback 'wallfacer: ...' prefix, got: %q", msg)
	}
	// Fallback uses the first line of the prompt.
	if !strings.Contains(msg, "Fix the login bug") {
		t.Fatalf("fallback should contain first line of prompt, got: %q", msg)
	}
	// Should NOT include any text from subsequent lines.
	if strings.Contains(msg, "second line") {
		t.Fatalf("fallback should only use first line of prompt, got: %q", msg)
	}
}

// TestGenerateCommitMessageFallbackOnCommandError verifies the fallback when
// the container command exits non-zero with no stdout.
func TestGenerateCommitMessageFallbackOnCommandError(t *testing.T) {
	cmd := fakeCmdScript(t, "", 1) // exits 1 with empty output
	runner := runnerWithCmd(t, cmd)

	msg := runner.generateCommitMessage(uuid.New(), "Refactor database layer", "db/*.go | 120 ++--")

	if !strings.HasPrefix(msg, "wallfacer: ") {
		t.Fatalf("expected fallback prefix, got: %q", msg)
	}
	if !strings.Contains(msg, "Refactor database layer") {
		t.Fatalf("fallback should contain prompt text, got: %q", msg)
	}
}

// TestGenerateCommitMessageFallbackOnBlankResult verifies the fallback when
// the container returns valid JSON but the result field is empty.
func TestGenerateCommitMessageFallbackOnBlankResult(t *testing.T) {
	blankResult := `{"result":"","session_id":"abc","stop_reason":"end_turn","is_error":false}`
	cmd := fakeCmdScript(t, blankResult, 0)
	runner := runnerWithCmd(t, cmd)

	msg := runner.generateCommitMessage(uuid.New(), "Update configuration", "config.go | 5 +-")

	if !strings.HasPrefix(msg, "wallfacer: ") {
		t.Fatalf("expected fallback for blank result, got: %q", msg)
	}
}

// TestGenerateCommitMessageFallbackTruncatesLongPrompt verifies that the
// fallback subject line is capped to a reasonable length regardless of how
// long the task prompt is.
func TestGenerateCommitMessageFallbackTruncatesLongPrompt(t *testing.T) {
	longPrompt := strings.Repeat("A", 200)
	runner := runnerWithCmd(t, "echo") // always triggers fallback

	msg := runner.generateCommitMessage(uuid.New(), longPrompt, "")

	// "wallfacer: " (11 chars) + truncate(prompt, 72) → max 86 chars total
	// because truncate appends "..." (3 chars) when the string is cut.
	if len(msg) > 86 {
		t.Fatalf("fallback message too long (%d chars): %q", len(msg), msg)
	}
	if !strings.HasPrefix(msg, "wallfacer: ") {
		t.Fatalf("expected 'wallfacer: ' prefix, got: %q", msg)
	}
}

// TestGenerateCommitMessageMultiline verifies that a multiline commit message
// (subject + blank line + body) produced by the container is returned intact.
func TestGenerateCommitMessageMultiline(t *testing.T) {
	// JSON \n sequences decode to real newlines via json.Unmarshal.
	multilineResult := `{"result":"Add auth endpoint\n\nImplements OAuth2 flow.\nUpdates token validation.","session_id":"abc","stop_reason":"end_turn","is_error":false}`
	cmd := fakeCmdScript(t, multilineResult, 0)
	runner := runnerWithCmd(t, cmd)

	msg := runner.generateCommitMessage(uuid.New(), "Add auth", "auth.go | 80 ++++")

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

	msg := runner.generateCommitMessage(uuid.New(), "Fix crash", "main.go | 2 +-")

	const want = "Fix null pointer dereference"
	if msg != want {
		t.Fatalf("expected %q from NDJSON output, got %q", want, msg)
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
	s, err := store.NewStore(dataDir)
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
		Workspaces:   repo,
		WorktreesDir: worktreesDir,
	})

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

	committed := runner.hostStageAndCommit(taskID, worktreePaths, "Add authentication")
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

// TestHostStageAndCommitFallsBackOnContainerFailure verifies that when the
// container command fails, hostStageAndCommit still creates a commit using
// the "wallfacer: <prompt>" fallback message.
func TestHostStageAndCommitFallsBackOnContainerFailure(t *testing.T) {
	repo := setupTestRepo(t)
	cmd := fakeCmdScript(t, "", 1) // always fails

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
	runner := NewRunner(s, RunnerConfig{
		Command:      cmd,
		SandboxImage: "test:latest",
		Workspaces:   repo,
		WorktreesDir: worktreesDir,
	})

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

	committed := runner.hostStageAndCommit(taskID, worktreePaths, "Add new feature")
	if !committed {
		t.Fatal("expected a commit to be created even when container fails")
	}

	subject := gitRun(t, wt, "log", "--format=%s", "-1")
	if !strings.HasPrefix(subject, "wallfacer: ") {
		t.Fatalf("expected fallback 'wallfacer: ...' commit message, got: %q", subject)
	}
	if !strings.Contains(subject, "Add new feature") {
		t.Fatalf("fallback commit message should contain prompt, got: %q", subject)
	}
}
