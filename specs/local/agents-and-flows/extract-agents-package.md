---
title: Extract internal/agents package from runner
status: validated
depends_on: []
affects:
  - internal/agents/
  - internal/runner/agent.go
  - internal/runner/title.go
  - internal/runner/oversight.go
  - internal/runner/commit.go
  - internal/runner/refine.go
  - internal/runner/ideate.go
  - internal/runner/container.go
effort: medium
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Extract internal/agents package from runner

## Goal

Decouple agent descriptors (data) from the runner's execution machinery
(behavior) by lifting `AgentRole`, `MountMode`, and the seven built-in
role descriptors out of `internal/runner/` into a new `internal/agents/`
package. After this task the runner imports `agents`; role definitions
have one obvious home; future flow-engine and handler code can depend on
`agents` without dragging the full runner along. Pure refactor — zero
behavior change; every existing runner test stays green.

## What to do

1. Create `internal/agents/` with:
   - `agent.go` — the public types moved from `internal/runner/agent.go`:
     ```go
     package agents

     type MountMode int
     const (
         MountNone MountMode = iota
         MountReadOnly
         MountReadWrite
     )

     type Role struct {
         Activity    store.SandboxActivity
         Name        string
         Timeout     func(*store.Task) time.Duration
         MountMode   MountMode
         MountBoard  bool
         SingleTurn  bool
         ParseResult func(output *runner.AgentOutput) (any, error)
         Model       func(sb sandbox.Type) string
     }
     ```
     (Rename `AgentRole` → `agents.Role` so the call site reads
     `agents.Role{…}` instead of stuttering `agent.AgentRole{…}`.)
   - `doc.go` — package doc explaining the decoupling intent.
   - One file per role family with the descriptor values:
     - `headless.go` — `Title`, `Oversight`, `CommitMessage` plus their
       `ParseResult` helpers.
     - `inspector.go` — `Refinement`, `IdeaAgentEphemeral`.
     - `heavyweight.go` — `Implementation`, `Testing` (both carry a
       pass-through `ParseResult`).

2. Resolve the circular-import hazard: the descriptors' `ParseResult`
   field currently references `*agentOutput`, an unexported runner
   type. Export it as `runner.AgentOutput` (rename the struct from
   `agentOutput` → `AgentOutput`) so `agents` can reference it without
   a back-pointer. This is the only runner symbol the agents package
   needs; everything else (containers, circuit breaker, live-log) stays
   private to the runner.

3. Update `internal/runner/agent.go`:
   - Remove the `AgentRole`, `MountMode` type declarations (now in
     `agents`).
   - `runAgent`'s first argument becomes `role agents.Role`.
   - `runAgentOpts`, `agentResult`, `launchOne`, `buildInspectorSpec`,
     `accumulateAgentUsage`, `recordFallbackEvent`, the circuit-breaker
     interface — all stay in the runner; they're execution concerns.

4. Update each role file (`title.go`, `oversight.go`, `commit.go`,
   `refine.go`, `ideate.go`, `container.go`) to reference the
   descriptor by its exported package-qualified name: `roleTitle` →
   `agents.Title`, `roleOversight` → `agents.Oversight`, and so on.
   The role-specific wrapper logic (GenerateTitle, runOversightAgent,
   RunRefinement, runIdeationEphemeral, runContainer) stays in the
   runner.

5. Move the `ParseResult` helpers (`parseTitleResult`,
   `parseOversightAgentResult`, `parseCommitMessageResult`,
   `passthroughParse`) into the agents package so they're colocated
   with their descriptors. The helpers only reference
   `runner.AgentOutput`; no runner internals leak.

6. Update tests:
   - `internal/runner/agent_test.go` gets import updates — it now
     constructs `agents.Role` values in its test helpers.
   - `internal/runner/title_test.go`, `oversight_test.go`,
     `commit_test.go`, `refine_test.go`, `ideate_test.go` — no changes
     needed beyond whatever imports `go build` requires.
   - Add a trivial `internal/agents/agents_test.go` asserting that
     the built-in descriptor list matches the expected seven-role
     count and each has a non-empty `Name`/`Activity` (catches a
     silent regression where a descriptor loses a required field).

## Tests

- Existing full runner suite (867 tests) must pass unchanged.
- `internal/agents/agents_test.go`:
  - `TestBuiltinAgents_AllHaveRequiredFields` — iterate the exported
    `BuiltinAgents` slice, assert each has `Name != ""`,
    `Activity != ""`, `ParseResult != nil`.
  - `TestBuiltinAgents_SlugsAreUnique` — no two descriptors share a
    `Name`.

## Boundaries

- Do NOT introduce the `Flow` type, flow engine, or sidebar tab in
  this task. Every other agents-and-flows child spec depends on this
  one; keep the scope surgical.
- Do NOT rename `runAgent` or the runner-private helpers.
- Do NOT change the descriptor field set beyond removing the
  struct-name prefix `Agent` → `` (`AgentRole` → `Role`).
- Do NOT move the on-disk loader / watcher here — user-editable
  agents land in a follow-up task.
- Do NOT touch `internal/routine/` or the handler layer.
