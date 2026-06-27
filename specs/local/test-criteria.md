---
title: Task Test Criteria for Post-Run Verification
status: complete
depends_on: []
affects:
  - internal/store/models.go
  - internal/store/tasks_create_delete.go
  - internal/store/tasks_update.go
  - internal/handler/tasks.go
  - internal/handler/execute.go
  - internal/handler/tasks_autopilot.go
  - frontend/src/api/types.ts
  - frontend/src/stores/tasks.ts
  - frontend/src/components/TaskComposer.vue
  - frontend/src/components/TaskDetail.vue
effort: medium
created: 2026-06-14
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# Task Test Criteria for Post-Run Verification

Supersedes the archived [[validation-barrier]] (specs/local/validation-barrier.md), which was written against the deleted vanilla-JS frontend and the removed `Goal` / refine subsystem. This spec is rewritten against the current Task data model and Vue frontend.

## Problem

The test verification agent runs after the implementation phase to check that a task's changes meet its requirements. Today it can receive free-form acceptance criteria, but only as a per-request parameter. `POST /api/tasks/{id}/test` accepts a `criteria` body field (`internal/handler/execute.go:621` `TestTask`), and the prompt builder already threads it into the template. There is no way to attach criteria to a task ahead of time.

This breaks the automated path. The auto-tester (`internal/handler/tasks_autopilot.go:699` `tryAutoTest`) builds its prompt with an empty criteria string (`tasks_autopilot.go:767`: `buildTestPrompt(t.Prompt, "", implResult, diff)`), so autopilot test runs always verify against a blank criteria block. A user who wants a task verified a specific way (run a particular command, check certain endpoints, confirm a migration is reversible) can only supply that when manually clicking Test, which the unattended pipeline never does.

## Goal

Persist a single user-defined, free-form `Criteria` string on the task, settable at creation and editable while in backlog. Feed it into the existing test-prompt path so both the manual test trigger and the auto-tester pick it up. The criteria are interpreted by the test agent, not parsed or enforced as a hard blocking gate.

## Current State (cited)

- `internal/store/models.go:261` `Task` struct holds `Prompt` (`:265`) and the other persisted fields. There is no criteria field. (The old `Goal` field this spec's predecessor mirrored was removed.)
- `internal/store/tasks_create_delete.go:30` `TaskCreateOptions` and `:70` `CreateTaskWithOptions` build the task on create; no criteria field flows through.
- `internal/store/tasks_update.go:310` `UpdateTaskBacklog` edits prompt/timeout/budget for backlog tasks via a positional-parameter signature.
- `internal/handler/tasks.go`: create handler request struct (`:176`) and opts (`:225`); batch opts (`batchTaskInput` struct at `:265`, `batchCreateRequest` at `:279`); PATCH request struct (`:595`); backlog-edit gate condition (`:677`) plus its `UpdateTaskBacklog` call (`:694`).
- `internal/handler/execute.go:682` `TestTask` resolves criteria from `req.Criteria` only (`:725` `buildTestPrompt(task.Prompt, req.Criteria, ...)`); no fallback to any persisted field.
- `internal/prompts/prompts.go:425` `TestData` struct / `:427` `Criteria string` field, and `internal/prompts/test.tmpl:6` guard the `## Acceptance Criteria` block with `{{if .Criteria}}`. These already render criteria correctly; no change needed.
- Frontend: `frontend/src/components/TaskComposer.vue:227` `sharedOpts` (create payload); `frontend/src/stores/tasks.ts:118` `createTask` / `:143` `batchCreateTasks` bodies, `:167` generic `patchTask` passthrough; `frontend/src/components/TaskDetail.vue:599` `testTask()` (per-run criteria dialog) and `:664` `saveBacklogEdit` (the backlog edit PATCH save).
- `frontend/src/api/types.ts:22` `Task` interface has `prompt` but no criteria field.

The gap: criteria exist per-request but are never persisted, so the auto-tester cannot use them.

## Design

### Data model (one field, no companion flag)

Add a single field to the `Task` struct in `internal/store/models.go`, near `Prompt`:

```go
// Criteria is user-defined free-form acceptance criteria for the test
// verification agent. When non-empty it renders into the test prompt's
// "Acceptance Criteria" section. Interpreted by the test agent, not a
// hard gate.
Criteria string `json:"criteria,omitempty"`
```

One field only. The archived spec carried a ` ...ManuallySet` companion to support refinement auto-population; the refine subsystem is gone, so there is no auto-source to distinguish from manual entry. `omitempty` lets existing `task.json` files deserialize cleanly with an empty string, so no migration is needed.

### Store layer

- `TaskCreateOptions`: add `Criteria string`. `CreateTaskWithOptions` sets `task.Criteria = opts.Criteria`.
- `UpdateTaskBacklog`: add a `criteria *string` parameter and persist it when non-nil. This is a positional signature, so update both the signature and its single caller at `internal/handler/tasks.go:694`.

Search indexing of `Criteria` (alongside `Prompt`/`Tags` in the index entry builder) is optional and out of scope here; note it as a possible later refinement.

### API

No new routes. The existing `POST /api/tasks`, `POST /api/tasks/batch`, `PATCH /api/tasks/{id}`, and `POST /api/tasks/{id}/test` carry the field.

- `POST /api/tasks` (`internal/handler/tasks.go` create handler): add `Criteria string \`json:"criteria"\`` to the request struct; pass into `TaskCreateOptions`.
- `POST /api/tasks/batch`: add the same per-task field; thread into each batch opts entry.
- `PATCH /api/tasks/{id}`: add `Criteria *string \`json:"criteria"\`` to the patch struct; include it in the backlog-edit gate (`:677`) and pass to `UpdateTaskBacklog`. Writable only in `backlog` status, same constraint as `prompt`.
- `POST /api/tasks/{id}/test`: unchanged signature; resolution changes below.

### Resolution order (the design spine)

Two call sites build the test prompt. The persisted field becomes the fallback:

- Manual / per-run, `internal/handler/execute.go:725` (currently `buildTestPrompt(task.Prompt, req.Criteria, ...)`):

  ```go
  criteria := strings.TrimSpace(req.Criteria)
  if criteria == "" {
      criteria = task.Criteria
  }
  testPrompt := buildTestPrompt(task.Prompt, criteria, implResult, diff)
  ```

- Auto-tester, `internal/handler/tasks_autopilot.go:771` (currently `buildTestPrompt(t.Prompt, "", ...)`): replace the empty string with the persisted value:

  ```go
  testPrompt := buildTestPrompt(t.Prompt, t.Criteria, implResult, diff)
  ```

This split is the whole point: persisted `task.Criteria` is upfront intent consumed by the unattended auto-test path, while the `testTask()` dialog supplies a late per-run override that wins when present. The `req.Criteria > task.Criteria > empty` order encodes exactly that. `test.tmpl` already drops the section when the resolved string is empty, preserving today's behavior for tasks without criteria.

### Frontend

- `frontend/src/api/types.ts`: add `criteria?: string` to the `Task` interface.
- `frontend/src/stores/tasks.ts`: include `criteria` in the `createTask` and `batchCreateTasks` request bodies when set. `patchTask` is a generic passthrough, so PATCH needs no store change.
- `frontend/src/components/TaskComposer.vue`: add a criteria input (textarea) to the create form, surfaced under the advanced/collapsible area so it does not clutter simple task creation. Include its value in `sharedOpts`. Placeholder: describe what the test agent should verify; empty means the agent decides.
- `frontend/src/components/TaskDetail.vue`: in the backlog edit surface (the `editPrompt` PATCH save at `:604-622`), add criteria editing that sends `criteria` in the patch. Optionally prefill the `testTask()` dialog (`:557`) from `task.criteria` so a manual test run defaults to the persisted value while still allowing an override. For non-backlog tasks, show the criteria read-only and omit the section when empty.

## Phasing / Acceptance Criteria

Phase 1 - model and store. Add `Task.Criteria`, thread through `TaskCreateOptions` / `CreateTaskWithOptions` and `UpdateTaskBacklog`. Tests: create with criteria persists it; create without leaves it empty; PATCH a backlog task sets it; PATCH a non-backlog task is rejected.

Phase 2 - handlers and resolution. Wire create / batch / PATCH request fields; apply the resolution order in `TestTask`; pass `t.Criteria` in `tryAutoTest`. Tests: `req.Criteria` wins over `task.Criteria`; empty body falls back to `task.Criteria`; both empty yields no criteria section; `tryAutoTest` builds a prompt containing the persisted criteria.

Phase 3 - frontend. Composer input, store payloads, detail edit/read surfaces, `types.ts` field. Acceptance: a task created with criteria in the composer shows them in detail; editing in backlog persists via PATCH; an auto-test run on that task renders the Acceptance Criteria block.

## Outcome (2026-06-27)

Implemented across three commits, all phases done.

- **Phase 1 (model + store):** `Task.Criteria` added (omitempty, no migration);
  threaded through `TaskCreateOptions` / `CreateTaskWithOptions`.
- **Phase 2 (handlers + resolution):** create / batch / PATCH carry the field;
  `TestTask` resolves `req.Criteria > task.Criteria > ""`; `tryAutoTest` passes
  `t.Criteria`; **`runAgon` passes `t.Criteria`** — this unblocked
  [[agon-adversarial-verification]] goal #7 (the primary motivation for landing
  this now).
- **Phase 3 (frontend):** `types.ts` field; composer "Test criteria" input;
  `TaskDetail` backlog-edit field; `vue-tsc` clean.

**Deviation from the spec:** rather than extend `UpdateTaskBacklog`'s positional
signature with a `criteria *string` parameter (which would have churned ~15 test
call sites), a dedicated `UpdateTaskCriteria` setter was added and called from
the same backlog-only PATCH gate. Same constraint, smaller blast radius.

**Not done (matches Non-Goals / deferred):** read-only criteria display for
non-backlog tasks (criteria is currently visible only via the backlog edit
form); search indexing of the field.

## Non-Goals

- Structured or programmatic criteria (assertion lists, machine-run command arrays). Criteria stay free-form text, like the task prompt.
- A hard blocking gate. Auto-submit gating is governed elsewhere; criteria only shape what the test agent checks.
- Per-turn criteria. The field applies to the test phase as a whole.
- Editing criteria after a task leaves backlog (matches the existing prompt-edit constraint). Late, per-run criteria remain available through the `testTask()` dialog.
- Search indexing of the criteria field (a possible later refinement, not required for the feature to work).
