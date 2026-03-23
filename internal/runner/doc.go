// Package runner orchestrates ephemeral sandbox containers for AI agent task
// execution.
//
// It manages the complete task lifecycle: worktree setup, container launching via
// os/exec (podman or docker), agent communication, commit pipelines, title and
// oversight generation, prompt refinement, brainstorm ideation, and error recovery
// with failure classification. The [Runner] struct implements the [Interface] which
// exposes a minimal surface for test mocking. Background operations (title gen,
// oversight, sync) are tracked via a labeled WaitGroup for clean shutdown.
//
// # Connected packages
//
// Depends on [store] (task mutations and event sourcing), [workspace] (workspace
// paths and instructions), [envconfig] (container environment), [constants] (timeouts
// and limits), [logger], [metrics], [prompts] (system prompt templates), and several
// internal/pkg utilities: [circuitbreaker] (container launch protection), [cmdexec]
// (git and container commands), [keyedmu] (per-task locking), [trackedwg] (goroutine
// tracking).
// Consumed by [handler] (all task orchestration actions) and [cli] (server startup).
// Changes to [store.Task] state machine, container image layout, or prompt templates
// directly affect runner behavior.
//
// # Usage
//
//	r := runner.New(store, workspaceMgr, envFile, configDir, registry)
//	r.RunBackground(taskID, prompt, sessionID, false)
//	r.Commit(taskID, sessionID)
//	containers, _ := r.ListContainers()
package runner
