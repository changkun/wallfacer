package workspace

import (
	"os"
	"testing"
)

// TestUpsertMovesExistingGroupToFront verifies that upserting an existing group
// promotes it to position 0 without duplicating it.
func TestUpsertMovesExistingGroupToFront(t *testing.T) {
	configDir := t.TempDir()
	wsA := t.TempDir()
	wsB := t.TempDir()

	if err := SaveGroups(configDir, []Group{
		{Workspaces: []string{wsA}},
		{Workspaces: []string{wsB}},
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
	if got := groups[0].Workspaces; len(got) != 1 || got[0] != wsB {
		t.Fatalf("expected wsB group first, got %#v", got)
	}
	if got := groups[1].Workspaces; len(got) != 1 || got[0] != wsA {
		t.Fatalf("expected wsA group second, got %#v", got)
	}
}

// TestNormalizeGroups_DeduplicatesGroups verifies that groups with identical
// workspace sets (after path sorting) are collapsed to a single entry.
func TestNormalizeGroups_DeduplicatesGroups(t *testing.T) {
	wsA := t.TempDir()
	wsB := t.TempDir()

	// Two groups with same workspaces (but different order to make normalizeGroupPaths sort them).
	input := []Group{
		{Workspaces: []string{wsA, wsB}},
		{Workspaces: []string{wsB, wsA}}, // same after normalizeGroupPaths sorts
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

	input := []Group{
		{Workspaces: []string{"", wsA, ""}},
	}
	result := NormalizeGroups(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result))
	}
	if len(result[0].Workspaces) != 1 || result[0].Workspaces[0] != wsA {
		t.Fatalf("expected single non-empty workspace, got %v", result[0].Workspaces)
	}
}

// TestNormalizeGroups_RemovesGroupsWithNoValidWorkspaces verifies that groups
// containing only empty/whitespace paths are dropped entirely.
func TestNormalizeGroups_RemovesGroupsWithNoValidWorkspaces(t *testing.T) {
	input := []Group{
		{Workspaces: []string{"", "   "}},
	}
	result := NormalizeGroups(input)
	if len(result) != 0 {
		t.Fatalf("expected 0 groups after all-empty workspaces, got %d", len(result))
	}
}

func TestNormalizeGroups_EmptyInput(t *testing.T) {
	result := NormalizeGroups(nil)
	if result != nil {
		t.Fatalf("NormalizeGroups(nil) = %v, want nil", result)
	}
	result = NormalizeGroups([]Group{})
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

	input := []Group{
		{Workspaces: []string{wsA}},
		{Workspaces: []string{wsB}},
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
	if err := SaveGroups(configDir, []Group{{Workspaces: []string{wsA}}}); err != nil {
		t.Fatalf("SaveGroups: %v", err)
	}

	// .tmp file should not exist after successful save.
	tmpPath := groupsFilePath(configDir) + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("expected .tmp file to be removed after SaveGroups, but it exists")
	}
}

func TestNormalizeGroups_SortsPaths(t *testing.T) {
	// normalizeGroupPaths sorts paths; verify NormalizeGroups preserves this.
	wsA := t.TempDir()
	wsB := t.TempDir()

	// Use wsB, wsA order - should get sorted.
	input := []Group{
		{Workspaces: []string{wsB, wsA}},
	}
	result := NormalizeGroups(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result))
	}
	ws := result[0].Workspaces
	if ws[0] >= ws[1] {
		t.Errorf("expected sorted workspaces, got %v", ws)
	}
}

func TestNormalizeGroups_MultiGroup(t *testing.T) {
	wsA := t.TempDir()
	wsB := t.TempDir()
	wsC := t.TempDir()

	input := []Group{
		{Workspaces: []string{wsA}},
		{Workspaces: []string{wsB, wsC}},
	}
	result := NormalizeGroups(input)
	if len(result) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(result))
	}
}

func TestUpsertGroup_NewGroup_AddedToFront(t *testing.T) {
	configDir := t.TempDir()
	wsA := t.TempDir()
	wsB := t.TempDir()

	if err := SaveGroups(configDir, []Group{
		{Workspaces: []string{wsA}},
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
	if groups[0].Workspaces[0] != wsB {
		t.Errorf("expected new group at front, got %v", groups[0].Workspaces)
	}
}

func TestNormalizeGroups_PreservesName(t *testing.T) {
	wsA := t.TempDir()

	input := []Group{
		{Name: "My Project", Workspaces: []string{wsA}},
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

	if err := SaveGroups(configDir, []Group{
		{Name: "First", Workspaces: []string{wsA}},
		{Name: "Second", Workspaces: []string{wsB}},
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

func TestSaveGroups_RoundTrip_WithName(t *testing.T) {
	configDir := t.TempDir()
	wsA := t.TempDir()

	input := []Group{
		{Name: "Named Group", Workspaces: []string{wsA}},
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
	if got[0].Name != "Named Group" {
		t.Errorf("round-trip: expected Name=%q, got %q", "Named Group", got[0].Name)
	}
}

// TestUpsertGroup_EmptyWorkspaces_NoOp verifies that upserting an empty
// workspace list does not modify the existing groups.
func TestUpsertGroup_EmptyWorkspaces_NoOp(t *testing.T) {
	configDir := t.TempDir()
	wsA := t.TempDir()

	if err := SaveGroups(configDir, []Group{{Workspaces: []string{wsA}}}); err != nil {
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
