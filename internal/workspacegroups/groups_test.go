package workspacegroups

import "testing"

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
