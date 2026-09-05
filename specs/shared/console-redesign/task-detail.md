---
title: Task Detail
status: drafted
depends_on:
  - specs/shared/console-redesign/board.md
affects:
  - frontend/src/components/TaskDetail.vue
  - frontend/src/components/TaskPrPanel.vue
  - frontend/src/components/ReviewVerification.vue
  - frontend/src/components/AgentTrace.vue
  - frontend/src/components/SpanFlamegraph.vue
  - frontend/src/components/DiffLineRow.vue
  - frontend/src/styles/modal.css
  - frontend/src/styles/task-detail.css
  - frontend/src/styles/diffs.css
  - frontend/src/styles/syntax.css
  - frontend/src/styles/mermaid.css
effort: large
created: 2026-09-05
updated: 2026-09-05
author: changkun
dispatched_task_id: null
---

# Task Detail

## Overview

The heaviest surface: a 2,392-line component with 611 lines of scoped CSS,
a 708-line `modal.css` written for it, and 607 lines of diff styling. It
becomes a sheet built from primitives: a header row, a tab strip, a scrolling
main column, and a right column of `.card`s with `.rows`, replacing the three
stacked action tiles, the 4px-radius chrome and the nine hardcoded colours.

## Current State

- `TaskDetail.vue`: header (status badge, harness badge, age, short id,
  close), title, tabs (Spec, Activity, Changes, Verification, Events,
  Timeline), main pane per tab, right pane with Blocked by, Actions (Start
  task / Edit task / Delete as large two-line tiles), Agent, Budget, Git,
  Links sections as label/value pairs. Edit mode swaps the spec for a
  textarea plus deps and schedule fields. Nine hex literals in its style
  block, three `backdrop-filter` sites in `modal.css`.
- `TaskPrPanel.vue` (15 hex literals), `ReviewVerification.vue`,
  `AgentTrace.vue` (149 scoped lines), `SpanFlamegraph.vue`, `DiffLineRow.vue`
  with `diffs.css` and `syntax.css` driven by the `--tint-*` pairs.
- `modal.css` holds the overlay, `.modal-card`, the wide layout selectors by
  id (`#modal-body`, `#modal-row`, `#modal-main-pane`, `#modal-main-content`)
  and the tab section visibility rules.

## Components

### Sheet

The modal overlay is `--glass-dim` with no blur. `.modal-card.modal-wide`
becomes `.sheet`: `--bg`, radius `--r-main`, 1px `--rule`, `--sh-pop`, max
width `min(96vw, 1440px)`, height `min(92vh, …)`, grid of `header / tabs /
body` rows and `main 1fr / aside 340px` columns. The id selectors are
replaced by classes (`.sheet-head`, `.sheet-tabs`, `.sheet-main`,
`.sheet-aside`), and the `data-main-tab-section` show/hide rule stays.

### Header

One row: state pill, qualifier pill, harness pill (`HarnessBadge` on
`.pill-neutral`), age and short id as `.muted.mono`, `.icon-btn` close at the
right. Title as `--fs-2xl` 600 below it. Edit mode replaces the title with a
`.field`.

### Tabs

`.tabs`: underline tabs at 13px 500, active `--ink` with a 2px `--accent`
underline, counts as `.count`. Shared with the plan and settings children
(defined in primitives as `.tabs`/`.tab` in this child, since this is its
first consumer).

### Main column per tab

- Spec: prose on `--bg`, section `.eyebrow` with Copy and Raw as
  `.btn.sm.ghost` right-aligned; the mermaid theme reads the ramp.
- Activity and Events: `AgentTrace` rows become `.rows` inside a `.card`,
  each `.row` with a tool glyph, mono summary, duration `.tabular`; the
  flamegraph colours read `--run`, `--warn`, `--err`, `--purple`.
- Changes: `diffs.css` restyled on tokens: file headers as `.card-head`,
  added lines on `color-mix(var(--ok) 10%)`, removed on `color-mix(var(--err)
  10%)`, hunk headers `--bg-sunk`, comments (`DiffLineRow`) as inset `.card`
  with `--accent-line` border. `syntax.css` maps to the ramp.
- Verification: `ReviewVerification` as a `.card` with a verdict pill in its
  head and criteria as `.rows`.
- Timeline: the same `.rows` shape with a left time column `.tabular`.

### Aside

Four `.card`s with `.card-head` eyebrows: Actions, Agent, Budget, Git; Blocked
by and Links become `.rows` in the Git and Agent cards respectively.
Actions: one `.btn` for the primary transition (Start, Resume, Done), a
`.btn.ghost` row for Edit and Test, and Delete as `.btn.ghost.danger` at the
bottom of the card. Every label/value pair is a `.row` with the label
`.muted` left and the value right, mono where it is a number or id.
`TaskPrPanel` becomes one more `.card` in the aside with its state as a pill.

## Testing Strategy

- `components/TaskDetail.test.ts` (new): renders per status with the right
  primary action, edit mode swaps fields, tab switch shows one section, PR
  panel appears when the task has a PR. Existing store tests for diff
  comments stay.
- `tests/designSystem.test.ts`: `TaskDetail.vue`, `TaskPrPanel.vue`,
  `AgentTrace.vue`, `modal.css`, `diffs.css`, `syntax.css` have no hex
  literal and no `backdrop-filter`; `modal.css` has no `#modal-` id selector.
- `checks.mjs` scene `task-detail`: open the first board card, sheet width
  within `min(96vw, 1440)`, aside width 340, tabs row height, primary `.btn`
  present in the Actions card, no overflow of the main column, switch to
  Changes and Verification without page errors. Screenshots `task-detail`
  light and dark, `task-detail-changes`.
