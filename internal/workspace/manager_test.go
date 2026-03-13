package workspace

import (
	"os"
	"testing"
)

func TestNewManagerWithoutWorkspacesCreatesScopedStore(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	envFile := t.TempDir() + "/.env"
	if err := os.WriteFile(envFile, nil, 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	m, err := NewManager(configDir, dataDir, envFile, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	snap := m.Snapshot()
	if snap.Store == nil {
		t.Fatal("expected store for empty workspace set")
	}
	if snap.ScopedDataDir == "" {
		t.Fatal("expected scoped data dir for empty workspace set")
	}
	if snap.Key == "" {
		t.Fatal("expected workspace key for empty workspace set")
	}
	if snap.InstructionsPath != "" {
		t.Fatalf("expected no instructions path for empty workspace set, got %q", snap.InstructionsPath)
	}
}
