---
title: Dispatch & Board Integration
status: drafted
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/spec-mode-ui-shell.md
affects:
  - ui/js/
  - internal/handler/
  - internal/store/
  - internal/spec/
  - internal/apicontract/
effort: medium
created: 2026-03-30
updated: 2026-04-04
author: changkun
dispatched_task_id: null
---

# Dispatch & Board Integration

## Design Problem

How does a validated spec become a kanban task, and how do the two views (spec mode and board mode) stay linked? Dispatch must translate spec content into a task prompt, wire `depends_on` edges from spec dependencies to task dependencies (via `dispatched_task_id`), and maintain bidirectional links so clicking a task navigates to its source spec and vice versa.

Key constraints:
- **Any validated spec is dispatchable** — both design specs (non-leaf) and implementation specs (leaf). The user decides when a spec is ready for execution. A design spec dispatched as a task means "the agent should implement this entire design." An implementation spec dispatched as a task means "the agent should make these specific code changes."
- **Breakdown as an alternative to dispatch.** The focused view offers two actions side by side: **Dispatch** (send to kanban as-is) and **Break Down** (create smaller child specs). The user chooses which action fits.
- Dispatch creates a kanban task where `prompt = spec body` and `DependsOn` maps from spec `depends_on` to `dispatched_task_id` of sibling specs
- Multi-select dispatch (batch) must wire dependencies atomically
- Undispatch (cancel) clears `dispatched_task_id` and returns spec to `validated`
- Mode switching preserves context: board highlights tasks from focused spec's subtree; clicking a task's spec badge navigates to spec mode
- The spec's status moves to `complete` when its dispatched task completes

## Current State

The following infrastructure already exists:

- **Spec model field**: `DispatchedTaskID *string` in `internal/spec/model.go:70`, parsed from YAML frontmatter
- **Dispatch validation**: `internal/spec/validate.go` enforces that non-leaf specs cannot have `dispatched_task_id` and that no two specs share the same ID
- **Batch task API**: `POST /api/tasks/batch` in `internal/handler/tasks.go:242` supports symbolic `depends_on_refs`, topological sort (Kahn's algorithm), and cycle detection — ideal for multi-dispatch dependency wiring
- **UI stubs**: `dispatchFocusedSpec()` in `ui/js/spec-mode.js:476` and `dispatchSelectedSpecs()` in `ui/js/spec-explorer.js:531` are wired to the dispatch button and `d` keyboard shortcut but are no-op stubs
- **Planning agent template**: `internal/planner/commands_templates/dispatch.tmpl` exists as a placeholder for agent-mediated dispatch
- **Mode switching**: Fully implemented in `ui/js/spec-mode.js` with board/spec/docs modes and localStorage persistence
- **Spec SSE stream**: `GET /api/specs/stream` is active and consumed by the UI for real-time tree updates

## Decision: Atomic Dispatch API (A+B Hybrid)

Combines the reuse benefits of Option A (direct API call using existing `POST /api/tasks/batch`) with the atomicity of Option B (dedicated server-side endpoint that coordinates both task creation and spec update).

**Architecture**: A dedicated `POST /api/specs/dispatch` endpoint that performs two operations atomically:
1. **Create the kanban task** — reads the spec body, resolves dependency `dispatched_task_id` values, and calls into the existing task creation logic (reusing `POST /api/tasks/batch` internals)
2. **Update the spec file** — writes `dispatched_task_id` back into the spec's YAML frontmatter

Both succeed or both fail. The UI dispatch button calls this endpoint directly. The planning agent can also trigger the same endpoint via chat (nice-to-have), providing a conversational alternative where the agent can validate, check dependencies, and provide feedback before dispatching.

**Batch variant**: `POST /api/specs/dispatch` accepts a list of spec paths for multi-select dispatch. Dependency wiring across the batch is resolved atomically using the same topological sort logic from the batch task API.

### Task Completion Feedback

Two-layer design separating deterministic metadata updates from non-deterministic analysis:

**Layer 1 — Server-side hook (deterministic).** When a task reaches `done`, the server checks if it was dispatched from a spec (via metadata linkage). If so, it updates the spec file's frontmatter: `status` → `complete`, timestamp, and any other mechanical fields. This is a reliable, instant metadata flip that requires no agent involvement.

**Layer 2 — Spec-diff agent (non-deterministic).** After the server-side hook fires, an agent (the tester, oversight agent, or a specialized spec-diff agent) compares the task's actual implementation against the spec's acceptance criteria and produces a structured diff report: which items were fully satisfied, which diverged, and what was implemented but not specified. This report is appended to the spec as an `## Outcome` section or stored as a sidecar artifact. The agent can also flag specs whose implementation drifted significantly, transitioning them to `stale` instead of `complete`.

This two-layer split ensures specs always get timely metadata updates (layer 1) while leaving room for richer, non-deterministic analysis (layer 2) that may take longer or require a running sandbox. Layer 2 is an extension point — the initial implementation can ship with layer 1 only, adding the spec-diff agent later without changing the dispatch or completion flow.

## Remaining Work

### Backend: Dispatch Endpoint

1. **Spec frontmatter writer** — Add `UpdateFrontmatter()` to `internal/spec/` that can write a single field (like `dispatched_task_id` or `status`) back to a spec file's YAML frontmatter without disturbing the markdown body. Currently the spec package only reads (`ParseFile`, `ParseBytes`).

2. **Dispatch API route** — Add `POST /api/specs/dispatch` to `internal/apicontract/routes.go`. Handler in `internal/handler/`. Request body: `{paths: []string, run: bool}`. The handler:
   - Reads and validates each spec (must be `validated` status)
   - Resolves `depends_on` → `dispatched_task_id` for each spec's dependencies
   - Creates tasks via existing batch creation logic (reuse `handleBatchCreate` internals from `internal/handler/tasks.go`)
   - Writes `dispatched_task_id` back to each spec file atomically
   - Returns created task IDs and any errors

3. **Undispatch API route** — Add `POST /api/specs/undispatch` (or `DELETE /api/specs/{path}/dispatch`). Cancels the linked kanban task, clears `dispatched_task_id`, and returns the spec to `validated` status.

4. **Spec-to-task metadata linkage** — Store the source spec path on the task so the reverse link (task → spec) works. Options: a label field on the Task model (`SpecSourcePath string`), or task metadata. This must survive task archival and soft-delete.

5. **Task completion hook (layer 1)** — In `internal/store/` or `internal/runner/`, when a task transitions to `done`, check for spec linkage and update the spec file's `status` to `complete` and `updated` timestamp via `UpdateFrontmatter()`. This is the deterministic metadata flip — no agent required.

6. **Spec-diff agent (layer 2, extension point)** — After the completion hook fires, optionally trigger an agent that diffs the task's implementation against the spec's acceptance criteria. The agent produces a structured report (satisfied / diverged / unspecified items) and appends an `## Outcome` section to the spec. Significant drift transitions the spec to `stale` instead of `complete`. Initial implementation can defer this — the hook in item 5 is sufficient for launch.

### Frontend: Dispatch UI

7. **Wire dispatch button** — Implement `dispatchFocusedSpec()` in `ui/js/spec-mode.js:476` to call `POST /api/specs/dispatch` with the focused spec's path. Show loading state, handle errors, and update the spec tree on success.

8. **Wire multi-select dispatch** — Implement `dispatchSelectedSpecs()` in `ui/js/spec-explorer.js:531` to call the batch dispatch endpoint with all selected spec paths.

9. **Spec badge on task cards** — In `ui/js/render.js`, render a small badge on task cards that were dispatched from a spec. Clicking the badge navigates to spec mode with that spec focused.

10. **"View Source Spec" link in task modal** — In `ui/js/modal-core.js`, add a link to the source spec when the task has spec metadata. Clicking navigates to spec mode.

11. **Board highlight from spec context** — When viewing a spec in focused mode, highlight its dispatched task(s) on the board. When switching to board mode from a focused spec, scroll to / filter for the relevant tasks.

### Agent Integration (Nice-to-Have)

12. **Planning agent dispatch command** — Update `internal/planner/commands_templates/dispatch.tmpl` so the `/dispatch` slash command calls the atomic `POST /api/specs/dispatch` endpoint instead of manually updating frontmatter. The agent can validate prerequisites and provide feedback before triggering the API call.

## Open Questions

1. Should the dispatch button be visible only on the focused view, or also available as a context menu action in the spec explorer?
2. When multi-dispatching, should the system enforce that all selected specs' dependencies are either also being dispatched or already have `dispatched_task_id` set? Or allow dispatching specs with unresolved dependencies (the kanban task will block on unmet deps)?
3. When a dispatched task fails and is retried, does the spec stay linked to the same task UUID or get re-dispatched as a new task?
4. When dispatching a design spec (non-leaf), should the task prompt include instructions to run `/wf-spec-breakdown` as part of execution? Or should the prompt simply be the spec body and let the agent decide how to approach it?
5. Should dispatching a non-leaf spec that already has children warn the user? (The children represent an existing breakdown — dispatching the parent as a single task may conflict with or duplicate the children's work.)

## Affects

- `internal/apicontract/routes.go` — new dispatch/undispatch routes
- `internal/handler/` — dispatch handler (new file or extension of existing)
- `internal/spec/` — `UpdateFrontmatter()` for writing fields back to spec files
- `internal/store/models.go` — `SpecSourcePath` field on Task for reverse linkage
- `internal/store/` or `internal/runner/` — task completion hook for spec status update
- `ui/js/spec-mode.js` — wire `dispatchFocusedSpec()`
- `ui/js/spec-explorer.js` — wire `dispatchSelectedSpecs()`
- `ui/js/render.js` — spec badge on task cards
- `ui/js/modal-core.js` — "View Source Spec" link
