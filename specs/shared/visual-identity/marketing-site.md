---
title: Animated Marketing Site Rebuild
status: drafted
depends_on:
  - specs/shared/visual-identity/design-tokens.md
affects:
  - frontend/src/views/ProductPage.vue
  - frontend/src/views/DocsIndex.vue
  - frontend/src/views/InstallPage.vue
  - frontend/src/views/NotFoundPage.vue
  - frontend/src/styles/animations.css
  - frontend/src/styles/app/
  - frontend/src/components/site/
  - frontend/public/static/
effort: large
created: 2026-07-05
updated: 2026-07-05
author: changkun
dispatched_task_id: null
---

# Animated Marketing Site Rebuild

## Overview

Rebuild the cloud-mode website (product page, docs landing, install page, 404)
as a modern, motion-rich surface in the new indigo-on-zinc identity. The
current site is static text plus product screenshots on the cream palette. The
new site replaces screenshots-as-hero with a live, animated product simulation
and adds scroll-driven reveals and interactive capability demos — all
hand-rolled (CSS + IntersectionObserver + rAF canvas/SVG), no animation
library, SSG-safe under vite-ssg.

## Current State

- `frontend/src/router.ts` cloud routes: `/` ProductPage, `/docs` DocsIndex,
  `/docs/:slug` DocPage, `/install` InstallPage, `*` NotFoundPage.
- `ProductPage.vue`: hero headline ("Wallfacer is an autonomous engineering
  platform"), static board screenshot, alternating feature sections with more
  screenshots, harness logo row (Claude/Codex/Cursor/OpenCode/Pi), capability
  stack grid, latere-ui footer.
- `frontend/src/styles/animations.css` has two shared keyframes
  (`wf-content-in`, `wf-text-shimmer`), reduced-motion gated;
  `styles/app/responsive-motion.css` has marketing responsive rules.
- vite-ssg prerenders these routes: any `window`/`document` access must live in
  `onMounted` or be guarded; SSE/rAF loops must start client-only.

## Components

### Motion utilities (`styles/animations.css` + a small composable)

- `useScrollReveal` composable: one shared IntersectionObserver; elements with
  `data-reveal` get `.is-revealed` (opacity/translate/blur transitions in
  CSS). Threshold/stagger via data attributes. No-ops under
  `prefers-reduced-motion` and during SSG (guarded in `onMounted`).
- Aura/glow helpers: `.wf-glow` positioned radial gradients using
  `--glow-accent`; subtle grid/dot background pattern for the hero
  (CSS-only, `background-image`).
- Count-up stat helper for animated numbers (rAF, starts on reveal).

### Hero simulation (`components/site/HeroSim.vue`)

The centerpiece: a self-playing product simulation instead of a screenshot.
Renders a stylized board + agent-graph in inline SVG (not canvas — crisper on
retina, styleable with tokens, SSR-serializable): task cards spawn in Backlog,
an agent node graph pulses as cards animate across columns to Done, edges
stream dots (dash-offset animation). Driven by a small deterministic
scripted timeline (rAF; starts in `onMounted`, pauses off-viewport
via IntersectionObserver, static first-frame under reduced motion / no-JS so
SSG output shows a complete scene).

### Section rebuilds (ProductPage)

- Hero: display-sans headline, mono eyebrow, gradient accent text, HeroSim,
  install one-liner with copy button.
- Product tour sections become interactive demos where cheap, screenshots
  where not: e.g. a spec-tree mini-diagram that expands on hover/scroll, an
  autonomy-spectrum slider (chat → spec → task → autopilot) the user can drag,
  animated terminal-style log ticker for the oversight section.
- Harness row: real logos, mono labels, marquee or staggered reveal.
- Capability stack: cards with hover lift + glow, scroll-staggered.
- Stats band: animated counters (tasks run, harnesses, cost visibility).

### DocsIndex / InstallPage / NotFoundPage

Same identity: zinc surfaces, indigo accents, reveals. InstallPage gets a
tabbed platform selector (macOS/Linux/Windows) with copy-to-clipboard. 404
gets a small indigo-glitch wordmark moment. DocsIndex nav cards derive from
the docs-rewrite child's single-source index (coordinate; if docs-rewrite
lands later, keep the current card set restyled).

### Assets

Re-capture `public/static/overview-*.png` pairs in the new theme once
design-tokens lands (ui-shots). Any remaining screenshots get consistent
framing (rounded card, cool shadow, thin rule).

## Testing Strategy

- `bun run build` (vue-tsc + vite-ssg) — prerender must succeed; grep dist
  HTML for hero first-frame content (SSG-safety proof).
- Vitest: `useScrollReveal` (reduced-motion no-op, observer wiring with
  happy-dom), HeroSim timeline module (pure functions for scene state).
- Playwright pass (scratch script or ui-shots cloud variant): each page light
  + dark, plus `prefers-reduced-motion: reduce` emulation — content fully
  visible without motion, no horizontal scroll at 375px/768px/1440px.
- Console-error-free load on all four routes (snap.mjs already surfaces page
  errors).
