package cli

import (
	"os"
	"testing"

	"latere.ai/x/pkg/authkit/cli"

	"latere.ai/x/wallfacer/internal/testenv"
)

// The home directory and sign-in token path the test process resolved when it
// started. Package-level initializers run before TestMain, so these hold the
// developer's real locations even after TestMain points HOME elsewhere.
var (
	startHome, startHomeErr           = os.UserHomeDir()
	startTokenPath, startTokenPathErr = cli.DefaultFileTokenStorePath()
)

// TestTokenStorePathIsIsolated fails when the shared latere sign-in file
// resolves to the location the process started with. That file is the user's
// real sign-in, shared with the latere command line, and tests in this package
// save and clear it through `wallfacer auth logout`, the coordination token
// store and the session bridge. The check compares paths only and touches no
// file, so it fails without reading or writing the real token.
func TestTokenStorePathIsIsolated(t *testing.T) {
	if startHomeErr != nil {
		t.Fatalf("resolve the starting home directory: %v", startHomeErr)
	}
	if startTokenPathErr != nil {
		t.Fatalf("resolve the starting token path: %v", startTokenPathErr)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve the home directory: %v", err)
	}
	tokenPath, err := cli.DefaultFileTokenStorePath()
	if err != nil {
		t.Fatalf("resolve the token path: %v", err)
	}
	if home == startHome {
		t.Errorf("home directory is still %s, the one the test process started with; TestMain must isolate it", home)
	}
	if tokenPath == startTokenPath {
		t.Errorf("token path is still %s, the user's real sign-in; TestMain must isolate the per-user config directory", tokenPath)
	}
	// A temporary directory may itself lie inside the starting home (TMPDIR
	// under HOME), so a path under the isolated home is accepted.
	if testenv.Within(tokenPath, startHome) && !testenv.Within(tokenPath, home) {
		t.Errorf("token path %s lies inside the starting home %s and outside the isolated home %s", tokenPath, startHome, home)
	}
}
