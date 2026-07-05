// Package runner orchestrates host-process AI agent execution for tasks.
//
// It manages the complete task lifecycle: worktree setup, launching the
// selected harness CLI as a host process via os/exec, agent communication,
// commit pipelines, title and oversight generation, and error recovery with
// failure classification. The [Runner] struct implements the [Interface] which
// exposes a minimal surface for test mocking. Background operations (title gen,
// oversight, sync) are tracked via a labeled WaitGroup for clean shutdown.
//
// # Connected packages
//
// Depends on [store] (task mutations and event sourcing), [workspace] (workspace
// paths and instructions), [envconfig] (agent environment), [harness] (per-CLI
// argv/event adapters), [constants] (timeouts and limits), [logger], [metrics],
// [prompts] (system prompt templates), and several internal/pkg utilities:
// [circuitbreaker] (launch protection), [cmdexec] (git and process commands),
// [keyedmu] (per-task locking), [trackedwg] (goroutine tracking).
// Consumed by [handler] (all task orchestration actions) and [cli] (server startup).
// Changes to the [store.Task] state machine, harness adapters, or prompt
// templates directly affect runner behavior.
//
// # Usage
//
//	r := runner.New(store, workspaceMgr, envFile, configDir, registry)
//	r.RunBackground(taskID, prompt, sessionID, false)
//	r.Commit(taskID, sessionID)
package runner
