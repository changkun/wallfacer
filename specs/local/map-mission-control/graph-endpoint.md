---
title: Server-Side Unified Graph Model + GET /api/graph
status: complete
depends_on: []
affects:
  - internal/graph/
  - internal/handler/graph.go
  - internal/handler/graph_test.go
  - internal/apicontract/routes.go
created: 2026-06-27
updated: 2026-06-27
author: changkun
dispatched_task_id: null
effort: medium
---

# Server-Side Unified Graph Model + GET /api/graph

## Goal

Make the unified spec+task graph authoritative server-side instead of
assembled client-side via `window` shims. Introduce a pure `internal/graph`
builder and a `GET /api/graph` endpoint the rebuilt Map (and, later, Board/Plan)
consume.

## What to do

1. New package `internal/graph/` with a pure builder:
   - Input: `spec.TreeResponse` (from `Handler.collectSpecTree`,
     `internal/handler/specs.go:34`) and the task list (`[]*task.Task` or the
     store's list method used by `ListTasks` in `internal/handler/tasks.go`).
   - Output struct `Graph`:
     - `Nodes []Node` — `{ ID, Kind (spec|task), Label, Status, Ref (spec path
       or task id), Depth/Lane int, Meta }`.
     - `Edges []Edge` — `{ From, To, Kind (containment|dispatch|spec_dep|task_dep) }`.
     - `CriticalPath []string` — longest dependency chain across the combined
       spec+task DAG; reuse `internal/pkg/dag/` longest-path utilities.
     - `Blocked []string` — node IDs whose prerequisites are unmet.
     - Per-node `AvailableActions []string` — e.g. `dispatch` on a validated
       leaf spec, `start` on a ready backlog task; derived from existing
       lifecycle rules (mirror the gating in `internal/handler/specs_dispatch.go`
       and the task status machine).
   - Port the node/edge derivation currently living in
     `frontend/src/vendor/depgraph/unified-graph.js` into typed Go. Keep the
     builder a pure function (no store mutation).
2. New `internal/handler/graph.go`:
   - `func (h *Handler) GetGraph(w http.ResponseWriter, r *http.Request)` —
     calls `collectSpecTree` + task list, runs the builder, writes JSON
     `{ nodes, edges, critical_path, blocked }`.
   - Honor `?archived=1` (include archived specs/tasks), matching the Map's
     existing "Show archived" semantics.
   - Apply the same principal-scoping / hidden-spec rules as `collectSpecTree`
     (see the leak fix in `internal/handler/config_test.go:332`).
3. Register the route in `internal/apicontract/routes.go` near
   `GetSpecTree` (line ~241): `GET /api/graph`, Name `GetGraph`.

## Tests

- `internal/graph/*_test.go` (table tests against a fixture spec tree + tasks):
  - `TestBuilder_NodesAndEdges` — every spec/task becomes a node; the four edge
    kinds are emitted correctly (containment, dispatch, spec_dep, task_dep).
  - `TestBuilder_CriticalPath` — longest combined chain is correct on a
    branching fixture.
  - `TestBuilder_AvailableActions` — validated leaf spec → `dispatch`; ready
    backlog task → `start`; blocked task → none.
  - `TestBuilder_Archived` — archived nodes excluded unless requested.
- `internal/handler/graph_test.go`:
  - `TestGetGraph_Shape` — response has nodes/edges/critical_path/blocked
    (mirror `specs_test.go:514`).
  - `TestGetGraph_HiddenForMismatchedPrincipal` — scoping parity with
    `config_test.go:332`.

## Boundaries

- Do NOT touch the frontend in this task; the wire contract is the deliverable.
- Do NOT add new lifecycle states or mutation routes — `AvailableActions` only
  *reports* what the existing transition/task APIs already allow.
- Do NOT add SSE streaming here (open question deferred); this is a plain GET.
