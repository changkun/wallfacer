---
title: Agent Graph
status: complete
depends_on:
  - specs/shared/console-redesign/shell.md
affects:
  - frontend/src/views/AgentGraphPage.vue
  - frontend/src/components/AgentGraphCanvas.vue
  - frontend/src/components/AgentEditor.vue
  - frontend/src/components/SystemPromptsManager.vue
  - frontend/src/styles/agents.css
effort: medium
created: 2026-09-05
updated: 2026-09-05
author: changkun
dispatched_task_id: null
---

# Agent Graph

## Overview

The agents surface: a list of agent documents, a graph canvas, and an editor.
It has 448 lines of scoped CSS in the page and 12 hex literals in the canvas.
The list and editor get the primitives; the canvas keeps its own drawing but
reads every colour from the ramp.

## Current State

- `AgentGraphPage.vue` (979 lines, 448 scoped, one `backdrop-filter`): left
  list of agents with kind badges and running state, centre canvas, right
  editor drawer.
- `AgentGraphCanvas.vue` (177 scoped, 12 hex): nodes, edges, selection,
  running pulse.
- `AgentEditor.vue` (478 lines, styles in `agents.css` 298 lines): name,
  kind, harness, model, prompt textarea, tools, save/run.
- `SystemPromptsManager.vue` modal.

## Components

### List

`.rows` in a 280px column on `--bg-sunk`: name, kind as `.pill-neutral`,
running as `.pill-run.pulse`, active row `--bg-card` + `--sh-card`. New agent
as `.btn.ghost` full width. The filter is a `.field`.

### Canvas

Nodes are `.card` geometry drawn in SVG: radius 14, `--bg-card` fill, 1px
`--rule` stroke, selected `--accent-line` stroke with an `--accent-soft`
halo; node label 13px 600, kind `.eyebrow`; edges `--rule-2` 1.5px with
`--accent` when selected; running nodes pulse the `--run` dot. The minimap and
zoom controls are `.icon-btn`s in a `.card` bottom right. The one
`backdrop-filter` (the drawer scrim) becomes `--glass-dim`.

### Editor drawer

A 420px `.card` docked right with `.card-head` (name as a `.field`, kind
`.seg`), `.rows` for harness, model and tools (each control right-aligned as
in settings), the prompt textarea borderless on `--bg-sunk`, and a foot with
`.btn` Save and `.btn.ghost` Run. `SystemPromptsManager` reuses the sheet from
the task-detail child with `.rows` of prompts and an `.icon-btn` edit.

## Testing Strategy

- Existing `stores/agentSession.test.ts` stays. Add
  `components/AgentGraphCanvas.test.ts`: node fill and stroke resolve to
  tokens (no literal), selected class applied, running class on running
  nodes.
- `tests/designSystem.test.ts`: `AgentGraphPage.vue`, `AgentGraphCanvas.vue`,
  `agents.css` have no hex literal and no `backdrop-filter`.
- `checks.mjs` scene `agents`: list column 280, at least one node rendered
  with radius 14, click a node opens the drawer at 420, drawer inside the
  main card. Screenshots `agents` light and dark, `agent-editor`.

## Outcome

**What shipped** (`50f72e8e`). `AgentGraphPage.vue` is a title, a lede and
a page-action cluster (New fleet as the ink button, the fleet `.field`
select), then a two-column body: the agent registry as a `.card` of
`.rows` with a search `.field` and a New agent ghost in the head, and the
fleet canvas as a `.card` whose head carries the fleet name, a built-in or
custom pill, the slug and Clone & edit. The canvas band is `--bg-sunk` with
a Fixed sequence / Lead delegates pill and a one-line explanation.
`AgentGraphCanvas.vue` draws nodes as 14px-radius card rectangles on
`--bg-card` with `--rule` strokes, the task node dashed, edges on
`--rule-2` and run overlays on the ramp (`--run`, `--ok`, `--err`) with
`om-pulse` for the live node. `AgentEditor.vue` uses `.field`, `.seg` and
`.btn` primitives with the turn model as a segmented control, on the
`.modal-card` surface. `SystemPromptsManager.vue` and `agents.css` carry
no hex literals or literal radii. `AgentGraphCanvas.test.ts` covers node
geometry from tokens, the dashed task node and run tinting; the `agents`
scene asserts the two cards, the pill, the node radius and every fleet
without errors. `make ui-test` passes thirteen scenes.

**Decisions made during implementation.**
- The editor stays a centered 720px dialog rather than the 420px drawer in
  the spec body: the system prompt is a wide mono textarea, and a drawer
  narrows it to a scroll-heavy column. The dialog reads as the same
  `.modal-card` surface the task sheet uses.
- `AgentEditor` keeps the legacy `agents-detail__segment-btn--active` class
  beside `.on` so the harness test that targets it stays valid.
- The registry list reuses `.rows` rather than a bespoke list so its hover
  and selected states match the settings and workspace rows.

**Deviations from the spec.** The editor is a dialog, not a drawer, as
above. `SystemPromptsManager` was tokenised in place, not rebuilt on the
sheet layout: it is a single list with one editor and gains nothing from
the aside.

**Follow-ups.** None beyond the sibling specs.
