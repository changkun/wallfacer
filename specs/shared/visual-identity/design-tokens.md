---
title: Indigo-on-Zinc Design Tokens & Type System
status: complete
depends_on: []
affects:
  - frontend/src/styles/tokens.css
  - frontend/src/styles/base.css
  - frontend/src/styles/board-tokens.css
  - frontend/src/styles/fonts.css
  - frontend/public/fonts/
  - frontend/index.html
  - frontend/src/components/TerminalPanel.vue
  - frontend/src/components/editor/FileEditor.vue
  - frontend/src/components/analytics/AnalyticsTabCost.vue
  - frontend/src/lib/mermaidRender.ts
effort: large
created: 2026-07-05
updated: 2026-07-05
author: changkun
dispatched_task_id: null
---

# Indigo-on-Zinc Design Tokens & Type System

## Overview

Replace the paper-ink/clay palette and serif display type with the indigo-on-
zinc system decided in the parent spec, keeping every token *name* stable so
the ~38 style files and all component-scoped CSS keep working. This child is
the foundation: the marketing site and regenerated docs screenshots both render
through these tokens.

## Current State

- `frontend/src/styles/tokens.css` — canonical light + dark token blocks
  (surfaces, ink ladder, rules, clay accent, semantic colors, tint pairs,
  shadow ladder `--sh-1..--sh-pop`, glass tokens, terminal tokens, `.serif` /
  `.mono` helpers).
- `frontend/src/styles/base.css` lines 1-80 — legacy duplicate token block
  that loads after tokens.css (see `src/main.ts` import order) and silently
  overrides (`--ink-3` differs today).
- `frontend/src/styles/board-tokens.css` — `--tag-bg-N`/`--tag-text-N` pairs.
- `frontend/src/styles/fonts.css` + `frontend/public/fonts/*.woff2` —
  Instrument Serif, Hanken Grotesk, Inter, LXGW WenKai TC; preloads in
  `frontend/index.html`. No mono face bundled despite `--font-mono`.
- `.wallfacer-brand` (Anthropic-coral gradient + Instrument Serif italic) comes
  from the pinned external `latere-ui` package.
- Four code paths read `data-theme` directly and carry their own theme maps:
  `TerminalPanel.vue` (xterm theme), `editor/FileEditor.vue` (CodeMirror
  one-dark), `analytics/AnalyticsTabCost.vue` (chart colors),
  `lib/mermaidRender.ts` (mermaid theme variables).

## Components

### tokens.css rewrite

Same selectors (`:root, [data-theme="light"]` / `[data-theme="dark"]`), same
token names, new values per the parent-spec palette table. Additions (new
tokens, additive only):

- `--accent-gradient: linear-gradient(135deg, #5b5bd6 0%, #7c6cf0 100%)` (dark
  mode: brighter stops) — consumed by the brand override and marketing site.
- `--glow-accent: radial-gradient(...)` aura helper for elevated/marketing
  surfaces.
- Shadow ladder recomputed on cool black (`rgba(9, 9, 18, …)`).
- Tint pairs re-derived on zinc neutrals; indigo replaces plum as the featured
  tag hue; semantic colors per parent table.
- Terminal tokens: dark `#0b0b0f` base, indigo cursor/selection.
- `.serif` helper deleted; `.mono` keeps working (JetBrains Mono now bundled).

### base.css reconciliation

Delete the duplicate token block from `base.css`; keep its reset/global rules.
`tokens.css` becomes the only definition site. Audit for any base.css-only
token the rest of the CSS depends on before deleting.

### Fonts

- Remove Instrument Serif woff2 + `@font-face` + preloads + `--font-serif`
  consumers (grep `--font-serif` and `font-serif` across `frontend/src` and
  latere-ui component overrides; re-point each to the display sans).
- Add Space Grotesk (500/700, latin) as display face; `--font-display` token
  introduced and used by headings/wordmark. Body stays Hanken Grotesk.
- Bundle JetBrains Mono (400/500) woff2; `--font-mono` lists it first.
- Update `index.html` preloads (drop serif, add display + mono) and keep the
  no-flash theme script byte-compatible.

### Brand override

In-repo override (e.g. `styles/brand-override.css`, loaded after latere-ui
styles): `.wallfacer-brand` renders in `--font-display`, normal style (not
italic), `background-image: var(--accent-gradient)`. The brick logo mark SVG
recolors to indigo (check `assets/` and latere-ui `Logo` component prop
surface for an in-repo way to tint; if the SVG is inlined from latere-ui,
override via CSS `filter`/`currentColor` only — do not fork latere-ui).

### data-theme observers

Update the four hardcoded theme maps to the new hues: xterm palette in
`TerminalPanel.vue`, CodeMirror theme in `FileEditor.vue` (one-dark may stay
but its chrome colors should sit on the new dark surfaces), chart palette in
`AnalyticsTabCost.vue`, mermaid `themeVariables` in `mermaidRender.ts`.

### Sweep for hardcoded warm colors

Grep `frontend/src` (and `assets/`) for the old literals (`#f4f1ea`, `#c45a33`,
`#d97757`, `#a84e2e`, `#ece8de`, warm rgba `31,29,26` etc.) and re-point any
component-scoped stragglers to tokens. `public/static/overview-*.png`
marketing screenshots are re-captured by the docs child, not here.

## Testing Strategy

- `bunx vitest run` and `make ui-test` stay green (geometry checks are
  theme-independent).
- Full ui-shots sweep light + dark (`seed.mjs` → boot → `snap.mjs` both
  themes); eyeball every surface for contrast regressions, especially badge
  tint pairs on dark.
- Manual WCAG spot checks: `--ink-3` on `--bg`, accent-on-card, status badge
  pairs (target ≥ 4.5:1 body, ≥ 3:1 large/UI).
- `make build` green (vue-tsc, vite-ssg, golangci-lint).

## Outcome

Shipped 2026-07-05 (commits 501d2c24, e500d7fa, 52499469, a5b319b0,
9550ee51). Deviations from the plan:

- The stale duplicate token block lived in `board-tokens.css`, not
  `base.css` (base.css holds the tag palette and reset). board-tokens.css
  was deleted outright; tokens.css is the single definition site.
- Body type consolidated on the already-bundled Inter instead of keeping
  Hanken Grotesk; Hanken and Instrument Serif woff2 files were removed.
  Space Grotesk (display) and JetBrains Mono ship as variable fonts.
- The latere-ui override needed a `[data-theme]` specificity prefix because
  component-imported CSS can emit after entry CSS; it also re-skins the
  console sidebar brand row (`.lu-cs-*`).
- The editorial drop caps were removed rather than restyled.
- The brick logo SVGs (Sidebar, BrandMark, SiteNav, InstallPage) and the
  1024px raster app icon were recolored to the indigo trio.
- Verified: full ui-shots sweep light+dark, `make ui-test` geometry checks,
  vitest, vue-tsc, `make build`, and GOWORK=off build.
