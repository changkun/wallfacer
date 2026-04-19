---
title: Runner delegates to flow engine for task execution
status: archived
depends_on:
  - specs/local/agents-and-flows/flow-engine.md
  - specs/local/agents-and-flows/composer-flow-picker.md
affects:
  - internal/runner/execute.go
  - internal/runner/runner.go
  - internal/runner/ideate.go
  - internal/runner/interface.go
  - internal/handler/handler.go
effort: large
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---


# Runner delegates to flow engine for task execution

## Goal

Wire the flow engine into the runner's task-execution entry so each
task's registered flow drives which agents run and in what order.
Preserves every existing observable behaviour — the turn loop
(session recovery, auto-continue, verdict inference, failure
classification) still lives in `execute.go`; the engine only decides
which descriptor to launch *for a given step*. Legacy tasks without
`FlowID` resolve via the `ResolvedFlowID` helper shipped in the
task-flow-field task so no data migration is needed.

## What to do

1. Add a `*flow.Engine` + `*flow.Registry` to `Runner`:
   - `runner.go`: new fields, wired in `NewRunner`.
   - `flow.NewEngine(r, agents.Default)` is constructed once; the
     runner implements the `AgentLauncher` interface defined in
     the flow-engine task.

2. `internal/runner/execute.go`:
   - At the top of `Run(taskID, prompt, sessionID, resumedFromWaiting)`,
     resolve `task.ResolvedFlowID(r.flows)` → `flow.Flow`.
   - If `flow.Slug == "implement"`, retain the existing execute
     path (the turn loop already implements the canonical
     refine → impl → test → commit → title/oversight sequence).
     The engine is bypassed for v1 because the turn loop owns
     multi-turn semantics that the linear engine doesn't express
     yet.
   - If `flow.Slug == "brainstorm"` (or any flow whose single
     step is `ideate`), delegate to `runIdeationTask` as today.
   - For any other flow, call `r.flowEngine.Execute(ctx, taskID,
     flow, task)`; on success, set status to done; on failure,
     reuse the existing `classifyFailure` + `tryAutoRetry`
     plumbing.
   - The above dispatch lives in a new small helper
     `dispatchFlow(ctx, task, prompt) error` called from `Run`.

3. `internal/runner/interface.go`:
   - Export the `RunAgent` wrapper method on `*Runner` (thin
     adapter around the unexported `runAgent`). The flow engine
     consumes this to launch each step.

4. Preserve the runner's public API surface:
   - `Run`, `RunBackground`, `GenerateTitle`, `GenerateOversight`,
     `RunRefinement`, `RunIdeation`, `GenerateCommitMessage`, etc.,
     all keep their signatures and call sites.
   - `MockRunner` in `mock.go` gains a no-op `RunAgent` to satisfy
     the interface.

5. `internal/handler/handler.go`:
   - No change to route shapes; the `POST /api/tasks` path already
     accepts the `flow` field (task-flow-field task). The runner
     reads it at Run time.

## Tests

- `internal/runner/execute_test.go`:
  - `TestRun_DispatchesToImplementFlowByDefault` — a task with no
    `FlowID` and `Kind == ""` goes through the existing implement
    path; observable output unchanged from today.
  - `TestRun_DispatchesBrainstormFlowToIdeationTask` — a task
    with `FlowID=brainstorm` calls `runIdeationTask`.
  - `TestRun_DispatchesCustomFlowThroughEngine` — seeds a
    `refine-only` flow, asserts only one agent launch (the
    refine descriptor) happens per the flow's single step.
- All 867 existing runner tests must stay green.

## Boundaries

- Do NOT rewrite the implement turn loop onto the engine. The
  linear engine doesn't express multi-turn semantics, and
  retrofitting that is a separate follow-up — the engine stays
  the authoritative driver for non-implement flows only.
- Do NOT remove `runIdeationTask`'s direct invocation of
  `RunIdeation`. The brainstorm flow delegates to it for
  backwards compatibility — the engine could drive it through a
  single-step Execute, but keeping the delegation means the
  agent's output-parse + create-backlog-tasks post-processing
  stays in one place for v1.
- Do NOT move `classifyFailure` / `tryAutoRetry` out of
  `execute.go`.
- Do NOT touch the routine engine integration.
