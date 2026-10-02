package handler

import (
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/testenv"
)

// TestGuardNoAgentCLIOnPath fails when an agent CLI resolves from PATH inside
// this package's test process. The runners built here carry no explicit
// binary path and resolve agents from PATH, and several handlers start an
// agent run on their own (the sandbox check, task runs, spec title
// generation), so a reachable CLI would run those prompts for real.
// TestMain hides the CLIs through testenv.RunIsolated.
func TestGuardNoAgentCLIOnPath(t *testing.T) {
	if found := testenv.AgentCLIsOnPath(); len(found) > 0 {
		t.Fatalf("agent CLIs resolve from PATH in the handler tests, so a handler can launch them: %s", strings.Join(found, ", "))
	}
}
