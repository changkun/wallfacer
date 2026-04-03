---
title: Spatial Canvas
status: vague
depends_on: []
affects:
  - ui/js/
  - ui/css/
  - ui/partials/
  - internal/handler/
  - internal/store/
effort: xlarge
created: 2026-04-03
updated: 2026-04-03
author: changkun
dispatched_task_id: null
---

# Spatial Canvas

## Overview

Explore whether Wallfacer's linear column-based task board should be augmented
(or replaced) with a spatial, infinite-canvas workspace where tasks, agent
sessions, plans, notes, and results coexist as freely positionable nodes on a
2D plane. Inspired by [OpenCove](https://github.com/DeadWaveWave/opencove),
which turns linear chat histories into a spatial workbench with side-by-side
agent sessions.

This is an exploratory spec. The core question is whether spatial layout adds
genuine value for Wallfacer's workflow — or whether the existing board + spec
tree + planning chat already provides sufficient structure.

## Motivation

The current board uses a Kanban-style column layout (Backlog → In Progress →
Done). This works well for sequential task tracking but has friction points:

1. **Dependency visualization** — task dependencies are invisible on the board;
   the spec tree shows them but lives in a separate mode.
2. **Context switching** — inspecting one task's diff, another's logs, and the
   planning chat requires panel switching. There is no way to see multiple
   artifacts simultaneously in a spatial arrangement.
3. **Planning artifacts** — brainstorm notes, architecture sketches, and
   intermediate ideas have no persistent home on the board.
4. **Session persistence** — the board state persists, but the spatial
   arrangement of open panels (diff, logs, terminal) does not survive a
   page reload.

OpenCove's thesis: replacing a linear chat with a spatial canvas makes the
topology of work visible. Multiple agent sessions run side-by-side, plans and
results coexist on the same plane, and the layout itself becomes a form of
working memory.

## Design Space

Questions to resolve before this spec can be promoted to `drafted`:

### Does spatial layout fit Wallfacer's model?

Wallfacer is task-centric: create a card, drag it, inspect the result. OpenCove
is conversation-centric: each node is an agent session. These are different
mental models. A spatial canvas for Wallfacer would need to answer what the
nodes are — tasks? Agent sessions? Arbitrary notes? All of the above?

Possible positions:
- **Canvas replaces the board** — tasks are free-form nodes, columns disappear.
  Risk: loses the at-a-glance status overview that Kanban provides.
- **Canvas augments the board** — a separate "canvas mode" for planning and
  multi-task inspection, while the board remains the primary execution view.
  Lower risk but adds another mode to an already multi-modal UI.
- **Canvas is the spec tree** — the dependency graph from `specs/README.md`
  becomes an interactive spatial view where nodes can be expanded, edited, and
  dispatched. This is closer to the existing spec-planning-ux spec.

### What are canvas nodes?

Candidates for first-class nodes:
- Task cards (linked to existing task state machine)
- Agent sessions (live or historical)
- Spec documents (from the spec tree)
- Free-form notes / sticky notes
- Terminal sessions
- Diff views
- File previews
- Media (images, diagrams)

### Persistence model

- Per-workspace canvas layout stored as JSON (node positions, connections,
  viewport state)?
- Snapshot/restore for returning to previous spatial arrangements?
- How does this interact with the existing workspace group model?

### Rendering approach

- HTML/CSS with absolute positioning (simplest, limited scale)?
- Canvas 2D / WebGL (handles thousands of nodes, but complex)?
- SVG (good for connections/edges, moderate scale)?
- Existing libraries: tldraw, reactflow, xyflow, excalidraw?

Note: Wallfacer's frontend is vanilla JS with no framework. Adopting a
React-based canvas library would be a significant architectural decision.

### Connection semantics

- Edges between nodes: what do they mean? Task dependencies? Data flow?
  Arbitrary user-drawn connections?
- How do connections relate to the existing `depends_on` DAG in spec documents
  and task dependencies?

### Auto-layout

- When the canvas gets cluttered, how to auto-organize?
- Force-directed layout? Hierarchical (dagre/elk)?
- Manual layout with optional auto-tidy?

## Prior Art

- **OpenCove** — Infinite canvas for AI agents, terminals, and notes. Spatial
  layout as primary interaction model.
- **tldraw** — Infinite canvas library. React-based.
- **Excalidraw** — Whiteboard with collaboration. Could embed for freeform
  sketching.
- **Linear** — Project management with a spatial "project view" alongside
  list/board views.
- **Muse** — Spatial canvas for thinking and research (now discontinued).
- **Kinopio** — Spatial thinking tool with cards and connections.

## Relationship to Existing Specs

- **spec-planning-ux** — The spec explorer and dependency minimap are a
  constrained form of spatial visualization. A canvas mode could subsume or
  extend this.
- **file-panel-viewer** — The tabbed file panel could become a canvas node
  type instead of a fixed panel.
- **pixel-agents** — The office view is already a spatial metaphor. Could
  the canvas and the office view coexist or merge?
- **intelligence-system** — Cross-task awareness and shared world model
  could benefit from spatial visualization of agent relationships.

## Open Questions

1. Is the complexity of a spatial canvas justified, or would targeted
   improvements to the existing board (e.g., dependency arrows, split-pane
   inspection) achieve 80% of the value at 20% of the cost?
2. What is the minimum viable canvas — what is the smallest useful subset
   that could be shipped to validate the concept?
3. How does a canvas interact with the existing SSE-driven live update
   model? Do node positions need to be server-persisted or client-only?
4. Would this require abandoning the vanilla JS frontend philosophy, or
   can a useful canvas be built without a framework?
