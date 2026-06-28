---
title: Agent Graph — end-to-end design and teardown of the legacy flow mechanism
status: drafted
depends_on:
  - local/unified-agent-graph-ui.md
  - local/topos-runtime-integration.md
affects:
  - internal/flow/
  - internal/runner/
  - internal/agentgraph/
  - internal/agents/
  - internal/handler/
  - frontend/src/
  - latere.ai/x/topos (possibly)
effort: xlarge
created: 2026-06-28
updated: 2026-06-28
author: changkun
dispatched_task_id: null
---

# Agent Graph — end-to-end design and teardown of the legacy flow mechanism

## Why this document

The agent-graph UI was built in isolation and does not cohere with how tasks
actually run. Author review surfaced concrete breakage (parallel agents drawn as
a false linear order), missing interactions (no way to re-add a removed agent,
no edge editing in mesh, sequence frozen while fleets are free-form), and the
deeper gap: the graph is not wired to the task board, and the board still speaks
"flow". This document reevaluates the whole surface against the code as it
actually is, defines a coherent end-to-end target, specifies one consistent
editing model, and plans a solid teardown of the old and suboptimal mechanism.
No code is written until this design is accepted.

## Current reality (grounded in the code, not assumptions)

### Three execution engines, one of them the real workhorse

Dispatch lives in `internal/runner/execute.go` (~L206-280). A task's flow slug
resolves via `flow.Registry.ResolveForTask` (FlowID, else legacy Kind, else
`implement`), then branches:

1. **Agentic / topos fleet** — `flow.Agentic == true` -> `runAgenticFlow`
   (`internal/runner/agentic.go`) -> `agentgraph.RunFlowWithModel` ->
   `topos.Region` (entry + peers, pinned or dynamic). Persists lineage. **Fully
   wired end to end, but used only by test fixtures.** No built-in flow is
   agentic; the only way to get an agentic flow is to author one via
   `POST /api/flows {agentic:true}`, which only the agent-graph editor does.
2. **Legacy flow engine** — non-agentic, non-`implement` -> `flow.engine.Execute`
   (`internal/flow/engine.go`): walks `buildParallelGroups(steps)`, running
   parallel siblings concurrently via `errgroup`. **No such flow exists in
   production** (the only built-in is `implement`), so this path is effectively
   dormant.
3. **Legacy multi-turn loop** — `flowSlug == "implement"` stays in the runner's
   own multi-turn agent loop (not the engine). **This is what actually runs for
   every real task**, since `implement` is the only built-in and the default.

So the workhorse (`implement`) runs on neither the flow engine nor the topos
fleet path. The "fleet" the agent-graph edits is a model the default task never
touches.

### Three overlapping surfaces, split vocabulary

- **Agents** (`AgentsPage.vue`, `/agents`) — agent CRUD.
- **Workflows** (`FlowsPage.vue`, `/workflows`, nav label "Workflows") — flow
  CRUD as a form/step composer.
- **Agent Graph** (`AgentGraphPage.vue`, `/agent-graph`) — the new visual fleet
  editor; also edits flows, additionally exposing agentic/topology.

FlowsPage and AgentGraphPage both edit flows (form vs canvas). AgentGraphPage
links out to AgentsPage to edit an agent. Terminology is split four ways: nav
**Workflows**, route **flows**, backend **flow**, new UI **fleet**.

### The board is not wired to fleets

`TaskComposer.vue` has a "Flow" dropdown (default `implement`) that sends a
`flow` slug -> stored as `Task.FlowID`. It never exposes agentic/topology, so a
user cannot create a delegating-fleet task from the board; they create tasks
blind to how they will run. `Task.flow_id` is stored but never shown. There is
no task -> graph link; the agent-graph can list a fleet's runs but the board
does not point back.

### Dead / vestigial

`flow.SpawnKind` and `Task.RoutineSpawnKind` are retained for YAML wire
compatibility but unused. Legacy `Kind`-> flow mapping survives only to keep old
records dispatching. `TaskKindPlanning`/`TaskKindRoutine` are not mapped by the
resolver (fall through to `implement`).

## The core decision: what is the primitive, and how many execution engines

The founding goal (define agents; define how they talk; let them discover and
delegate; spawn subagent graphs) points at one primitive: an **agent graph** — a
named set of agents with a lead and a coordination policy, that a task is handed
to and that works the task to an outcome. The honest tension is execution:

- The **delegation fleet** (topos) is what the user wants conceptually, but the
  proven coding workhorse (`implement`) is the legacy multi-turn loop, which also
  owns worktrees, the commit pipeline, agon verification, and oversight. Topos
  runs agents but has not been shown to replicate that wallfacer-specific
  machinery.
- The **deterministic DAG** (steps + parallel) is clear and proven for ordered
  work, but it is the "flow/pipeline" framing the author finds confusing.

**Recommendation (north star + de-risked path):** one authoring primitive (the
**agent graph**) and a commitment to converge on **one execution engine (topos)**
— but staged behind a feasibility spike, so the unified, coherent product ships
before the risky execution migration. Concretely:

- The agent graph is the single thing a user authors. It carries its
  **coordination**: *delegating* (lead/mesh, runs on topos) or *deterministic*
  (ordered DAG with parallel groups, the simple case). Both are "agent graphs";
  the edges differ in meaning and are labelled as such.
- A task runs on a chosen agent graph. The board picks a graph (not a "flow").
- Execution converges on topos over time: a spike proves whether topos can carry
  the `implement` loop's duties (worktree, commit, agon, oversight, multi-turn
  coding). If yes, `implement` becomes a built-in agent graph on topos and the
  legacy loop + flow engine are torn out. If not, the two execution semantics are
  formalized cleanly under one surface and one dispatch, and the spike result
  scopes a follow-up.

This keeps the user's model (graphs, delegation, a task worked to an outcome),
delivers a coherent surface immediately, and makes the execution teardown an
explicit, gated step rather than a leap.

## Target end-to-end

### Concept

- **Agent** — a role (prompt + tools + harness). Authored once; reusable.
- **Agent graph** — a named graph of agents: a **lead** (the entry that receives
  the task), members, and a **coordination policy**. Replaces "flow" entirely.
- **Task** — work assigned to an agent graph. Enters at the lead; the graph works
  it to an outcome; the run's lineage is the graph lit up by what actually
  happened.
- **Run / lineage** — the executed graph (status per node, real delegations).

### Authoring surface (one surface)

The Agent Graph page is the single authoring surface and absorbs both
AgentsPage and FlowsPage:

- **Left: agent library.** Create/edit/clone/delete agents inline (absorb
  AgentsPage's editor as a panel, so there is no separate page and no cross-page
  navigation just to tweak a prompt).
- **Center: the graph canvas.** Compose the graph: drag agents in, set the lead,
  draw edges, pick coordination. One consistent, free-form canvas (see editing
  model). The canvas IS the graph definition.
- The graph persists through one CRUD (the flow store, renamed in concept to the
  graph store; see teardown for the rename plan).

### Board integration (the missing wire)

- The composer picks an **agent graph**, not a "flow"; the label and the data
  speak the same word. Default is the built-in `implement` graph.
- A task card / detail shows which graph it ran and links to the graph with its
  run overlaid (the M6.3 overlay, reached from the task, not only from the graph
  page).
- Whether a task runs delegating or deterministic is a property of the chosen
  graph, not a hidden surprise — the composer shows the graph's coordination.

### Execution (staged convergence on topos)

- Short term: dispatch keeps the existing branches but is presented coherently;
  the agent-graph the user sees matches the engine that runs it (a deterministic
  graph renders order/parallel; a delegating graph renders delegation).
- Target: a single topos-based engine. The spike (Milestone S below) decides the
  timeline. The legacy flow engine (`internal/flow/engine.go`) and the special
  `implement` multi-turn loop are the teardown targets.

### Vocabulary (one word)

Pick one user-facing term and use it everywhere: **agent graph** (with "fleet"
as acceptable shorthand in prose, never in UI chrome). Eliminate "flow" and
"workflow" from all UI, routes, and (eventually) the API. See teardown.

## The unified editing model (answers the review gaps)

One model for the canvas, applied in every coordination mode — no special-casing:

- **Free-form everywhere.** Every node is freely positioned by dragging, in all
  modes (including the deterministic graph). Positions are part of the graph
  definition and persist with it (promoted out of localStorage into the saved
  model, so layout travels and is not lost). The auto-layout is only the initial
  placement for a graph that has no saved positions.
- **Parallel rendered as parallel, never as false order.** In a deterministic
  graph, agents with no ordering between them (the `implement` commit-msg / title
  / oversight trio) render as a parallel fan from their common predecessor — not
  a line. The edge meaning in a deterministic graph is "then / feeds", and
  concurrent siblings share a rank. The current linear flattening is a bug to
  delete.
- **Edges are first-class and editable.** Draw an edge by dragging from a node's
  out-port to another node; delete by selecting the edge. In a delegating graph
  an edge means "may delegate to"; in a deterministic graph it means "runs
  after / feeds". This requires storing **explicit edges** for delegating graphs
  rather than only a `topology` enum (mesh/orchestrator-worker become presets
  that seed edges, not the only expressible shapes). NOTE: arbitrary delegation
  adjacency may require extending the topos `Region` model (today: entry + peers
  + topology enum) to a per-agent reachability/directory; flagged as a topos-level
  change with its own spike.
- **Add / remove / undo are obvious.** Removing a node moves it back to the
  palette (it is not destroyed); re-adding is dragging it back, and there is an
  explicit undo for graph edits. The palette always shows every agent, so nothing
  is ever stranded.
- **Lead is explicit and movable** (already have promote-to-lead); the lead is
  the task entry in every mode.
- **Agon is not a graph node.** Agon is the Testing agent's internal verification
  (critic/proposer rounds), surfaced as a detail of the test node (a badge /
  drill-in), not a box on the graph. Document this in the node detail so it stops
  reading as a missing concept.

## Teardown of the old and suboptimal design (first-class, not an afterthought)

Staged so the tree compiles and the product stays usable at each step; nothing
destructive lands before its replacement is proven.

1. **Surface teardown.** Fold AgentsPage's editor into the agent-graph library
   panel; retire `FlowsPage.vue` and its `/workflows` + `/flows` routes
   (redirect to the agent graph); collapse the sidebar's Agents + Workflows +
   Agent Graph into a single entry. Delete `FlowsPage.vue` once the graph surface
   has full parity (it now has clone/edit/save/delete; remaining: agent CRUD
   inline). Keep the components in git history; remove from the build.
2. **Terminology teardown.** Remove every user-facing "flow"/"workflow" string
   (inventory exists: `TaskComposer.vue`, `Sidebar.vue`, `FlowsPage.vue`,
   `RoutinesPage.vue`, `router.ts`) and rename to "agent graph". Stage the API
   rename (`/api/flows` -> `/api/agent-graphs`, `Task.flow` -> `graph`) behind a
   compatibility shim, then drop the shim.
3. **Execution teardown (gated on spike S).** If topos can carry the `implement`
   duties: delete `internal/flow/engine.go` (the dormant flow engine), delete the
   special `implement` multi-turn loop branch in `execute.go`, and make
   `implement` a built-in agent graph that runs through `agentgraph`/topos. This
   is the big one and the main payoff of the cleanup — one engine, not three.
4. **Data-model teardown.** Remove `flow.SpawnKind`, `Task.RoutineSpawnKind`, and
   the legacy `Kind`->flow mapping once no records depend on them (a one-time
   migration normalizes old rows to an explicit graph id). Collapse `TaskKind` to
   what is actually dispatched.
5. **Vestigial UI teardown.** Remove the localStorage position hack once
   positions live in the saved graph; remove the agentic/dynamic/topology raw
   controls in favor of the coordination + edge-editing model; remove dead flow
   badges / pickers.

Each teardown step ships with a regression test proving the capability survives
(per the repo's test rule) and a `make build` gate.

## Milestones (sequenced; design-review gates the build)

- **D: this design.** Accepted by the author before any code.
- **S: execution feasibility spike.** Can topos run the `implement` job
  (worktree, commit pipeline, agon, oversight, multi-turn coding) end to end?
  Time-boxed; output is a go/no-go that scopes Milestone 3 of the teardown.
- **A1: editing model fix (no execution change).** Parallel-as-parallel,
  free-form everywhere, explicit editable edges (frontend model first; topos
  adjacency spike if needed), add/remove/undo, agon-as-detail. Makes the editor
  correct and operable.
- **A2: surface + terminology unification.** Fold agent CRUD in; retire
  FlowsPage; one nav entry; "agent graph" everywhere (UI first, API shim).
- **A3: board wiring.** Composer picks an agent graph (shows coordination); task
  shows + links its run on the graph; default `implement` graph.
- **A4: execution convergence (gated by S).** Converge on topos; tear out the
  flow engine and the special implement loop; `implement` becomes a graph.
- **A5: final cleanup.** Data-model teardown, API rename drop-shim, vestigial UI
  removal, dead-code sweep.

## Open questions for the author

1. **One model or two semantics?** Commit to "everything becomes a delegating
   fleet on topos" (max cleanup, riskier — gated by spike S), or keep two clear
   coordination semantics (deterministic DAG + delegating fleet) under one
   surface (less risky, two engines longer)? The plan above assumes the former as
   the north star with a spike gate; confirm.
2. **Editable arbitrary delegation edges** likely needs a topos `Region` change
   (from topology-enum to explicit adjacency). Acceptable to touch topos, or keep
   to the orchestrator-worker / mesh presets for now?
3. **Agents page**: fold entirely into the graph surface (no `/agents`), or keep
   `/agents` as a secondary list? Plan assumes fold-in.
4. **`implement` semantics**: it is a multi-turn coding loop, not a clean
   single-turn fan. When it becomes a graph, is it a one-node graph (the coding
   agent) that then delegates to test/commit/title/oversight, or a fixed
   deterministic graph? This shapes both rendering and execution.

## Notes

This supersedes the framing in `unified-agent-graph-ui.md` (which built the
editor before the board/execution wiring and before the fleet reframe was
validated against the real engines). That spec's shipped pieces (palette, canvas,
CRUD, run overlay, fleet rendering) are reused; this document re-roots them in an
end-to-end model and an explicit teardown.
