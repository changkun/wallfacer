//go:build !opencode_integration

package executor

import (
	"os"
	"testing"

	"latere.ai/x/wallfacer/internal/testenv"
)

// TestMain runs the tests with an isolated home and the agent CLIs hidden from
// PATH. A host backend resolves every agent it was given no explicit path for
// from PATH, so the backends built here hold the real CLIs for the agents a
// test does not supply, one Launch away from running them. The tagged opencode
// integration test needs the real opencode, so the tag that enables it leaves
// this file out.
func TestMain(m *testing.M) { os.Exit(testenv.RunIsolated(m)) }
