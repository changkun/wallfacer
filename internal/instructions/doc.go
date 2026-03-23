// Package instructions manages workspace-level AGENTS.md instruction files
// that are mounted into every task container.
//
// Each unique combination of workspace directories gets its own AGENTS.md file,
// identified by a SHA-256 fingerprint of the sorted workspace paths. On first run,
// the file is assembled from a built-in default template plus references to
// per-repo AGENTS.md or CLAUDE.md files found in the workspaces. Users can edit
// the file in the UI or regenerate it at any time.
//
// # Connected packages
//
// No internal dependencies (stdlib only). Consumed by [workspace] (derives the
// instructions file path during workspace switching), [handler] (API endpoints
// for reading, writing, and reinitializing AGENTS.md), and [runner] (mounts the
// file read-only into task containers). When changing the default template or
// key derivation, all containers will pick up the new content on next launch.
//
// # Usage
//
//	key := instructions.Key(workspaces)
//	path, err := instructions.Ensure(configDir, workspaces)
//	content := instructions.BuildContent(workspaces)
package instructions
