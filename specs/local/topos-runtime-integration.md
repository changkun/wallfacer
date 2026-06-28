---
title: Topos Runtime Integration (Embed the Agent-Graph Runtime)
status: drafted
depends_on:
  - ../../agents/specs/architecture/agent-sdk-mesh-foundation.md
affects:
  - go.mod
  - internal/runner/
  - internal/flow/
  - internal/agents/
  - internal/graph/
  - internal/handler/
  - internal/store/
  - frontend/src/views/
  - frontend/src/components/map/
effort: xlarge
created: 2026-06-28
updated: 2026-06-28
author: changkun
dispatched_task_id: null
---

# Feature: Topos Runtime Integration (Embed the Agent-Graph Runtime)

## Goal

Embed the public embeddable runtime SDK `latere.ai/x/topos` into the wallfacer
backend as a new in-process execution path, and surface its deterministic lineage
as a live agent graph. This is the foundation for merging the separate Agents and
Flows surfaces into one agent-graph model: a flow becomes a region of agents, a run
produces a lineage graph, and the Map renders it live.

## Why now

The runtime is extracted, public, and stable (`latere.ai/x/topos` v0.0.4: pure SDK,
linted, covered). wallfacer already consumes sibling latere modules (`agon`, `pkg`),
so the dependency pattern exists. The current Agents and Flows pages are two disjoint
surfaces; topos gives one model (agents, regions, topology, delegation, lineage) that
unifies them and powers an actual multi-agent run, not just a static pipeline.

## Approach

Five layers, smallest first.

**1. Wiring + boundary.** Add `require latere.ai/x/topos v0.0.3` to go.mod. During
co-development use a `go.work` (wallfacer + ../topos). Add a consumer import guard (a
test that fails if wallfacer imports anything other than the root `latere.ai/x/topos`
package, not the engine subpackages) to keep the boundary the embeddable SDK intends.

**2. Execution seam.** Add a topos execution path in `internal/runner/execute.go`,
beside the existing brainstorm / flow-engine / implement branches. A new flow kind
(`agentic`, resolved by `Registry.ResolveForTask`) routes a task to a topos run:
build a `topos.Region` from the flow + the agents registry, call
`topos.NewRunner(opts).Run(ctx, region, task.Prompt)`, and map the `RunResult`
(final text + lineage) back onto the task through the same
`in_progress -> waiting -> committing -> done` state machine. Non-topos flows are
unchanged.

**3. Flow/agents -> region adapter.** Map `flow.Flow.Steps` and `agents.Role`
(`internal/flow`, `internal/agents`) onto `topos.AgentSpec`/`Region`: a pinned flow
is a deterministic chain (`Autonomy: Pinned`); a flow marked dynamic exposes its
agents as a peer directory (`Autonomy: Dynamic`, `Topology` from a flow field).
`Role.PromptTmpl`/`Capabilities`/`Harness` map to `AgentSpec` system prompt, scopes,
and tools.

**4. Model + sandbox wiring.** Configure `topos.ModelOptions` from wallfacer's
existing credential/harness settings, routed through Lux. Provide a `topos.Sandbox`
(`Options.Sandbox`) either as the topos local sandbox or an adapter over wallfacer's
`executor.Backend`, so a topos run executes tools where wallfacer already runs work.

**5. Live lineage in the Map.** Persist the `topos.Lineage` (nodes: id/name/role/
status/grants/sandbox; edges: delegate/deliver/next) on the task (a new field or
`Task.Result`). Extend `internal/graph` + `GET /api/graph` (or a task-scoped endpoint)
to return it, and render it in `GraphCanvas` as a sub-graph under the task node, so a
running mesh handoff is visible live. The unified Agents/Flows UI builds on this:
editing the graph edits the underlying agents/flows YAML registries.

## Milestones

- **M1: wiring + guard. DONE** (`84be42bf`). `internal/agentgraph` is the single seam
  importing the root topos package; `go.mod` requires `topos v0.0.4`; a `go.work`
  (gitignored) uses local `../topos`; the import-guard test enforces the boundary.
- **M2: execution seam (headless). DONE** (`a8abfa3b`). A flow flagged
  `flow.Flow.Agentic` runs in-process via `agentgraph.RunFlow` and the topos fake
  model, persists the lineage to a typed `Task.Lineage` JSON field, and drives the
  existing `in_progress -> waiting -> committing -> done` state machine with zero
  container launches. The runner consumes a topos-free `Result` mirror, so only
  `internal/agentgraph` names a topos type. Existing flows unchanged; tested.
- **M3: flow/agents adapter (depth).** M2 landed the pinned `FromFlow` mapping
  (`Role -> AgentSpec`, entry + peers chain). Remaining: the **dynamic/mesh** path
  (a flow opts into `Autonomy: Dynamic` + `Topology`, peers as a directory) and a
  richer `Role -> AgentSpec` mapping (Harness/Capabilities -> tools/scopes).
- **M4: model + sandbox.** Lux model wiring; sandbox via `Options.Sandbox`.
- **M5: live lineage in the Map.** Persist + serve + render the lineage sub-graph.
- **M6: unified Agents/Flows graph UI.** Merge the two pages into the agent-graph
  editor over the same YAML registries. (Pairs with the onboarding spec
  [first-run-onboarding.md](first-run-onboarding.md).)

## Test strategy

- M1: the import-guard test (compile-time boundary).
- M2-M4: backend tests using the topos `ModelFake` so runs are deterministic and need
  no network; assert task state transitions and the persisted lineage.
- M5: graph endpoint returns the lineage; a frontend test renders the sub-graph.
- No change to existing flow execution paths is allowed to regress (run the current
  runner/flow tests).

## Out of scope

- Changing wallfacer's existing flow/harness execution for non-topos flows.
- The first-run onboarding UX (its own spec).
- Cloud/hosted execution of topos runs (local in-process first).

## Open questions

- **OQ-1**: sandbox strategy. Use the topos local sandbox, or adapt
  `executor.Backend` to `topos.Sandbox`? Local is simplest for M2-M4; the adapter
  lets a topos run share wallfacer's container/host execution. Decide at M4.
- **OQ-2 RESOLVED** (M2): lineage is a typed `Task.Lineage *string` JSON field (an
  opaque string so the store never imports topos), keeping `Task.Result` for the
  agent's final text. `omitempty` means non-agentic tasks serialize byte-identically.
- **OQ-3**: how a dynamic flow declares topology (orchestrator-worker vs mesh) and the
  handoff-depth bound in the flow YAML schema.

## Notes

Builds on the embeddable SDK foundation
([agent-sdk-mesh-foundation](../../agents/specs/architecture/agent-sdk-mesh-foundation.md)).
The runtime is consumed only through the root `topos` package; the engine subpackages
stay an implementation detail behind the import guard.
