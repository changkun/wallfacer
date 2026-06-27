---
title: Hand-Rolled SVG Graph Renderer (RAF Drag + Curved Edges)
status: validated
depends_on:
  - specs/local/map-mission-control/graph-endpoint.md
affects:
  - frontend/src/components/map/GraphCanvas.vue
  - frontend/src/components/map/layout.ts
  - frontend/src/components/map/edges.ts
  - frontend/src/components/map/GraphCanvas.drag.test.ts
  - frontend/src/components/map/layout.test.ts
  - frontend/src/api/types.ts
created: 2026-06-27
updated: 2026-06-27
author: changkun
dispatched_task_id: null
effort: large
---

# Hand-Rolled SVG Graph Renderer (RAF Drag + Curved Edges)

## Goal

Build a clean, standalone Vue + hand-rolled SVG graph renderer with no new
dependency, fixing the legacy view's three rendering defects: overlapping
straight edges, drag lag, and arrows detaching on fast drag. Built in isolation
here; wired into MapPage in the next task so the existing Map keeps working.

## What to do

1. Add the TS graph types to `frontend/src/api/types.ts` matching the
   `graph-endpoint` wire contract (`GraphNode`, `GraphEdge`, `Graph`).
2. `frontend/src/components/map/layout.ts` — pure layered/hierarchical layout:
   input `Graph` → node coordinates with adequate inter-node gaps so hierarchy
   and connectivity read clearly. Keep it a pure function for testability
   (port the intent of `vendor/depgraph/unified-graph.layout` /
   `unified-graph.layout.test.ts`, not the code).
3. `frontend/src/components/map/edges.ts` — cubic-Bézier path generation per
   edge kind (containment / dispatch / spec_dep / task_dep). Helper
   `edgePath(from, to)` returns a curved `d`. Both endpoints are recomputed
   from live node positions (no frozen middle waypoints — this is the
   detachment fix).
4. `frontend/src/components/map/GraphCanvas.vue` — pure SVG renderer:
   - Props: a `Graph`; emits `select(nodeId)`.
   - Draw nodes (status/lifecycle colors per the existing legend) and curved
     typed edges; arrowhead markers via `<marker>` defs.
   - **Drag**: pointer events with `requestAnimationFrame`-batched updates —
     accumulate the latest pointer delta and rewrite node transform + incident
     edge paths at most once per frame (the lag fix). Each frame re-aims both
     endpoints of every incident edge so they track the live node (the
     detachment fix).
   - **Pan/zoom**: Space-drag pan, Ctrl/⌘+scroll zoom (preserve the keyboard
     model from the current MapPage header copy).

## Tests

- `GraphCanvas.drag.test.ts` (the test-a-bug regression for defect #2):
  - `TestDrag_BatchesPerFrame` — drive many rapid pointer-move events within
    one frame; assert edge `d` is recomputed at most once per animation frame
    (stub `requestAnimationFrame`), not once per event.
  - `TestDrag_EdgesStayAttached` — after a fast drag sequence, every incident
    edge's moving endpoint equals the node's final live position (no slack).
- `layout.test.ts`:
  - `TestLayout_NoOverlap` — output coordinates produce non-overlapping nodes.
  - `TestLayout_Layering` — nodes are placed in dependency-correct layers.

## Boundaries

- Do NOT modify `MapPage.vue` or delete the vendored `depgraph/*` yet — that is
  the `map-integration` task. The existing Map must keep working.
- Do NOT add any npm dependency (no vue-flow / cytoscape / d3 / dagre).
- Do NOT implement node *actions* here (next-next task) — only `select` emit.
