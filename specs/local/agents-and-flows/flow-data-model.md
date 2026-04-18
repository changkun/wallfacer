---
title: internal/flow package with data model and seeded built-ins
status: validated
depends_on:
  - specs/local/agents-and-flows/extract-agents-package.md
affects:
  - internal/flow/
effort: medium
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# internal/flow package with data model and seeded built-ins

## Goal

Introduce the Flow primitive as a new self-contained package:
`internal/flow/` owns the `Flow` + `Step` types, the embedded
built-in flows, and the loader/registry surface that later tasks
(handler API, flow engine, composer UI) consume. Pure data + read
access in this task — execution wiring, user-authored flows from
disk, and mutation endpoints land in sibling tasks.

## What to do

1. `internal/flow/flow.go`:
   ```go
   package flow

   type Flow struct {
       Slug        string
       Name        string
       Description string
       // Steps runs linearly in declared order. v1 scope is
       // linear-only; a Graph field replaces Steps when DAG
       // flows land as a follow-up.
       Steps       []Step
       // SpawnKind preserves the legacy TaskKind that tasks of
       // this flow run as: "" for normal tasks, "idea-agent" for
       // brainstorm. Kept on the flow (not the agent) because a
       // flow's first step drives the task's execution mode.
       SpawnKind   store.TaskKind
       Builtin     bool
   }

   type Step struct {
       // AgentSlug references agents.Role.Name from
       // internal/agents. Resolution happens at engine execute
       // time so flows don't hold direct agent pointers.
       AgentSlug string
       // Optional steps can be skipped by the composer (e.g.
       // refine on implement). The engine treats skipped steps
       // as no-ops.
       Optional bool
       // InputFrom names a previous step whose ParseResult feeds
       // this step's prompt. Empty → use the task's prompt.
       InputFrom string
       // RunInParallelWith names sibling step slugs that execute
       // concurrently with this one. Parallel peers' engines
       // share an errgroup wait.
       RunInParallelWith []string
   }
   ```

2. `internal/flow/builtins.go`: the four seeded flows —
   `implement`, `brainstorm`, `refine-only`, `test-only`. Each
   references agents by their `agents.Role.Name` so slug-only
   identity is the contract between packages.
   ```go
   var builtins = []Flow{
       {Slug: "implement", Name: "Implement", Description: "...",
        Steps: []Step{
            {AgentSlug: "refine", Optional: true},
            {AgentSlug: "impl"},
            {AgentSlug: "test"},
            {AgentSlug: "commit-msg", RunInParallelWith: []string{"title", "oversight"}},
            {AgentSlug: "title", RunInParallelWith: []string{"commit-msg", "oversight"}},
            {AgentSlug: "oversight", RunInParallelWith: []string{"commit-msg", "title"}},
        }},
       {Slug: "brainstorm", Name: "Brainstorm", SpawnKind: store.TaskKindIdeaAgent,
        Steps: []Step{{AgentSlug: "ideate"}}},
       {Slug: "refine-only", Name: "Refine only",
        Steps: []Step{{AgentSlug: "refine"}}},
       {Slug: "test-only", Name: "Test only",
        Steps: []Step{{AgentSlug: "test"}}},
   }
   ```

3. `internal/flow/registry.go`:
   - `Registry` holds the merged set of built-ins + (future)
     user-defined flows.
   - `func NewBuiltinRegistry() *Registry` returns the embedded
     set with `Builtin: true`.
   - `Registry.Get(slug string) (Flow, bool)`.
   - `Registry.List() []Flow` — returns a deep copy so callers
     can't mutate registry state.
   - `Registry.ResolveLegacyKind(store.TaskKind) (Flow, bool)` —
     maps `""` → `implement`, `idea-agent` → `brainstorm`,
     `planning` → `planning` (future; for now returns false so
     task tests that still create `Kind="planning"` keep working
     untouched).

4. `internal/flow/doc.go`: package doc explaining that a Flow is
   a user-facing composition of agents and that `agents` is the
   authoritative source of agent capabilities.

5. No handler / UI / runner wiring in this task.

## Tests

- `internal/flow/flow_test.go`:
  - `TestBuiltinRegistry_HasFourFlows` — asserts slugs exactly
    `{implement, brainstorm, refine-only, test-only}`.
  - `TestBuiltinRegistry_ImplementReferencesRealAgents` — every
    step's AgentSlug exists in `agents.BuiltinAgents` (cross-
    package test).
  - `TestRegistry_ResolveLegacyKind_MapsEmptyToImplement`.
  - `TestRegistry_ResolveLegacyKind_MapsIdeaAgentToBrainstorm`.
  - `TestRegistry_ListReturnsDeepCopy` — mutating the returned
    slice does not affect subsequent calls.

## Boundaries

- Do NOT implement the flow engine (sequencer). That's the
  sibling `flow-engine` task.
- Do NOT add HTTP endpoints, sidebar tabs, or composer wiring.
- Do NOT load flows from `~/.wallfacer/flows/*.yaml`. Built-in
  only; user-authored flows land in a later task.
- Do NOT change the task model (`Task.FlowID`, `FlowSnapshot`) —
  that's the `task-flow-field` task.
- Do NOT migrate the routine engine (`RoutineSpawnKind` →
  `RoutineSpawnFlow`) — that's deferred.
