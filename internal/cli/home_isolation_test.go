package cli

import (
	"os"
	"testing"

	"latere.ai/x/wallfacer/internal/testenv"
)

// The home and per-user config directories the test process resolved when it
// started. Package-level initializers run before TestMain, so these hold the
// developer's real locations even after TestMain points HOME elsewhere.
var (
	startHome, startHomeErr           = os.UserHomeDir()
	startConfigDir, startConfigDirErr = os.UserConfigDir()
)

// TestConfigDirIsIsolated fails when the per-user config directory resolves to
// the location the process started with, where the user's real configuration
// lives. The check compares paths only and touches no file.
func TestConfigDirIsIsolated(t *testing.T) {
	if startHomeErr != nil {
		t.Fatalf("resolve the starting home directory: %v", startHomeErr)
	}
	if startConfigDirErr != nil {
		t.Fatalf("resolve the starting config directory: %v", startConfigDirErr)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve the home directory: %v", err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("resolve the config directory: %v", err)
	}
	if home == startHome {
		t.Errorf("home directory is still %s, the one the test process started with; TestMain must isolate it", home)
	}
	if configDir == startConfigDir {
		t.Errorf("config directory is still %s, the user's real one; TestMain must isolate it", configDir)
	}
	// A temporary directory may itself lie inside the starting home (TMPDIR
	// under HOME), so a path under the isolated home is accepted.
	if testenv.Within(configDir, startHome) && !testenv.Within(configDir, home) {
		t.Errorf("config directory %s lies inside the starting home %s and outside the isolated home %s", configDir, startHome, home)
	}
}
