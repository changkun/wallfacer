---
title: "Validation Barrier — User-Defined Test Criteria for Tasks"
status: drafted
track: local
depends_on: []
affects:
  - internal/store/models.go
  - internal/store/tasks_create_delete.go
  - internal/store/tasks_update.go
  - internal/handler/tasks.go
  - internal/handler/execute.go
  - internal/handler/tasks_autopilot.go
  - internal/prompts/test.tmpl
  - internal/apicontract/routes.go
  - ui/js/tasks.js
  - ui/js/render.js
  - ui/js/modal.js
  - ui/partials/task-detail-modal.html
effort: medium
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Validation Barrier — User-Defined Test Criteria for Tasks

---

## Problem

When the full automation pipeline is enabled (autopilot → auto-test → auto-submit), the test verification agent runs with no user-specified criteria. The only inputs it receives are the original task prompt, the implementation summary, and the git diff. This makes the test phase a best-effort guess — the agent decides what to verify based on its own interpretation of the prompt.

There is no way to pre-specify test criteria at task creation time. The `criteria` parameter on `POST /api/tasks/{id}/test` only works for manual test triggers, and the auto-tester passes an empty string. Users who want specific validation behavior (e.g., "run `make test-integration`", "verify the API returns 200 for these three endpoints", "check that the migration is reversible") have no mechanism to express this upfront.

This is particularly problematic for:

1. **Automated pipelines** — auto-test cannot use criteria it doesn't have.
2. **Batch task creation** — tasks created via `POST /api/tasks/batch` execute without supervision; pre-specified criteria are essential for meaningful validation.
3. **Domain-specific testing** — some tasks require non-obvious verification steps (performance benchmarks, visual checks, specific CLI invocations) that the test agent cannot infer from the implementation prompt alone.

---

## Goal

1. Add a persistent `validation_barrier` field to the task data model, editable at creation time and while in backlog — following the same pattern as the existing `goal` field.
2. When present, feed the validation barrier text into the test verification agent as the `Criteria` parameter, replacing the current empty-string default.
3. Make the auto-tester use the stored validation barrier so fully automated pipelines produce targeted, user-defined verification.
4. Expose the field in the task creation form, batch creation API, task detail modal, and PATCH endpoint.

---

## Design

### Data Model

Add two fields to `Task` in `internal/store/models.go`, placed adjacent to the existing `Goal` / `GoalManuallySet` pair:

```go
// ValidationBarrier describes user-specified acceptance criteria for the
// test verification agent. When non-empty, this text is injected into the
// test prompt's "Acceptance Criteria" section, overriding the default
// behavior where the test agent infers what to check from the task prompt.
ValidationBarrier          string `json:"validation_barrier,omitempty"`

// ValidationBarrierManuallySet is true when the user explicitly provided
// or edited the validation barrier (as opposed to it being auto-generated
// by refinement). Follows the same semantics as GoalManuallySet.
ValidationBarrierManuallySet bool `json:"validation_barrier_manually_set,omitempty"`
```

### API Changes

**`POST /api/tasks`** — Add `validation_barrier` string field to the request body. When non-empty, sets `ValidationBarrierManuallySet = true`.

**`POST /api/tasks/batch`** — Add `validation_barrier` per-task entry. Same semantics.

**`PATCH /api/tasks/{id}`** — Accept `validation_barrier` in the patch body. Only writable when task is in `backlog` status (same constraint as `goal` and `prompt`). Sets `ValidationBarrierManuallySet = true`.

**`POST /api/tasks/{id}/test`** — Continue accepting `criteria` in the request body. Resolution order:
1. If the request body contains a non-empty `criteria`, use it (explicit override for this test run).
2. Otherwise, fall back to `task.ValidationBarrier`.
3. If both are empty, the test agent runs with no criteria (current behavior).

No new endpoints required.

### Test Prompt Integration

In `internal/handler/execute.go`, update `TestTask()`:

```go
// Resolve criteria: explicit request body > stored validation barrier > empty.
criteria := strings.TrimSpace(req.Criteria)
if criteria == "" {
    criteria = strings.TrimSpace(task.ValidationBarrier)
}
testPrompt := buildTestPrompt(task.Prompt, criteria, implResult, diff)
```

In `internal/handler/tasks_autopilot.go`, update `tryAutoTest()` to pass the stored barrier instead of `""`:

```go
testPrompt := buildTestPrompt(t.Prompt, t.ValidationBarrier, implResult, diff)
```

The existing `test.tmpl` template already handles the `Criteria` field correctly — the `{{if .Criteria}}` block renders the "Acceptance Criteria" section only when non-empty. No template changes needed.

### Store Layer

**`CreateTaskWithOptions`** — Add `ValidationBarrier string` to `TaskCreateOptions`. Set `ValidationBarrierManuallySet = (opts.ValidationBarrier != "")`.

**`UpdateTaskBacklog`** — Accept and persist `ValidationBarrier`. Set `ValidationBarrierManuallySet = true` when provided.

**Search index** — Index `ValidationBarrier` alongside `Goal` and `Prompt` for full-text search.

### Refinement Integration

In `internal/runner/refine.go`, extract a `# Validation Barrier` heading from refinement output (same pattern as `extractGoalFromRefinement()`). When the user applies a refinement result, populate `ValidationBarrier` if extracted and not already manually set.

### UI Changes

**Task creation form** (`ui/js/tasks.js`):
- Add a "Validation Barrier" textarea below the Goal field.
- Placeholder text: `"Describe how the test agent should verify this task (e.g., specific commands, endpoints to check, expected outputs). Leave empty to let the test agent decide."`
- Collapsible by default under an "Advanced" section to avoid cluttering simple task creation.

**Task detail modal** (`ui/partials/task-detail-modal.html`):
- Show the validation barrier in the same edit/preview tab pattern used by Goal.
- Editable only in `backlog` status; read-only rendered markdown otherwise.
- Omit the section entirely when empty and task is not in backlog (no visual noise for tasks that don't use it).

**Task card** (`ui/js/render.js`):
- No change to card display. The validation barrier is operational metadata, not a display-priority field like Goal. It appears only in the detail modal.

**Batch creation dialog** (if present):
- Include `validation_barrier` in the per-task fields.

### SSE Delta

`ValidationBarrier` is a string field on `Task`, so it flows through the existing SSE delta system automatically — no additional plumbing needed.

---

## Test Plan

1. **Unit: store creation** — Create task with `ValidationBarrier` set; verify it persists and `ValidationBarrierManuallySet` is `true`. Create without; verify both fields are zero-valued.

2. **Unit: store update** — PATCH a backlog task with `validation_barrier`; verify persistence. Attempt PATCH on a non-backlog task; verify rejection.

3. **Unit: test prompt resolution** — Call `TestTask` with:
   - (a) explicit `criteria` in body → body criteria used.
   - (b) empty body, task has `ValidationBarrier` → barrier used.
   - (c) both present → body criteria wins.
   - (d) both empty → no criteria section in prompt.

4. **Unit: auto-tester** — Verify `tryAutoTest` passes `task.ValidationBarrier` into `buildTestPrompt`.

5. **Unit: search** — Create task with validation barrier; search for a keyword from the barrier; verify task is found.

6. **Unit: refinement extraction** — Feed refinement output containing `# Validation Barrier` heading; verify extraction and population.

7. **Integration: full pipeline** — Create task with validation barrier, enable autopilot + auto-test. Verify the test agent's prompt contains the "Acceptance Criteria" section with the barrier text.

---

## Migration

No schema migration needed. The field is persisted as part of the JSON task file. Existing tasks will have an empty `ValidationBarrier`, which preserves current behavior (test agent infers criteria from prompt).

---

## Non-Goals

- **Structured/programmatic criteria** (e.g., a list of assertions or test commands to run mechanically). The validation barrier is free-form text interpreted by the test agent, same as the task prompt is interpreted by the implementation agent.
- **Blocking gate semantics** — This spec does not introduce a hard gate that prevents auto-submit on test failure. That behavior is already controlled by the auto-submit configuration. The validation barrier only affects what the test agent checks, not what happens after.
- **Per-turn criteria** — The barrier applies to the test phase as a whole, not to individual implementation turns.
