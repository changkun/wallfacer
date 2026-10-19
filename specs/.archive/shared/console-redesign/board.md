---
title: Board
status: complete
depends_on:
  - specs/shared/console-redesign/shell.md
affects:
  - frontend/src/views/BoardPage.vue
  - frontend/src/components/TaskCard.vue
  - frontend/src/components/TaskComposer.vue
  - frontend/src/components/SearchBar.vue
  - frontend/src/components/AutomationMenu.vue
  - frontend/src/components/HarnessBadge.vue
  - frontend/src/components/HarnessLogo.vue
  - frontend/src/styles/board.css
  - frontend/src/styles/search.css
  - frontend/src/styles/palettes.css
effort: medium
created: 2026-09-05
updated: 2026-09-05
author: changkun
dispatched_task_id: null
---

# Board

## Overview

The kanban is the first screen and the densest one. It gets the card
geometry, a badge budget, eyebrow column headers, and a composer that reads as
one control. The four-column layout, drag and drop, sort modes, archive
controls and the explorer rail keep their behavior.

## Current State

- `BoardPage.vue` (566 lines, 59 scoped): `.app-header` (moves to the topbar
  in the shell child), `.board-grid` four columns, `.col-hd` with dot, name,
  count, stats, per-column buttons (`Sort`, `Show archived`, `Archive all`),
  `draggable` lists of `TaskCard`, mobile column nav.
- `TaskCard.vue` (647 lines, all styling in `board.css`): `.card` 6px radius
  with hairline top, row 1 of `#rank`, status badge, verify badge, harness,
  timeout, age; title; tag badges (priority, impact, labels) from the
  `--tag-bg-N` slots; description; last output or error block; turns and
  cost; action buttons per state (Plan, Start, Resume, Test, Done, Retry) in
  `.card-action-*` color variants.
- `TaskComposer.vue` (636 lines, 162 scoped): the "+ New Task" dashed button
  expanding into a textarea with mentions, harness select, deps picker.
- `board.css` 607 lines, `search.css` 78 lines.

## Components

### Column header

`.col-hd` becomes `.eyebrow` with the state dot (5px circle in the column's
ramp color, `--col-*` now aliases of the ramp), the count as `.count`
(mono, `--ink-3`), and the column controls as `.icon-btn`s that appear on
hover of the header, matching replichai's borderless icon buttons. The
"max N" parallel tag on In Progress is a `.pill-neutral`.

### Card

`.card` from primitives: radius `--r-lg` (14), padding 12, `--bg-card`, 1px
`--rule`, `--sh-card`; hover lifts to `--sh-pop` at 40% and `--rule-2`;
dragging (`sortable-chosen`) uses `--sh-pop` and an `--accent-line` border.

Badge budget. A card carries at most one state pill and one qualifier pill on
its first row, then the title, then one meta line:

```
[ #12 ] [ ● waiting ] [ verified ]              claude · 15h · 30d
Migrate frontend store to Pinia setup
medium · impact 4 · frontend
```

- Rank and status are `.pill-neutral` and the state pill in ramp color
  (`backlog` neutral, `in_progress` run with `.pill-dot.pulse`, `waiting`
  warn, `done` ok, `failed` err, `cancelled`/`archived` neutral).
- Verification (`verified`, `unverified`, `verify failed`) is the qualifier
  pill (`ok`, neutral, `err`).
- Harness, timeout, age go to the right of row 1 as `.muted` mono text with
  the harness logo at 12px.
- Priority, impact and labels stop being tinted badges. They are one mono
  meta line under the title, `--ink-3`, separated by `·`, with priority first
  and colored only when it is `high` (`--warn`) or `critical` (`--err`). The
  `--tag-bg-N` slots and the palette tag block in `palettes.css` are deleted.
- The last output or error block is a `.card-out` well: `--bg-sunk`, radius
  `--r-sm`, mono 11px, an `err` variant with `--err` text on its tint.
- Turns and cost are `.tabular` on the meta line of the last row.
- Actions: one `.btn.sm` for the primary transition of the column (Start,
  Resume, Done) and `.btn.sm.ghost` for the rest (Plan, Test, Retry). The
  `card-action-*` color variants are deleted.

### Composer

The collapsed state is a `.btn.ghost` full-width row with a `+` glyph ("New
task"). Expanded, it is a `.card` with the textarea borderless inside, and a
footer row: harness select as `.seg` when there are three or fewer harnesses
and a `.field` select otherwise, deps picker as `.pill-neutral` chips, the
submit as `.btn.sm`. The scoped CSS drops to the layout of that footer.

### Search bar and automation menu

`SearchBar` becomes a `.field` with the search glyph and the `/` kbd hint;
`search.css` reduces to the dropdown results list on a `.card` with `--sh-pop`.
`AutomationMenu` is a `.card` popover with `.rows` of label plus a toggle,
the toggle being the `.seg` two-state form.

## Testing Strategy

- `TaskCard` unit tests (existing `stores/tasks.test.ts` covers data; add
  `components/TaskCard.test.ts`): the state pill class per status, the
  qualifier pill per verification state, the meta line content for tags,
  primary action per column.
- `tests/designSystem.test.ts`: `board.css` has no hex literal and no
  `border-radius` literal; `palettes.css` has no `--tag-bg-` key.
- `checks.mjs` scene `board` extended: four columns equal width within 1px,
  every `.card` has computed radius 14, first-row pills of the first card
  count at most two, no card overflows its column, composer expands on click
  and the textarea has focus. Screenshots `board` light and dark, and
  `composer` open.

## Outcome

**Status:** complete, 2026-09-05. Commits `d2a41f02` (tag classifier, primary
action helper) and `1068e86a` (card, columns, composer, search, checks).

**What shipped.** `TaskCard.vue` renders the badge budget: rank, one state pill
(ramp color, pulsing dot while running) and one qualifier chosen from
verification, failure category, dependency state, schedule and PR state, with
the rest as plain text on the meta line. Tags are one mono meta line ordered
priority, impact, labels, provenance; only `high` and `critical` carry color
(`lib/tagBadge.ts`, `orderTags`). Actions are `.btn.sm` with the forward
transition as the ink button (`primaryCardAction`). Column headers are
eyebrows with quiet pill controls; the tray is 18px with 14px cards.
`TaskComposer.vue` is a card with a borderless prompt and field controls; the
collapsed state is a dashed ghost button. `search.css` is the field look. The
task card class is `.task-card` (the `.card` primitive is free for cards).
`tests/designSystem.test.ts` holds `board.css`, `search.css`, `rail.css` and
`topbar.css` to tokens only. `checks.mjs` `board` asserts equal columns, 14px
radii, the pill budget, no overflow and the composer focus. `make ui-test`
passes eleven scenes.

**Decisions made during implementation.**
- The qualifier order is verification, failure category, dependency,
  schedule, PR: the most decisive signal for the column wins the pill.
- Waiting's primary is Done, failed's is Resume when a session exists and
  Retry otherwise; a card never carries two ink buttons.
- Column controls stay visible but quiet (ink-3 text pills) rather than
  hover-only: Show archived and Archive all are the only way to reach those
  states and hiding them costs discoverability.
- `--accent-fg`, `--r-xs` and `--r-row` were added to the ladder for text on
  an accent fill, nested 6px controls and 12px rows; no literal remains.
- `.composer__btn` stays as a global alias in `board.css` for the task-detail
  edit form and the workspace-required prompt until their specs land.

**Deviations from the spec.** The automation menu keeps its toggle switch
rather than a two-state `.seg`; the switch already reads through tokens and a
seg would widen every row for no gain.

**Surprises.** The command palette bound `CardActionDef.cls` to a class; the
field is gone and the binding with it.

**Follow-ups.** None beyond the sibling specs.
