---
title: Composer Flow picker replaces Type picker and Agent overrides
status: complete
depends_on:
  - specs/local/agents-and-flows/flows-api-and-tab.md
  - specs/local/agents-and-flows/task-flow-field.md
affects:
  - ui/partials/board.html
  - ui/js/tasks.js
  - ui/js/tests/tasks-coverage.test.js
  - docs/guide/board-and-tasks.md
effort: medium
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Composer Flow picker replaces Type picker and Agent overrides

## Goal

Collapse the new-task composer onto the Flow primitive: a single
Flow dropdown replaces the Implement/Brainstorm Type picker, and the
Agent-overrides disclosure goes away (per-step CLI choice now lives
on the flow's agent references). Existing flows (`implement`,
`brainstorm`) preserve the old user-visible paths; future flows
become one dropdown entry away from composable.

## What to do

1. `ui/partials/board.html`, inside the composer head row:
   - Replace the `#new-task-kind` select with a `#new-task-flow`
     select populated dynamically from `/api/flows`. Default option
     is `implement`; `brainstorm` is listed alongside. Slug is the
     option value; flow name is the label.
   - Remove the "Agent overrides" `<details>` disclosure block
     entirely. Its IDs (`new-sandbox-implementation`,
     `new-sandbox-testing`, etc.) are no longer referenced.
   - Keep "Agent" (default CLI) and "Timeout" — those still apply
     per task.
   - Keep Share siblings, Scheduling, Budget, Depends on.

2. `ui/js/tasks.js`:
   - `showNewTaskForm()` fetches `/api/flows` on first open and
     caches the list; populates the select with one option per
     flow.
   - On flow change, update the prompt placeholder from the
     flow's `description` (fall back to today's brainstorm /
     implement placeholders keyed off slug for the built-ins).
   - `createTask()`:
     - Drops `kind` from the POST body.
     - Sends `flow: <slug>`.
     - When `flow === "brainstorm"` (or any flow whose
       `SpawnKind === "idea-agent"` from the fetched list),
       allow an empty prompt — same rule the current Type
       picker implements.
     - Drops `sandbox_by_activity` from the POST body.
   - `hideNewTaskForm()` resets the flow select to `implement`.

3. Routine composer toggle: keep "Repeat on a schedule". When
   ticked, the routine is created with `spawn_flow: <slug>`
   (field accepted by the routines API now — coordinate with
   the routine-spawn-flow follow-up task which is scheduled to
   land in parallel; until that's merged, fall back to
   `spawn_kind` mapping for `brainstorm` → `idea-agent` and
   everything else → `""`).

4. `docs/guide/board-and-tasks.md`:
   - Replace the Implement/Brainstorm Type-picker paragraph with
     a Flow picker walk-through.
   - Note that per-activity Agent overrides now live on the flow
     definition itself (Flows tab).

## Tests

- `ui/js/tests/tasks-coverage.test.js` updates:
  - Replace the kind-related assertions with flow-based ones:
    * `TestCreateTask_SendsFlow` — POST body includes `flow:
      "implement"` for the default path.
    * `TestCreateTask_BrainstormAllowsEmptyPrompt` —
      `flow: "brainstorm"` with no prompt passes through.
    * `TestCreateTask_FlowFallbackImplement` — composer open
      with no prior selection defaults to `implement`.
- Remove any lingering test that asserts
  `sandbox_by_activity` keys in the POST body.

## Boundaries

- Do NOT add inline flow editing to the composer — editing lives
  on the Flows tab.
- Do NOT remove the `kind` field from the POST /api/tasks
  contract yet. Legacy clients and the routines API still read
  it; the task-flow-field task's back-compat path keeps it
  working.
- Do NOT rewire the runner to consume the flow. That's the
  `runner-flow-integration` task.
- Do NOT touch the Agents / Flows sidebar tabs. This task only
  modifies the composer.
