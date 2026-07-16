---
title: Visual Verification for UI Changes
status: archived
depends_on: []
affects:
  - frontend/scripts/ui-shots/checks.mjs
  - frontend/scripts/ui-shots/ui-test.sh
  - .github/workflows/test.yml
  - Makefile
effort: small
created: 2026-03-21
updated: 2026-07-16
author: changkun
dispatched_task_id: null
---

# Plan: Visual Verification for UI Changes

(Moved from specs/oversight/ to specs/local/ on 2026-06-14.)

## Problem

UI changes could merge without visual verification. CSS and layout
issues (broken flex containers, overflowing menus, misaligned elements)
shipped because the Vitest suite only covers logic and component state,
not real browser layout.

## Current State (as of 2026-07-16)

The verification layer this spec called for now exists, built as
**deterministic geometry assertions** rather than the pixel-diff
baseline approach originally proposed below:

- **Capture harness**: `frontend/scripts/ui-shots/` (`seed.mjs` +
  `snap.mjs`) seeds deterministic demo data in an isolated config home
  and screenshots named surfaces at retina 2x, in light and dark. Still
  capture-only (no baseline comparison) — used for docs/marketing
  screenshots (`regen.sh`), not regression detection.
- **Regression checks**: `frontend/scripts/ui-shots/checks.mjs` is a
  Playwright assertion runner that measures real `getBoundingClientRect`
  geometry against a browser, catching the two regression classes jsdom
  cannot: render crashes that blank a region (uncaught page errors +
  zero-size structural checks; scenes `board`, `switcher`) and broken
  layout (row width/offset/overlap/clipping checks; scene `picker`),
  plus `settings` / `plan` / `analytics` / `agents` / `flows` smoke
  (route renders, no uncaught error). Added in commit `62f623f8`,
  validated end to end (re-injecting the picker regression makes the
  check fail as intended).
- **Orchestration**: `frontend/scripts/ui-shots/ui-test.sh` builds the
  SPA + binary, seeds, boots wallfacer, runs `checks.mjs`, and exits
  non-zero on regression. Wired as `make ui-test`
  (`SKIP_BUILD=1 make ui-test` reuses an existing binary for fast
  iteration). Playwright runs from a throwaway `/tmp` sandbox
  (`/tmp/ui-shots-pw`), deliberately not a `frontend/` devDependency —
  an npm install under `frontend/` breaks the `vite-ssg` build.
- **Deliberately not pixel-diffing**: the commit message for
  `checks.mjs` records the rationale directly — "deterministic geometry,
  not pixel-diffing, so no cross-machine drift." This sidesteps the
  cross-platform Chromium rendering variance (macOS vs Linux) that would
  otherwise force a pixel-diff CI step to stay advisory-only.

What remains is wiring `make ui-test` into CI. It is not yet referenced
by `.github/workflows/test.yml`.

---

## Approach: Wire `make ui-test` into CI

The verification mechanism (`checks.mjs` via `make ui-test`) already
works locally and needs no new tooling. The remaining work is operational:
run it in CI so regressions are caught before merge, not just when a
developer remembers to run it locally.

Baseline pixel-diffing (committed PNGs, `pixelmatch`/`@playwright/test`
`toHaveScreenshot()`) is no longer the planned direction — geometry
assertions already catch the render-crash and broken-layout classes this
spec set out to catch, without the cross-machine flakiness that would
otherwise require a pixel-diff CI step to stay advisory. Pixel-diff
baselines remain an option only if a future regression class geometry
checks can't express (e.g. color/contrast regressions) makes it worth
the added maintenance.

### Constraints inherited from the harness

- The board only renders cards when a non-empty workspace group is
  active. The config dir is `$HOME/.wallfacer` and is not overridable by
  a flag, so seeding must use the isolated-HOME mechanism in `seed.mjs`
  (which writes `<home>/.wallfacer/workspace-groups.json` and seeds tasks
  under the matching group key). Do not seed via runtime API calls in a
  fresh server expecting the board to render.
- `snap.mjs` injects boot config via `window.__WALLFACER__` and pins the
  theme via the `wallfacer-theme` localStorage key (read by `prefs.ts`).
- Working selectors come from the live Vue app: the board card root is
  `.card`; surfaces are reached by route (`/settings`, `/plan`, etc.).
  Do not reuse old vanilla-UI class names (`.board-col`, `.app-header`,
  `.modal-card`).

### Architecture

```
┌────────────────────────────────────────────────────────┐
│ make ui-test (ui-test.sh)                              │
│                                                        │
│  1. make frontend-build && go build -o wallfacer .     │
│     (skipped with SKIP_BUILD=1)                        │
│  2. node seed.mjs             → deterministic store +  │
│                                 isolated config home    │
│  3. HOME=/tmp/wf-uitest-home ./wallfacer run \         │
│       -data /tmp/wf-uitest-data -addr :8097 -no-browser│
│  4. node checks.mjs --base :8097  → geometry assertions │
│     per scene; exit non-zero on any failure            │
│  5. Kill server (trap on exit)                         │
└────────────────────────────────────────────────────────┘
```

---

## Phase 1: CI Integration

Add a step to `.github/workflows/test.yml` (the `test-linux` job, which
already runs on `ubuntu-latest` and builds the frontend with bun) that
runs `make ui-test` after the frontend build:

```yaml
      - name: UI regression checks
        continue-on-error: true
        run: make ui-test
```

Keep it advisory (`continue-on-error: true`) initially: `checks.mjs`
launches its own Playwright/Chromium install into `/tmp/ui-shots-pw` on
first run, which needs validating on a fresh CI runner (network access,
sandbox permissions) before it can gate merges. Promote to a blocking
step once a run has been observed green on CI.

### Files

- `.github/workflows/test.yml` (add the `UI regression checks` step to
  `test-linux`)

---

## Phase 2: Scene Coverage (optional, as regressions are found)

`checks.mjs` already covers `board`, `switcher`, `picker`, and smoke
routes for `settings` / `plan` / `analytics` / `agents` / `flows`. Add a
new scene to the `SCENES` table (or a new `SMOKE_ROUTES` entry) whenever
a jsdom-invisible layout bug ships — following the pattern in commit
`62f623f8`: reproduce the regression, add geometry assertions that fail
against the broken state, confirm they pass against the fix.

---

## Risk Areas

1. **Server startup time**: `seed.mjs` then `wallfacer run` must be up
   before `checks.mjs` connects. `ui-test.sh` already polls
   `curl -sf $BASE/` for up to 40s before failing with the server log
   tail; widen the timeout if CI runners prove slower.

2. **Playwright install on CI**: `ui-test.sh` installs `playwright` +
   the `chromium` binary into `/tmp/ui-shots-pw` on first run (no
   `frontend/` devDependency, so the `vite-ssg` build stays untouched).
   The first CI run pays this install cost; confirm the runner allows
   the npm install and browser download before promoting the step from
   advisory to blocking.

3. **`in_progress` caveat**: startup recovery
   (`internal/runner/recovery.go`) reconciles a seeded `in_progress` task
   with no live process to `waiting`. Any new scene asserting on task
   status must account for this rather than expect `in_progress` to
   persist.

---

## Verification

1. `make ui-test` passes locally against the current UI.
2. The CI `UI regression checks` step runs (advisory) on a clean
   checkout and reports scene failures in the job log.
3. Re-introducing a known regression (e.g. the picker grid width bug
   fixed in `fc13baa2`) makes the corresponding scene fail, confirming
   the CI step actually catches what it claims to.
