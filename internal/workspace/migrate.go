package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"latere.ai/x/wallfacer/internal/pkg/atomicfile"
	"latere.ai/x/wallfacer/internal/prompts"
)

// dataKeyRe matches the 16-hex-character directory names used for scoped data.
var dataKeyRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// MigrateToWorkspaces performs the one-time migration from the legacy
// path-keyed model (workspace-groups.json + data/<hash>/ directories) to the
// stable-identity model (workspaces.json). It is idempotent: once
// workspaces.json exists it is a no-op. Returns whether a migration was written.
//
// Live groups become workspaces with DataKey = WorkspaceDataKey(folders) — the
// very hash that already names their data directory — so no data moves. Each
// data/<hash>/ directory that holds task history but matches no live group is
// adopted as a dormant workspace (folders best-effort recovered from the
// contained task.json worktree_paths), so stranded history stays reachable.
// Empty orphan directories are left untouched and unlisted.
//
// stamp is a filesystem-safe token (e.g. "20060102-150405") used to name the
// backup directory; the caller supplies it so the name is deterministic.
func MigrateToWorkspaces(configDir, dataDir, stamp string) (bool, error) {
	if configDir == "" {
		return false, nil
	}
	if _, err := os.Stat(workspacesFilePath(configDir)); err == nil {
		return false, nil // already migrated
	}

	legacyExists := false
	if _, err := os.Stat(legacyGroupsFilePath(configDir)); err == nil {
		legacyExists = true
	}

	// LoadGroups falls back to workspace-groups.json here, since workspaces.json
	// is absent. Records are already normalized.
	groups, err := LoadGroups(configDir)
	if err != nil {
		return false, fmt.Errorf("load legacy groups: %w", err)
	}

	liveKeys := make(map[string]bool, len(groups))
	for i := range groups {
		if groups[i].ID == "" {
			groups[i].ID = newWorkspaceID()
		}
		if groups[i].DataKey == "" {
			groups[i].DataKey = prompts.WorkspaceDataKey(groups[i].Folders)
		}
		if groups[i].CreatedAt == "" {
			groups[i].CreatedAt = stamp
		}
		groups[i].UpdatedAt = stamp
		liveKeys[groups[i].DataKey] = true
	}

	adopted := adoptOrphans(dataDir, liveKeys, stamp)
	groups = append(groups, adopted...)

	// Fresh install with nothing to record: leave workspaces.json absent so
	// first-run stays clean; migration re-runs harmlessly next time.
	if len(groups) == 0 {
		return false, nil
	}

	if err := writeMigrationBackup(configDir, stamp, legacyExists, adopted); err != nil {
		return false, fmt.Errorf("migration backup: %w", err)
	}
	if err := SaveGroups(configDir, groups); err != nil {
		return false, fmt.Errorf("write workspaces.json: %w", err)
	}
	return true, nil
}

// adoptOrphans returns dormant workspaces for each data/<hash>/ directory that
// holds task history but is not among liveKeys. Empty directories are skipped.
func adoptOrphans(dataDir string, liveKeys map[string]bool, stamp string) []Workspace {
	if dataDir == "" {
		return nil
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil
	}
	var out []Workspace
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !dataKeyRe.MatchString(name) || liveKeys[name] {
			continue
		}
		dir := filepath.Join(dataDir, name)
		if !dataDirHasTaskHistory(dir) {
			continue
		}
		out = append(out, Workspace{
			ID:        newWorkspaceID(),
			Name:      "Recovered " + name[:6],
			Folders:   recoverFolders(dir),
			DataKey:   name,
			Dormant:   true,
			CreatedAt: stamp,
			UpdatedAt: stamp,
		})
	}
	return out
}

// dataDirHasTaskHistory reports whether dir contains at least one task
// subdirectory (a child directory holding a task.json).
func dataDirHasTaskHistory(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "task.json")); err == nil {
			return true
		}
	}
	return false
}

// recoverFolders best-effort reconstructs a workspace's folder set from the
// task history in dir: each task.json records its source folders as the keys of
// worktree_paths. Only paths that still exist as directories are returned,
// normalized. An empty result is valid (the workspace stays dormant with no
// folders until the owner re-points it).
func recoverFolders(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name(), "task.json"))
		if err != nil {
			continue
		}
		var t struct {
			WorktreePaths map[string]string `json:"worktree_paths"`
		}
		if json.Unmarshal(raw, &t) != nil {
			continue
		}
		for src := range t.WorktreePaths {
			if info, err := os.Stat(src); err == nil && info.IsDir() {
				seen[src] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return normalizeGroupPaths(out)
}

// writeMigrationBackup snapshots the legacy file and records what was adopted,
// under configDir/migration-backup-workspaces-<stamp>/, before workspaces.json
// is written. The migration never deletes data, but the backup makes the
// pre-migration state trivially recoverable.
func writeMigrationBackup(configDir, stamp string, legacyExists bool, adopted []Workspace) error {
	backupDir := filepath.Join(configDir, "migration-backup-workspaces-"+sanitizeStamp(stamp))
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return err
	}
	if legacyExists {
		if raw, err := os.ReadFile(legacyGroupsFilePath(configDir)); err == nil {
			if err := os.WriteFile(filepath.Join(backupDir, "workspace-groups.json"), raw, 0o644); err != nil {
				return err
			}
		}
	}
	adoptedKeys := make([]string, 0, len(adopted))
	for _, w := range adopted {
		adoptedKeys = append(adoptedKeys, w.DataKey)
	}
	manifest := map[string]any{
		"stamp":           stamp,
		"adopted_orphans": adoptedKeys,
	}
	return atomicfile.WriteJSON(filepath.Join(backupDir, "manifest.json"), manifest, 0o644)
}

// sanitizeStamp keeps a stamp safe as a path component (RFC3339 contains colons,
// which are problematic on some filesystems).
func sanitizeStamp(stamp string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, stamp)
}
