---
title: Secondary Screens
status: complete
depends_on:
  - specs/shared/console-redesign/shell.md
affects:
  - frontend/src/views/AnalyticsPage.vue
  - frontend/src/components/analytics/AnalyticsTabCost.vue
  - frontend/src/components/analytics/AnalyticsTabTiming.vue
  - frontend/src/components/analytics/AnalyticsTabUsage.vue
  - frontend/src/views/RoutinesPage.vue
  - frontend/src/styles/routines.css
  - frontend/src/views/MapPage.vue
  - frontend/src/components/map/GraphCanvas.vue
  - frontend/src/components/map/MapNodePopup.vue
  - frontend/src/views/WhiteboardPage.vue
  - frontend/src/styles/whiteboard.css
  - frontend/src/views/ArtifactsView.vue
  - frontend/src/views/LocalDocsPage.vue
  - frontend/src/styles/docs.css
  - frontend/src/lib/mermaidRender.ts
effort: large
created: 2026-09-05
updated: 2026-09-05
author: changkun
dispatched_task_id: null
---

# Secondary Screens

## Overview

The six screens a session visits less often: Analytics, Routines, Mission
Control, Whiteboard, Artifacts, and the local docs. Each is small on its own
and none justifies its own spec, but together they carry 900 lines of scoped
CSS and the largest hex count in the app (Mission Control alone has 48). They
land as one child so the console has no old screen left after it.

## Current State

- `AnalyticsPage.vue` with three tabs; `AnalyticsTabCost.vue` observes
  `data-theme` for chart colors; tiles and tables inline.
- `RoutinesPage.vue` (210 lines) + `routines.css` (175): routine rows,
  schedule, enable toggle, run now.
- `MapPage.vue` (529 lines, 187 scoped, 21 hex), `map/GraphCanvas.vue` (16
  hex), `map/MapNodePopup.vue` (11 hex): the task dependency map.
- `WhiteboardPage.vue` (Excalidraw host, 32 scoped) + `whiteboard.css`.
- `ArtifactsView.vue` (259 lines, 157 scoped, 2 hex): artifact cards.
- `LocalDocsPage.vue` (553 lines, 275 scoped) + `docs.css`: doc nav and
  prose; `lib/mermaidRender.ts` theme map.

## Components

### Analytics

The page keeps its tabs as `.tabs`. Stat tiles become two `.card`s in the
replichai boundary-card shape (eyebrow, big `.tabular` number, one-line
qualifier, a bar) rather than a row of counters; tables are `.rows` with mono
`.tabular` values. Chart colors come from a `chartPalette()` helper that
reads the ramp from computed style at mount and on `data-theme` change, so
`AnalyticsTabCost` stops carrying its own color map.

### Routines

Routines are `.rows` in one `.card`: name, schedule as `.pill-neutral` mono,
next run `.muted`, enabled as the two-state `.seg`, Run now as
`.btn.sm.ghost`. The editor is the dialog from the panels child.

### Mission Control

The map canvas reads every color from the ramp through the same
`chartPalette()` helper (node fill `--bg-card`, stroke `--rule-2`, state
color on the dot and edge, selected `--accent`). The node popup is a `.pop`
with the task's state pill, title and a `.btn.sm` Open. The legend is `.rows`
of dot plus label. The 48 hex literals go to zero.

### Whiteboard

The Excalidraw host takes `theme` from the prefs store (it already does) and
the toolbar frame is a `.card` on `--bg`. `whiteboard.css` reduces to the
frame.

### Artifacts

Artifact cards are `.card`s in a grid of `minmax(240px, 1fr)`: preview,
name 13px 600, kind `.pill-neutral`, age `.muted`, open as `.link`. Empty
state is a centered `.eyebrow` plus one sentence.

### Local docs

The doc nav is the plan child's tree row on `--bg-sunk` at 260px; the reading
column is the same 76ch prose as the focused spec, so `docs.css` is shared
without a second set of rules. `mermaidRender.ts` maps its theme variables to
the ramp and the surfaces.

## Testing Strategy

- `lib/chartPalette.test.ts` (new): returns the ramp values from computed
  style, updates on theme change. Existing `docsIndex.test.ts` stays.
- `tests/designSystem.test.ts`: every listed file has no hex literal; `.vue`
  guard turns on for all of them, which completes the app-wide `.vue`
  assertion.
- `checks.mjs` scenes `analytics` (two boundary cards, tables aligned),
  `routines` (rows, seg toggles), `mission` (canvas renders nodes, popup
  opens inside the viewport), `whiteboard` (Excalidraw mounts), `artifacts`
  (grid, empty state), `docs` (nav 260, prose ≤ 76ch). Screenshots for each,
  light and dark.

## Outcome

**What shipped** (`0d1c2b81`, `fab2d54a`, `b84aaff0`, `384240a1`, `92fb38b2`).
Analytics is a 1040px column: the title and lede, the three tabs as `.tabs`,
then per tab a grid of `.card` stat tiles (eyebrow, big tabular number,
qualifier, the spend tile with a bar) and `.card` sections each holding a
table whose numeric cells are mono tabular and whose status cells are
pills. The daily spend canvas reads `lib/chartPalette.ts`, which returns the
ramp and surfaces from computed style and re-fires on a theme or palette
change; the timing tones and legend swatches are `--ok`, `--warn` and `--err`
classes. Routines is an 860px column with a create card (the prompt as a
`.field`, Every and Agent graph selects, the ink button) and a `.card` of
rows: the title, an `every N min` pill, the agent graph, the countdown and
the last fire, then the interval select, the Off / On switch, Run now and a
danger delete; `lib/routineTime.ts` holds the countdown wording the board
card also uses. Mission Control's head carries the lede with `kbd.key` hints
and a search field; the inspector is 300px of cards on `--bg-sunk` (selection
with state and kind pills, ready and critical lists as rows, the legend as a
two-column grid); the node popover is a `.pop`; `map/nodeColors.ts` maps
every state to a token expression on the ramp, applied through `style` so
`color-mix` works on SVG. The whiteboard variables, the artifact viewer, the
local docs (a 260px nav of tree rows, the article at 76ch as the shared
`.prose-content`) and the mermaid theme read tokens only. Tests:
`chartPalette.test.ts`, `routineTime.test.ts`, `RoutinesPage.test.ts`, the
rewritten `nodeColors.test.ts`; the guard covers all sixteen files. Scenes
`analytics`, `routines`, `mission`, `whiteboard`, `artifacts` and `docs`
replace the analytics smoke; `make ui-test` passes twenty scenes.

**Decisions made during implementation.**
- Analytics tables stay `<table>` elements styled to the rows geometry
  rather than `.rows` divs: six numeric columns need real column alignment,
  which the scene asserts cell by cell.
- The routine schedule edits inline in the row. The schedule endpoint accepts
  the interval and the enabled flag only, so a dialog would hold one field.
- The map's color map keeps a distinct expression per state (mixes toward a
  neighbor where two states share a ramp hue) so the legend never shows two
  identical dots.
- The mission chrome moved from `docs.css` into `mission.css`; `docs.css`
  keeps the screenshot pair rules only.

**Deviations from the spec.** Artifacts keeps its preview-first layout (a
picker, a tool bar and the iframe) instead of a grid of cards: artifacts have
no thumbnails, so a grid would show sixteen identical placeholders. The
inspector is 300px rather than the 272px it had; the spec set no width.

**Follow-ups.** None beyond the verification child.
