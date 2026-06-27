---
title: First-Run Onboarding & Agent-Graph Discoverability
status: vague
depends_on:
  - ../../agents/specs/architecture/agent-sdk-mesh-foundation.md
affects:
  - frontend/src/views/
  - frontend/src/components/
  - frontend/src/components/WorkspaceRequired.vue
  - frontend/src/views/AgentsPage.vue
  - frontend/src/views/FlowsPage.vue
  - docs/guide/getting-started.md
effort: large
created: 2026-06-28
updated: 2026-06-28
author: changkun
dispatched_task_id: null
---

# Feature: First-Run Onboarding & Agent-Graph Discoverability

## Goal

A brand-new user can understand what Wallfacer is and get to a first useful action
without prior knowledge — and can understand the merged **agent graph** (Agents +
Flows unified per
[agent-sdk-mesh-foundation](../../agents/specs/architecture/agent-sdk-mesh-foundation.md))
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
- Live lineage in the Map/`GraphCanvas` as the place a user watches a run.

## Out of Scope

- The SDK engine itself (the upstream foundation spec owns that).

## Notes

Blocked on the SDK foundation landing enough of the merged-graph model to surface.
Pick up after the foundation's wallfacer-integration milestone.
