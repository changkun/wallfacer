---
title: Inline Node Actions + Operational Critical-Path Highlighting
status: validated
depends_on:
  - specs/local/map-mission-control/map-integration.md
affects:
  - frontend/src/views/MapPage.vue
  - frontend/src/components/map/GraphCanvas.vue
  - frontend/src/components/map/NodeActions.vue
  - frontend/src/views/MapPage.test.ts
  - docs/guide/
created: 2026-06-27
updated: 2026-06-27
author: changkun
dispatched_task_id: null
effort: medium
---

# Inline Node Actions + Operational Critical-Path Highlighting

## Goal

Make the Map operational: act on a node inline (dispatch a validated spec,
start/retract a task, deep-jump to Board/Plan) and surface "what is actionable
now" via critical-path / blocked highlighting. This is the leg that makes the
Map do something Board and Plan don't — driving the cross-cutting flow.

## What to do

1. `frontend/src/components/map/NodeActions.vue` (or an inspector section in
   MapPage): given the selected node and its `available_actions` from
   `/api/graph`, render the affordances:
   - **Spec node** (validated leaf, action `dispatch`): *Dispatch* →
     `POST /api/specs/transition` (action `dispatch`); *Open in Plan* → route
     `/plan?spec=<path>`.
   - **Task node** (action `start`): *Start* → `PATCH /api/tasks/{id}`
     (`status: in_progress`); *Retract/Cancel* → `PATCH`/`DELETE /api/tasks/{id}`;
     *Open in Board* → open `TaskDetail` (reuse `selectedTaskId`).
   - Render only actions the backend marked available (no client-side lifecycle
     re-derivation).
2. After any action, refetch `/api/graph` (or apply the task SSE delta) so the
   affordances re-sync; show a toast on 4xx/5xx and refetch so the UI never
   leaves a stale affordance (per the spec's Error Handling).
3. Operational highlighting: use `critical_path` + `blocked` to visually
   emphasize ready-to-act nodes (specs ready to dispatch, tasks ready to start)
   and de-emphasize blocked ones. The inspector "Critical path" section lists
   actionable next steps, not a static chain.
4. Update `docs/guide/` to describe the Map mission-control surface, the
   `/api/graph` model, and the inline actions, per the docs-update rule.

## Tests

- `MapPage.test.ts` additions:
  - `TestNodeAction_DispatchSpec` — clicking Dispatch on a validated leaf spec
    POSTs `/api/specs/transition` and refetches the graph.
  - `TestNodeAction_StartTask` — clicking Start PATCHes `/api/tasks/{id}` to
    `in_progress`.
  - `TestNodeAction_DeepJump` — Open in Plan/Board routes / opens TaskDetail.
  - `TestNodeAction_FailureToast` — a 409 on dispatch shows a toast and
    refetches the graph.

## Boundaries

- Do NOT add new backend mutation routes; reuse `transition` / task APIs.
- Do NOT introduce optimistic lifecycle state on the client — the server's
  `available_actions` is the source of truth.
- Keep visual verification (ui-shots) green on a branching graph.
