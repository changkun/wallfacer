package cli

import (
	"os"
	"testing"

	"latere.ai/x/wallfacer/internal/testenv"
)

// TestMain points HOME and the per-user config, data and cache directories at
// a temporary directory: tests must never touch the user's real sign-in or
// configuration, and this package saves and clears the shared latere token.
// TestTokenStorePathIsIsolated, kept in a separate file, fails without it.
func TestMain(m *testing.M) { os.Exit(testenv.RunIsolated(m)) }
