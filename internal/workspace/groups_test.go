package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestUnmarshalLegacyWorkspacesKey verifies that a record persisted by the
// pre-redesign format (which used the "workspaces" key for the folder set, and
// had no id/data_key) still loads, populating Folders from the legacy key. This
// is what lets the existing workspace-groups.json keep working until migration
// rewrites it.
func TestUnmarshalLegacyWorkspacesKey(t *testing.T) {
	const legacy = `{"name":"Legacy","workspaces":["/a","/b"],"autoimplement":false}`
	var w Workspace
	if err := json.Unmarshal([]byte(legacy), &w); err != nil {
		t.Fatalf("unmarshal legacy record: %v", err)
	}
	if w.Name != "Legacy" {
		t.Errorf("name: got %q, want %q", w.Name, "Legacy")
	}
	if len(w.Folders) != 2 || w.Folders[0] != "/a" || w.Folders[1] != "/b" {
		t.Errorf("folders not populated from legacy workspaces key: got %v", w.Folders)
	}
	if w.ID != "" || w.DataKey != "" {
		t.Errorf("legacy record must have empty id/data_key, got id=%q data_key=%q", w.ID, w.DataKey)
	}
}

// TestUnmarshalFoldersKeyWinsOverLegacy verifies the current "folders" key takes
// precedence when both are present, and that new fields round-trip.
func TestUnmarshalFoldersKeyWinsOverLegacy(t *testing.T) {
	const both = `{"id":"id-1","data_key":"abc123","folders":["/new"],"workspaces":["/old"],"dormant":true}`
	var w Workspace
	if err := json.Unmarshal([]byte(both), &w); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(w.Folders) != 1 || w.Folders[0] != "/new" {
		t.Errorf("folders key should win over legacy workspaces: got %v", w.Folders)
	}
	if w.ID != "id-1" || w.DataKey != "abc123" || !w.Dormant {
		t.Errorf("new fields did not round-trip: id=%q data_key=%q dormant=%v", w.ID, w.DataKey, w.Dormant)
	}
}

// TestUpsertMovesExistingGroupToFront verifies that upserting an existing group
// promotes it to position 0 without duplicating it.
func TestUpsertMovesExistingGroupToFront(t *testing.T) {
	configDir := t.TempDir()
	wsA := t.TempDir()
	wsB := t.TempDir()

	if err := SaveGroups(configDir, []Workspace{
		{Folders: []string{wsA}},
		{Folders: []string{wsB}},
	}); err != nil {
		t.Fatalf("SaveGroups: %v", err)
	}

	if err := UpsertGroup(configDir, []string{wsB}); err != nil {
		t.Fatalf("UpsertGroup: %v", err)
	}

	groups, err := LoadGroups(configDir)
	if err != nil {
		t.Fatalf("LoadGroups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if got := groups[0].Folders; len(got) != 1 || got[0] != wsB {
		t.Fatalf("expected wsB group first, got %#v", got)
	}
	if got := groups[1].Folders; len(got) != 1 || got[0] != wsA {
		t.Fatalf("expected wsA group second, got %#v", got)
	}
}

// TestNormalizeGroups_DeduplicatesGroups verifies that groups with identical
// workspace sets (after path sorting) are collapsed to a single entry.
func TestNormalizeGroups_DeduplicatesGroups(t *testing.T) {
	wsA := t.TempDir()
	wsB := t.TempDir()

	// Two groups with same workspaces (but different order to make normalizeGroupPaths sort them).
	input := []Workspace{
		{Folders: []string{wsA, wsB}},
		{Folders: []string{wsB, wsA}}, // same after normalizeGroupPaths sorts
	}
	result := NormalizeGroups(input)
	if len(result) != 1 {
		t.Fatalf("NormalizeGroups deduplicated %d groups, want 1", len(result))
	}
}

// TestNormalizeGroups_RemovesEmptyWorkspaces verifies that empty-string entries
// are stripped from workspace lists within a group.
func TestNormalizeGroups_RemovesEmptyWorkspaces(t *testing.T) {
	wsA := t.TempDir()

	input := []Workspace{
		{Folders: []string{"", wsA, ""}},
	}
	result := NormalizeGroups(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result))
	}
	if len(result[0].Folders) != 1 || result[0].Folders[0] != wsA {
		t.Fatalf("expected single non-empty workspace, got %v", result[0].Folders)
	}
}

// TestNormalizeGroups_RemovesGroupsWithNoValidWorkspaces verifies that groups
// containing only empty/whitespace paths are dropped entirely.
func TestNormalizeGroups_RemovesGroupsWithNoValidWorkspaces(t *testing.T) {
	input := []Workspace{
		{Folders: []string{"", "   "}},
	}
	result := NormalizeGroups(input)
	if len(result) != 0 {
		t.Fatalf("expected 0 groups after all-empty workspaces, got %d", len(result))
	}
}

// TestNormalizeGroups_EmptyInput verifies nil and empty-slice inputs return nil.
func TestNormalizeGroups_EmptyInput(t *testing.T) {
	result := NormalizeGroups(nil)
	if result != nil {
		t.Fatalf("NormalizeGroups(nil) = %v, want nil", result)
	}
	result = NormalizeGroups([]Workspace{})
	if result != nil {
		t.Fatalf("NormalizeGroups([]) = %v, want nil", result)
	}
}

// TestLoadGroups_MissingFile_ReturnsNilNil verifies graceful handling when the
// groups file does not exist (first-run scenario).
func TestLoadGroups_MissingFile_ReturnsNilNil(t *testing.T) {
	configDir := t.TempDir()
	// No file written, so it should not exist.
	groups, err := LoadGroups(configDir)
	if err != nil {
		t.Fatalf("LoadGroups on missing file: %v", err)
	}
	if groups != nil {
		t.Fatalf("LoadGroups on missing file: expected nil, got %v", groups)
	}
}

// TestLoadGroups_MissingDirectory_ReturnsNilNil verifies graceful handling when
// the entire config directory does not exist.
func TestLoadGroups_MissingDirectory_ReturnsNilNil(t *testing.T) {
	configDir := t.TempDir() + "/nonexistent"
	groups, err := LoadGroups(configDir)
	if err != nil {
		t.Fatalf("LoadGroups on missing dir: %v", err)
	}
	if groups != nil {
		t.Fatalf("LoadGroups on missing dir: expected nil, got %v", groups)
	}
}

// TestSaveGroups_RoundTrip verifies that saved groups can be loaded back
// with identical content.
func TestSaveGroups_RoundTrip(t *testing.T) {
	configDir := t.TempDir()
	wsA := t.TempDir()
	wsB := t.TempDir()

	input := []Workspace{
		{Folders: []string{wsA}},
		{Folders: []string{wsB}},
	}
	if err := SaveGroups(configDir, input); err != nil {
		t.Fatalf("SaveGroups: %v", err)
	}

	got, err := LoadGroups(configDir)
	if err != nil {
		t.Fatalf("LoadGroups: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("round-trip: expected 2 groups, got %d", len(got))
	}
}

// TestSaveGroups_AtomicWrite verifies that no temporary file is left behind
// after a successful save (atomic rename semantics).
func TestSaveGroups_AtomicWrite(t *testing.T) {
	// Verify that .tmp file is cleaned up and the actual file exists.
	configDir := t.TempDir()
	wsA := t.TempDir()
	if err := SaveGroups(configDir, []Workspace{{Folders: []string{wsA}}}); err != nil {
		t.Fatalf("SaveGroups: %v", err)
	}

	// .tmp file should not exist after successful save.
	tmpPath := workspacesFilePath(configDir) + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("expected .tmp file to be removed after SaveGroups, but it exists")
	}
}

// TestNormalizeGroups_SortsPaths verifies that workspace paths within a group
// are sorted lexicographically after normalization.
func TestNormalizeGroups_SortsPaths(t *testing.T) {
	// normalizeGroupPaths sorts paths; verify NormalizeGroups preserves this.
	wsA := t.TempDir()
	wsB := t.TempDir()

	// Use wsB, wsA order - should get sorted.
	input := []Workspace{
		{Folders: []string{wsB, wsA}},
	}
	result := NormalizeGroups(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result))
	}
	ws := result[0].Folders
	if ws[0] >= ws[1] {
		t.Errorf("expected sorted workspaces, got %v", ws)
	}
}

// TestNormalizeGroups_MultiGroup verifies that distinct groups are preserved.
func TestNormalizeGroups_MultiGroup(t *testing.T) {
	wsA := t.TempDir()
	wsB := t.TempDir()
	wsC := t.TempDir()

	input := []Workspace{
		{Folders: []string{wsA}},
		{Folders: []string{wsB, wsC}},
	}
	result := NormalizeGroups(input)
	if len(result) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(result))
	}
}

// TestUpsertGroup_NewGroup_AddedToFront verifies that a previously unseen
// group is prepended to the list.
func TestUpsertGroup_NewGroup_AddedToFront(t *testing.T) {
	configDir := t.TempDir()
	wsA := t.TempDir()
	wsB := t.TempDir()

	if err := SaveGroups(configDir, []Workspace{
		{Folders: []string{wsA}},
	}); err != nil {
		t.Fatalf("SaveGroups: %v", err)
	}

	if err := UpsertGroup(configDir, []string{wsB}); err != nil {
		t.Fatalf("UpsertGroup new group: %v", err)
	}

	groups, err := LoadGroups(configDir)
	if err != nil {
		t.Fatalf("LoadGroups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].Folders[0] != wsB {
		t.Errorf("expected new group at front, got %v", groups[0].Folders)
	}
}

// TestNormalizeGroups_PreservesName verifies that user-assigned group names
// survive normalization.
func TestNormalizeGroups_PreservesName(t *testing.T) {
	wsA := t.TempDir()

	input := []Workspace{
		{Name: "My Project", Folders: []string{wsA}},
	}
	result := NormalizeGroups(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result))
	}
	if result[0].Name != "My Project" {
		t.Errorf("expected Name=%q, got %q", "My Project", result[0].Name)
	}
}

// TestUpsertGroup_PreservesExistingName verifies that promoting a group to the
// front retains its user-assigned name.
func TestUpsertGroup_PreservesExistingName(t *testing.T) {
	configDir := t.TempDir()
	wsA := t.TempDir()
	wsB := t.TempDir()

	if err := SaveGroups(configDir, []Workspace{
		{Name: "First", Folders: []string{wsA}},
		{Name: "Second", Folders: []string{wsB}},
	}); err != nil {
		t.Fatalf("SaveGroups: %v", err)
	}

	// Promote wsB to front — its name "Second" should be preserved.
	if err := UpsertGroup(configDir, []string{wsB}); err != nil {
		t.Fatalf("UpsertGroup: %v", err)
	}

	groups, err := LoadGroups(configDir)
	if err != nil {
		t.Fatalf("LoadGroups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].Name != "Second" {
		t.Errorf("promoted group: expected Name=%q, got %q", "Second", groups[0].Name)
	}
	if groups[1].Name != "First" {
		t.Errorf("remaining group: expected Name=%q, got %q", "First", groups[1].Name)
	}
}

// TestSaveGroups_RoundTrip_WithName verifies that named groups survive a
// save-then-load cycle.
func TestSaveGroups_RoundTrip_WithName(t *testing.T) {
	configDir := t.TempDir()
	wsA := t.TempDir()

	input := []Workspace{
		{Name: "Named Workspace", Folders: []string{wsA}},
	}
	if err := SaveGroups(configDir, input); err != nil {
		t.Fatalf("SaveGroups: %v", err)
	}

	got, err := LoadGroups(configDir)
	if err != nil {
		t.Fatalf("LoadGroups: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("round-trip: expected 1 group, got %d", len(got))
	}
	if got[0].Name != "Named Workspace" {
		t.Errorf("round-trip: expected Name=%q, got %q", "Named Workspace", got[0].Name)
	}
}

// TestSaveGroups_RoundTrip_WithLimits verifies that per-group parallel
// limit overrides survive a save/load cycle, that negative values are
// sanitized to nil (inherit default), and that zero is preserved as a
// deliberate "unlimited" override.
func TestSaveGroups_RoundTrip_WithLimits(t *testing.T) {
	configDir := t.TempDir()
	wsA := t.TempDir()
	wsB := t.TempDir()
	wsC := t.TempDir()

	mp, mtp := 3, 2
	zero := 0
	neg := -1
	input := []Workspace{
		{Name: "Limited", Folders: []string{wsA}, MaxParallel: &mp, MaxTestParallel: &mtp},
		{Name: "Unlimited", Folders: []string{wsB}, MaxParallel: &zero},
		{Name: "NegativeSanitized", Folders: []string{wsC}, MaxParallel: &neg},
	}
	if err := SaveGroups(configDir, input); err != nil {
		t.Fatalf("SaveGroups: %v", err)
	}
	got, err := LoadGroups(configDir)
	if err != nil {
		t.Fatalf("LoadGroups: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("round-trip: expected 3 groups, got %d", len(got))
	}

	byName := map[string]Workspace{}
	for _, g := range got {
		byName[g.Name] = g
	}
	limited := byName["Limited"]
	if limited.MaxParallel == nil || *limited.MaxParallel != 3 {
		t.Errorf("Limited.MaxParallel: want 3, got %v", limited.MaxParallel)
	}
	if limited.MaxTestParallel == nil || *limited.MaxTestParallel != 2 {
		t.Errorf("Limited.MaxTestParallel: want 2, got %v", limited.MaxTestParallel)
	}

	unlimited := byName["Unlimited"]
	if unlimited.MaxParallel == nil || *unlimited.MaxParallel != 0 {
		t.Errorf("Unlimited.MaxParallel: want 0, got %v", unlimited.MaxParallel)
	}

	sanitized := byName["NegativeSanitized"]
	if sanitized.MaxParallel != nil {
		t.Errorf("NegativeSanitized.MaxParallel: want nil after sanitize, got %v", *sanitized.MaxParallel)
	}
}

// TestUpsertGroup_EmptyWorkspaces_NoOp verifies that upserting an empty
// workspace list does not modify the existing groups.
func TestUpsertGroup_EmptyWorkspaces_NoOp(t *testing.T) {
	configDir := t.TempDir()
	wsA := t.TempDir()

	if err := SaveGroups(configDir, []Workspace{{Folders: []string{wsA}}}); err != nil {
		t.Fatalf("SaveGroups: %v", err)
	}

	// UpsertGroup with empty slice should be a no-op.
	if err := UpsertGroup(configDir, []string{}); err != nil {
		t.Fatalf("UpsertGroup empty: %v", err)
	}

	groups, err := LoadGroups(configDir)
	if err != nil {
		t.Fatalf("LoadGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group after no-op upsert, got %d", len(groups))
	}
}

// TestLoadGroups_ReadError verifies that a non-ErrNotExist read error is returned.
func TestLoadGroups_ReadError(t *testing.T) {
	configDir := t.TempDir()
	// Place a directory where the file is expected, causing a read error.
	if err := os.MkdirAll(workspacesFilePath(configDir), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := LoadGroups(configDir)
	if err == nil {
		t.Fatal("expected error when groups file is a directory")
	}
}

// TestLoadGroups_InvalidJSON verifies that malformed JSON returns an error.
func TestLoadGroups_InvalidJSON(t *testing.T) {
	configDir := t.TempDir()
	if err := os.WriteFile(workspacesFilePath(configDir), []byte("not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadGroups(configDir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// TestSaveGroups_MkdirAllError verifies that SaveGroups returns an error
// when the parent directory cannot be created.
func TestSaveGroups_MkdirAllError(t *testing.T) {
	// Use a path where a regular file blocks directory creation.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	// configDir is blocker/sub — MkdirAll will fail because blocker is a file.
	configDir := filepath.Join(blocker, "sub")
	err := SaveGroups(configDir, []Workspace{{Folders: []string{"/a"}}})
	if err == nil {
		t.Fatal("expected error when parent dir cannot be created")
	}
}

// TestUpsertGroup_LoadError verifies that UpsertGroup propagates LoadGroups errors.
func TestUpsertGroup_LoadError(t *testing.T) {
	configDir := t.TempDir()
	// Place invalid JSON so LoadGroups fails.
	if err := os.WriteFile(workspacesFilePath(configDir), []byte("bad"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	err := UpsertGroup(configDir, []string{t.TempDir()})
	if err == nil {
		t.Fatal("expected error when LoadGroups fails")
	}
}

// TestNormalizeGroupPaths_DeduplicatesPaths verifies that duplicate paths
// are collapsed to a single entry.
func TestNormalizeGroupPaths_DeduplicatesPaths(t *testing.T) {
	ws := t.TempDir()
	result := normalizeGroupPaths([]string{ws, ws, ws})
	if len(result) != 1 {
		t.Fatalf("expected 1 path after dedup, got %d", len(result))
	}
	if result[0] != ws {
		t.Fatalf("expected %q, got %q", ws, result[0])
	}
}
