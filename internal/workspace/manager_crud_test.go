package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/store/storetest"
)

// newCountingManager builds a Manager whose store factory counts how many
// scoped stores it opens, so tests can assert that an operation did (or did
// not) re-create a store. The manager starts with no active workspace.
func newCountingManager(t *testing.T) (m *Manager, configDir string, opens *int) {
	t.Helper()
	configDir = t.TempDir()
	dataDir := t.TempDir()
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, nil, 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	mgr, err := NewManager(configDir, dataDir, envFile, []string{}) // empty: no restore
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	count := 0
	mgr.newStore = func(dir string) (*store.Store, error) {
		count++
		return storetest.NewFileStore(t, dir)
	}
	return mgr, configDir, &count
}

// TestUpdateFolders_PreservesIdentityAndHistory is the keystone test of the
// redesign: editing a workspace's folder set must NOT re-key its storage. The
// store pointer, DataKey, and agent-session history must all survive, while the
// snapshot's folder paths update.
func TestUpdateFolders_PreservesIdentityAndHistory(t *testing.T) {
	m, configDir, opens := newCountingManager(t)
	dirA, dirB := t.TempDir(), t.TempDir()

	ws, err := m.Create("proj", []string{dirA}, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	snap1, err := m.SwitchByID(ws.ID)
	if err != nil {
		t.Fatalf("SwitchByID: %v", err)
	}
	if *opens != 1 {
		t.Fatalf("expected exactly 1 store open after activation, got %d", *opens)
	}

	// Write agent-session history under the active DataKey — this is the
	// directory that the orphaning bug used to strand on a folder edit.
	if err := store.AppendAgentSessionUsage(configDir, snap1.Key, store.TurnUsageRecord{Turn: 1, InputTokens: 42}); err != nil {
		t.Fatalf("seed agent-session usage: %v", err)
	}

	// The membership edit: add a folder.
	if _, err := m.UpdateFolders(ws.ID, []string{dirA, dirB}); err != nil {
		t.Fatalf("UpdateFolders: %v", err)
	}
	snap2 := m.Snapshot()

	if *opens != 1 {
		t.Errorf("UpdateFolders must not re-open a store: opens went to %d", *opens)
	}
	if snap2.WorkspaceID != ws.ID {
		t.Errorf("workspace id changed: got %q want %q", snap2.WorkspaceID, ws.ID)
	}
	if snap2.Key != snap1.Key {
		t.Errorf("DataKey changed on folder edit: got %q want %q", snap2.Key, snap1.Key)
	}
	if snap2.Store != snap1.Store {
		t.Error("store pointer changed on folder edit; history would be orphaned")
	}
	if len(snap2.Workspaces) != 2 || snap2.Workspaces[0] != min(dirA, dirB) {
		t.Errorf("folders not updated: got %v", snap2.Workspaces)
	}
	recs, err := store.ReadAgentSessionUsage(configDir, snap2.Key, time.Time{})
	if err != nil {
		t.Fatalf("read agent-session usage after edit: %v", err)
	}
	if len(recs) != 1 || recs[0].InputTokens != 42 {
		t.Fatalf("agent-session history lost after folder edit: got %v", recs)
	}
}

// TestCreate_RandomKeyStartsEmpty verifies acceptance criterion 4: a new
// workspace whose folders coincide with an existing one starts empty, because
// Create assigns a random DataKey rather than seeding from the folder set.
func TestCreate_RandomKeyStartsEmpty(t *testing.T) {
	m, configDir, _ := newCountingManager(t)
	dirA := t.TempDir()

	ws1, err := m.Create("first", []string{dirA}, nil)
	if err != nil {
		t.Fatalf("Create ws1: %v", err)
	}
	snap1, err := m.SwitchByID(ws1.ID)
	if err != nil {
		t.Fatalf("SwitchByID ws1: %v", err)
	}
	if err := store.AppendAgentSessionUsage(configDir, snap1.Key, store.TurnUsageRecord{Turn: 1, InputTokens: 7}); err != nil {
		t.Fatalf("seed usage: %v", err)
	}

	ws2, err := m.Create("second", []string{dirA}, nil)
	if err != nil {
		t.Fatalf("Create ws2: %v", err)
	}
	if ws2.DataKey == ws1.DataKey {
		t.Fatal("two workspaces over the same folders must not share a DataKey")
	}
	snap2, err := m.SwitchByID(ws2.ID)
	if err != nil {
		t.Fatalf("SwitchByID ws2: %v", err)
	}
	if snap2.Key == snap1.Key {
		t.Fatal("active key collided across two same-folder workspaces")
	}
	recs, err := store.ReadAgentSessionUsage(configDir, snap2.Key, time.Time{})
	if err != nil {
		t.Fatalf("read usage ws2: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("new workspace inherited history from a same-folder workspace: %v", recs)
	}
}

// TestSameFolderWorkspacesCoexist verifies criterion 3: two workspaces may list
// the same folder; both persist (deduped by id, not folder set) and switching
// between them by id is never a silent no-op (the id-aware short-circuit).
func TestSameFolderWorkspacesCoexist(t *testing.T) {
	m, _, _ := newCountingManager(t)
	dirA := t.TempDir()

	ws1, err := m.Create("one", []string{dirA}, nil)
	if err != nil {
		t.Fatalf("Create ws1: %v", err)
	}
	ws2, err := m.Create("two", []string{dirA}, nil)
	if err != nil {
		t.Fatalf("Create ws2: %v", err)
	}

	got, err := m.ListWorkspaces(nil)
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 same-folder workspaces to coexist, got %d", len(got))
	}

	if _, err := m.SwitchByID(ws1.ID); err != nil {
		t.Fatalf("SwitchByID ws1: %v", err)
	}
	snap, err := m.SwitchByID(ws2.ID)
	if err != nil {
		t.Fatalf("SwitchByID ws2: %v", err)
	}
	if snap.WorkspaceID != ws2.ID {
		t.Fatalf("id-aware switch failed: still on %q, want %q (path-equality short-circuit fired)", snap.WorkspaceID, ws2.ID)
	}
}

// TestDelete_WipesDataAndAutoSwitches verifies the redesigned delete: the
// active workspace CAN be deleted; its scoped data + agent-session history are
// wiped from disk; the board auto-switches to the next usable workspace; and
// deleting the last one leaves the empty (no-active) state. Idempotent.
func TestDelete_WipesDataAndAutoSwitches(t *testing.T) {
	m, configDir, _ := newCountingManager(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	a, err := m.Create("A", []string{dirA}, nil)
	if err != nil {
		t.Fatalf("Create A: %v", err)
	}
	b, err := m.Create("B", []string{dirB}, nil)
	if err != nil {
		t.Fatalf("Create B: %v", err)
	}

	// Activate A and seed both its scoped store dir and its agent-session dir.
	snapA, err := m.SwitchByID(a.ID)
	if err != nil {
		t.Fatalf("SwitchByID A: %v", err)
	}
	if err := store.AppendAgentSessionUsage(configDir, snapA.Key, store.TurnUsageRecord{Turn: 1}); err != nil {
		t.Fatalf("seed usage: %v", err)
	}
	dataA := filepath.Join(m.dataDir, snapA.Key)
	sessA := store.AgentSessionUsageDir(configDir, snapA.Key)
	if _, err := os.Stat(dataA); err != nil {
		t.Fatalf("A data dir should exist before delete: %v", err)
	}
	if _, err := os.Stat(sessA); err != nil {
		t.Fatalf("A session dir should exist before delete: %v", err)
	}

	// Delete the ACTIVE workspace A → auto-switch to B, wipe A's data.
	snap, err := m.Delete(a.ID)
	if err != nil {
		t.Fatalf("Delete active A: %v", err)
	}
	if snap.WorkspaceID != b.ID {
		t.Fatalf("expected auto-switch to B (%s), got %q", b.ID, snap.WorkspaceID)
	}
	if _, found, _ := m.WorkspaceByID(a.ID); found {
		t.Fatal("deleted workspace A still present")
	}
	if _, err := os.Stat(dataA); !os.IsNotExist(err) {
		t.Errorf("A data dir not wiped: %v", err)
	}
	if _, err := os.Stat(sessA); !os.IsNotExist(err) {
		t.Errorf("A session dir not wiped: %v", err)
	}

	// Delete the LAST workspace B (now active) → empty active state.
	snap2, err := m.Delete(b.ID)
	if err != nil {
		t.Fatalf("Delete last B: %v", err)
	}
	if snap2.WorkspaceID != "" || len(snap2.Workspaces) != 0 {
		t.Fatalf("expected no active workspace after deleting the last, got id=%q ws=%v", snap2.WorkspaceID, snap2.Workspaces)
	}
	if list, _ := m.ListWorkspaces(nil); len(list) != 0 {
		t.Fatalf("expected empty registry, got %d", len(list))
	}

	// Idempotent: deleting an already-gone id is a no-op.
	if _, err := m.Delete(b.ID); err != nil {
		t.Fatalf("idempotent delete: %v", err)
	}
}

// TestRename updates the display name without disturbing identity.
func TestRename(t *testing.T) {
	m, _, _ := newCountingManager(t)
	ws, err := m.Create("old", []string{t.TempDir()}, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	renamed, err := m.Rename(ws.ID, "new")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if renamed.Name != "new" || renamed.ID != ws.ID || renamed.DataKey != ws.DataKey {
		t.Fatalf("rename altered identity: %+v", renamed)
	}
}

// TestCreate_StampsOwner verifies a signed-in principal is recorded at creation,
// replacing the lazy ClaimGroup-on-switch path.
func TestCreate_StampsOwner(t *testing.T) {
	m, _, _ := newCountingManager(t)
	ws, err := m.Create("p", []string{t.TempDir()}, &Principal{Sub: "alice", OrgID: "org-a"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ws.CreatedBy != "alice" || ws.OrgID != "org-a" {
		t.Fatalf("owner not stamped at creation: %+v", ws)
	}
}
