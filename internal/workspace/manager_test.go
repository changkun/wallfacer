package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/store"
)

// TestNewManagerWithoutWorkspacesCreatesScopedStore verifies that even with no
// workspaces, a scoped store and key are still created (for the "empty" workspace set).
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

// TestNewManagerWithoutWorkspacesLoadsMostRecentWorkspaceGroup verifies session
// restore: when no initial workspaces are provided (nil), the most recently used
// group is loaded from disk.
func TestNewManagerWithoutWorkspacesLoadsMostRecentWorkspaceGroup(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, nil, 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	wsA := t.TempDir()
	wsB := t.TempDir()
	if err := SaveGroups(configDir, []Group{
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

// TestNewManagerExplicitEmptyWorkspacesDoesNotRestoreSavedGroup verifies that
// passing an explicit empty slice (non-nil) suppresses session restore,
// distinguishing "no workspaces requested" from "use last session".
func TestNewManagerExplicitEmptyWorkspacesDoesNotRestoreSavedGroup(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, nil, 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	ws := t.TempDir()
	if err := SaveGroups(configDir, []Group{
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

// newTestManager is a helper that creates a Manager with a writable env file
// and returns the manager plus the env file path.
func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	configDir := t.TempDir()
	dataDir := t.TempDir()
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, nil, 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	m, err := NewManager(configDir, dataDir, envFile, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m, envFile
}

// --- Switch tests ---

func TestSwitch_ToValidWorkspace(t *testing.T) {
	m, _ := newTestManager(t)
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
	m, _ := newTestManager(t)
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
	m, _ := newTestManager(t)
	_, err := m.Switch([]string{"relative/path"})
	if err == nil {
		t.Fatal("expected error for non-absolute path")
	}
}

func TestSwitch_InvalidPath_NonExistent(t *testing.T) {
	m, _ := newTestManager(t)
	_, err := m.Switch([]string{"/nonexistent/workspace/path"})
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
}

func TestSwitch_DeduplicatesPaths(t *testing.T) {
	m, _ := newTestManager(t)
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
	m, _ := newTestManager(t)
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
	m, _ := newTestManager(t)

	s, ok := m.Store()
	if !ok {
		t.Fatal("expected ok=true from Store()")
	}
	if s == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestHasStore_TrueWhenStorePresent(t *testing.T) {
	m, _ := newTestManager(t)
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
	m, _ := newTestManager(t)
	if p := m.InstructionsPath(); p != "" {
		t.Fatalf("expected empty instructions path, got %q", p)
	}
}

func TestInstructionsPath_SetAfterSwitch(t *testing.T) {
	m, _ := newTestManager(t)
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
	m, _ := newTestManager(t)
	if ws := m.Workspaces(); len(ws) != 0 {
		t.Fatalf("expected no workspaces, got %v", ws)
	}
}

func TestWorkspaces_ReturnsCurrentAfterSwitch(t *testing.T) {
	m, _ := newTestManager(t)
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
	m, _ := newTestManager(t)
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
	m, _ := newTestManager(t)
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
	m, _ := newTestManager(t)
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
	s, err := store.NewFileStore(storeDir)
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

// --- Transactional switch tests ---

// TestSwitch_NoOpWhenWorkspacesMatch verifies that switching to the same
// normalized workspace set does not increment the generation counter and does
// not rewrite persisted workspace groups.
func TestSwitch_NoOpWhenWorkspacesMatch(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, nil, 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	ws := t.TempDir()
	m, err := NewManager(configDir, dataDir, envFile, []string{ws})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	firstSnap := m.Snapshot()
	if len(firstSnap.Workspaces) != 1 || firstSnap.Workspaces[0] != ws {
		t.Fatalf("unexpected initial workspaces: %v", firstSnap.Workspaces)
	}

	// Record workspace groups state before the no-op switch.
	groupsBefore, err := LoadGroups(configDir)
	if err != nil {
		t.Fatalf("load groups before: %v", err)
	}

	// Switch to the same workspace — should be a no-op.
	snap, err := m.Switch([]string{ws})
	if err != nil {
		t.Fatalf("Switch (no-op): %v", err)
	}

	if snap.Generation != firstSnap.Generation {
		t.Errorf("generation should not change on no-op switch: before=%d after=%d",
			firstSnap.Generation, snap.Generation)
	}
	if m.Snapshot().Generation != firstSnap.Generation {
		t.Errorf("manager generation should not change on no-op switch")
	}

	// Workspace groups file must not have been rewritten.
	groupsAfter, err := LoadGroups(configDir)
	if err != nil {
		t.Fatalf("load groups after: %v", err)
	}
	if len(groupsBefore) != len(groupsAfter) {
		t.Errorf("workspace groups count changed on no-op switch: before=%d after=%d",
			len(groupsBefore), len(groupsAfter))
	}
}

// TestSwitch_FailedInstructionClosesCandidate verifies that when
// instructions.Ensure fails the candidate store is closed (not leaked) and
// the manager's current snapshot is left unchanged.
func TestSwitch_FailedInstructionClosesCandidate(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, nil, 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	m, err := NewManager(configDir, dataDir, envFile, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	originalSnap := m.Snapshot()

	// Intercept the next store.NewStore call so we can verify Close() is
	// called on the candidate when the switch fails.
	var candidateStore *store.Store
	m.newStore = func(dir string) (*store.Store, error) {
		s, err := store.NewFileStore(dir)
		if err == nil {
			candidateStore = s
		}
		return s, err
	}

	// Block instructions.Ensure by placing a regular file where it expects to
	// create the "instructions" sub-directory.
	instrBlocker := filepath.Join(configDir, "instructions")
	if err := os.WriteFile(instrBlocker, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("create blocker: %v", err)
	}

	ws := t.TempDir()
	_, err = m.Switch([]string{ws})
	if err == nil {
		t.Fatal("expected Switch to fail when instructions creation is blocked")
	}

	// The candidate store must have been closed.
	if candidateStore == nil {
		t.Fatal("expected a candidate store to have been created")
	}
	if !candidateStore.IsClosed() {
		t.Error("expected candidate store to be closed after failed Switch")
	}

	// The manager's snapshot must be unchanged.
	snap := m.Snapshot()
	if snap.Generation != originalSnap.Generation {
		t.Errorf("generation changed after failed switch: before=%d after=%d",
			originalSnap.Generation, snap.Generation)
	}
	if !workspacesEqual(snap.Workspaces, originalSnap.Workspaces) {
		t.Errorf("workspaces changed after failed switch: before=%v after=%v",
			originalSnap.Workspaces, snap.Workspaces)
	}
}

// TestSwitch_FailedEnvPersistenceRollsBack verifies that when the env-file
// update fails the manager's current snapshot is not replaced.
func TestSwitch_FailedEnvPersistenceRollsBack(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, nil, 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	// Start with a real workspace so there's something to roll back to.
	wsA := t.TempDir()
	m, err := NewManager(configDir, dataDir, envFile, []string{wsA})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	previousSnap := m.Snapshot()

	// Replace envFile with a directory so envconfig.Update fails at read time.
	if err := os.Remove(envFile); err != nil {
		t.Fatalf("remove env file: %v", err)
	}
	if err := os.Mkdir(envFile, 0o755); err != nil {
		t.Fatalf("mkdir at env file path: %v", err)
	}

	wsB := t.TempDir()
	_, err = m.Switch([]string{wsB})
	if err == nil {
		t.Fatal("expected Switch to fail when env file is unwritable")
	}

	// The snapshot must be identical to what it was before the failed switch.
	snap := m.Snapshot()
	if snap.Generation != previousSnap.Generation {
		t.Errorf("generation changed after failed env update: before=%d after=%d",
			previousSnap.Generation, snap.Generation)
	}
	if !workspacesEqual(snap.Workspaces, previousSnap.Workspaces) {
		t.Errorf("workspaces changed after failed env update: before=%v after=%v",
			previousSnap.Workspaces, snap.Workspaces)
	}
	if snap.Store != previousSnap.Store {
		t.Error("store pointer changed after failed env update")
	}
}

// TestSwitch_SuccessClosesPreviousStore verifies that after a successful
// workspace switch the store from the previous snapshot is closed.
func TestSwitch_SuccessClosesPreviousStore(t *testing.T) {
	m, _ := newTestManager(t)

	// Capture the initial store before switching.
	initialStore := m.Snapshot().Store
	if initialStore == nil {
		t.Fatal("expected non-nil initial store")
	}

	// Switch to a new workspace set.
	ws := t.TempDir()
	_, err := m.Switch([]string{ws})
	if err != nil {
		t.Fatalf("Switch: %v", err)
	}

	// The old store should now be closed.
	if !initialStore.IsClosed() {
		t.Error("expected previous store to be closed after successful Switch")
	}

	// The new snapshot should have a different (non-closed) store.
	newSnap := m.Snapshot()
	if newSnap.Store == nil {
		t.Fatal("expected non-nil store in new snapshot")
	}
	if newSnap.Store.IsClosed() {
		t.Error("expected new store to remain open")
	}
}

// --- activeGroups tests ---

// TestActiveGroupsInitialization verifies that after NewManager, activeGroups
// has exactly one entry matching the initial snapshot.
func TestActiveGroupsInitialization(t *testing.T) {
	m, _ := newTestManager(t)
	keys := m.ActiveGroupKeys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 active group, got %d", len(keys))
	}
	snap := m.Snapshot()
	if keys[0] != snap.Key {
		t.Fatalf("active group key %q does not match snapshot key %q", keys[0], snap.Key)
	}
	s, ok := m.StoreForKey(snap.Key)
	if !ok {
		t.Fatal("StoreForKey returned false for the initial group")
	}
	if s != snap.Store {
		t.Fatal("StoreForKey returned a different store than the snapshot")
	}
}

// TestActiveGroupsInitializationStatic verifies that NewStatic also initializes
// activeGroups with exactly one entry.
func TestActiveGroupsInitializationStatic(t *testing.T) {
	storeDir := t.TempDir()
	s, err := store.NewFileStore(storeDir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	m := NewStatic(s, []string{"/fake/ws"}, "")
	keys := m.ActiveGroupKeys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 active group, got %d", len(keys))
	}
}

// TestIncrementDecrementTaskCount verifies that incrementing and decrementing
// task counts works, and that a non-viewed group is cleaned up when its count
// reaches zero.
func TestIncrementDecrementTaskCount(t *testing.T) {
	m, _ := newTestManager(t)

	// Insert a second active group to simulate a background group.
	bgStoreDir := t.TempDir()
	bgStore, err := store.NewFileStore(bgStoreDir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	bgKey := "background-group-key"
	m.mu.Lock()
	m.activeGroups[bgKey] = &activeGroup{
		snapshot: Snapshot{
			Key:   bgKey,
			Store: bgStore,
		},
	}
	m.mu.Unlock()

	// Increment twice.
	m.IncrementTaskCount(bgKey)
	m.IncrementTaskCount(bgKey)

	// Decrement once — group should still exist.
	m.DecrementAndCleanup(bgKey)
	if _, ok := m.StoreForKey(bgKey); !ok {
		t.Fatal("expected background group to still exist after first decrement")
	}
	if bgStore.IsClosed() {
		t.Fatal("expected background store to remain open")
	}

	// Decrement to zero — non-viewed group should be cleaned up.
	m.DecrementAndCleanup(bgKey)
	if _, ok := m.StoreForKey(bgKey); ok {
		t.Fatal("expected background group to be removed after count reached zero")
	}
	if !bgStore.IsClosed() {
		t.Fatal("expected background store to be closed after cleanup")
	}
}

// TestDecrementViewedGroupNotRemoved verifies that the currently viewed group
// is not removed from activeGroups even when its task count reaches zero.
func TestDecrementViewedGroupNotRemoved(t *testing.T) {
	m, _ := newTestManager(t)
	snap := m.Snapshot()

	// Increment and decrement on the viewed group.
	m.IncrementTaskCount(snap.Key)
	m.DecrementAndCleanup(snap.Key)

	// The viewed group should still exist.
	if _, ok := m.StoreForKey(snap.Key); !ok {
		t.Fatal("expected viewed group to remain in activeGroups after count reached zero")
	}
	if snap.Store.IsClosed() {
		t.Fatal("expected viewed group store to remain open")
	}
}

// TestAllActiveSnapshots verifies that AllActiveSnapshots returns all groups.
func TestAllActiveSnapshots(t *testing.T) {
	m, _ := newTestManager(t)

	// Insert a second active group.
	bgStoreDir := t.TempDir()
	bgStore, err := store.NewFileStore(bgStoreDir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	bgKey := "another-group"
	m.mu.Lock()
	m.activeGroups[bgKey] = &activeGroup{
		snapshot: Snapshot{
			Key:   bgKey,
			Store: bgStore,
		},
	}
	m.mu.Unlock()

	snaps := m.AllActiveSnapshots()
	if len(snaps) != 2 {
		t.Fatalf("expected 2 active snapshots, got %d", len(snaps))
	}

	// Verify both keys are present.
	keys := make(map[string]bool)
	for _, s := range snaps {
		keys[s.Key] = true
	}
	viewedKey := m.Snapshot().Key
	if !keys[viewedKey] {
		t.Fatalf("expected viewed group key %q in snapshots", viewedKey)
	}
	if !keys[bgKey] {
		t.Fatalf("expected background group key %q in snapshots", bgKey)
	}
}

// TestStoreForKey verifies correct store lookup and false for unknown keys.
func TestStoreForKey(t *testing.T) {
	m, _ := newTestManager(t)
	snap := m.Snapshot()

	// Known key returns the correct store.
	s, ok := m.StoreForKey(snap.Key)
	if !ok {
		t.Fatal("expected ok=true for known key")
	}
	if s != snap.Store {
		t.Fatal("expected store to match snapshot store")
	}

	// Unknown key returns false.
	_, ok = m.StoreForKey("nonexistent-key")
	if ok {
		t.Fatal("expected ok=false for unknown key")
	}
}

// TestIncrementUnknownKeyIsNoOp verifies that incrementing an unknown key
// does not panic or create an entry.
func TestIncrementUnknownKeyIsNoOp(t *testing.T) {
	m, _ := newTestManager(t)
	m.IncrementTaskCount("nonexistent")
	// Should not panic or add an entry.
	if len(m.ActiveGroupKeys()) != 1 {
		t.Fatal("expected no new group created for unknown key")
	}
}

// TestDecrementUnknownKeyIsNoOp verifies that decrementing an unknown key
// does not panic.
func TestDecrementUnknownKeyIsNoOp(t *testing.T) {
	m, _ := newTestManager(t)
	m.DecrementAndCleanup("nonexistent")
	// Should not panic.
}
