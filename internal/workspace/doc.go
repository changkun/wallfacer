// Package workspace manages workspace lifecycle, scoped store switching, and
// change subscriptions.
//
// A workspace is an owned, stably-identified host folder that tasks operate on.
// Its identity (ID) and storage handle (DataKey) are independent of the folder,
// so the workspace can be re-pointed without re-keying the scoped store,
// transcripts, or planning state. A record written when a workspace could span
// several folders loads dormant until the owner re-points it to one folder.
// The [Manager] coordinates runtime switching: it atomically swaps the
// underlying [store.Store] keyed by DataKey and notifies subscribers via
// channels. Workspaces are persisted as JSON (workspaces.json)
// in the config directory for session restore; [MigrateToWorkspaces] performs
// the one-time migration from the legacy path-keyed workspace-groups.json,
// adopting orphaned data directories as dormant workspaces.
//
// # Connected packages
//
// Depends on [envconfig] (env file path management), [prompts] (AGENTS.md key
// and path derivation), [store] (creates scoped store per workspace), [logger],
// and utilities [pkg/atomicfile] and [pkg/set].
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
