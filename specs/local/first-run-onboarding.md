---
title: First-Run Onboarding & Agent-Graph Discoverability
status: vague
depends_on: []
affects:
  - frontend/src/views/
  - frontend/src/components/
  - frontend/src/components/WorkspaceRequired.vue
  - frontend/src/views/AgentsPage.vue
  - frontend/src/views/FlowsPage.vue
  - docs/guide/getting-started.md
effort: large
created: 2026-06-28
updated: 2026-10-02
author: changkun
dispatched_task_id: null
---

# Feature: First-Run Onboarding & Agent-Graph Discoverability

> **Stale as of 2026-10-02:** the text below is the 2026-06-28 stub, unchanged.
> These statements in it are false today.
>
> - **"Blocked on the SDK foundation."** The embedded runtime, its trace, and
>   its live events shipped (`topos-runtime-integration`,
>   `topos-live-agent-events`, both complete). Nothing upstream blocks this
>   spec.
> - **"Replace the separate Agents and Flows pages with the unified graph."**
>   Done. One page at `/agent-graph` holds the agent registry and the fleet
>   editor, and `AgentsPage.vue` and `FlowsPage.vue`, both listed under
>   `affects`, are deleted (`d1b264f9`, `8a04ce68`). The record is
>   [unified-agent-graph-ui](../.archive/local/unified-agent-graph-ui.md).
> - **"Pinned vs dynamic regions, the peer directory."** The page does not
>   show regions or a peer directory. It shows one coordination choice per
>   fleet: Fixed sequence, Lead delegates, or Open mesh.
> - **"Live trace in the Map/`GraphCanvas`."** A run's trace renders in the
>   task detail (`AgentTrace.vue`), on the task timeline, and as a run overlay
>   on the agent-graph page. Mission Control does not render it.
> - **"Not just 'Pick a workspace'."** Partly addressed outside this spec:
>   the workspace dialog says what a workspace is (`188f5daa`), an empty Plan
>   names the next steps (`261df08e`), an empty Board opens the task composer,
>   and a browser check holds the first two in place (`56c7098b`).
>   `WorkspaceRequired.vue` still says only "Pick a workspace to begin".
>
> One decision blocks a refresh to `drafted`, and it is the maintainer's.
>
> 1. **The first useful action.** A new user can start from the Board (a
>    task), from Plan (a spec), or from Chat. The Board and Plan each carry
>    their own empty-state copy. A guided first run needs one of them chosen
>    as the path, or a statement that there is none and the work is copy only.
>
> Decided 2026-10-02: **agents and fleets are not part of a first run.** The
> agent-graph page is being removed
> ([retire-agent-fleets](../.archive/shared/platform-native/retire-agent-fleets.md)),
> so the half of this stub about teaching it has no subject. What remains is
> the first decision above, and it is best taken after the harness migration,
> when a first run also means signing in and choosing a model.

## Goal

A brand-new user can understand what Wallfacer is and get to a first useful action
without prior knowledge — and can understand the merged **agent graph** (Agents +
Flows unified per the Topos agent-SDK mesh foundation)
rather than facing two disjoint, jargon-heavy surfaces.

## Why this exists (and why it is deferred)

This is the **founding concern** of the agent-platform work: "a fresh user lands in
the app with no clue how to get started," and "agents and workflows are very
difficult to understand." That work pivoted first to getting the *engine* right
(the embeddable SDK, mesh discovery, per-region autonomy). Deferring onboarding was
deliberate — but it must not be lost. This stub is the tracked node so it is not
just prose.

Tension to resolve in the design: the new engine is **more** powerful (full-mesh
discovery, dynamic autonomy), which can *worsen* onboarding if surfaced raw. The
onboarding design has to make the powerful thing legible, not just expose it.

## Scope (to be designed — currently `vague`)

- First-run experience: what a user sees with no workspace / no tasks / no agents;
  a guided path to a first useful action (not just "Pick a workspace").
- Empty-state and explanatory copy across Board, Plan, Agents/Flows, Map that
  teaches the model (chat → spec → task → code) instead of assuming it.
- The merged **agent graph** UI: replace the separate Agents and Flows pages with
  the unified graph (pinned vs dynamic regions, the peer directory) the SDK exposes;
  make pinned (deterministic flow) vs dynamic (mesh) legible to a newcomer.
- Live trace in the Map/`GraphCanvas` as the place a user watches a run.

## Out of Scope

- The SDK engine itself (the upstream foundation spec owns that).

## Notes

Blocked on the SDK foundation landing enough of the merged-graph model to surface.
Pick up after the foundation's wallfacer-integration milestone.
