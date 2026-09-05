---
title: Plan
status: drafted
depends_on:
  - specs/shared/console-redesign/chat.md
affects:
  - frontend/src/views/PlanPage.vue
  - frontend/src/components/plan/SpecTreePanel.vue
  - frontend/src/components/plan/SpecFocusedView.vue
  - frontend/src/components/plan/SpecCommentsLayer.vue
  - frontend/src/components/plan/FloatingToc.vue
  - frontend/src/styles/spec-mode.css
  - frontend/src/styles/docs.css
effort: large
created: 2026-09-05
updated: 2026-09-05
author: changkun
dispatched_task_id: null
---

# Plan

## Overview

The three-pane planning surface: spec tree, focused spec, agent chat. It
carries 1,300 lines of scoped CSS across three components. Tree and reading
pane get the primitives; the chat pane is the chat child's panel dropped in.

## Current State

- `PlanPage.vue` (393 lines, 58 scoped): `data-layout` three-pane or focused,
  resizable tree width, collapsed tree rail button, chat panel right.
- `SpecTreePanel.vue` (1,110 lines, 433 scoped): filter field, status select,
  "Show archived" checkbox, "Rescan staleness" link, Task Prompts group,
  Roadmap row, track folders with chevrons, spec rows with status dots,
  context menus.
- `SpecFocusedView.vue` (1,174 lines, 445 scoped): the rendered spec with
  frontmatter header, status controls, lifecycle actions, comments anchors;
  `FloatingToc.vue`.
- `SpecCommentsLayer.vue` (1,138 lines, 417 scoped, 12 hex): anchored
  comment threads, composer, resolve.
- `spec-mode.css` (6 lines), `docs.css` (237 lines, prose styles shared with
  local docs).

## Components

### Tree panel

Header: filter as `.field` with the search glyph, status as `.seg` when it is
All / Active / Archived, otherwise `.field` select; Rescan as `.btn.sm.ghost`.
Groups are `.eyebrow` rows with a chevron `.icon-btn`; spec rows are the
rail's nav row geometry (34px, radius 12) with a 5px status dot from the ramp
(drafted neutral, in progress run, blocked warn, complete ok, stale err,
archived `--ink-4`), active row `--bg-card` + `--sh-card`. Task prompt rows
are the same row with a `.pill-neutral` count. The collapsed rail is a 40px
strip with an `.icon-btn`. The resize handle is 4px, `--rule` on hover
`--accent-line`.

### Focused view

A reading column max width 76ch on `--bg`, the spec's frontmatter as a
`.card` with `.rows` (status pill, effort, dates, author, dispatched task
link), lifecycle actions as one `.btn` for the next transition and
`.btn.ghost` for the rest in the card's foot. `docs.css` prose: headings on
the type ladder, code on `--bg-sunk`, tables with `--rule` hairlines, mermaid
on the ramp. The floating TOC is a `.card` with `.rows` of `.link`s, active
`--accent`.

### Comments layer

Anchors are 18px `--accent` circles with the count; threads open as a `.card`
with `--sh-pop` beside the anchor, replies as `.rows` with author `.eyebrow`
and age `.muted`, composer as a `.field` textarea with `.btn.sm` post and
`.btn.sm.ghost` resolve. All 12 hex literals resolve to tokens.

## Testing Strategy

- Existing `plan/*` composable tests stay. Add `components/SpecTreePanel.test.ts`:
  status dot class per status, filter narrows rows, archived toggle, collapse
  emits; `components/SpecFocusedView.test.ts`: primary action per status.
- `tests/designSystem.test.ts`: the three plan components and `docs.css` have
  no hex literal, no `backdrop-filter`, no `border-radius` literal.
- `checks.mjs` scene `plan`: tree width persists after reload, tree rows
  height 34, reading column ≤ 76ch, chat panel present, collapse the tree
  and the rail strip is 40px, open a comment thread and it stays in the
  viewport. Screenshots `plan` light and dark, `plan-focused`,
  `plan-comments`.
