package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"latere.ai/x/wallfacer/internal/prompts"
)

// writeTaskDir creates a data/<key>/<uuid>/task.json fixture whose
// worktree_paths records the given source folders, simulating real task
// history under a data directory.
func writeTaskDir(t *testing.T, dataDir, key, uuid string, srcFolders []string) {
	t.Helper()
	wt := map[string]string{}
	for _, f := range srcFolders {
		wt[f] = filepath.Join("/worktrees", uuid, filepath.Base(f))
	}
	body, _ := json.Marshal(map[string]any{"worktree_paths": wt})
	dir := filepath.Join(dataDir, key, uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.json"), body, 0o644); err != nil {
		t.Fatalf("write task.json: %v", err)
	}
}

func findByDataKey(groups []Workspace, key string) (Workspace, bool) {
	for _, g := range groups {
		if g.DataKey == key {
			return g, true
		}
	}
	return Workspace{}, false
}

// TestMigrateToWorkspaces covers the whole migration: live groups map with a
// path-seeded DataKey (zero data movement), non-empty orphans are adopted as
// dormant workspaces with recovered folders, empty orphans are skipped, a
// backup is written, and a second run is a no-op.
func TestMigrateToWorkspaces(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()

	// A live group over dirA; its data dir is named by the path hash.
	dirA := t.TempDir()
	liveKey := prompts.WorkspaceDataKey([]string{dirA})
	writeTaskDir(t, dataDir, liveKey, "task-live", []string{dirA})
	legacy := `[{"name":"Live","workspaces":["` + dirA + `"]}]`
	if err := os.WriteFile(legacyGroupsFilePath(configDir), []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	// A non-empty orphan over dirB whose data dir matches no live group.
	dirB := t.TempDir()
	const orphanKey = "abc0000000000001"
	writeTaskDir(t, dataDir, orphanKey, "task-orphan", []string{dirB})

	// An empty orphan directory (no task history) — must be skipped.
	const emptyKey = "abc0000000000002"
	if err := os.MkdirAll(filepath.Join(dataDir, emptyKey), 0o755); err != nil {
		t.Fatalf("mkdir empty orphan: %v", err)
	}

	// Record the orphan's task.json mtime to assert it is not moved/rewritten.
	orphanTask := filepath.Join(dataDir, orphanKey, "task-orphan", "task.json")
	pre, err := os.Stat(orphanTask)
	if err != nil {
		t.Fatalf("stat orphan task: %v", err)
	}

	migrated, err := MigrateToWorkspaces(configDir, dataDir, "teststamp")
	if err != nil {
		t.Fatalf("MigrateToWorkspaces: %v", err)
	}
	if !migrated {
		t.Fatal("expected migration to run")
	}

	groups, err := LoadGroups(configDir)
	if err != nil {
		t.Fatalf("LoadGroups after migration: %v", err)
	}

	// Live group: present, non-dormant, DataKey == the existing path hash.
	live, ok := findByDataKey(groups, liveKey)
	if !ok {
		t.Fatalf("live workspace missing after migration; got %+v", groups)
	}
	if live.Dormant {
		t.Error("live workspace must not be dormant")
	}
	if live.ID == "" {
		t.Error("live workspace must get a stable id")
	}
	if live.Name != "Live" || len(live.Folders) != 1 || live.Folders[0] != dirA {
		t.Errorf("live workspace fields wrong: %+v", live)
	}

	// Orphan: adopted as dormant with recovered folders.
	orphan, ok := findByDataKey(groups, orphanKey)
	if !ok {
		t.Fatalf("orphan workspace not adopted; got %+v", groups)
	}
	if !orphan.Dormant {
		t.Error("adopted orphan must be dormant")
	}
	if len(orphan.Folders) != 1 || orphan.Folders[0] != dirB {
		t.Errorf("orphan folders not recovered from task.json: %+v", orphan.Folders)
	}

	// Empty orphan: never adopted.
	if _, ok := findByDataKey(groups, emptyKey); ok {
		t.Error("empty orphan directory must not be adopted")
	}

	// Zero data movement: the orphan's task.json is untouched in place.
	post, err := os.Stat(orphanTask)
	if err != nil {
		t.Fatalf("orphan task.json moved or removed: %v", err)
	}
	if !post.ModTime().Equal(pre.ModTime()) {
		t.Error("orphan task.json was rewritten; migration must not move data")
	}

	// Backup written.
	backupDir := filepath.Join(configDir, "migration-backup-workspaces-teststamp")
	if _, err := os.Stat(filepath.Join(backupDir, "workspace-groups.json")); err != nil {
		t.Errorf("legacy backup not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "manifest.json")); err != nil {
		t.Errorf("manifest not written: %v", err)
	}

	// Idempotent: a second run is a no-op and leaves identity stable.
	migrated2, err := MigrateToWorkspaces(configDir, dataDir, "teststamp2")
	if err != nil {
		t.Fatalf("second migration: %v", err)
	}
	if migrated2 {
		t.Error("second migration must be a no-op")
	}
	groups2, _ := LoadGroups(configDir)
	again, _ := findByDataKey(groups2, liveKey)
	if again.ID != live.ID {
		t.Errorf("workspace id changed across idempotent re-run: %q -> %q", live.ID, again.ID)
	}
}
