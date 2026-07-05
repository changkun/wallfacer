---
title: User-Selectable Color Themes
status: complete
depends_on:
  - specs/shared/visual-identity/design-tokens.md
affects:
  - frontend/src/styles/tokens.css
  - frontend/src/styles/palettes.css
  - frontend/src/stores/prefs.ts
  - frontend/index.html
  - frontend/src/components/AccountControl.vue
  - frontend/src/views/SettingsPage.vue
  - frontend/src/components/settings/
  - frontend/public/static/wallfacer-icon.png
  - docs/guide/configuration.md
effort: large
created: 2026-07-05
updated: 2026-07-05
author: changkun
dispatched_task_id: null
---

# User-Selectable Color Themes

## Overview

After the indigo-on-zinc rebrand, the decision landed on a third option: the
original paper-ink/clay palette returns as the default, and the color system
becomes user-selectable, Slack-style. Users pick a color theme (palette) from
named presets in Settings; the light/dark/auto mode remains an independent
axis. The rebrand's palettes survive as presets rather than as the sole
identity, and the typography change (Space Grotesk / Inter / JetBrains Mono,
no serif) is kept.

## Current State

- `frontend/src/styles/tokens.css` holds one palette (indigo-on-zinc) as
  `:root, [data-theme="light"]` + `[data-theme="dark"]` blocks; token names
  are the stable contract consumed by ~38 stylesheets and all components.
- `frontend/src/stores/prefs.ts` owns a single `Theme = 'light' | 'dark' |
  'auto'` axis persisted at `wallfacer-theme`, applied as
  `<html data-theme>`; `frontend/index.html` has a no-flash script.
- The original clay palette exists in git history
  (`6c74ab04:frontend/src/styles/tokens.css`).
- Brand marks read tokens where it matters (`Sidebar.vue` brick SVG uses
  `var(--accent)`; `.wallfacer-brand` uses `--accent-gradient`), so palettes
  re-skin the brand automatically. `SiteNav.vue`/`InstallPage.vue` SVGs and
  the raster app icon are hardcoded indigo today.
- Warm palette candidates (amber/rose/copper on stone) were designed during
  the rebrand discussion and previewed in an HTML comparison.

## Architecture

Two orthogonal attributes on `<html>`:

- `data-theme`: `light | dark` (resolved from light/dark/auto, unchanged).
- `data-palette`: `PaletteName` — new axis, default `clay`, persisted at
  `wallfacer-palette`, applied by the prefs store and the no-flash script.

`tokens.css` defines the default palette (clay, the restored original values)
plus everything palette-independent (typography, spacing, radii, shadows
stay per-palette where hue-tinted). A new `palettes.css` (imported after
tokens.css) defines the non-default palettes as full color-token override
blocks:

```css
:root[data-palette="indigo"] { /* light values */ }
[data-palette="indigo"][data-theme="dark"] { /* dark values */ }
```

`:root[data-palette=...]` outranks `:root, [data-theme="light"]`;
the two-attribute selector outranks `[data-theme="dark"]`.

### Palette roster (typed enum, no plain strings)

| Name | Character |
|---|---|
| `clay` | The original paper-ink + clay accent (default) |
| `indigo` | The 2026-07 indigo-on-zinc rebrand palette |
| `amber` | Honey amber on warm stone |
| `rose` | Raspberry rose on warm stone |
| `copper` | Burnished copper on warm paper |

Each palette defines the complete color-token set for both modes: surfaces,
ink ladder, rules, accent (+gradient/soft/tint), semantic, column chips,
tint pairs, glass, grid/glow, terminal tokens. Non-color tokens are defined
once in tokens.css only.

## Components

### prefs store (`stores/prefs.ts`)

`type PaletteName = 'clay' | 'indigo' | 'amber' | 'rose' | 'copper'` with a
`PALETTES` descriptor list (name, label, preview swatch hexes for the
picker). State `palette` (default `clay`), `setPalette` persists +
`applyPalette` writes `data-palette` (skips the attribute entirely for
`clay` so the default needs no attribute); SSG-safe like `applyTheme`.

### No-flash boot (`index.html`)

Extend the inline script: read `wallfacer-palette`, validate against the
known list, set `data-palette` before first paint.

### Picker UI

New Appearance tab on the Settings page (`SettingsTabAppearance.vue`):
mode control (Light / Dark / Auto, moved-shared with the account menu
toggle) plus a swatch grid of palette cards (Slack-style: color dot pair +
name), current selection highlighted, applied instantly. The account menu
keeps its compact mode toggle.

### Brand assets

Restore the original coral brick app icon (raster) since clay is the
default; re-point the hardcoded SVG fills in `SiteNav.vue` /
`InstallPage.vue` to `var(--accent)`-family tokens so marks follow the
active palette like the sidebar mark already does.

### data-theme observers

`TerminalPanel.vue`, `AnalyticsTabCost.vue`, `mermaidRender.ts` read CSS
vars at render time; they must also re-read when `data-palette` changes
(extend their MutationObserver attribute filters).

## Testing Strategy

- prefs store unit tests: palette default, persistence, attribute
  application, invalid stored value falls back to clay.
- fontLoading-style static test asserting the no-flash script handles both
  keys.
- Component test for the Appearance tab (renders roster from the typed
  enum, click applies).
- ui-shots: default (clay) light+dark sweep; spot-check one non-default
  palette via a `--palette` flag added to snap.mjs.
- Docs: configuration.md gains an Appearance section; screenshots
  regenerate in the default clay theme.

## Outcome

Shipped 2026-07-05 (commits f284bbe7, 69352338, cd73e879), implemented
directly. As specced: clay restored as the attribute-free default in
tokens.css; palettes.css defines indigo/amber/rose/copper override blocks;
prefs store gained the typed PaletteName axis + PALETTES roster persisted
at wallfacer-palette; index.html no-flash script extended; new Settings >
Appearance tab (mode control + swatch-card picker); theme observers
(terminal/editor/mermaid) react to data-palette; brand marks and the
About-tab/chart accents derive from tokens so every palette re-skins the
brand; coral brick icon restored; screenshots regenerated in clay.
Additions beyond the spec: Lato adopted as the primary UI sans (user
pick, Inter fallback). Deviations: warm palettes inherit clay's ink/tint/
shadow tokens (only surfaces + accent family override), keeping
palettes.css small; the snap.mjs --palette flag was not added (a temp
script verified amber/rose/indigo visually); AccountControl's compact
mode toggle was left as is rather than shared with the tab. Verified:
palette store unit tests, full vitest, vue-tsc, make build, and
screenshots of clay/amber/rose/indigo across modes.
