package runner

import (
	"context"
	"os"
	"strings"
	"testing"
)

// The tests in this file cover the title of a chat thread, which has no task:
// its harness comes from the env file's title or default harness. They run
// against the real host backend, which refuses an in-process harness id (the
// mock backend accepts any id and would hide that), with a fake agent binary
// standing in for the subprocess harnesses and a scripted model standing in
// for the gateway the in-process harness calls.

// appendEnv adds lines to an existing env file.
func appendEnv(t *testing.T, path, lines string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open env file: %v", err)
	}
	if _, err := f.WriteString(lines); err != nil {
		t.Fatalf("append env file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close env file: %v", err)
	}
}

// TestGenerateAgentSessionTitle_InProcessDefault asserts a chat thread gets a
// title when the configured default harness runs in-process: the title is the
// in-process model's reply, not the subprocess binary's output, and the
// request names the title model.
func TestGenerateAgentSessionTitle_InProcessDefault(t *testing.T) {
	cmd := fakeCmdScript(t, `{"result":"Subprocess Title","session_id":"s1","stop_reason":"end_turn","is_error":false}`, 0)
	_, r := setupRunnerWithCmd(t, nil, cmd)
	envFile, requests := scriptedModel(t, `"Health Endpoint"`)
	appendEnv(t, envFile, "WALLFACER_DEFAULT_SANDBOX=topos\n")
	r.envFile = envFile

	title, err := r.GenerateAgentSessionTitle(context.Background(), "add a health endpoint")
	if err != nil {
		t.Fatalf("GenerateAgentSessionTitle: %v", err)
	}
	if title != "Health Endpoint" {
		t.Fatalf("title = %q, want the in-process model's reply %q", title, "Health Endpoint")
	}
	got := requests()
	if len(got) != 1 {
		t.Fatalf("model requests = %d, want 1", len(got))
	}
	if !strings.Contains(got[0], "title-model") {
		t.Errorf("title request does not name the configured title model: %s", got[0])
	}
}

// TestGenerateAgentSessionTitle_SubprocessDefault asserts a subprocess default
// harness still produces the thread title through the agent binary.
func TestGenerateAgentSessionTitle_SubprocessDefault(t *testing.T) {
	cmd := fakeCmdScript(t, `{"result":"\"Subprocess Title\"","session_id":"s1","stop_reason":"end_turn","is_error":false}`, 0)
	_, r := setupRunnerWithCmd(t, nil, cmd)
	r.envFile = writeEnvFile(t, "WALLFACER_DEFAULT_SANDBOX=claude\n")

	title, err := r.GenerateAgentSessionTitle(context.Background(), "add a health endpoint")
	if err != nil {
		t.Fatalf("GenerateAgentSessionTitle: %v", err)
	}
	if title != "Subprocess Title" {
		t.Fatalf("title = %q, want the agent binary's result %q", title, "Subprocess Title")
	}
}
