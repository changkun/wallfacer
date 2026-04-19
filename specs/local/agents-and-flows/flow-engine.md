---
title: Flow engine — sequencer driving agents via runAgent
status: complete
depends_on:
  - specs/local/agents-and-flows/flow-data-model.md
  - specs/local/agents-and-flows/task-flow-field.md
affects:
  - internal/flow/engine.go
  - internal/flow/engine_test.go
  - internal/runner/interface.go
  - internal/store/models.go
effort: large
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Flow engine — sequencer driving agents via runAgent

## Goal

Implement the flow engine that walks a Flow's step chain, dispatches
each step through the runner's `runAgent`, and threads parsed results
between steps. Linear execution with parallel-sibling fan-out; no DAG
or conditional edges in v1 (deferred). The engine is agnostic to the
runner's container internals — it talks to a narrow `AgentRunner`
interface so tests can swap a fake.

## What to do

1. Add `FlowSnapshot *flow.Flow` to `store.Task` — stored as a deep
   copy when execution begins so in-flight tasks are immune to
   concurrent flow edits.

2. `internal/runner/interface.go`: extend the exported runner
   interface with a narrow launcher the engine consumes:
   ```go
   type AgentLauncher interface {
       RunAgent(ctx context.Context, role agents.Role,
                task *store.Task, prompt string,
                opts RunAgentOpts) (*AgentResult, error)
   }
   ```
   Promote the currently-unexported `runAgentOpts` / `agentResult`
   types to exported names, or re-export minimal field sets —
   `RunAgentOpts`, `AgentResult`. The runner satisfies this interface
   via a method wrapper that calls the existing unexported
   `runAgent`.

3. `internal/flow/engine.go`:
   ```go
   type Engine struct {
       runner  AgentLauncher
       agents  *agents.Registry
   }

   func NewEngine(r AgentLauncher, a *agents.Registry) *Engine

   // Execute walks the flow's steps linearly. Each step's prompt
   // defaults to the task.Prompt; a step's InputFrom points at a
   // prior step's ParseResult, which the engine stringifies and
   // feeds as the next prompt. Parallel-sibling groups run
   // concurrently via errgroup; the group's first error cancels
   // the rest.
   func (e *Engine) Execute(ctx context.Context, taskID uuid.UUID,
                            f flow.Flow, task *store.Task) error
   ```

4. Prompt threading:
   - Root step (no `InputFrom`) receives `task.Prompt`.
   - A step with `InputFrom: "<slug>"` receives the string-cast of
     the named step's `ParseResult`. If `ParseResult` isn't a
     string, the engine calls `fmt.Sprint(parsed)` — unusual types
     are rare in built-ins.
   - Parallel-sibling peers each receive the same input as the
     root of their group.

5. Error handling:
   - Mandatory step failure → return the error; caller decides
     whether to mark the task failed (matches today's behaviour
     where execute.go does that).
   - Optional step failure → log a warning, skip, continue to the
     next step. Optional applies only when `Step.Optional == true`.
   - Context cancellation propagates to every in-flight launch.

6. Tests (`internal/flow/engine_test.go`) using a fake
   `AgentLauncher` that records calls and returns stubbed
   `AgentResult`s:
   - `TestExecute_LinearChain` — three sequential steps; order of
     RunAgent calls matches Steps order.
   - `TestExecute_InputFromThreading` — step B's prompt equals
     step A's parsed result.
   - `TestExecute_ParallelSiblings_RunConcurrently` — three
     siblings announce their start via a counting channel; engine
     waits for all before returning.
   - `TestExecute_MandatoryStepError_StopsChain`.
   - `TestExecute_OptionalStepError_Continues`.
   - `TestExecute_CancellationAbortsInFlight`.
   - `TestExecute_FlowSnapshotIsStable` — editing the registry's
     Flow mid-execution doesn't affect the running engine (engine
     operates on its `f flow.Flow` argument copy, not a pointer).

## Boundaries

- Do NOT rewire the runner's `runContainer` / turn loop to use the
  engine yet — that's the `runner-flow-integration` task.
- Do NOT add user-configurable flow loading from YAML. Engine
  consumes flows from `*flow.Registry` which the caller prepares.
- Do NOT introduce DAG edges or conditional steps. Linear +
  parallel-sibling only.
- Do NOT emit UI-specific progress events. The engine's caller
  (the runner, later) owns span events.
