package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"latere.ai/x/wallfacer/internal/prompts"
	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/store/storetest"
)

// writeWorkspacesFile writes records to workspaces.json verbatim, the way a
// release that allowed several folders per workspace left the file on disk.
func writeWorkspacesFile(t *testing.T, configDir string, records []map[string]any) {
	t.Helper()
	raw, err := json.Marshal(records)
	if err != nil {
		t.Fatalf("marshal workspaces: %v", err)
	}
	if err := os.WriteFile(workspacesFilePath(configDir), raw, 0o644); err != nil {
		t.Fatalf("write workspaces.json: %v", err)
	}
}

// TestValidate_SeveralFoldersRefused verifies that more than one distinct
// folder is refused with ErrSeveralFolders, while the same folder named twice
// (once with a trailing separator) is still one folder.
func TestValidate_SeveralFoldersRefused(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if _, err := validate([]string{a, b}); !errors.Is(err, ErrSeveralFolders) {
		t.Fatalf("validate(two folders) error = %v, want ErrSeveralFolders", err)
	}
	got, err := validate([]string{a, a + string(os.PathSeparator)})
	if err != nil {
		t.Fatalf("validate(same folder twice): %v", err)
	}
	if !slices.Equal(got, []string{a}) {
		t.Fatalf("validate(same folder twice) = %v, want [%s]", got, a)
	}
}

// TestCreateAndUpdateFolders_RefuseSeveralFolders verifies that the two
// writers of a workspace's folder refuse more than one folder and leave the
// saved records untouched when they do.
func TestCreateAndUpdateFolders_RefuseSeveralFolders(t *testing.T) {
	m, _, _ := newCountingManager(t)
	a, b := t.TempDir(), t.TempDir()

	if _, err := m.Create("two", []string{a, b}); !errors.Is(err, ErrSeveralFolders) {
		t.Fatalf("Create(two folders) error = %v, want ErrSeveralFolders", err)
	}
	if list, err := m.ListWorkspaces(); err != nil || len(list) != 0 {
		t.Fatalf("a refused Create saved a record: list=%v err=%v", list, err)
	}

	ws, err := m.Create("one", []string{a})
	if err != nil {
		t.Fatalf("Create(one folder): %v", err)
	}
	if _, err := m.UpdateFolders(ws.ID, []string{a, b}); !errors.Is(err, ErrSeveralFolders) {
		t.Fatalf("UpdateFolders(two folders) error = %v, want ErrSeveralFolders", err)
	}
	got, found, err := m.WorkspaceByID(ws.ID)
	if err != nil || !found {
		t.Fatalf("WorkspaceByID: found=%v err=%v", found, err)
	}
	if !slices.Equal(got.Folders, []string{a}) {
		t.Fatalf("a refused UpdateFolders changed the folder: got %v", got.Folders)
	}
}

// TestLoadGroups_SeveralFoldersLoadDormant verifies the load path for a record
// saved when a workspace could span several folders: it keeps its ID, name,
// DataKey and every folder (none is picked on the owner's behalf), and loads
// dormant. A one-folder record next to it is unaffected.
func TestLoadGroups_SeveralFoldersLoadDormant(t *testing.T) {
	configDir := t.TempDir()
	a, b := t.TempDir(), t.TempDir()
	writeWorkspacesFile(t, configDir, []map[string]any{
		{"id": "multi-id", "name": "Both repos", "folders": []string{b, a}, "data_key": "0123456789abcdef"},
		{"id": "single-id", "name": "One repo", "folders": []string{a}, "data_key": "fedcba9876543210"},
	})

	groups, err := LoadGroups(configDir)
	if err != nil {
		t.Fatalf("LoadGroups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("LoadGroups returned %d records, want 2", len(groups))
	}
	multi := groups[findByID(groups, "multi-id")]
	if !multi.Dormant {
		t.Error("a record with several folders did not load dormant")
	}
	if multi.Name != "Both repos" || multi.DataKey != "0123456789abcdef" {
		t.Errorf("a record with several folders lost its identity: %+v", multi)
	}
	if !slices.Equal(multi.Folders, []string{min(a, b), max(a, b)}) {
		t.Errorf("a record with several folders lost a folder: got %v", multi.Folders)
	}
	single := groups[findByID(groups, "single-id")]
	if single.Dormant {
		t.Error("a one-folder record loaded dormant")
	}
	if n := CountSeveralFolders(groups); n != 1 {
		t.Errorf("CountSeveralFolders = %d, want 1", n)
	}
}

// TestNewManager_SeveralFolderWorkspaceWaitsForRepoint covers a saved
// workspace with several folders from startup to re-point. Startup does not
// restore it even though it was the last one open, opening it is refused, and
// re-pointing it to one folder keeps its id, name and DataKey, clears the
// dormant mark, and brings its task history back on the board.
func TestNewManager_SeveralFolderWorkspaceWaitsForRepoint(t *testing.T) {
	ctx := context.Background()
	configDir, dataDir := t.TempDir(), t.TempDir()
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	const multiKey = "0123456789abcdef"

	// Task history under the several-folder workspace's data key.
	seed, err := storetest.NewFileStore(t, filepath.Join(dataDir, multiKey))
	if err != nil {
		t.Fatalf("open seed store: %v", err)
	}
	task, err := seed.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "kept history", Timeout: 5})
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
	seed.Close()

	// Most-recently-used first: the several-folder workspace was open last.
	writeWorkspacesFile(t, configDir, []map[string]any{
		{"id": "multi-id", "name": "Both repos", "folders": []string{a, b}, "data_key": multiKey},
		{"id": "single-id", "name": "One repo", "folders": []string{c}, "data_key": "fedcba9876543210"},
	})

	m, err := NewManager(configDir, dataDir, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if snap := m.Snapshot(); !slices.Equal(snap.Workspaces, []string{c}) {
		t.Fatalf("startup restored %v, want the one-folder workspace [%s]", snap.Workspaces, c)
	}

	if _, err := m.SwitchByID("multi-id"); !errors.Is(err, ErrSeveralFolders) {
		t.Fatalf("SwitchByID(several folders) error = %v, want ErrSeveralFolders", err)
	}

	ws, err := m.UpdateFolders("multi-id", []string{b})
	if err != nil {
		t.Fatalf("UpdateFolders(one folder): %v", err)
	}
	if ws.Dormant || ws.ID != "multi-id" || ws.Name != "Both repos" || ws.DataKey != multiKey {
		t.Fatalf("re-point did not keep identity and clear dormant: %+v", ws)
	}
	saved, found, err := m.WorkspaceByID("multi-id")
	if err != nil || !found {
		t.Fatalf("WorkspaceByID after re-point: found=%v err=%v", found, err)
	}
	if saved.Dormant || !slices.Equal(saved.Folders, []string{b}) {
		t.Fatalf("saved record after re-point = %+v, want one folder and not dormant", saved)
	}

	snap, err := m.SwitchByID("multi-id")
	if err != nil {
		t.Fatalf("SwitchByID after re-point: %v", err)
	}
	if snap.Key != multiKey {
		t.Fatalf("active key = %q, want the kept DataKey %q", snap.Key, multiKey)
	}
	got, err := snap.Store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("task history did not reappear after re-point: %v", err)
	}
	if got.Prompt != "kept history" {
		t.Fatalf("reappeared task prompt = %q, want %q", got.Prompt, "kept history")
	}
}

// TestMigrateToWorkspaces_SeveralFolderGroupMigratesDormant verifies that a
// legacy path-keyed group naming several folders migrates with the path-seeded
// DataKey that already names its data directory, keeps every folder, and is
// written to workspaces.json as dormant so the mark survives the next load.
func TestMigrateToWorkspaces_SeveralFolderGroupMigratesDormant(t *testing.T) {
	configDir, dataDir := t.TempDir(), t.TempDir()
	a, b := t.TempDir(), t.TempDir()
	legacy, err := json.Marshal([]map[string]any{{"name": "Pair", "workspaces": []string{a, b}}})
	if err != nil {
		t.Fatalf("marshal legacy groups: %v", err)
	}
	if err := os.WriteFile(legacyGroupsFilePath(configDir), legacy, 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	if _, err := MigrateToWorkspaces(configDir, dataDir, "teststamp"); err != nil {
		t.Fatalf("MigrateToWorkspaces: %v", err)
	}

	raw, err := os.ReadFile(workspacesFilePath(configDir))
	if err != nil {
		t.Fatalf("read workspaces.json: %v", err)
	}
	var written []Workspace
	if err := json.Unmarshal(raw, &written); err != nil {
		t.Fatalf("decode workspaces.json: %v", err)
	}
	if len(written) != 1 {
		t.Fatalf("workspaces.json holds %d records, want 1", len(written))
	}
	got := written[0]
	if !got.Dormant {
		t.Error("migrated several-folder group was not written dormant")
	}
	if got.ID == "" || got.Name != "Pair" {
		t.Errorf("migrated group lost its identity: %+v", got)
	}
	if want := prompts.WorkspaceDataKey([]string{a, b}); got.DataKey != want {
		t.Errorf("DataKey = %q, want the path-seeded key %q that names its history", got.DataKey, want)
	}
	if len(got.Folders) != 2 {
		t.Errorf("migrated group lost a folder: %v", got.Folders)
	}
}
