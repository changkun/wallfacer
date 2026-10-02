package handler

import (
	"os"
	"testing"

	"latere.ai/x/wallfacer/internal/testenv"
)

// TestMain points HOME and the per-user config, data and cache directories at
// a temporary directory: tests must never touch the user's real sign-in or
// configuration, and the runners built here create ~/.wallfacer/agents and
// ~/.wallfacer/flows and spawn agent CLIs and shells that write under HOME.
func TestMain(m *testing.M) { os.Exit(testenv.RunIsolated(m)) }
