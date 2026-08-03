// Package prompts provides template-based rendering for all agent system
// prompts, with optional per-user overrides, and derives the storage keys that
// name a workspace's data directory.
//
// The built-in prompt templates are embedded in the binary; knownNames is the
// authoritative list. The [Manager] checks for user overrides in
// ~/.wallfacer/prompts/ before falling back to the embedded defaults. Templates
// use Go text/template syntax with custom arithmetic and ratio functions. This
// allows users to customize agent behavior without modifying the wallfacer
// binary.
//
// instructions.go holds the two per-repo instructions filenames agents read
// ([CodexInstructionsFilename], [ClaudeInstructionsFilename]) and the workspace
// storage keys: [WorkspaceDataKey] hashes a sorted folder set and is used only
// when migrating legacy workspace groups whose directories are already named by
// that hash, while [NewDataKey] mints a random key for every new workspace.
//
// # Connected packages
//
// Depends on [latere.ai/x/wallfacer/internal/logger] for logging and
// [latere.ai/x/wallfacer/internal/pkg/atomicfile] for writing override files.
// Consumed by [workspace] (derives a workspace's data key on create, rename, and
// migration), [runner] (renders prompts for every agent invocation), [handler]
// (system prompt CRUD API), [store], and [cli] (manager initialization). When
// adding a new prompt template, add the .tmpl file, register it in knownNames,
// and add the corresponding Manager method.
//
// # Usage
//
//	mgr := prompts.NewManager(userDir)
//	rendered := mgr.Title(taskPrompt)
//	content, hasOverride, err := mgr.Content("title")
package prompts
