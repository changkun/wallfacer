---
title: Unified Agent-Graph UI (merge Agents and Flows)
status: drafted
depends_on:
  - local/topos-runtime-integration.md
affects:
  - frontend/src/views/
  - frontend/src/components/
  - frontend/src/components/map/
  - internal/handler/
  - internal/agents/
  - internal/flow/
effort: xlarge
created: 2026-06-28
updated: 2026-06-28
author: changkun
dispatched_task_id: null
---

# Feature: Unified Agent-Graph UI (merge Agents and Flows)

## Goal

Replace the two disjoint surfaces (Agents, a list of roles; Flows, a step composer)
with one **agent-graph** surface. Agents are nodes; composing them into a graph is
authoring a flow; the graph's shape is the topology (pinned chain or dynamic mesh);
running it overlays the live lineage. This is the founding goal: make "define an
agent", "compose a flow", and "watch a run" one understandable thing, powered by the
topos model already wired in (`topos-runtime-integration.md`).

This is the **full unified surface** (the chosen merge depth). It is built additively
first (a new view alongside the existing pages) so the working product is never
broken mid-build, then the old pages are retired once it proves out.

## The surface

A single view (provisional route `/agents`, eventually replacing both
`AgentsPage` and `FlowsPage`):

- **Left: agent library / palette.** The built-in + user agents (the merged registry,
  same data as today's AgentsPage). Search, create, clone, edit an agent. An agent is
  a card; editing it edits its YAML (the existing `~/.wallfacer/agents/*.yaml` via the
  agents CRUD API).
- **Center: the graph canvas.** The selected flow rendered as a graph of agent-nodes.
  Drag an agent from the palette onto the canvas to add a step; connect nodes to set
  order; the canvas IS the flow. Editing the graph writes the flow YAML (the existing
  `~/.wallfacer/flows/*.yaml` via the flows CRUD API: steps, parallel groups, and the
  new `agentic`/`dynamic`/`topology`/`max_handoff_depth` fields from M3).
- **Topology controls.** A flow toggles pinned (deterministic chain) vs dynamic
  (mesh), sets the topology (orchestrator-worker | mesh) and the handoff-depth bound.
  These map directly to the topos `Region`/`Options` the runner already builds.
- **Run overlay.** When a task runs an agentic flow, overlay its lineage (the
  `AgentLineage` data from M5: node status, delegate/deliver/next edges) on the same
  canvas, so a live mesh handoff is visible on the graph that authored it. Reuse the
  M5 lineage endpoint.

The canvas reuses the existing `GraphCanvas` patterns (hand-rolled SVG, RAF-batched
drag, curved edges) where it fits; a new graph component is acceptable if entangling
with the spec/task GraphCanvas is worse than a focused agent-graph renderer. Do not
regress the Map's spec/task graph.

## Data flow

Nothing new is invented for storage: agent nodes <-> agents YAML registry; the graph
<-> flows YAML registry; the run overlay <-> the M5 task-lineage endpoint. The UI is a
graph editor over the two existing registries plus a lineage overlay. Any new backend
is thin (e.g. a combined read for the editor); prefer the existing agents/flows CRUD.

## Milestones (built additively, reviewed visually each step)

- **M6.1: read-only scaffold. DONE** (`4528f636`, `b0032915`). New `/agent-graph`
  route + `AgentGraphPage`: searchable agent palette + flow picker + `AgentGraphCanvas`
  (a focused read-only SVG renderer, forked rather than reusing `GraphCanvas` which is
  bound to the spec/task model). Renders nodes per step, order edges, parallel
  siblings, a topology indicator. Additive (new nav entry; AgentsPage/FlowsPage/
  GraphCanvas byte-identical). Also fixed `GET /api/flows` to serialize the M3 agentic
  fields so the topology indicator reflects real flows. **Needs the author's visual
  review before M6.2.**
- **M6.2: graph editing.** Drag-from-palette to add a step, connect/reorder, mark
  parallel/optional, set agentic + topology + depth; persist to the flow YAML via the
  existing flow CRUD. Edit an agent node -> the agent editor (existing).
  - **M6.2a DONE** (`4a67b18e`). Backend: `POST/PUT /api/flows` accept the agentic
    fields (agentic/dynamic/topology/max_handoff_depth), validated against the
    topology enum + a non-negative depth, so the editor can persist topology.
  - **M6.2b DONE.** Frontend editable scaffold: a flow is cloned (built-ins are
    read-only -> POST a new user flow) or edited in place (-> PUT) into a draft
    (`lib/flowDraft.ts`, pure + unit-tested); palette cards become draggable;
    dropping an agent on the canvas appends a step (duplicate-agent guarded); a
    name/slug + Save/Cancel toolbar persists via the flow CRUD. Validated
    same-origin end-to-end (clone/edit -> drag-add -> save -> persisted YAML).
    The read-only render path (and its M6.1 test) is unchanged.
  - **M6.2c DONE.** Per-node remove: the canvas takes an `editable` prop and
    emits `remove` (keyed by agent_slug); a hover × on each step node deletes
    it, pruning any `run_in_parallel_with` references so the flow stays valid
    (`removeStep` in `lib/flowDraft.ts`, unit-tested; wiring component-tested).
  - **M6.2d DONE.** Topology toolbar: Agentic / Dynamic toggles, an
    orchestrator-worker|mesh select, and a handoff-depth input, bound to the
    draft and serialized by `draftToPayload` onto the M6.2a flow fields. The
    canvas topology indicator updates live from the draft (component-tested).
    Validated same-origin end-to-end: clone -> agentic + dynamic + mesh + depth
    -> save -> the flow round-trips as `agentic:true, topology:mesh, depth:4`.
  - **Remaining:** node-level drag to reorder / mark parallel, and
    edit-an-agent-node -> the agent editor.
- **M6.3: run overlay.** Overlay a running/finished agentic task's lineage on the
  canvas (status colors, handoff edges), polling/streaming the M5 endpoint.
- **M6.4: retire the old pages.** Point the Agents/Flows nav at the unified surface;
  remove or redirect `AgentsPage`/`FlowsPage` once parity is confirmed. (Pairs with
  the onboarding spec: first-run lands here.)

## Test strategy

- Frontend: `bun run build` (vite + vue-tsc) green at every slice; component tests
  (vitest) for the palette, the graph render from a flow, the edit->YAML round-trip,
  and the lineage overlay. I cannot verify pixels, so each slice is reviewed visually
  by the author before the next.
- Backend: any new/changed handler has a Go test; existing agents/flows/lineage
  handlers must not regress.
- The topos import guard and the integration tests stay green (this is UI over the
  existing registries + the M5 endpoint; it adds no topos import).

## Out of scope

- Changing the topos runtime or the execution path (done in M1-M5).
- The first-run onboarding flow (its own spec; it builds on this surface).

## Risks

- **Built blind (no pixel feedback in the agent loop).** Mitigate by building
  additively, shipping each slice behind the new route, and having the author review
  visually before retiring anything. Never break the working Agents/Flows pages until
  M6.4 parity is confirmed.
- **Entangling with the Map's `GraphCanvas`.** If reuse fights the spec/task graph, a
  focused agent-graph renderer is preferable to overloading GraphCanvas.

## Notes

The UX merge that motivated the whole topos effort. Builds entirely on shipped pieces:
the agents/flows YAML registries, the M3 flow fields, and the M5 lineage endpoint.
