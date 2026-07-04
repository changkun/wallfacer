---
title: Visual Identity Rebrand & Docs Rewrite
status: drafted
depends_on: []
affects:
  - frontend/src/styles/
  - frontend/public/fonts/
  - frontend/index.html
  - frontend/src/views/ProductPage.vue
  - frontend/src/views/DocsIndex.vue
  - frontend/src/views/InstallPage.vue
  - frontend/src/views/NotFoundPage.vue
  - frontend/src/data/docs.ts
  - frontend/scripts/ui-shots/
  - docs/
  - internal/cli/cli.go
  - internal/cli/server.go
  - README.md
effort: xlarge
created: 2026-07-05
updated: 2026-07-05
author: changkun
dispatched_task_id: null
---

# Visual Identity Rebrand & Docs Rewrite

## Overview

Wallfacer's current visual identity reads as an Anthropic/Claude product: a warm
cream canvas (`#f4f1ea`, near Claude's `#faf9f5`), a terracotta/clay accent
(`#c45a33` in-app, literal Anthropic coral `#d97757` in the wordmark gradient),
and Instrument Serif italic display type. Users have repeatedly pointed out the
resemblance. This spec replaces that identity with a distinct "indigo on zinc"
system, rebuilds the marketing site as a modern animated/interactive surface,
and tears down and rewrites the documentation set, which has drifted badly from
the shipped application (removed features still documented, shipped features
undocumented).

Decisions already made with the user:

- **Palette**: indigo on zinc. Cool neutral grays, electric indigo accent
  (`#5b5bd6 → #7c6cf0` gradient), no cream, no terracotta.
- **Typography**: all-sans plus mono accents. The serif (Instrument Serif)
  leaves wallfacer entirely, including the wordmark rendering inside this app.
- **Marketing site**: full interactive rebuild — animated hero simulation,
  scroll-triggered reveals, interactive capability demos. No heavy animation
  dependencies.
- **Docs**: complete re-architecture from the current application surface, not
  an incremental edit.

## Current State

### Theme system

- `frontend/src/styles/tokens.css` is the canonical token file ("Paper-ink
  palette with clay accent"): `:root, [data-theme="light"]` and
  `[data-theme="dark"]` blocks defining surfaces (`--bg`, `--bg-sunk`,
  `--bg-elevated`, `--bg-card`, `--bg-sidebar`), ink ladder (`--ink` …
  `--ink-4`), rules, accent (`--accent: #c45a33`, `--accent-2`,
  `--accent-soft`, `--accent-tint`), semantic colors, tint pairs, elevation
  shadows, glass tokens, and terminal-surface tokens.
- `frontend/src/styles/base.css:1-80` duplicates a subset of the same tokens
  with one divergent value (`--ink-3`); it loads after tokens.css in
  `src/main.ts`, so its values win. Must be reconciled.
- `frontend/src/styles/board-tokens.css` maps tag/badge tints.
- ~38 CSS files in `frontend/src/styles/` and all component-scoped styles
  consume tokens by name; components observing `data-theme` directly:
  `TerminalPanel.vue`, `editor/FileEditor.vue`, `analytics/AnalyticsTabCost.vue`,
  `lib/mermaidRender.ts`.
- The wordmark gradient (`.wallfacer-brand`, Anthropic coral `#d97757`) and
  Instrument Serif italic brand style live in the external pinned `latere-ui`
  package (`src/styles/brand.css`); wallfacer overrides them in-repo rather
  than forking the package.
- Fonts: Instrument Serif (display), Hanken Grotesk (UI sans), Inter
  (fallback), LXGW WenKai TC (zh), all self-hosted woff2 in
  `frontend/public/fonts/`, preloaded in `frontend/index.html`, declared in
  `frontend/src/styles/fonts.css`. `--font-mono` references JetBrains Mono but
  no mono woff2 is bundled.

### Marketing site

Cloud-mode routes in `frontend/src/router.ts`: `ProductPage.vue` (static hero
text + product screenshots + capability grid), `DocsIndex.vue` (hardcoded card
nav), `InstallPage.vue`, `NotFoundPage.vue`. No motion beyond trivial CSS; the
visuals are `<img>` screenshots (`/static/overview-*.png`). Prerendered by
vite-ssg, so all page code must be SSG-safe.

### Docs

- Source of truth: `docs/guide/` (13 guides + images), `docs/internals/` (10
  files), `docs/releases/`, `docs/cloud/`, embedded via `//go:embed docs`.
- Two independent nav indexes that drift: the Go server parses the "Reading
  Order" sections of `docs/guide/usage.md` + `docs/internals/internals.md`
  (`parseReadingOrder` in `internal/cli/server.go`) for local mode, while cloud
  mode uses the static `docIndex` array in `frontend/src/data/docs.ts` plus
  hardcoded cards in `DocsIndex.vue`.
- Confirmed drift: guides document the removed brainstorm/idea-agent flow and
  the retired separate Agents/Flows pages; agent-graph (`/agent-graph`),
  whiteboard, Mission Control (`/mission`), GitHub integration
  (`internal/github`), device sign-in (`wallfacer auth`), and the
  `wallfacer web` command are undocumented. `internal/cli/cli.go` `PrintUsage`
  omits `auth` and `web`. `CONTRIBUTING.md`'s internals table omits
  `plan-mode.md`. The `internal/runner` package comment still describes
  container sandboxes under the host-process model.

## Architecture

Three child specs, sequenced:

1. **[design-tokens.md](visual-identity/design-tokens.md)** — the new token
   and font system for the workspace app. Everything else renders through it.
2. **[marketing-site.md](visual-identity/marketing-site.md)** — the animated
   marketing/docs site rebuild on top of the new tokens.
3. **[docs-rewrite.md](visual-identity/docs-rewrite.md)** — docs teardown,
   re-architecture, rewrite, and screenshot regeneration (screenshots depend on
   1; prose can proceed in parallel).

The token contract is the load-bearing decision: **token names do not change**,
only values (plus any additive tokens the new design needs, e.g. gradient and
glow tokens). That keeps the ~38 style files and all component-scoped CSS
working without a mass rewrite, and keeps the redesign reviewable.

### New palette (both themes stay first-class)

| Token role | Light | Dark |
|---|---|---|
| Canvas `--bg` | `#fafafa` | `#0b0b0f` |
| Sunk `--bg-sunk` | `#f0f0f2` | `#08080b` |
| Elevated `--bg-elevated` | `#ffffff` | `#131318` |
| Card `--bg-card` | `#ffffff` | `#16161d` |
| Sidebar `--bg-sidebar` | `#f4f4f6` | `#0e0e13` |
| Ink `--ink` | `#18181b` | `#e4e4ea` |
| Accent `--accent` | `#5b5bd6` | `#7c6cf0` |
| Accent gradient | `#5b5bd6 → #7c6cf0` | same, brighter stops |
| Ok / warn / err | `#2ea36b` / `#d9930d` / `#d64545` | lifted variants |

Shadows go cool (blue-black instead of warm brown-black); glass tokens get the
new base hues; tag tint pairs re-derived on the zinc neutrals with indigo as
the featured hue.

### Typography

- Display + UI: one geometric/grotesque sans (Space Grotesk for display,
  keep Hanken Grotesk or Inter for body — final call in the child spec after
  rendering trials), self-hosted woff2.
- Mono: bundle JetBrains Mono woff2 (regular + medium) so `--font-mono` stops
  depending on system fonts; mono is a visible part of the new identity
  (labels, stats, section eyebrows).
- Instrument Serif files and `--font-serif` usages are removed from this repo;
  the in-app wordmark re-renders in the new sans with the indigo gradient via
  an in-repo `.wallfacer-brand` override (latere-ui stays untouched/pinned).
- LXGW WenKai TC stays for zh body text.

## Components

See child specs. Boundaries:

- **design-tokens** owns: `tokens.css`, `base.css` reconciliation (token block
  deleted from base.css, single source in tokens.css), `board-tokens.css`,
  `fonts.css`, `public/fonts/`, `index.html` preloads + no-flash script, the
  `.wallfacer-brand` override, dark/light verification of the four
  `data-theme`-observing components, and a full ui-shots light+dark sweep.
- **marketing-site** owns: the four cloud views, new shared motion utilities
  (`styles/animations.css` additions: scroll-reveal classes, glow/aura
  helpers, reduced-motion gating), the hero board/agent-graph canvas or SVG
  simulation component, interactive capability demos, and updated static
  assets. SSG-safety is an acceptance criterion (`bun run build` passes; no
  `window` access during setup/render of prerendered routes).
- **docs-rewrite** owns: the new docs information architecture, all rewritten
  guide + internals content, deletion of stale content, the single-source nav
  index (server-parsed reading order stays canonical; `frontend/src/data/docs.ts`
  and `DocsIndex.vue` derive from it at build time or are regenerated from it),
  CLI usage text fixes (`auth`, `web`), `CONTRIBUTING.md`/`README.md` refresh,
  and regenerated light+dark screenshots via ui-shots after the retheme lands.

## Testing Strategy

- `make build` (vue-tsc + vite-ssg + golangci-lint) green at every commit.
- Existing unit/visual harnesses: `bunx vitest run`, `make ui-test`
  (geometry assertions are theme-independent and must stay green).
- ui-shots full sweep light+dark before/after for the retheme; marketing pages
  captured via a cloud-mode variant snap.
- Docs: `internal/cli` doc-index tests (reading-order parse) updated to the new
  structure; every doc link checked (no dangling slugs between `data/docs.ts`,
  `DocsIndex.vue`, and `docs/guide/`).
- Reduced-motion: marketing pages verified with `prefers-reduced-motion:
  reduce` emulation (no scroll-jacking, content readable without JS motion).
