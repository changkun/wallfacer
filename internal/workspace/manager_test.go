package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/store"
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

// --- Switch tests ---

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	configDir := t.TempDir()
	dataDir := t.TempDir()
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, nil, 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	m, err := NewManager(configDir, dataDir, envFile, []string{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func TestSwitch_ToValidWorkspace(t *testing.T) {
	m := newTestManager(t)
	ws := t.TempDir()

	snap, err := m.Switch([]string{ws})
	if err != nil {
		t.Fatalf("Switch: %v", err)
	}
	if len(snap.Workspaces) != 1 || snap.Workspaces[0] != ws {
		t.Fatalf("expected workspace %q, got %v", ws, snap.Workspaces)
	}
	if snap.Store == nil {
		t.Fatal("expected store after Switch")
	}
	if snap.InstructionsPath == "" {
		t.Fatal("expected instructions path after Switch with workspace")
	}
}

func TestSwitch_ToEmptyWorkspaces(t *testing.T) {
	m := newTestManager(t)
	ws := t.TempDir()
	if _, err := m.Switch([]string{ws}); err != nil {
		t.Fatalf("first Switch: %v", err)
	}

	snap, err := m.Switch([]string{})
	if err != nil {
		t.Fatalf("Switch to empty: %v", err)
	}
	if len(snap.Workspaces) != 0 {
		t.Fatalf("expected no workspaces after empty switch, got %v", snap.Workspaces)
	}
	if snap.InstructionsPath != "" {
		t.Fatalf("expected no instructions path after empty switch, got %q", snap.InstructionsPath)
	}
}

func TestSwitch_InvalidPath_NonAbsolute(t *testing.T) {
	m := newTestManager(t)
	_, err := m.Switch([]string{"relative/path"})
	if err == nil {
		t.Fatal("expected error for non-absolute path")
	}
}

func TestSwitch_InvalidPath_NonExistent(t *testing.T) {
	m := newTestManager(t)
	_, err := m.Switch([]string{"/nonexistent/workspace/path"})
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
}

func TestSwitch_DeduplicatesPaths(t *testing.T) {
	m := newTestManager(t)
	ws := t.TempDir()

	snap, err := m.Switch([]string{ws, ws})
	if err != nil {
		t.Fatalf("Switch with duplicate: %v", err)
	}
	if len(snap.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace after dedup, got %d", len(snap.Workspaces))
	}
}

func TestSwitch_IncrementsGeneration(t *testing.T) {
	m := newTestManager(t)
	initial := m.Snapshot()

	ws := t.TempDir()
	snap, err := m.Switch([]string{ws})
	if err != nil {
		t.Fatalf("Switch: %v", err)
	}
	if snap.Generation <= initial.Generation {
		t.Fatalf("expected generation to increment: initial=%d after=%d", initial.Generation, snap.Generation)
	}
}

// --- Store / HasStore tests ---

func TestStore_ReturnsCurrentStore(t *testing.T) {
	m := newTestManager(t)

	s, ok := m.Store()
	if !ok {
		t.Fatal("expected ok=true from Store()")
	}
	if s == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestHasStore_TrueWhenStorePresent(t *testing.T) {
	m := newTestManager(t)
	if !m.HasStore() {
		t.Fatal("expected HasStore()=true")
	}
}

func TestHasStore_FalseForNilStore(t *testing.T) {
	// NewStatic with nil store should have HasStore()=false.
	m := NewStatic(nil, nil, "")
	if m.HasStore() {
		t.Fatal("expected HasStore()=false for nil store")
	}
}

// --- InstructionsPath tests ---

func TestInstructionsPath_EmptyWithNoWorkspaces(t *testing.T) {
	m := newTestManager(t)
	if p := m.InstructionsPath(); p != "" {
		t.Fatalf("expected empty instructions path, got %q", p)
	}
}

func TestInstructionsPath_SetAfterSwitch(t *testing.T) {
	m := newTestManager(t)
	ws := t.TempDir()
	if _, err := m.Switch([]string{ws}); err != nil {
		t.Fatalf("Switch: %v", err)
	}
	if p := m.InstructionsPath(); p == "" {
		t.Fatal("expected non-empty instructions path after Switch with workspace")
	}
}

// --- Workspaces tests ---

func TestWorkspaces_EmptyInitially(t *testing.T) {
	m := newTestManager(t)
	if ws := m.Workspaces(); len(ws) != 0 {
		t.Fatalf("expected no workspaces, got %v", ws)
	}
}

func TestWorkspaces_ReturnsCurrentAfterSwitch(t *testing.T) {
	m := newTestManager(t)
	ws := t.TempDir()
	if _, err := m.Switch([]string{ws}); err != nil {
		t.Fatalf("Switch: %v", err)
	}
	got := m.Workspaces()
	if len(got) != 1 || got[0] != ws {
		t.Fatalf("expected [%q], got %v", ws, got)
	}
}

// --- Subscribe / Unsubscribe tests ---

func TestSubscribe_ReceivesSnapshotOnSwitch(t *testing.T) {
	m := newTestManager(t)
	ws := t.TempDir()

	id, ch := m.Subscribe()
	defer m.Unsubscribe(id)

	if _, err := m.Switch([]string{ws}); err != nil {
		t.Fatalf("Switch: %v", err)
	}

	select {
	case snap := <-ch:
		if len(snap.Workspaces) != 1 || snap.Workspaces[0] != ws {
			t.Fatalf("expected workspace %q in snapshot, got %v", ws, snap.Workspaces)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for snapshot from subscriber")
	}
}

func TestUnsubscribe_ClosesChannel(t *testing.T) {
	m := newTestManager(t)
	id, ch := m.Subscribe()
	m.Unsubscribe(id)

	// Channel should be closed.
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel after Unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("channel not closed after Unsubscribe")
	}
}

func TestSubscribe_MultipleSubscribersAllReceive(t *testing.T) {
	m := newTestManager(t)
	ws := t.TempDir()

	id1, ch1 := m.Subscribe()
	id2, ch2 := m.Subscribe()
	defer m.Unsubscribe(id1)
	defer m.Unsubscribe(id2)

	if _, err := m.Switch([]string{ws}); err != nil {
		t.Fatalf("Switch: %v", err)
	}

	for i, ch := range []<-chan Snapshot{ch1, ch2} {
		select {
		case snap := <-ch:
			if len(snap.Workspaces) != 1 {
				t.Fatalf("subscriber %d: expected 1 workspace, got %d", i, len(snap.Workspaces))
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out waiting for snapshot", i)
		}
	}
}

// --- NewStatic tests ---

func TestNewStatic_SnapshotReflectsInputs(t *testing.T) {
	storeDir := t.TempDir()
	s, err := store.NewStore(storeDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	ws := []string{"/fake/ws"}
	m := NewStatic(s, ws, "/path/to/instructions")

	snap := m.Snapshot()
	if snap.Store != s {
		t.Fatal("expected store pointer to match")
	}
	if snap.InstructionsPath != "/path/to/instructions" {
		t.Fatalf("expected instructions path, got %q", snap.InstructionsPath)
	}
	if snap.Generation != 1 {
		t.Fatalf("expected generation=1, got %d", snap.Generation)
	}
}
