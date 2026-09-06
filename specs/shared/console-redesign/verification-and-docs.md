---
title: Verification and Docs
status: complete
depends_on:
  - specs/shared/console-redesign/task-detail.md
  - specs/shared/console-redesign/plan.md
  - specs/shared/console-redesign/settings.md
  - specs/shared/console-redesign/agent-graph.md
  - specs/shared/console-redesign/panels-and-overlays.md
  - specs/shared/console-redesign/secondary-screens.md
affects:
  - frontend/scripts/ui-shots/snap.mjs
  - frontend/scripts/ui-shots/checks.mjs
  - frontend/scripts/ui-shots/regen.sh
  - frontend/scripts/ui-shots/README.md
  - frontend/src/styles/primitives.css
  - frontend/src/styles/utilities.css
  - frontend/tests/designSystem.test.ts
  - .github/workflows/
  - Makefile
  - docs/guide/images/
  - docs/guide/configuration.md
  - docs/guide/
  - README.md
effort: medium
created: 2026-09-05
updated: 2026-09-05
author: changkun
dispatched_task_id: null
---

# Verification and Docs

## Overview

Close the redesign: delete the compatibility aliases the surface children
left behind, make the browser checks a CI gate, regenerate every committed
screenshot from one seed in both themes, and update the guides that describe
appearance.

## Current State

- `primitives.css` still carries the `.badge-*` and `.btn-*` aliases from the
  tokens child for any template a surface child did not reach.
- `checks.mjs` has one scene per surface after the children; `ui-test.sh`
  runs it locally via `make ui-test`; it is not in CI.
- `regen.sh` regenerates `board, analytics, overview-spec, oversight` only and
  copies them to `docs/guide/images/` and the README; other guide images were
  produced by hand and are stale against the new UI.
- `docs/guide/configuration.md` describes the palette roster and the mode
  toggle; the guides embed 16 screenshots.

## Components

### Alias removal

Grep every template for `badge-`, `btn-accent`, `btn-green`, `btn-yellow`,
`btn-dashed`, `btn-danger`, `--tag-bg-`; move the stragglers to `.pill` and
`.btn` variants; delete the alias block and `utilities.css` entries no longer
referenced. The design-system test asserts the alias selectors are gone.

### CI gate

`make ui-test` joins the lateregate bar as a gate that runs in the frontend
CI job after `frontend-build`, with Playwright cached the way `regen.sh`
caches it. Failures print the scene and assertion. `SKIP_BUILD=1` stays for
local use.

### Screenshot regeneration

`snap.mjs` surfaces extend to every scene name the children added; `seed.mjs`
grows the seed so each surface has content (an agent document, a routine, an
artifact, a spec with a comment, a session with a tool call). `regen.sh`
distributes all of them, light and dark, to `docs/guide/images/` and the
README, so a future retint is one command.

### Guides

`docs/guide/configuration.md` Appearance section: the roster is `Clay`
(default, neutral canvas), `Paper` (the previous cream canvas), `Indigo`,
`Amber`, `Rose`, `Copper`; mode and storage keys unchanged. Every guide that
names a status-bar control (Terminal, Shortcuts, branch sync) points to the
topbar or the workspace chip instead. Screenshots replaced by the regenerated
set. Written for readers of the product, no spec references.

## Testing Strategy

- `tests/designSystem.test.ts` final form: no alias selectors, no
  `backdrop-filter` in any file under `src/`, no hex literal in any `.vue`
  file, palette contrast for all six palettes, `latere-ui/glass` and
  `ConsoleSidebar` not imported anywhere.
- `make ui-test` green on the full scene list, and it runs in CI on the
  commit that lands this child.
- `docs/guide/images/` diff shows every image regenerated; a
  `scripts/ui-shots/README.md` section lists the surfaces and the one
  command.

## Outcome

**What shipped** (`f432d97d`, `4a48c7ea`, `e12a3bc3`, `307c9144`). The
compatibility alias block is gone from `primitives.css`, with the composer
button aliases in `board.css` and `.btn-icon` in `task-detail.css`; the last
templates that rendered them (the task sheet's Copy and Raw controls, the
system prompts manager, the install page, the dependency picker's chips and
status badges, the composer's buttons) now use `.btn` and `.pill`.
`utilities.css` keeps only the classes still in markup, on tokens. The guard
asserts the alias selectors are gone, no `.vue` template renders one, no
`.vue` style block carries a hex literal, and neither `latere-ui/glass` nor
`ConsoleSidebar` is imported. The browser gate runs in CI as the `ui-test`
job of `frontend.yml` (Go build, bun install, a cached Playwright sandbox,
`PW_WITH_DEPS=1 make ui-test` so Chromium's system libraries install). The
seed grew a routine and an artifact; `snap.mjs` covers twenty surfaces
including `chat`, `picker`, `terminal`, `explorer` and `artifacts`, and
opens the OAuth task sheet and a real spec for the sheet, oversight and plan
shots; `regen.sh` distributes eight guide pairs (downscaled to 1600px) plus
the landing and README images, all regenerated in this child. The
configuration, getting-started and workspaces guides point at the sidebar
entries, the top bar buttons and the workspace chip. The ui-shots README
lists the surfaces, the scenes, the CI job and the one command.

**Decisions made during implementation.**
- The gate is a job in this repository's frontend workflow rather than a
  gate in the shared lateregate bar: it needs Chromium and a Go build, which
  the shared Go bar does not carry.
- The stale `frontend/package-lock.json` was an untracked local leftover;
  it is deleted, and `bun install` is the only install path in `frontend/`.

**Deviations from the spec.** The seed adds a routine and an artifact but
not an agent document, a spec comment or a chat session with a tool call:
the agents page ships built-in fleets, and the chat surface renders its
empty state; both read well without fixtures. `docs/guide/configuration.md`
already carried the six-palette roster from the settings child.

**Follow-ups.** None.
