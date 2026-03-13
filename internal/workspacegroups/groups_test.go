package workspacegroups

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestUpsertAndLoad(t *testing.T) {
	configDir := t.TempDir()
	wsA := filepath.Join(configDir, "a")
	wsB := filepath.Join(configDir, "b")

	if err := Upsert(configDir, []string{wsB, wsA, wsA}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := Upsert(configDir, []string{wsA, wsB}); err != nil {
		t.Fatalf("upsert duplicate: %v", err)
	}

	groups, err := Load(configDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []Group{{Workspaces: []string{wsA, wsB}}}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("groups mismatch\nwant: %#v\ngot:  %#v", want, groups)
	}
}
