---
title: Task.FlowID + legacy Kind→Flow resolver
status: archived
depends_on:
  - specs/local/agents-and-flows/flow-data-model.md
affects:
  - internal/store/models.go
  - internal/store/tasks_create_delete.go
  - internal/store/tasks_update.go
  - internal/handler/tasks.go
effort: small
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---


# Task.FlowID + legacy Kind→Flow resolver

## Goal

Add the `FlowID` field to `store.Task` so tasks can reference a
flow by slug. Accept a `flow` field on `POST /api/tasks`, default
it to `"implement"`, and resolve legacy `Kind`-only task records
on read so existing tasks continue to run without any data
migration. No execution path yet — the flow-engine task rewires
the runner next.

## What to do

1. `internal/store/models.go`:
   - Add `FlowID string` on `Task`, tagged
     `json:"flow_id,omitempty"`.
   - Add `(*Task).ResolvedFlowID(reg *flow.Registry) string` — if
     `FlowID != ""` return it; otherwise resolve via
     `reg.ResolveLegacyKind(t.Kind)` and return that flow's slug;
     otherwise return `"implement"`. Callers that need the full
     Flow struct call `reg.Get(slug)`.

2. `internal/store/tasks_create_delete.go`:
   - Extend `TaskCreateOptions` with `FlowID string`.
   - Persist it on creation. When empty, leave the field empty on
     the stored record so `ResolvedFlowID` can compute the right
     default per registry.

3. `internal/store/tasks_update.go`:
   - `UpdateTaskFlow(ctx, id, flowID string) error` — writer for
     post-create updates (e.g. the composer's "reflow" follow-up).

4. `internal/handler/tasks.go`:
   - `POST /api/tasks` accepts a `flow` field on the JSON body;
     relays it to `TaskCreateOptions.FlowID`.
   - When `flow: "brainstorm"`, the existing empty-prompt
     allowance continues to apply (check `FlowID == "brainstorm"`
     OR legacy `Kind == "idea-agent"`).
   - The response echoes `flow_id` alongside the existing fields.
   - Back-compat: `kind: "idea-agent"` continues to work; when
     both `kind` and `flow` are present, `flow` wins.

5. Tests:
   - `internal/store/tasks_test.go` additions:
     - `TestCreateTaskWithFlow_PersistsFlowID`.
     - `TestResolvedFlowID_EmptyFallsBackToImplement`.
     - `TestResolvedFlowID_IdeaAgentKindResolvesToBrainstorm`.
   - `internal/handler/tasks_test.go`:
     - `TestCreateTask_FlowBrainstormAllowsEmptyPrompt`.
     - `TestCreateTask_FlowFieldOverridesKind`.

## Boundaries

- Do NOT add `FlowSnapshot` to `Task` yet. The engine task adds it
  when it needs deep-copy isolation.
- Do NOT remove `TaskKind` or wire up the runner. The flow-engine
  + runner-flow-integration tasks handle execution.
- Do NOT update the composer UI. That's the
  `composer-flow-picker` task.
- Do NOT touch the routine engine's `RoutineSpawnKind`; routine
  migration is deferred.
