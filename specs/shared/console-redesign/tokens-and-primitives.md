---
title: Tokens and Primitives
status: drafted
depends_on:
  - specs/shared/visual-identity/theme-system.md
affects:
  - frontend/src/styles/tokens.css
  - frontend/src/styles/palettes.css
  - frontend/src/styles/base.css
  - frontend/src/styles/buttons.css
  - frontend/src/styles/badges.css
  - frontend/src/styles/forms.css
  - frontend/src/styles/primitives.css
  - frontend/src/main.ts
  - frontend/src/App.vue
  - frontend/src/stores/prefs.ts
  - frontend/index.html
  - frontend/tests/glassV2Surfaces.test.ts
  - frontend/tests/designSystem.test.ts
effort: large
created: 2026-09-05
updated: 2026-09-05
author: changkun
dispatched_task_id: null
---

# Tokens and Primitives

## Overview

The foundation every other child renders through: palette P2 as the default
token values, the cream palette demoted to a preset, a type and radius ladder,
matte shadows, the removal of Liquid Glass, and the shared primitive classes
(`.btn`, `.icon-btn`, `.pill`, `.card`, `.rows`, `.row`, `.seg`, `.field`,
`.eyebrow`). Nothing in this child changes a screen's layout. It changes how
every screen is coloured and what classes the surface specs may compose from.

## Current State

- `styles/tokens.css`: default `clay` palette on cream, glass tokens
  (`--glass-bg`, `--glass-filter`, `--hairline-top`, `--sh-inset`), a radii
  ladder `--radius-xs..2xl` used only by glass chrome, `--r-sm/md/lg/xl`
  (4/6/10/14) used by app CSS, `--fs-*` starting at 10px with `--fs-base` 12px.
- `styles/palettes.css`: `indigo`, `amber`, `rose`, `copper` full overrides,
  each with its own glass tokens.
- `stores/prefs.ts`: `PaletteName` union and `PALETTES` roster with swatches;
  `applyPalette` removes the attribute for the default. `index.html` no-flash
  script mirrors it.
- `styles/buttons.css`: `.btn` 8px radius, `.btn-accent/-yellow/-green` with
  hardcoded greens and ambers, `.btn-ghost`, `.btn-dashed`, `.btn-danger`.
- `styles/badges.css`: `.badge` 4px radius 10px sans, per-status tint pairs.
- `styles/forms.css` (45 lines), `styles/utilities.css`.
- `main.ts` imports `latere-ui/glass` last; `App.vue` calls `useLiquidGlass`.
- `tests/glassV2Surfaces.test.ts` asserts glass ladder consumption in
  `navbar-auth.css`, `content-header.css`, `buttons-hero.css` and asserts no
  glass over content. `tests/fontLoading.test.ts` reads `fonts.css`.

## Components

### tokens.css

Rewrite the two default blocks with the P2 table from the parent spec. Keep
every existing token name. Additions: `--run` (with `--info` aliased to it),
`--accent-soft`, `--accent-line`, `--accent-ring`, `--sh-card`, `--sh-main`,
`--ease-fluid: cubic-bezier(0.22, 1, 0.36, 1)`, `--dur-hover: 0.16s`,
`--gap-row: 18px`, `--gap-inline: 10px`, `--rail-w: 236px`,
`--rail-w-fold: 64px` (the old `--sb-w`/`--sb-w-icon` alias to these).

Tint pairs become the formula from the parent spec
(`--tint-green: color-mix(in srgb, var(--ok) 14%, transparent)` and
`--tint-green-ink: var(--ok)`), so palettes only set the ramp.

Radii: `--r-sm: 8px`, `--r-md: 10px`, `--r-lg: 14px`, `--r-xl: 18px`,
`--r-main: 20px`, `--r-pill: 999px`. The glass ladder `--radius-*` is deleted.
Type: `--fs-9: 10px`, `--fs-10: 11px`, `--fs-base: 13px`, `--fs-md: 14px`,
`--fs-lg: 15px`, `--fs-xl: 17px`, `--fs-2xl: 22px`, `--fs-3xl: 30px`.

Glass tokens are kept as names only, pinned to opaque values so latere-ui's
`AccountMenu` and `SiteFooter` render matte: `--glass-bg: var(--bg-card)`,
`--glass-bg-thin/ultrathin/thick: var(--bg-card)`, `--glass-blur*: 0`,
`--glass-saturate: 100%`, `--glass-border: var(--rule)`, `--shadow-glass:
var(--sh-pop)`, `--glass-pill-fill: var(--bg-sunk)`, `--glass-edge-top: none`.
`--hairline-top`, `--sh-inset`, `--glass-filter` and `--glass-sidebar` are
deleted; the shell and surface children delete their consumers.

Terminal tokens follow the new surfaces (`--terminal-bg: var(--bg-sunk)` in
light, `var(--bg-deep)` in dark).

### palettes.css

Add `paper`: the current default values verbatim (cream canvas, clay accent,
warm ink). Retune `indigo`, `amber`, `rose`, `copper` to the same structure:
they set only surfaces, ink, rules, accent and the five-state ramp; tint pairs
and shadows derive. Every palette block drops its glass tokens.

### prefs.ts and index.html

`PaletteName` gains `'paper'`; `PALETTES` gains its entry after `clay` with the
old swatches; the `clay` swatches update to the neutral canvas. The no-flash
script is unchanged in logic and stays in sync by the existing roster test.

### primitives.css

New file, imported after `base.css`, replacing `buttons.css`, `badges.css` and
`forms.css` (deleted). It holds exactly the primitives from the parent
geometry table, styled through tokens only. Class names follow replichai so a
reader can move between the two codebases: `.btn`, `.btn.ghost`, `.btn.danger`,
`.btn.sm`, `.btn.lg`, `.btn.pending`, `.icon-btn`, `.icon-btn.danger`, `.pill`
with `.pill-neutral/-brand/-ok/-warn/-run/-err/-pub`, `.pill-dot`, `.card`,
`.card-pad`, `.card-head`, `.rows`, `.row`, `.seg`, `.seg-btn`, `.field`,
`.eyebrow`, `.link`, `.muted`, `.tabular`.

The status badge classes the templates already use (`.badge-backlog`,
`.badge-in_progress`, `.badge-waiting`, `.badge-done`, `.badge-failed`,
`.badge-cancelled`, `.badge-archived`, `.badge-priority`, `.badge-testing`,
`.badge-verified`, and the tag slots `--tag-bg-N`) are kept as aliases onto
`.pill` variants so no template changes in this child. Surface children then
swap templates to `.pill` and delete the aliases they no longer need; the
verification child deletes what is left.

`.btn-accent`, `.btn-yellow`, `.btn-green`, `.btn-dashed`, `.btn-danger` are
aliased the same way (`.btn-accent` → `.btn`, `.btn-green` → `.btn`,
`.btn-yellow` → `.btn.ghost`, `.btn-danger` → `.btn.ghost.danger`,
`.btn-dashed` keeps a dashed ghost variant for "add" rows).

### base.css

Body 13px/1.5 Inter, the single focus ring rule from replichai
(`:where(input, button, select, a, [tabindex]):focus-visible` with
`--accent-ring`), `::selection` on `--accent-soft`, the `om-*` keyframes
(`pulse`, `fade-in`, `fade-up`) and the reduced-motion override. Scrollbar
rules stay OS default as documented today.

### Glass removal

Delete the `latere-ui/glass` import from `main.ts`, the `useLiquidGlass` call
from `App.vue`, and every `backdrop-filter` and `-webkit-backdrop-filter` in
`styles/` (tokens, palettes, command-palette, modal, explorer, header,
navbar-auth). Overlay scrims become a plain `--glass-dim` tint. Component
`<style>` blocks that still carry blur are listed in their surface child;
this child only removes the shared ones and installs the guard that fails on
the rest.

## Testing Strategy

- `tests/designSystem.test.ts` (new, replaces `glassV2Surfaces.test.ts`):
  - no `backdrop-filter` in any file under `src/styles/` and, after the last
    surface child, in any `.vue` file (the `.vue` assertion starts as
    `it.todo` listing the files, and each surface child turns its files on);
  - the contrast formulas from the parent spec evaluated against every
    palette block in light and dark, parsed from `tokens.css` and
    `palettes.css`;
  - every `--tint-*` in a palette block is absent (derived), every palette sets
    the same key set;
  - `primitives.css` defines each class in the parent geometry table;
  - `main.ts` does not import `latere-ui/glass`.
- `stores/prefs.palette.test.ts` extended for `paper` and the roster sync with
  `index.html`.
- `checks.mjs` gains a `no-glass` scene: walk every element on the board and
  assert computed `backdropFilter === 'none'`; and a `contrast` scene reading
  computed `--ink-3`/`--bg` per theme.
- Screenshots: board light and dark, to eyeball the retint before the shell
  child moves geometry.
