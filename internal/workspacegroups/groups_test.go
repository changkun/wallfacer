package workspacegroups

import (
	"os"
	"testing"
)

func TestUpsertMovesExistingGroupToFront(t *testing.T) {
	configDir := t.TempDir()
	wsA := t.TempDir()
	wsB := t.TempDir()

	if err := Save(configDir, []Group{
		{Workspaces: []string{wsA}},
		{Workspaces: []string{wsB}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := Upsert(configDir, []string{wsB}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	groups, err := Load(configDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
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

func TestNormalize_DeduplicatesGroups(t *testing.T) {
	wsA := t.TempDir()
	wsB := t.TempDir()

	// Two groups with same workspaces (but different order to make normalizePaths sort them).
	input := []Group{
		{Workspaces: []string{wsA, wsB}},
		{Workspaces: []string{wsB, wsA}}, // same after normalizePaths sorts
	}
	result := Normalize(input)
	if len(result) != 1 {
		t.Fatalf("Normalize deduplicated %d groups, want 1", len(result))
	}
}

func TestNormalize_RemovesEmptyWorkspaces(t *testing.T) {
	wsA := t.TempDir()

	input := []Group{
		{Workspaces: []string{"", wsA, ""}},
	}
	result := Normalize(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result))
	}
	if len(result[0].Workspaces) != 1 || result[0].Workspaces[0] != wsA {
		t.Fatalf("expected single non-empty workspace, got %v", result[0].Workspaces)
	}
}

func TestNormalize_RemovesGroupsWithNoValidWorkspaces(t *testing.T) {
	input := []Group{
		{Workspaces: []string{"", "   "}},
	}
	result := Normalize(input)
	if len(result) != 0 {
		t.Fatalf("expected 0 groups after all-empty workspaces, got %d", len(result))
	}
}

func TestNormalize_EmptyInput(t *testing.T) {
	result := Normalize(nil)
	if result != nil {
		t.Fatalf("Normalize(nil) = %v, want nil", result)
	}
	result = Normalize([]Group{})
	if result != nil {
		t.Fatalf("Normalize([]) = %v, want nil", result)
	}
}

func TestLoad_MissingFile_ReturnsNilNil(t *testing.T) {
	configDir := t.TempDir()
	// No file written, so it should not exist.
	groups, err := Load(configDir)
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if groups != nil {
		t.Fatalf("Load on missing file: expected nil, got %v", groups)
	}
}

func TestLoad_MissingDirectory_ReturnsNilNil(t *testing.T) {
	configDir := t.TempDir() + "/nonexistent"
	groups, err := Load(configDir)
	if err != nil {
		t.Fatalf("Load on missing dir: %v", err)
	}
	if groups != nil {
		t.Fatalf("Load on missing dir: expected nil, got %v", groups)
	}
}

func TestSave_RoundTrip(t *testing.T) {
	configDir := t.TempDir()
	wsA := t.TempDir()
	wsB := t.TempDir()

	input := []Group{
		{Workspaces: []string{wsA}},
		{Workspaces: []string{wsB}},
	}
	if err := Save(configDir, input); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(configDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("round-trip: expected 2 groups, got %d", len(got))
	}
}

func TestSave_AtomicWrite(t *testing.T) {
	// Verify that .tmp file is cleaned up and the actual file exists.
	configDir := t.TempDir()
	wsA := t.TempDir()
	if err := Save(configDir, []Group{{Workspaces: []string{wsA}}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// .tmp file should not exist after successful save.
	tmpPath := filePath(configDir) + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("expected .tmp file to be removed after Save, but it exists")
	}
}

func TestNormalize_SortsPaths(t *testing.T) {
	// normalizePaths sorts paths; verify Normalize preserves this.
	wsA := t.TempDir()
	wsB := t.TempDir()

	// Use wsB, wsA order - should get sorted.
	input := []Group{
		{Workspaces: []string{wsB, wsA}},
	}
	result := Normalize(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result))
	}
	ws := result[0].Workspaces
	if ws[0] >= ws[1] {
		t.Errorf("expected sorted workspaces, got %v", ws)
	}
}

func TestNormalize_MultiGroup(t *testing.T) {
	wsA := t.TempDir()
	wsB := t.TempDir()
	wsC := t.TempDir()

	input := []Group{
		{Workspaces: []string{wsA}},
		{Workspaces: []string{wsB, wsC}},
	}
	result := Normalize(input)
	if len(result) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(result))
	}
}

func TestUpsert_NewGroup_AddedToFront(t *testing.T) {
	configDir := t.TempDir()
	wsA := t.TempDir()
	wsB := t.TempDir()

	if err := Save(configDir, []Group{
		{Workspaces: []string{wsA}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := Upsert(configDir, []string{wsB}); err != nil {
		t.Fatalf("Upsert new group: %v", err)
	}

	groups, err := Load(configDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].Workspaces[0] != wsB {
		t.Errorf("expected new group at front, got %v", groups[0].Workspaces)
	}
}

func TestUpsert_EmptyWorkspaces_NoOp(t *testing.T) {
	configDir := t.TempDir()
	wsA := t.TempDir()

	if err := Save(configDir, []Group{{Workspaces: []string{wsA}}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Upsert with empty slice should be a no-op.
	if err := Upsert(configDir, []string{}); err != nil {
		t.Fatalf("Upsert empty: %v", err)
	}

	groups, err := Load(configDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group after no-op upsert, got %d", len(groups))
	}
}
