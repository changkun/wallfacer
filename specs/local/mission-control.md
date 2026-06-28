---
title: Mission Control — Graph-Driven Coordination & Live Execution
status: complete
depends_on:
  - specs/local/map-mission-control.md
affects:
  - frontend/src/views/MapPage.vue
  - frontend/src/components/map/
  - frontend/src/router.ts
  - frontend/src/components/Sidebar.vue
  - frontend/src/stores/agentSession.ts
  - frontend/src/components/plan/SpecChatPopup.vue
  - internal/graph/
  - internal/handler/graph.go
  - docs/guide/
effort: large
created: 2026-06-28
updated: 2026-06-28
author: changkun
dispatched_task_id: null
---

# Mission Control — Graph-Driven Coordination & Live Execution

## Overview

The Map (shipped in [[map-mission-control]]) turned the orphaned dependency
graph into an actionable surface: a server-side unified spec+task graph with
inline dispatch/start. This spec takes it the rest of the way to its name — a
true **mission control**: (1) rename the surface so the UI says what it is, (2)
let a user drive the *whole* spec-coordination lifecycle from the graph (not
just dispatch), and (3) make a running pipeline *legible in motion* — nodes
animate as their tasks execute, and the active work is highlighted "you are
here" along the critical path.

The guiding constraint, top to bottom: the graph is a **context-setter and
launcher over flows that already exist** — the spec state machine, the
`/api/specs/transition` actions, the extracted chat core, and the task SSE
stream — not a new flow engine. Every piece below is designed as reuse.

## Current State

- **Surface:** `/map` → `MapPage.vue` + `components/map/` (network-node canvas,
  inspector with legend-as-filter, double-click `MapNodePopup` read-only spec
  view). Nav label "Map" in `Sidebar.vue`.
- **Graph model:** `internal/graph` builder emits per-node `available_actions`,
  but only two: `dispatch` (validated leaf spec) and `start` (ready backlog
  task). `GET /api/graph`.
- **Spec transitions (server, instant):** `POST /api/specs/transition` already
  supports `dispatch`, `undispatch`, `archive`, `unarchive`, `validate`,
  `stale`, `unstale`, `dismiss-stale`, `force-complete`, `migrate`
  (`internal/handler/specs_dispatch.go`); legality is governed by
  `internal/pkg/statemachine`.
- **Generative ops (agent, chat):** `/create /refine /validate /impact
  /break-down /review-breakdown /dispatch /review-impl /diff /wrapup`
  (`internal/agentsession/commands.go`) run the planner against the **focused
  spec**. The chat core is already extracted (`useChatSession`,
  `SpecChatPopup`) and "mounted by both surfaces" (see
  [[dedicated-chat-ui]]). **Key binding:** `useChatSession()` reads
  `agentStore.focusedSpecPath` (not a prop) and sends `focused_spec` with each
  message — so *setting that store value scopes the chat to a spec*.
- **Live state:** the Board consumes `/api/tasks/stream` (SSE) for live task
  status. The Map instead refetches the whole graph on a task-fingerprint
  watch, which is correct for structure but discards drag/pin state and cannot
  animate a transition.

## Architecture

Three independent workstreams over the shipped graph; none introduces a new
engine.

```mermaid
graph TD
  subgraph Rename
    R[/map -> /mission + redirect; nav 'Mission Control']
  end
  subgraph Coordination
    SM[internal/pkg/statemachine] --> GB[internal/graph: full legal
    available_actions per node]
    GB --> NM[Node action menu -> POST /api/specs/transition]
    AS[agentStore.focusedSpecPath] --> CH[mount SpecChatPopup
    focused on the node's spec]
  end
  subgraph Live
    TS[/api/tasks/stream SSE] --> LP[in-place node state updates]
    LP --> AN[client-side animation: pulse/ring/settle]
    CP[critical_path] --> YH['you are here' active-chain highlight]
  end
```

## Components

### 1. Rename: Map → Mission Control

- Route `/mission` with `/map` redirecting (mirror the Flows→Workflows rename
  in [[workflows-graph-ux]]). Nav label "Mission Control" (or "Mission" if the
  sidebar is tight) + page title/copy. Update `docs/guide/board-and-tasks.md`
  Map section. The component files may keep the `map/` directory name to avoid
  churn; only user-facing nouns change. (Decision recorded; rename ships with
  this work, not before.)

### 2. Spec coordination from the graph (reuse, two tiers)

**Tier A — server transitions as node actions (the cheap, high-value half).**
- Backend: extend the `internal/graph` builder so `available_actions` carries
  the *full legal transition set* for each node's current state, derived from
  `internal/pkg/statemachine` (which already encodes legality). E.g. a
  `drafted` spec → `validate`; `validated` leaf → `dispatch`; `complete` →
  `archive`/`mark-stale`; a dispatched spec → `undispatch`. The client renders
  whatever is available; it never re-derives lifecycle rules.
- Frontend: a node action menu (extend the inspector's existing action block;
  optionally a right-click / hover context menu on the node itself) lists the
  available transitions, each calling `POST /api/specs/transition`. Re-sync
  after via the live layer (§3) or a targeted refetch.

**Tier B — generative ops via focused-spec + the existing chat core.** Do
**not** grow `MapNodePopup` into a second chat host. Instead: selecting/acting
on a node sets `agentStore.focusedSpecPath = node.ref` and mounts the existing
`SpecChatPopup`. The slash commands (`/refine`, `/break-down`, `/review-impl`,
`/diff`, `/wrapup`, …) then operate on that spec unchanged. Likely surfacing: a
"Discuss / refine" affordance on the node and inside `MapNodePopup`, opening the
focused chat. (`MapNodePopup` stays the read-only viewer; the chat is the
shared popup.)

**Tier C — create from the graph (later).** A "+ new spec" affordance runs the
`/create` flow through the same chat (focus = none → `/create <title>`),
dropping the new node into the graph. Lower priority than A/B.

### 3. Live task visualization

- **Separate structure from state.** Subscribe to `/api/tasks/stream` and apply
  task status/activity deltas to nodes **in place**, preserving drag/pin
  positions. Re-run `computeLayout` only when the node/edge *set* changes (new
  spec/task/edge), not on a status change. This replaces the current
  refetch-the-whole-graph-on-any-change behavior.
- **Animate node states client-side:** pulse on `in_progress`, a progress
  ring/fill while running, an amber attention pulse on `waiting`, a settle/flash
  on `done`, a shake/desaturate on `failed`. Optionally animate the edges
  feeding an active node (dash-offset "flow") to show work moving downstream.
  Respect `prefers-reduced-motion`.
- **"You are here":** tie live active nodes to the already-computed
  `critical_path` — emphasize the active node *on the chain* so a viewer sees
  where in the idea→spec→task→done pipeline work currently is, and what's next.
  The existing critical-path feature does double duty.
- **Scope v1 deliberately:** animated node states + active-chain highlight is a
  clean first cut. Per-agent-step activity (current tool, turn count, live token
  burn, streaming log peek) is a much larger surface — named as a later phase,
  not designed into v1.

## API Surface

- `GET /api/graph`: `available_actions` widened to the full legal transition set
  per node (additive; existing `dispatch`/`start` remain).
- No new spec-mutation routes: reuse `POST /api/specs/transition` and the task
  transition routes.
- Live: reuse `/api/tasks/stream`; consider whether structural changes need a
  graph-level SSE or whether task-stream + the existing spec-tree stream
  suffice (see Open Questions).

## Phased Breakdown (to be split via /wf-spec-breakdown)

| # | Phase | Depends on | Effort | Status |
|---|-------|-----------|--------|--------|
| 1 | Rename Map → Mission Control (route+redirect, nav, copy, docs) | — | small | complete |
| 2 | Full legal `available_actions` from the state machine + node action menu (Tier A) | 1 | medium | complete |
| 3 | Focused-spec generative ops via `SpecChatPopup` reuse (Tier B) | 1 | medium | complete |
| 4 | Live task visualization: SSE-driven in-place updates, node animation, "you are here" | 1 | large | complete |

## Outcome (2026-06-28)

Implemented directly, end to end, across the four phases (all reuse, no new
engine), each verified by `make build` + the booted app:

- **Phase 1 — rename** (`8b244478`): route `/mission` with `/map` redirecting;
  sidebar label, page title/copy, and the docs guide now say "Mission Control".
  `MapPage.vue` keeps its filename.
- **Phase 2 — full coordination actions** (`0e58e0a3` backend, `78afb62d`
  frontend): the graph builder derives each spec's legal forward-flow verbs
  (validate, dispatch, undispatch, force-complete, unstale, unarchive) from the
  canonical `spec.StatusMachine`, guarded by a drift test; the inspector renders
  a button per available action wired to `POST /api/specs/transition` / the task
  routes.
- **Phase 3 — generative ops** (`fef2a2a5`): a "Refine / discuss" affordance on
  spec nodes (and the double-click popup) sets `agentStore.focusSpec` and opens
  the existing `SpecChatPopup`, so `/refine` `/break-down` etc. run from the
  graph with zero new chat code.
- **Phase 4 — live visualization** (`4f401cfc`): `GraphCanvas` relays out only
  on structural change (positions survive a run), running/waiting nodes radiate
  a pulse ring, running discs breathe, and a running node on the critical path
  gets the "you are here" accent. Live status flows via the app-wide
  `/api/tasks/stream` subscription. Respects `prefers-reduced-motion`.
- **Follow-up** (`8bbf1014`): scoped the "ready to act" highlight to the forward
  verbs (validate/dispatch/start) so recovery verbs don't swamp the list on a
  repo full of stale specs.

Deferred per the design: per-agent-step live activity (current tool, turns,
token burn), Tier C "create from the graph", and dragged-position preservation
across structural relayouts.

## Open Questions

- **Generative ops surfacing:** embed a chat panel in the canvas, or
  launch-the-focused-`SpecChatPopup` on demand? (Lean: launch the existing
  focused popup — the core already supports per-surface mounting; an embedded
  canvas chat is much more surface for little gain.)
- **Node action menu UX:** inspector-only, or an on-canvas context menu
  (right-click / hover) on the node? Right-click is discoverable but collides
  with browser menus; hover affordance may be cleaner.
- **Live structural updates:** is task-stream (state) + spec-tree-stream
  (structure) enough, or is a dedicated graph SSE worth it for v1? (Lean:
  reuse the two existing streams; add a graph stream only if coordinating them
  client-side proves fiddly.)
- **Animation budget:** how much motion before it's noise on a 350-node graph?
  Likely animate only `in_progress`/`waiting` nodes and the active chain, with a
  reduced-motion fallback.
- **Multi-workspace focused chat:** `MapNodePopup` already resolves a spec
  against the first configured workspace; the focused-chat path inherits the
  same limitation — fix the workspace resolution once for both.

## Non-Goals

- No new flow/execution engine — strictly reuse the state machine, transition
  API, chat core, and task SSE.
- No rebuild of the planning chat — the map sets focus and mounts the existing
  `SpecChatPopup`.
- Per-agent-step live activity (tool/turn/token streaming) is deferred to a
  later phase, not v1.
- No change to the spec lifecycle states or the runner.
