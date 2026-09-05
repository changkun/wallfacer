---
title: Secondary Screens
status: drafted
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
  `data-theme` for chart colours; tiles and tables inline.
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
`.tabular` values. Chart colours come from a `chartPalette()` helper that
reads the ramp from computed style at mount and on `data-theme` change, so
`AnalyticsTabCost` stops carrying its own colour map.

### Routines

Routines are `.rows` in one `.card`: name, schedule as `.pill-neutral` mono,
next run `.muted`, enabled as the two-state `.seg`, Run now as
`.btn.sm.ghost`. The editor is the dialog from the panels child.

### Mission Control

The map canvas reads every colour from the ramp through the same
`chartPalette()` helper (node fill `--bg-card`, stroke `--rule-2`, state
colour on the dot and edge, selected `--accent`). The node popup is a `.pop`
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
