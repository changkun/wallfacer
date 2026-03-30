---
title: Dispatch & Board Integration
status: drafted
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/spec-mode-ui-shell.md
affects:
  - ui/js/
  - internal/handler/
  - internal/store/
effort: medium
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Dispatch & Board Integration

## Design Problem

How does a validated leaf spec become a kanban task, and how do the two views (spec mode and board mode) stay linked? Dispatch must translate spec content into a task prompt, wire `depends_on` edges from spec dependencies to task dependencies (via `dispatched_task_id`), and maintain bidirectional links so clicking a task navigates to its source spec and vice versa.

Key constraints:
- Only leaf specs with `status: validated` are dispatchable
- Dispatch creates a kanban task where `prompt = spec body` and `DependsOn` maps from spec `depends_on` to `dispatched_task_id` of sibling specs
- Multi-select dispatch (batch) must wire dependencies atomically
- Undispatch (cancel) clears `dispatched_task_id` and returns spec to `validated`
- Mode switching preserves context: board highlights tasks from focused spec's subtree; clicking a task's spec badge navigates to spec mode
- The spec's status moves to `complete` when its dispatched task completes

## Context

The existing task creation API supports:
- `POST /api/tasks` — single task creation with prompt, goal, timeout
- `POST /api/tasks/batch` — atomic batch creation with symbolic `depends_on_refs` and cycle detection (Kahn's algorithm + DFS reachability)
- `DependsOn` field: `[]string` of task UUIDs, enforced by `AreDependenciesSatisfied()` in the auto-promoter

The batch API's ref-based dependency wiring is ideal for multi-dispatch: each spec's `depends_on` can be resolved to the `dispatched_task_id` of the dependency spec, and the batch API handles cycle detection.

The spec system tracks dispatch state via `dispatched_task_id` in frontmatter. When a task completes, the spec's status should transition to `complete`. This requires a feedback loop from the task store to the spec files.

## Options

**Option A — Direct API call from UI.** The dispatch button calls `POST /api/tasks` (or `/api/tasks/batch` for multi-dispatch) with the spec body as prompt. The UI then updates the spec file's `dispatched_task_id` via the planning agent or a direct file write.

- Pro: Simple. Uses existing task creation infrastructure. No new backend endpoints.
- Con: Two-step process (create task + update spec file) is not atomic. If task creation succeeds but spec update fails, state diverges. The UI must know how to write YAML frontmatter.

**Option B — Dedicated dispatch endpoint.** A new `POST /api/specs/{path}/dispatch` endpoint reads the spec file, creates the kanban task, and atomically updates the spec's `dispatched_task_id`. Batch dispatch via `POST /api/specs/dispatch` accepts a list of spec paths.

- Pro: Atomic: task creation and spec update happen in one server-side transaction. The server reads the spec, validates it, creates the task, and writes the updated frontmatter. No client-side spec file manipulation.
- Con: New API surface. The server must parse and write spec YAML frontmatter (currently only done by `internal/spec/` for reading).

**Option C — Agent-mediated dispatch.** The user says "dispatch this spec" in the chat. The planning agent reads the spec, calls the task creation API, and updates the spec's `dispatched_task_id`. The dispatch button is a shortcut that sends a chat message.

- Pro: Consistent with the chat-driven model. The agent can validate the spec, check dependencies, and provide feedback before dispatching. No new API endpoint.
- Con: Slower than a direct API call (agent must process the message). Requires the planning agent to have task creation permissions. The agent becomes a bottleneck for a mechanical operation.

### Task Completion → Spec Update

**Option X — Server-side hook.** When a task reaches `done`, the server checks if it has a spec source (via a label or metadata). If so, it updates the spec file's status to `complete` and clears any dependent specs' staleness.

- Pro: Automatic. No user action required. The server has full context.
- Con: The server writing to spec files outside a planning session may conflict with the concurrent edit model.

**Option Y — Agent-triggered on status change.** The planning agent observes task status changes (via SSE) and proactively updates the spec. This aligns with the entry-point staleness decision (agent proactively updates documents).

- Pro: Consistent with the planning agent's role as the spec file writer. The agent can do more than just update status — it can update the parent spec's children summary, flag downstream specs, etc.
- Con: Requires the planning agent to be running. If the user isn't in spec mode, updates are deferred.

## Open Questions

1. Should the dispatch button be visible only on the focused view, or also available as a context menu action in the spec explorer?
2. When multi-dispatching, should the system enforce that all selected specs' dependencies are either also being dispatched or already have `dispatched_task_id` set? Or allow dispatching specs with unresolved dependencies (the kanban task will block on unmet deps)?
3. How should the spec-to-task link be stored? Options: task label (`wallfacer.spec.path`), task metadata field, or a separate mapping file. The link must survive task archival and soft-delete.
4. When a dispatched task fails and is retried, does the spec stay linked to the same task UUID or get re-dispatched as a new task?

## Affects

- `internal/handler/` — new dispatch endpoint(s), or extension of existing task creation handlers
- `internal/store/models.go` — task metadata for spec source path linkage
- `internal/spec/parse.go` — may need `UpdateFrontmatter()` function for writing `dispatched_task_id` back
- `ui/js/` — dispatch button in focused view, spec badge on task cards, mode-switching context links
- `ui/js/render.js` — task card rendering with spec badge
- `ui/js/modal-core.js` — task detail modal with "View Source Spec" link
