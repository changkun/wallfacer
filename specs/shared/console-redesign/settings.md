---
title: Settings
status: complete
depends_on:
  - specs/shared/console-redesign/shell.md
affects:
  - frontend/src/views/SettingsPage.vue
  - frontend/src/components/settings/SettingsTabExecution.vue
  - frontend/src/components/settings/SettingsTabAppearance.vue
  - frontend/src/components/settings/SettingsTabSandbox.vue
  - frontend/src/components/settings/SettingsTabGithub.vue
  - frontend/src/components/settings/SettingsTabAbout.vue
  - frontend/src/components/AppSelect.vue
  - frontend/src/components/HarnessSelect.vue
  - frontend/src/styles/settings-page.css
  - frontend/src/styles/settings-modal.css
  - frontend/src/styles/forms.css
effort: medium
created: 2026-09-05
updated: 2026-09-05
author: changkun
dispatched_task_id: null
---

# Settings

## Overview

Settings becomes the replichai settings shape: a title, underline tabs, and
one `.card` per section with `.rows` of label, help text and control. The
Appearance tab is where the new palette roster is picked, so this child also
owns the `paper` preset's UI.

## Current State

- `SettingsPage.vue`: eyebrow, title, `.set-grid` with a left `.set-side`
  tab list and `.set-body`; `settings-page.css` (164) and
  `settings-modal.css` (110, a leftover from when settings was a modal).
- Tabs: Execution (452 lines: harness defaults, parallelism, timeouts,
  budgets), Appearance (mode `.seg`, palette swatch grid), Sandbox (874
  lines: Cella/host config, credentials), GitHub (74, 8 hex), About (293:
  version, diagnostics, links).
- `AppSelect.vue`, `HarnessSelect.vue` custom selects; `forms.css` inputs.

## Components

### Page

Centered column max width 760px: `h1` on `--fs-2xl`, `.tabs` underline row
(Execution, Appearance, Sandbox, GitHub, About) replacing the side list, and
the active tab's cards below with `--gap-row` between cards.
`settings-modal.css` is deleted; `settings-page.css` keeps only the column.

### Rows

Every setting is a `.row`: a 22px tinted glyph square, label `--ink` 13px 500,
help text `.muted` 12px under it, control right-aligned. Controls: `.seg` for
two to four states, `.field` for text and numbers, `AppSelect`/`HarnessSelect`
restyled as `.field` triggers with a `.card` `--sh-pop` popover of `.rows`,
toggles as the two-state `.seg`. Destructive rows (revoke, clear) use
`.btn.sm.ghost.danger` and sit last in their card.

### Appearance

Mode as `.seg` Auto / Light / Dark with glyphs. Palette as a row of swatch
buttons: each a 44px `.card` showing the four swatches from `PALETTES`, name
under it, selected with an `--accent-line` border and `--accent-soft` fill.
The roster comes from the prefs store so `paper` appears without a template
change.

### About

Version and build as a `.card` with `.rows` (mono values, copy `.icon-btn`),
diagnostics as `.rows` with state pills, links as `.link`s.

## Testing Strategy

- `components/settings/SettingsTabAppearance.test.ts`: swatch list equals
  `PALETTES`, click sets `prefs.palette`, mode seg sets `prefs.theme`.
- `views/SettingsPage.test.ts`: tab from the route query, one tab body at a
  time.
- `tests/designSystem.test.ts`: `SettingsTabGithub.vue` and the settings
  styles have no hex literal; `settings-modal.css` and `forms.css` no longer
  exist.
- `checks.mjs` scene `settings`: column width ≤ 760, tabs row present, every
  `.row` control right edge aligned within 1px, switch each tab without page
  errors, Appearance shows six swatches. Screenshots `settings` light and
  dark, `settings-appearance`.

## Outcome

**Status:** complete, 2026-09-05. Commit `7ca61208`.

**What shipped.** `SettingsPage.vue` is a 760px column with a title, the
`.tab` strip and one tab body at a time; the side tab list and
`settings-modal.css` are gone (the trash banner rules it held moved to
`modal.css` on tokens). Every tab is cards of `.set-row`s: label, help and
a right-aligned control. Execution groups Automation (a `SettingToggle`
per watcher), Limits, Auto push and Maintenance. Appearance is one card: the
mode as a `.seg` with glyphs and the six-palette roster as 44px swatch cards
read from the prefs store, so `paper` appears without a template change.
Harness is one card per harness with mono `.field`s, the sign-in as the ink
button (a ghost once credentials exist), Test in the card foot, a routing
card and a Save / Revert foot. GitHub and About are cards with rows and
pills. `AppSelect` and `HarnessSelect` triggers are fields with popover-card
menus. `SettingsTabAppearance.test.ts` and `SettingsPage.test.ts` cover the
roster, the picks, the query-driven tab and the single body; the `settings`
scene asserts the column width, the active tab, control alignment, every tab
without errors and six swatches. `make ui-test` passes thirteen scenes.

**Decisions made during implementation.**
- Rows carry no per-setting glyph: 30 settings would need 30 icons for a
  cue the label already gives.
- A two-state toggle is a segmented Off / On with `role="switch"`, in
  `SettingToggle.vue`, rather than a checkbox or a custom switch.
- The harness cards keep every field id (`env-*`) so nothing that targets
  them by id changes.

**Deviations from the spec.** None material; the glyph column is dropped as
above.

**Follow-ups.** None beyond the sibling specs.
