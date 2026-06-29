// Package workspace manages workspace lifecycle, scoped store switching, and
// change subscriptions.
//
// A workspace is an owned, stably-identified set of host directories that tasks
// operate on. Its identity (ID) and storage handle (DataKey) are independent of
// its folder set, so folders can be edited without re-keying the scoped store,
// transcripts, or planning state. The [Manager] coordinates runtime switching:
// it atomically swaps the underlying [store.Store] keyed by DataKey and notifies
// subscribers via channels. Workspaces are persisted as JSON (workspaces.json)
// in the config directory for session restore; [MigrateToWorkspaces] performs
// the one-time migration from the legacy path-keyed workspace-groups.json,
// adopting orphaned data directories as dormant workspaces.
//
// # Connected packages
//
// Depends on [envconfig] (env file path management), [instructions] (AGENTS.md key
// and path derivation), [store] (creates scoped store per workspace), and utilities
// [pkg/atomicfile] and [pkg/set].
// Consumed by [handler] (workspace switching UI), [runner] (workspace paths for
// container mounts), and [cli] (server startup initialization).
// Changes to workspace key derivation or group persistence format affect session
// restore and instructions file mapping.
//
// # Usage
//
//	mgr, err := workspace.NewManager(configDir, dataDir, envFile, paths)
//	snap := mgr.Snapshot()
//	newSnap, err := mgr.Switch(newPaths)
//	id, ch := mgr.Subscribe()
//	defer mgr.Unsubscribe(id)
package workspace
