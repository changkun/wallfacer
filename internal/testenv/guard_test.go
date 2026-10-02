package testenv

import (
	"strings"
	"testing"
)

// TestGuardNoAgentCLIOnPath fails when an agent CLI resolves from PATH inside
// a test process set up by RunIsolated. Code under test that is built without
// an explicit binary path resolves the agent from PATH, and on a developer
// machine the real CLI can authenticate from the system credential store
// whatever HOME is, so a reachable CLI runs real prompts on the developer's
// account.
func TestGuardNoAgentCLIOnPath(t *testing.T) {
	if found := AgentCLIsOnPath(); len(found) > 0 {
		t.Fatalf("agent CLIs resolve from PATH inside the isolated test process, so code under test can launch them: %s", strings.Join(found, ", "))
	}
}
