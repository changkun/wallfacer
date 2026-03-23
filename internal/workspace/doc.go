// Package workspace manages workspace lifecycle, scoped store switching, and
// change subscriptions.
//
// A workspace is a set of host directories that tasks operate on. The [Manager]
// coordinates runtime workspace switching: when the user selects different
// workspaces, it atomically swaps the underlying [store.Store], derives the
// instructions file path, and notifies subscribers via channels. Workspace groups
// are persisted as JSON in the config directory for session restore. Each unique
// workspace combination is identified by a deterministic key for scoped data
// isolation.
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
