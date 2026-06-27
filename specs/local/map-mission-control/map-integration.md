---
title: Rewire MapPage onto /api/graph + GraphCanvas; Remove Vendored Depgraph
status: validated
depends_on:
  - specs/local/map-mission-control/graph-endpoint.md
  - specs/local/map-mission-control/renderer-rebuild.md
affects:
  - frontend/src/views/MapPage.vue
  - frontend/src/views/MapPage.test.ts
  - frontend/src/vendor/depgraph/
  - frontend/src/env.d.ts
  - frontend/src/api/client.ts
created: 2026-06-27
updated: 2026-06-27
author: changkun
dispatched_task_id: null
effort: medium
---

# Rewire MapPage onto /api/graph + GraphCanvas; Remove Vendored Depgraph

## Goal

Move the Map fully onto the new server graph + new renderer, then delete the
~4,583 lines of vendored legacy code and its `window` shims. After this task
the Map renders from `/api/graph` with no `ui/`/`vendor` dependency.

## What to do

1. Rewrite `frontend/src/views/MapPage.vue`:
   - Fetch `GET /api/graph` (add a typed client call in `api/client.ts`),
     honoring the "Show archived" toggle via `?archived=1`.
   - Render `components/map/GraphCanvas` with the fetched `Graph`; keep the
     header (search/filter/reset) and the right-side inspector (legend,
     selection, critical path).
   - Remove ALL `window` shim installation (`specModeState`, `depGraphEnabled`,
     `openTaskModal`, `focusSpec`, `switchMode`, `scheduleRender`,
     `setMapShowArchived`, `setMapSearch`, `resetMapLayout`, `hideDependencyGraph`,
     `_resetMapCentering`) and the dynamic `import('../vendor/depgraph/...')`
     calls.
   - Re-sync after task SSE deltas: keep a watch that refetches or patches the
     graph when `store.tasks` fingerprint changes (the existing fingerprint
     watch pattern in the current MapPage).
2. Delete `frontend/src/vendor/depgraph/unified-graph.js`,
   `depgraph.js`, `unified-graph.drag.test.ts`, `unified-graph.layout.test.ts`,
   and the now-empty `vendor/depgraph/` directory.
3. Remove the ambient `window` shim declarations for these renderers from
   `frontend/src/env.d.ts`.
4. Grep-verify no remaining `frontend/` reference to `vendor/depgraph` or `ui/`.

## Tests

- Update `frontend/src/views/MapPage.test.ts`:
  - `TestMapPage_RendersFromGraphEndpoint` — mounts with a mocked `/api/graph`
    response and asserts nodes/edges render via GraphCanvas.
  - `TestMapPage_NoWindowShims` — asserts MapPage mounts without defining the
    legacy `window` globals.
  - `TestMapPage_ArchivedToggle` — toggling "Show archived" refetches with
    `?archived=1`.

## Boundaries

- Do NOT implement inline node *actions* (dispatch/start/retract) here — that is
  the `inline-actions` task. Selecting a node may open `TaskDetail` /
  navigate to Plan as it does today, but no new mutations.
- Do NOT change the backend; consume the endpoint as-is.
- Ensure `vue-tsc --noEmit` is clean after the deletions.
