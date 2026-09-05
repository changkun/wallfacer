---
title: Board
status: drafted
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
controls and the explorer rail keep their behaviour.

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
  `.card-action-*` colour variants.
- `TaskComposer.vue` (636 lines, 162 scoped): the "+ New Task" dashed button
  expanding into a textarea with mentions, harness select, deps picker.
- `board.css` 607 lines, `search.css` 78 lines.

## Components

### Column header

`.col-hd` becomes `.eyebrow` with the state dot (5px circle in the column's
ramp colour, `--col-*` now aliases of the ramp), the count as `.count`
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

- Rank and status are `.pill-neutral` and the state pill in ramp colour
  (`backlog` neutral, `in_progress` run with `.pill-dot.pulse`, `waiting`
  warn, `done` ok, `failed` err, `cancelled`/`archived` neutral).
- Verification (`verified`, `unverified`, `verify failed`) is the qualifier
  pill (`ok`, neutral, `err`).
- Harness, timeout, age go to the right of row 1 as `.muted` mono text with
  the harness logo at 12px.
- Priority, impact and labels stop being tinted badges. They are one mono
  meta line under the title, `--ink-3`, separated by `·`, with priority first
  and coloured only when it is `high` (`--warn`) or `critical` (`--err`). The
  `--tag-bg-N` slots and the palette tag block in `palettes.css` are deleted.
- The last output or error block is a `.card-out` well: `--bg-sunk`, radius
  `--r-sm`, mono 11px, an `err` variant with `--err` text on its tint.
- Turns and cost are `.tabular` on the meta line of the last row.
- Actions: one `.btn.sm` for the primary transition of the column (Start,
  Resume, Done) and `.btn.sm.ghost` for the rest (Plan, Test, Retry). The
  `card-action-*` colour variants are deleted.

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
