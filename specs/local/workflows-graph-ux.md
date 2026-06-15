---
title: Workflows Graph UX
status: drafted
depends_on:
  - specs/local/agents-and-flows.md
affects:
  - frontend/src/views/FlowsPage.vue
  - frontend/src/styles/flows.css
  - frontend/src/router.ts
  - internal/flow/
  - docs/guide/
effort: large
created: 2026-06-15
updated: 2026-06-15
author: changkun
dispatched_task_id: null
---

# Workflows Graph UX

## Overview

The Flows feature already models "an ordered chain of sub-agents a task runs
against" (see [[agents-and-flows]]), but the UI does not communicate that.
The current `FlowsPage` reads as a flat list of named steps with no sense of
how agents connect or hand off, so users cannot tell what a Flow *is* or how
to compose one. The mental model users actually want is a **workflow**: a
connected set (graph) of agents that collaborate to complete a task.

This spec reframes the surface around that model:

1. **Rename** the concept and tab from "Flows" to "Workflows" (route, nav
   label, page copy, docs). The backend `internal/flow` package and the
   `flow_id` field stay as-is to avoid a data migration; only the
   user-facing noun changes.
2. **Redesign the editor** so steps render as **connected nodes** on a
   left-to-right pipeline, with parallel groups shown as stacked lanes and
   optional steps visually marked. Edges show handoff order; the canvas makes
   "ordered chain + parallel fan-out" legible at a glance instead of as a
   list.
3. Keep the existing data model (ordered steps, parallel-group ids, optional
   flags) — this is a presentation + interaction change, not a new execution
   engine.

## Current State

- Route `/flows` → `frontend/src/views/FlowsPage.vue`; styles in
  `frontend/src/styles/flows.css`.
- Built-ins (`Implement` 5 steps, `Brainstorm` 1 step, `Test only` 1 step)
  come from `internal/flow/builtins.go`; custom flows persist via
  `internal/flow/store.go`.
- The editor lists steps top-to-bottom with drag-reorder, an optional flag,
  and parallel grouping, but renders no connections, so the "graph of agents"
  intent is invisible. The empty state says "Pick a flow on the left, or
  click + New Flow."
- Each step references an Agent role (see the Agents tab); a Flow is the
  ordered graph of those roles with handoff rules.

## Goals

- A user landing on the tab immediately understands a Workflow is a connected
  pipeline of agents, without reading the description paragraph.
- Composing/editing reads as wiring nodes: add a node, set its agent, mark it
  optional, group nodes to run in parallel, reorder by dragging nodes on the
  canvas.
- No backend data migration; built-ins and saved flows render unchanged under
  the new visualization.
- Consistent with the consolidated design language (shared tokens, badges,
  surfaces) introduced alongside the UI cleanup pass.

## Non-Goals

- No free-form DAG with arbitrary cross-edges. The execution model remains an
  ordered sequence of stages where a stage may fan out to parallel siblings;
  the graph view visualizes *that* structure, it does not introduce new
  topology the engine cannot run.
- No change to how a task selects/runs a flow (`flow_id` wiring stays).
- No rename of the `internal/flow` package or API fields in this pass.

## Proposed Design

### Naming

- Route `/flows` → keep the path or add `/workflows` with a redirect (decide
  during impl; prefer adding `/workflows` and redirecting `/flows` to avoid
  breaking existing links). Nav label and page title become "Workflows".
- Page copy: "A workflow connects a set of agents into a pipeline a task runs
  against. Clone a built-in or start from scratch; reorder nodes, mark any
  node optional, or group nodes to run in parallel."

### Canvas

- Replace the vertical step list with a horizontal pipeline of **node cards**,
  one per step, connected by edges indicating order.
- A parallel group renders as stacked lanes between the same pair of edges
  (fan-out then fan-in), so concurrency is visible.
- Optional nodes get a dashed border + "optional" tag.
- Each node shows: agent role name, harness badge (reuse `HarnessBadge`), and
  a compact menu (set agent, toggle optional, remove).
- Drag a node to reorder; drag onto another node's lane to group in parallel.
- Empty state shows a single "+ add first agent" node so the canvas is never
  a blank box.

### Reuse

- Build on the consolidated tokens/components; the node card uses the same
  surface/border treatment as other cards. No bespoke palette.
- Keep `flows.css` but restructure around the canvas; rename to
  `workflows.css` only if the route renames.

## Acceptance Criteria

- [ ] Tab/nav/page read "Workflows"; `/flows` still resolves (redirect or
      retained path).
- [ ] Built-in `Implement` renders as a connected pipeline with its parallel
      stage shown as stacked lanes, not a flat list.
- [ ] A user can add, reorder (drag), mark optional, and parallel-group nodes
      on the canvas; saved flows round-trip through `internal/flow/store.go`
      unchanged.
- [ ] No backend migration; existing saved flows load and render.
- [ ] Docs under `docs/guide/` describe the Workflows tab and the graph model;
      any renamed UI path is updated per the docs-update rule.
- [ ] `vue-tsc --noEmit` clean; visual verification via the ui-shots harness.

## Open Questions

- Rename route to `/workflows` (with redirect) or keep `/flows`?
- Should the Agents tab fold into Workflows as a node inspector, or stay a
  separate tab? (Out of scope here; note for a follow-up.)
