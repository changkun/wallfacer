package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"changkun.de/wallfacer/internal/workspacegroups"
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

func TestNewManagerWithoutWorkspacesLoadsMostRecentWorkspaceGroup(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, nil, 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	wsA := t.TempDir()
	wsB := t.TempDir()
	if err := workspacegroups.Save(configDir, []workspacegroups.Group{
		{Workspaces: []string{wsA, wsB}},
	}); err != nil {
		t.Fatalf("save workspace groups: %v", err)
	}

	m, err := NewManager(configDir, dataDir, envFile, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	snap := m.Snapshot()
	if len(snap.Workspaces) != 2 || snap.Workspaces[0] != wsA || snap.Workspaces[1] != wsB {
		t.Fatalf("expected saved workspace group to load, got %v", snap.Workspaces)
	}
	if snap.InstructionsPath == "" {
		t.Fatal("expected instructions path for restored workspace group")
	}
	if snap.Store == nil {
		t.Fatal("expected store for restored workspace group")
	}
}

func TestNewManagerExplicitEmptyWorkspacesDoesNotRestoreSavedGroup(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, nil, 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	ws := t.TempDir()
	if err := workspacegroups.Save(configDir, []workspacegroups.Group{
		{Workspaces: []string{ws}},
	}); err != nil {
		t.Fatalf("save workspace groups: %v", err)
	}

	m, err := NewManager(configDir, dataDir, envFile, []string{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	snap := m.Snapshot()
	if len(snap.Workspaces) != 0 {
		t.Fatalf("expected no restored workspaces, got %v", snap.Workspaces)
	}
	if snap.InstructionsPath != "" {
		t.Fatalf("expected no instructions path for explicit empty startup, got %q", snap.InstructionsPath)
	}
	if snap.Store == nil {
		t.Fatal("expected store for explicit empty workspace set")
	}
}
