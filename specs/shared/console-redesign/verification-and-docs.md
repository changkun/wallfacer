---
title: Verification and Docs
status: drafted
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
