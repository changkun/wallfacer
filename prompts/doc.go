// Package prompts provides template-based rendering for all agent system prompts
// with optional per-user overrides.
//
// Seven built-in prompt templates (title, commit, refinement, oversight, test,
// ideation, conflict resolution) are embedded in the binary. The [Manager] checks
// for user overrides in ~/.wallfacer/prompts/ before falling back to the embedded
// defaults. Templates use Go text/template syntax with custom arithmetic and ratio
// functions. This allows users to customize agent behavior without modifying the
// wallfacer binary.
//
// # Connected packages
//
// Depends on [changkun.de/x/wallfacer/internal/logger] for logging and
// [changkun.de/x/wallfacer/internal/pkg/atomicfile] for writing override files.
// Consumed by [runner] (renders prompts for every agent invocation: title gen,
// commit messages, oversight, refinement, ideation, test verification), [handler]
// (system prompt CRUD API), and [cli] (manager initialization).
// When adding a new prompt template, add the .tmpl file, register it in the
// Manager, and add the corresponding Render method.
//
// # Usage
//
//	mgr := prompts.NewManager(userDir)
//	rendered := mgr.RenderTitle(titleData)
//	content, hasOverride, err := mgr.Content("title")
package prompts
