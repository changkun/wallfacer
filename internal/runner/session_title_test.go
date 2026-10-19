package runner

import (
	"context"
	"testing"
)

// The tests in this file cover the title of a chat thread, which has no task:
// its harness comes from the env file's title or default harness. They run
// against the real host backend, which refuses an in-process harness id (the
// mock backend accepts any id and would hide that), with a fake agent binary
// standing in for the subprocess harnesses and a scripted model standing in
// for the gateway the in-process harness calls.

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
