# ui-shots — seed + screenshot harness

Durable harness for UI work: seed deterministic demo data, boot wallfacer
against it, screenshot named surfaces at retina 2x.

## One-command flow

```bash
# 1. Seed demo data + isolated config home (prints the exact boot command)
node frontend/scripts/ui-shots/seed.mjs

# 2. Boot wallfacer pointed at the demo data, isolated HOME (background)
HOME=/tmp/wf-demo-home ./wallfacer run \
  -data /tmp/wf-demo-data -addr :8099 -no-browser &

# 3. Snap all surfaces to /tmp/wf-shots
node frontend/scripts/ui-shots/snap.mjs --base http://localhost:8099 --out /tmp/wf-shots
```

`seed.mjs` prints a JSON blob whose `boot` field is the exact step-2 command.

## seed.mjs

Writes a deterministic store under `<data>/<groupKey>/<uuid>/` matching the
real on-disk schema (`internal/store/models.go`): `task.json`, `traces/`,
`oversight.json`. Seven tasks span backlog / in_progress / waiting / done /
failed, with varied badges: `priority:*` / `impact:*` tags, test pass/fail
(`last_test_result`), and a backlog card with `depends_on` deps.

Why an isolated HOME: the config dir is `$HOME/.wallfacer` (no flag overrides
it). The board only renders cards when a non-empty workspace set is active, so
seed.mjs creates a demo workspace dir, computes its group key the same way the
Go server does (`sha256(sorted_paths joined by ":")[:8]`), seeds tasks under
that key, and writes `<home>/.wallfacer/workspace-groups.json` so a boot with
`HOME=<home>` restores that group.

Idempotent (group dir wiped + rewritten) and deterministic (fixed UUIDs and
timestamps). Re-running regenerates byte-identical data.

Flags: `--data` (default `/tmp/wf-demo-data`), `--home`
(`/tmp/wf-demo-home`), `--ws` (`/tmp/wf-demo-ws`).

### in_progress caveat

Startup recovery (`internal/runner/recovery.go`) reconciles orphaned
in_progress tasks with no live process — they move to `waiting` (or `failed`
if worktree paths are missing). So a board card cannot be frozen in
in_progress; the seeded in_progress task renders as `waiting` after boot. All
other states are stable.

## snap.mjs

Self-contained Playwright capture (no dependency on `.parity/`, which is
throwaway). Injects local-mode boot config, screenshots each surface at 2x.

```bash
node snap.mjs --base http://localhost:8099 --out /tmp/wf-shots          # all
node snap.mjs --base http://localhost:5173 --only board,palette,settings # subset
node snap.mjs --list                                                     # surface names
```

Surfaces: `board`, `palette`, `task-detail`, `settings`, `analytics`,
`plan`, `routines`, `agents`, `flows`, `docs`. Prints JSON
`[{name, file, errors}]` so callers can detect page errors.

Works against any local-mode origin: the embedded SPA on the booted server
(`:8099`), the golden UI (`:8092`), or the Vite dev server (`:5173`).

Flags: `--base` (default `http://localhost:8099`), `--out`
(`/tmp/wf-shots`), `--only <a,b,c>`, `--list`, `--width` / `--height`
(`1440x900`; deviceScaleFactor is always 2).

### Light + dark capture

`snap.mjs --theme dark` pins `wallfacer-theme=dark` (read by prefs.ts on init)
and suffixes output files with `-dark`. Capture both for theme-adaptive docs:

```bash
node frontend/scripts/ui-shots/snap.mjs --base http://localhost:8099 --out /tmp/wf-shots
node frontend/scripts/ui-shots/snap.mjs --base http://localhost:8099 --out /tmp/wf-shots --theme dark
```

## UI regression checks (`checks.mjs` + `ui-test.sh`)

`snap.mjs` captures images; `checks.mjs` **asserts** invariants in a real
browser, so it catches the two regression classes jsdom unit tests cannot:

- **Render crashes** — a thrown render blanks a region (e.g. "the entire
  sidebar disappeared"). Detected via uncaught page errors + structural
  presence checks (the element has a non-zero bounding box).
- **Broken layout** — CSS mislays / clips / overlaps elements (e.g. the
  Select Workspace list crammed into the wizard's narrow grid cell). Detected
  by measuring `getBoundingClientRect` geometry: row width vs. modal width,
  left offset, vertical overlap, overflow.

This is deterministic (geometry assertions), not pixel-diffing, so it does not
drift across machines/fonts.

```bash
make ui-test                  # build SPA + binary, seed, boot, assert; non-zero on regression
SKIP_BUILD=1 make ui-test     # reuse an existing ./wallfacer + dist (fast iteration)
node checks.mjs --base http://localhost:8099 --only picker,board   # against an already-booted server
node checks.mjs --list        # scene names
```

Scenes: `board` (app shell + sidebar present), `switcher` (popover open doesn't
drop the sidebar), `picker` (Select Workspace list is a full-width,
non-overlapping, in-bounds column), plus `settings` / `plan` / `analytics` /
`agents` / `flows` smoke (route renders, no uncaught error). Add a scene by
appending to the `SCENES` table in `checks.mjs`. Playwright runs from the same
throwaway `/tmp` sandbox as `snap.mjs` (never under `frontend/`, which would
break the vite-ssg build).

## Docs screenshots

Guide images live in `docs/guide/images/` as a `foo.png` (light) + `foo-dark.png`
(dark) pair. Reference only the light name in markdown: `![alt](images/foo.png)`.
The docs renderer (`frontend/src/lib/markdown.ts`) emits both variants toggled by
`[data-theme]`, served via the `/api/docs-asset/<category>/<path>` route. Downscale
the 2x captures to a doc-friendly width first, e.g. `sips --resampleWidth 1600`.
