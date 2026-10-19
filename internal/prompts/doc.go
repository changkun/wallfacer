// Package prompts provides template-based rendering for all agent system
// prompts, and derives the storage keys that name a workspace's data directory.
//
// The prompt templates are embedded in the binary and rendered as shipped.
// Templates use Go text/template syntax with custom arithmetic and ratio
// functions.
//
// instructions.go holds the workspace storage keys: [WorkspaceDataKey] hashes a sorted folder set and is used only
// when migrating legacy workspace groups whose directories are already named by
// that hash, while [NewDataKey] mints a random key for every new workspace.
//
// # Connected packages
//
// Consumed by [workspace] (derives a workspace's data key on create, rename, and
// migration), [runner] (renders prompts for every agent invocation), [handler]
// (spec-mode and task-mode system prompts), and [store]. When adding a new
// prompt template, add the .tmpl file, its data type, and a rendering function.
//
// # Usage
//
//	rendered := prompts.Title(taskPrompt)
//	commit := prompts.CommitMessage(prompts.CommitData{Prompt: p, DiffStat: stat})
package prompts
