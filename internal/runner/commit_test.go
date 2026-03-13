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

	msg, err := runner.generateCommitMessage(uuid.New(), "Add authentication", "auth.go | 50 ++++", "")
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

	_, err := runner.generateCommitMessage(uuid.New(), "Fix the login bug", "login.go | 3 +-", "")
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

	_, err := runner.generateCommitMessage(uuid.New(), "Refactor database layer", "db/*.go | 120 ++--", "")
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

	_, err := runner.generateCommitMessage(uuid.New(), "Update configuration", "config.go | 5 +-", "")
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

	msg, err := runner.generateCommitMessage(uuid.New(), "Add auth", "auth.go | 80 ++++", "")
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

	msg, err := runner.generateCommitMessage(uuid.New(), "Fix crash", "main.go | 2 +-", "")
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

	msg, err := runner.generateCommitMessage(uuid.New(), "Add authentication", "auth.go | 50 ++++", "")
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

	committed, err := runner.hostStageAndCommit(taskID, worktreePaths, "Add authentication")
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

func TestHostStageAndCommitStopsOnCommitMessageFailure(t *testing.T) {
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

	committed, err := runner.hostStageAndCommit(taskID, worktreePaths, "Add new feature")
	if err == nil {
		t.Fatal("expected hostStageAndCommit to fail when commit message generation fails")
	}
	if !IsCommitMessageGenerationError(err) {
		t.Fatalf("expected commit message generation error, got %v", err)
	}
	if committed {
		t.Fatal("expected committed=false when commit message generation fails")
	}
	if got := gitRun(t, wt, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("expected no new commit to be created, got %s commits", got)
	}
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
	committed, err := runner.hostStageAndCommit(taskID, worktreePaths, "some prompt")
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

	committed, err := runner.hostStageAndCommit(uuid.New(), nil, "some prompt")
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
		Command:      "echo",
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

	// Add a missing worktree alongside the real one.
	worktreePaths["/tmp/missing-repo"] = "/nonexistent/worktree"

	committed, err := runner.hostStageAndCommit(taskID, worktreePaths, "some prompt")
	if err != nil {
		t.Fatalf("unexpected error when only some worktrees are missing: %v", err)
	}
	if committed {
		t.Fatal("expected committed=false when no worktree has changes")
	}
}
