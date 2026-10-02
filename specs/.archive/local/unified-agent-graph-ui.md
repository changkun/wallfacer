---
title: Unified Agent-Graph UI (merge Agents and Flows)
status: archived
depends_on:
  - specs/local/topos-runtime-integration.md
affects:
  - frontend/src/views/
  - frontend/src/components/
  - frontend/src/components/map/
  - internal/handler/
  - internal/agents/
  - internal/flow/
effort: xlarge
created: 2026-06-28
updated: 2026-10-02
author: changkun
dispatched_task_id: null
---

# Feature: Unified Agent-Graph UI (merge Agents and Flows)

> **Archived 2026-10-02: shipped.** The single agent-graph surface this spec
> set out to build is in the code at `/agent-graph`, and the Agents and Flows
> pages it replaced are deleted. The [Outcome](#outcome-2026-10-02) section
> records what shipped, what shipped differently, and what was dropped.
>
> A note placed here on 2026-07-18 called this spec superseded by a
> consolidation of the agent-graph model onto `latere.ai/x/topos/graph`. That
> consolidation changed how a flow is compiled for the embedded runtime
> (`internal/agentgraph/graph.go`) and left the surface as this spec built it.
> The note also said wallfacer retires its flow runtime; `internal/flow` and
> its engine are still in the tree and still run.
>
> The text between this note and the Outcome is kept as written. Its commit
> hashes for M6.1 and M6.2a do not resolve in this repository; the Outcome
> cites the ones that do.

## Goal

Replace the two disjoint surfaces (Agents, a list of roles; Flows, a step composer)
with one **agent-graph** surface. Agents are nodes; composing them into a graph is
authoring a flow; the graph's shape is the topology (pinned chain or dynamic mesh);
running it overlays the live trace. This is the founding goal: make "define an
agent", "compose a flow", and "watch a run" one understandable thing, powered by the
topos model already wired in (`topos-runtime-integration.md`).

This is the **full unified surface** (the chosen merge depth). It is built additively
first (a new view alongside the existing pages) so the working product is never
broken mid-build, then the old pages are retired once it proves out.

## Reframe (2026-06-28): the primitive is an agent FLEET, not a pipeline

Author review of M6.1-M6.2 surfaced that "flow as an ordered pipeline of steps"
is the wrong mental model and is confusing. The chosen primitive is an **agent
fleet (delegation graph)**, matching the founding goal: define agents, define how
they talk, let them discover and delegate to peers, spawn subagent graphs. The
backend already runs exactly this (`agentgraph.FromFlow`: `steps[0]` is the fleet
**lead/entry**, the rest are **peers**; `dynamic` + topology gates who may
delegate to whom). The reframe is in the UI's concepts and rendering, not the
data model or runtime.

- A node is an agent; an **edge means "can delegate to / hand off to"**, NOT
  "runs after". A task enters at the lead and the fleet works it to an outcome by
  delegating.
- **Coordination** (one control, replacing the agentic/dynamic/topology toggles
  as the user-facing concept): **Lead delegates** (orchestrator-worker: only the
  lead hands off to workers) | **Open mesh** (any agent delegates to any peer,
  bounded by handoff depth) | **Fixed sequence** (the legacy pinned chain: runs
  the agents in order; the simple deterministic special case). These map onto
  `dynamic=false` (Fixed sequence) and `dynamic=true` + `topology` (the two
  delegation modes).
- The first agent is the **lead**; the rest are members. Lead delegates / mesh
  render delegation edges from the coordination mode; Fixed sequence keeps the
  ordered-chain rendering for the simple case.
- Pipeline-only gestures (mark-parallel, reorder by stage gaps) apply only to
  Fixed sequence; a delegating fleet has no inherent order, so members are just
  positioned (free-form, the author's arrangement).
- Language throughout speaks fleet terms (lead, members, delegates to, "a task
  enters the fleet and is worked to an outcome"), not flow/step/pipeline.

Storage is unchanged: a fleet persists as a flow (lead = step 0, members =
the rest, coordination = dynamic + topology) through the existing CRUD.

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
- **Run overlay.** When a task runs an agentic flow, overlay its trace (the
  `AgentTrace` data from M5: node status, delegate/deliver/next edges) on the same
  canvas, so a live mesh handoff is visible on the graph that authored it. Reuse the
  M5 trace endpoint.

The canvas reuses the existing `GraphCanvas` patterns (hand-rolled SVG, RAF-batched
drag, curved edges) where it fits; a new graph component is acceptable if entangling
with the spec/task GraphCanvas is worse than a focused agent-graph renderer. Do not
regress the Map's spec/task graph.

## Data flow

Nothing new is invented for storage: agent nodes <-> agents YAML registry; the graph
<-> flows YAML registry; the run overlay <-> the M5 task-trace endpoint. The UI is a
graph editor over the two existing registries plus a trace overlay. Any new backend
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
  - **M6.2e DONE.** Mark parallel via node drag: step nodes are pointer-draggable
    (SVG does not fire HTML5 dragstart reliably, and a pointer model generalizes
    to reordering), and dropping one node on another groups them into a parallel
    stage (`setParallel` merges the transitive groups into a fully-mutual,
    contiguous column). A per-node ungroup control pulls a step back out
    (`clearParallel`, dissolving a singleton remainder). Pure ops unit-tested,
    ungroup wiring component-tested, the drag validated in a real browser:
    drag -> group -> save round-trips `run_in_parallel_with`.
  - **M6.2f DONE.** Reorder by node drag: while a node drags, gap drop-zones
    appear between stages; dropping a node in a gap moves its whole parallel
    stage to that position (`moveStage` over `stagesOf`, keeping groups intact),
    while dropping on a node still groups (nodes win the hit-test). Pure ops
    unit-tested; the drag + reserved trailing gap validated in a real browser:
    drag -> reorder -> save round-trips the new step order.
  - **M6.2g DONE.** Edit-an-agent-node -> the agent editor: double-clicking a
    step node or a palette agent (in the read-only graph, so an in-progress flow
    draft is never lost to navigation) routes to `/agents?agent=<slug>`, and
    AgentsPage reads that query to open the agent. Validated in a real browser.
  - **M6.2 COMPLETE.** The agent-graph canvas is a full flow editor: clone/edit,
    drag-to-add, remove, mark-parallel, ungroup, reorder, the agentic/topology
    controls, and a jump to the agent editor -- all persisting through the flow
    and agents CRUD.
- **M6.3: run overlay. DONE** (fleet model). A read-only run picker lists the
  selected fleet's agentic runs (tasks with `flow_id` == the fleet + a trace);
  choosing one fetches `GET /api/tasks/{id}/trace` and colors the agent nodes
  by status (running / done / failed), matched by trace node name == agent
  slug. Component-tested (filter by fleet, name-keyed status coloring).
- **M6.4: retire the old pages. PARITY DONE; cutover deferred.** The unified
  fleet surface now has full CRUD parity with `FlowsPage`: clone/edit/save (M6.2),
  delete a user fleet (inline two-step confirm), the run overlay (M6.3), and a
  jump to the agent editor for per-agent edits (so `AgentsPage` stays as the
  agent CRUD surface the fleet links to). The remaining step is the user-facing
  nav cutover (redirect `/workflows` -> the fleet surface, drop the Workflows
  item). That is a one-way change held for the author's go-ahead, since the
  fleet reframe is under active review; the `FlowsPage`/`AgentsPage` components
  are untouched so the cutover stays a trivial, reversible follow-up.

## Test strategy

- Frontend: `bun run build` (vite + vue-tsc) green at every slice; component tests
  (vitest) for the palette, the graph render from a flow, the edit->YAML round-trip,
  and the trace overlay. I cannot verify pixels, so each slice is reviewed visually
  by the author before the next.
- Backend: any new/changed handler has a Go test; existing agents/flows/trace
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
the agents/flows YAML registries, the M3 flow fields, and the M5 trace endpoint.

## Outcome (2026-10-02)

**Summary.** Implemented directly between 2026-06-28 and 2026-06-30, with a
restyle on 2026-09-05. One page, `frontend/src/views/AgentGraphPage.vue` at
`/agent-graph`, holds the agent registry, the fleet editor, and the run
overlay. `FlowsPage.vue`, `AgentsPage.vue`, and `flows.css` are deleted;
`/agents`, `/workflows`, and `/flows` redirect to `/agent-graph`
(`frontend/src/router.ts`), and the rail has one entry, labeled "Agents"
(`frontend/src/lib/nav.ts`). The surface is documented in
[docs/guide/agent-graph.md](../../../docs/guide/agent-graph.md).

**What shipped.**

- **M6.1, read-only scaffold** (`b4b9a472`, `eb200bdb`): the route, the page,
  the searchable palette, the fleet picker, and
  `frontend/src/components/AgentGraphCanvas.vue`, a focused SVG renderer
  separate from the Map's `GraphCanvas`. `GET /api/flows` serializes the
  `agentic`, `dynamic`, `topology`, and `max_handoff_depth` fields.
- **M6.2a, writable execution fields** (`b3e676ce`): `POST` and `PUT
  /api/flows` accept and validate those fields
  (`internal/handler/flows.go`).
- **M6.2b to M6.2d, editing** (`e0c9f05b`, `26bfb956`, `feca2eec`): clone a
  built-in or edit a user fleet into a draft, drag an agent from the palette
  to add it, remove a node, and persist through the flow CRUD. The draft model
  is `frontend/src/lib/flowDraft.ts`.
- **M6.3, run overlay** (`a8445918`): a run picker lists the selected fleet's
  tasks that carry a trace, and `GET /api/tasks/{id}/trace` colors each agent
  node by its status in that run.
- **M6.4, parity and cutover** (`dd4afc5a`, `63e1e833`, `8a04ce68`,
  `e8639f7a`, `3d9b534a`, `d1b264f9`): delete a user fleet with an inline
  confirm; redirect `/workflows` and `/flows` and drop the Workflows nav
  entry; delete `FlowsPage.vue` and `flows.css`; extract
  `frontend/src/components/AgentEditor.vue` and embed it in the page as a
  dialog; delete `AgentsPage.vue`, redirect `/agents`, and collapse the nav to
  one entry.
- **Start from blank** (`8a5311c8`): a "New fleet" action opens an empty
  draft, so a fleet is no longer clone-only.
- **Restyle** (`15d8e13e`, `50f72e8e`): the split-pane styles left over from
  the Agents page were removed, and the page moved onto the console design
  system's rows, cards, and tokens under
  [console-redesign/agent-graph](../../shared/console-redesign/agent-graph.md).

**What shipped differently, and why.**

- **Route.** The provisional `/agents` became `/agent-graph`; `/agents` is a
  redirect. The page was built beside the old pages first, and the name stayed
  when they were retired.
- **The fleet reframe replaced the pipeline editor** (`da84d6e2`,
  `272f6fac`). After M6.2 the canvas was rebuilt around a lead and its
  members. The parallel-grouping and reorder gestures of M6.2e and M6.2f
  (`013c28f9`, `5be3ebee`) were removed in that rebuild, and their draft
  operations (`setParallel`, `clearParallel`, `stagesOf`, `moveStage`) were
  deleted as unused in `4908717d`. A draft in fixed-sequence coordination can
  add and remove agents; it cannot reorder them, group them in parallel, or
  mark one optional. A cloned fleet keeps the grouping it was cloned with.
- **One coordination control instead of three toggles** (`da84d6e2`). The
  Agentic and Dynamic toggles and the topology select of M6.2d became a single
  select, Fixed sequence, Lead delegates, or Open mesh, mapped onto the same
  flow fields by `coordinationOf` and `setCoordination`. The handoff-depth
  input shows for Open mesh only.
- **Agent editing is in place, not a route jump** (`3d9b534a`). M6.2g
  (`8943c4f4`) routed a double-click to `/agents?agent=<slug>`. The editor is
  now a dialog over the canvas, opened by double-clicking a palette row or a
  node, or by "New agent". This is what let the Agents page be deleted, which
  M6.4 as written kept.
- **Parallel agents draw as a fan** (`c6593a66`). The reframe had flattened a
  fixed sequence into a line. The canvas now groups steps into stages by the
  transitive closure of `run_in_parallel_with`, so the built-in `implement`
  fleet shows commit message, title, and oversight side by side.
- **Positions.** In the two delegating modes a node can be dragged anywhere.
  The position is stored per fleet in the browser's `localStorage`, not in the
  flow, so it does not travel with the fleet.
- **Vocabulary.** The page and its controls say "fleet" (`0f02e232`); the task
  composer and the routine form say "Agent graph"; the API and the YAML
  directory say "flow". The run graph was renamed from "lineage" to "trace"
  across the API, the store, and the UI (`9bed1f6b`, `dbc0896e`).
- **Delegating modes are labeled experimental** (`66a9a757`), following the
  execution findings in
  [agent-graph-e2e-design](agent-graph-e2e-design.md). That spec
  tracks what the label says against what the runner does.

**Dropped.**

- Connecting nodes to set order. Edges are derived from the coordination mode
  and the step list; none can be drawn or deleted.
- The pipeline gestures listed above (reorder and parallel grouping).
- Marking a step optional from the canvas. M6.2 listed it; no slice built it.
- A topology indicator on the canvas as a separate element. The coordination
  label above the canvas carries it.

**Tests.** `frontend/src/views/AgentGraphPage.test.ts`,
`frontend/src/components/AgentGraphCanvas.test.ts`, and
`frontend/src/lib/flowDraft.test.ts` cover rendering a fleet and a fixed
sequence, clone and edit, remove, promote to lead, the coordination control,
the run overlay, delete, and the blank draft.

**Follow-ups.** The remaining authoring work (editable edges, undo, positions
saved with the fleet, the link from a task to its fleet) is carried by
[agent-graph-e2e-design](agent-graph-e2e-design.md). First-run
guidance for this surface is
[first-run-onboarding](../../local/first-run-onboarding.md).

Later, 2026-10-02: user-authored agents and fleets were retired, and with them the page and engines this spec delivered. See [retire-agent-fleets](../../shared/platform-native/retire-agent-fleets.md).
