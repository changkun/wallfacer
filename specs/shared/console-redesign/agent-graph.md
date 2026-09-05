---
title: Agent Graph
status: drafted
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
