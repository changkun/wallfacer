// Package prompts provides template-based rendering for all agent system prompts
// with optional per-user overrides, and manages workspace-level AGENTS.md
// instruction files that are mounted into every task container.
//
// Eight built-in prompt templates (title, commit, refinement, oversight, test,
// ideation, conflict resolution, instructions) are embedded in the binary. The
// [Manager] checks for user overrides in ~/.wallfacer/prompts/ before falling
// back to the embedded defaults. Templates use Go text/template syntax with
// custom arithmetic and ratio functions. This allows users to customize agent
// behavior without modifying the wallfacer binary.
//
// The instructions template generates the workspace AGENTS.md content. Each
// unique combination of workspace directories gets its own AGENTS.md file,
// identified by a SHA-256 fingerprint of the sorted workspace paths. On first
// run, the file is assembled from the instructions template plus references to
// per-repo AGENTS.md or CLAUDE.md files found in the workspaces. Users can
// edit the file in the UI or regenerate it at any time.
//
// # Connected packages
//
// Depends on [changkun.de/x/wallfacer/internal/logger] for logging and
// [changkun.de/x/wallfacer/internal/pkg/atomicfile] for writing override files.
// Consumed by [workspace] (derives the instructions file path during workspace
// switching), [runner] (renders prompts for every agent invocation and mounts
// the instructions file read-only into task containers), [handler] (system prompt
// CRUD API and instructions endpoints), and [cli] (manager initialization).
// When adding a new prompt template, add the .tmpl file, register it in the
// Manager, and add the corresponding Render method.
//
// # Usage
//
//	mgr := prompts.NewManager(userDir)
//	rendered := mgr.RenderTitle(titleData)
//	content, hasOverride, err := mgr.Content("title")
package prompts
