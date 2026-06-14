---
title: Visual Verification for UI Changes
status: drafted
depends_on: []
affects:
  - frontend/scripts/ui-shots/diff.mjs
  - frontend/tests/visual/
  - frontend/package.json
  - .github/workflows/test.yml
  - .gitignore
effort: medium
created: 2026-03-21
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Plan: Visual Verification for UI Changes

(Moved from specs/oversight/ to specs/local/ on 2026-06-14.)

## Problem

UI changes are merged without visual verification. CSS and layout issues
(broken flex containers, overflowing menus, misaligned elements) ship
because the Vitest suite only covers logic and component state. There are
no committed screenshot baselines and no pixel-diff assertion, so neither
CI nor an agent can confirm a change still looks correct.

## Current State (as of 2026-06-14)

- **Frontend tests**: Vitest files (under `frontend/tests/` and
  `frontend/src/**/*.test.ts`) run in a Node VM context with mocked
  browser APIs. They assert on state and rendered markup, not pixels, so
  they cannot detect visual regressions.
- **Backend tests**: Go test suite via `go test ./...`. No browser
  integration.
- **UI serving**: The Vue frontend is built by Vite (`vite-ssg build`)
  into `frontend/dist`, embedded into the Go binary via
  `//go:embed all:dist` in `internal/webserver/spa/embed.go`, and served
  by `MountSPA` (`internal/webserver/spa.go`). `MountSPA` serves
  `index.html` directly (no Go templating) and static assets from the
  embedded FS. A Vite dev server (`:5173`) exists for local development.
- **CI**: `.github/workflows/test.yml` builds the frontend with bun,
  runs `go test ./...`, and runs `bunx vitest run` with coverage. No
  browser-based step.
- **Capture harness already exists**: `frontend/scripts/ui-shots/`
  (`seed.mjs` + `snap.mjs`) is a Playwright-based seed-and-screenshot
  harness. `seed.mjs` writes a deterministic on-disk store and an
  isolated config home; `snap.mjs` boots a browser against any local-mode
  origin and screenshots named surfaces (`board`, `palette`,
  `task-detail`, `settings`, `analytics`, `plan`, `routines`, `agents`,
  `flows`, `docs`) at retina 2x, in light and dark. It **captures** PNGs;
  it does **not** assert against committed baselines.

What is missing is the verification layer: committed baseline images,
a pixel-diff assertion step, and a CI job that runs it. `@playwright/test`
is also not yet a declared frontend devDependency (snap.mjs relies on an
ambient `playwright` install).

---

## Approach: Baseline Pixel-Diff on Top of ui-shots

Reuse the existing `ui-shots` capture harness rather than building a new
one. The harness already solves the hard parts: deterministic seeding,
the active-workspace-group constraint, boot-config injection, theme
pinning, and surface enumeration. The remaining work is to:

1. Commit a baseline PNG per surface (and theme/viewport variant).
2. Add a diff step that compares a fresh capture against the baseline and
   fails when the pixel delta exceeds a tolerance.
3. Wire that diff step into CI as advisory tooling that uploads diff
   artifacts on mismatch.

Two viable diff mechanisms:

- **Light**: keep `snap.mjs` as the capturer and add a small Node diff
  script (`pixelmatch` + `pngjs`) that compares `--out` against a
  committed baseline dir and writes diff PNGs. No new test runner.
- **Heavier**: adopt `@playwright/test` and its built-in
  `toHaveScreenshot()` baseline machinery, reusing the seed step and the
  boot-config injection from `snap.mjs`.

Prefer the light approach first: it reuses the working harness with the
least new surface area and avoids a second test runner alongside Vitest.

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
│ Visual diff run                                        │
│                                                        │
│  1. node seed.mjs            → deterministic store +   │
│                                isolated config home    │
│  2. HOME=/tmp/wf-demo-home ./wallfacer run \           │
│       -data /tmp/wf-demo-data -addr :8099 -no-browser  │
│  3. node snap.mjs --base :8099 --out /tmp/wf-shots     │
│       (and --theme dark for the dark variants)         │
│  4. diff /tmp/wf-shots against committed baselines;    │
│     write diff PNGs for any surface over tolerance     │
│  5. Kill server                                        │
│                                                        │
│  Baselines: frontend/tests/visual/baselines/           │
│  Diffs on mismatch: frontend/tests/visual/diffs/       │
└────────────────────────────────────────────────────────┘
```

---

## Phase 1: Diff Tooling (remaining)

### 1.1 Add a diff dependency

Add `pixelmatch` + `pngjs` (or `@playwright/test` if the heavier path is
chosen) as frontend devDependencies, plus a declared `playwright` /
`@playwright/test` devDependency so the harness no longer relies on an
ambient install. Use bun (`bun add -d ...`) to match the repo toolchain.

Add scripts to `frontend/package.json`:

```json
{
  "scripts": {
    "test:visual": "node scripts/ui-shots/diff.mjs",
    "test:visual:update": "node scripts/ui-shots/diff.mjs --update"
  }
}
```

### 1.2 Diff script

Create `frontend/scripts/ui-shots/diff.mjs`:

- Run (or accept the output of) `snap.mjs` for the selected surfaces and
  themes.
- For each captured PNG, compare against
  `frontend/tests/visual/baselines/<name><suffix>.png` with `pixelmatch`.
- On mismatch over tolerance, write a diff PNG to
  `frontend/tests/visual/diffs/` and exit non-zero.
- `--update` copies fresh captures into the baseline dir.

### 1.3 Gitignore

Add to `.gitignore`:

```
frontend/tests/visual/diffs/
```

Baseline screenshots (`frontend/tests/visual/baselines/`) are committed.

### Files

- `frontend/scripts/ui-shots/diff.mjs` (new)
- `frontend/package.json` (add devDependencies + scripts)
- `.gitignore` (add diffs dir)

---

## Phase 2: Baseline Coverage

Capture and commit baselines for the surfaces `snap.mjs` already exposes.
Start with the highest-value layouts:

| Surface | What it catches |
|---------|-----------------|
| `board` | Card rendering, status colors, column spacing |
| `palette` | Command palette overlay, list alignment |
| `task-detail` | Detail drawer: logs, oversight, diff sections |
| `settings` | Settings layout, tab strip, form controls |
| `plan` | Spec tree + focused view + planning chat layout |

Capture each in both themes (`snap.mjs --theme dark`). The harness
already names dark outputs with a `-dark` suffix, so baselines pair as
`board.png` / `board-dark.png`.

### Files

- `frontend/tests/visual/baselines/*.png` (new, committed)

---

## Phase 3: CI Integration

Add a step (or a small job) to `.github/workflows/test.yml`, matching the
existing toolchain: go-version from `go.mod`, frontend built with bun, no
hardcoded versions.

```yaml
      - name: Visual diff
        continue-on-error: true
        run: |
          go build -o wallfacer .
          node frontend/scripts/ui-shots/seed.mjs
          HOME=/tmp/wf-demo-home ./wallfacer run \
            -data /tmp/wf-demo-data -addr :8099 -no-browser &
          bunx playwright install --with-deps chromium
          bun run test:visual --base http://localhost:8099
      - name: Upload visual diffs
        if: failure()
        uses: actions/upload-artifact@v4
        with:
          name: visual-diffs
          path: frontend/tests/visual/diffs/
          retention-days: 7
```

Keep it advisory (`continue-on-error: true`) rather than a blocking gate.
Cross-platform Chromium rendering differs between macOS and Linux (see
Risk Areas), so a hard gate would produce false failures; uploading diff
artifacts lets reviewers see exactly what changed without blocking merges.

---

## Phase 4: Viewport Coverage (optional)

`snap.mjs` already takes `--width` / `--height`. Add baseline variants at
common breakpoints (e.g. `1440x900` desktop, `768x1024` tablet,
`390x844` mobile) by capturing per size and suffixing baselines
accordingly. Defer until the desktop baselines are stable to avoid
multiplying baseline maintenance early.

Theme coverage is already handled by `snap.mjs --theme`.

---

## Phase 5: Agent-Friendly Workflow

### 5.1 Programmatic verification

An agent making UI changes runs the seed, boot, and diff steps, then
inspects the diff PNGs under `frontend/tests/visual/diffs/` to understand
what broke.

### 5.2 Updating baselines

After an intentional visual change:

```bash
bun run test:visual:update
git add frontend/tests/visual/baselines/
```

Updated baselines are committed alongside the code change so reviewers can
inspect the visual diff in the PR.

### 5.3 Selective runs

Reuse `snap.mjs --only <a,b,c>` to capture (and diff) a subset of surfaces
while working on a single area.

---

## Implementation Order

```
Phase 1 (Diff tooling):-1diff.mjs, devDeps, scripts, gitignore
Phase 2 (Baselines):-1capture + commit baseline PNGs per surface/theme
Phase 3 (CI):-1advisory visual-diff step, artifact upload
Phase 4 (Viewports):-1optional breakpoint baselines
Phase 5 (Agent workflow):-1document the seed/boot/diff/update loop
```

---

## Risk Areas

1. **Flaky screenshots**: Timestamps, animations, and async rendering
   cause non-deterministic pixels. Mitigate with:
   - a pixel-diff tolerance (small `maxDiffPixelRatio`)
   - the harness already waits after `load` rather than `networkidle`
     (the board holds SSE connections open), and the seed data uses fixed
     UUIDs and timestamps
   - inject `* { animation: none !important; }` before the screenshot
   - note the in_progress caveat: startup recovery reconciles the seeded
     in_progress task to `waiting`, so baselines must reflect that

2. **Server startup time**: `seed.mjs` then `wallfacer run` must be up
   before `snap.mjs` connects. Poll the address (or wait for a healthy
   response) before capturing on slow CI machines.

3. **Baseline maintenance**: Baselines are committed and must be updated
   when visual changes are intentional. The `test:visual:update` script
   makes this a one-liner.

4. **Cross-platform rendering**: Chromium renders slightly differently on
   macOS vs Linux. CI baselines must be generated on the same OS as CI
   (Ubuntu). Developers on macOS should refresh baselines via
   `test:visual:update` and let CI be the source of truth. This is why the
   CI step stays advisory rather than a blocking gate.

5. **Ambient Playwright**: `snap.mjs` currently imports `playwright` from
   an ambient install. CI must declare and install it (devDependency +
   `playwright install chromium`) or the diff step cannot run.

---

## Verification

1. `bun run test:visual` passes locally against committed baselines.
2. The CI visual-diff step runs (advisory) on a clean checkout and
   uploads diff artifacts on mismatch.
3. Intentional CSS breakage (e.g. removing flex from the board layout)
   produces a clear diff PNG for the affected surface.
4. An agent can run the seed/boot/diff loop and interpret the diff output.
