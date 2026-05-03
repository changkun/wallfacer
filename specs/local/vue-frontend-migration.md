---
title: Vue Frontend Migration
status: drafted
depends_on: []
affects:
  - frontend/
  - ui/
  - internal/cli/server.go
  - internal/webserver/
  - Makefile
  - .github/workflows/
effort: xlarge
created: 2026-04-12
updated: 2026-05-03
author: changkun
dispatched_task_id: null
---

# Vue Frontend Migration

Converge both wallfacer frontends (the vanilla JS task board in `ui/` and the
Vue+TS cloud site in `frontend/`) into a single Vue 3 + TypeScript SPA. The
unified app renders the task board directly in local mode and adds landing
page, docs, and pricing routes in cloud mode, following the same runtime
mode-switching pattern used by the latere.ai sandbox project.

This spec supersedes `specs/local/typescript-migration.md` (now archived).

---

## Motivation

Wallfacer currently maintains two completely separate frontend codebases:

- **`ui/`** — ~30K LOC of hand-authored vanilla JS (53 modules + 12 lib
  files), 39 HTML partials (Go templates), 27 CSS files, 152 Vitest tests.
  No framework, no bundler, no ES modules. Globals dumped into `window` via
  `<script>` tags. Served by `wallfacer run` via `//go:embed ui`.

- **`frontend/`** — ~4.5K LOC Vue 3 + TypeScript SPA (Vite, Vue Router,
  Pinia). 28 tracked source files. Marketing/docs site for wf.latere.ai.
  Served by `wallfacer web` via `//go:embed all:dist`.

Maintaining two frontend stacks with different build pipelines, test
frameworks, CSS systems, and patterns is costly. Every UI pattern
(modals, SSE streaming, state management) is implemented twice: once with
globals + DOM manipulation, once with Vue composition. Bug fixes and design
changes touch two codebases. New features (planning chat, explorer,
terminal) must be wired into the old architecture each time.

Converging on Vue+TS gives:

- One component model, one build pipeline, one test framework.
- Compile-time type safety across the entire frontend.
- Reactive state management (Pinia) instead of global variables.
- Component-scoped CSS instead of 27 global stylesheets with implicit coupling.
- Hot-reload dev experience (Vite) instead of manual browser refresh.
- Shared components between local board and cloud site (modals, markdown
  rendering, API client, design tokens).

## Scope

**In scope**

- Rewrite the task board UI as Vue pages and components inside `frontend/`.
- Add runtime mode detection so the same SPA serves both local (`wallfacer
  run`) and cloud (`wallfacer web`) use cases.
- Migrate the Go server (`internal/cli/server.go`) to serve the Vue SPA's
  `dist/` output instead of Go-templated `ui/index.html`.
- Migrate SSE streaming, WebSocket terminal, drag-and-drop, and all
  interactive features to Vue composables.
- Rewrite tests as Vue component tests (`@vue/test-utils` + Vitest).
- Update the Wails desktop build to embed the Vue `dist/`.
- Update CI workflows for the unified build.
- Retire the old `ui/` directory, `make ui-ts`, `make ui-css`, and the
  esbuild/tsc toolchain from the TypeScript migration pilot.

**Out of scope**

- Server-side rendering (SSR). The app is an SPA; `vite-ssg` handles
  static pre-rendering for the marketing pages.
- Rewriting Go API handlers or changing the HTTP API contract.
- Mobile-responsive redesign (separate initiative if needed).
- i18n for the task board (the cloud marketing pages already have en/zh;
  the board stays English-only for now).

## Architectural Decisions

### AD-1: One Vue app, runtime mode switching

The `frontend/` directory becomes the single Vue app for both local and
cloud. No second app, no build flags.

**Mode detection**: the Go server injects runtime config into the served
`index.html` via a `<script>` tag containing `window.__WALLFACER__`:

```html
<script>
  window.__WALLFACER__ = {
    mode: "local",          // or "cloud"
    serverApiKey: "...",    // empty in cloud mode (session-based auth)
    version: "v0.9.0",
  };
</script>
```

This mirrors the existing `<meta name="wallfacer-token">` injection
pattern and extends it. The Go handler renders this into the SPA's
`index.html` at serve time (not at build time), so the same `dist/`
artifact works in both modes.

A Pinia store (`stores/boot.ts`) reads `window.__WALLFACER__` on app
init and exposes `mode`, `serverApiKey`, etc. reactively. The router
and components branch on `boot.mode`.

**Why not `/api/config`?** That requires an async fetch before the router
can decide what to render, causing a flash of wrong content. Server-injected
config is synchronous and zero-latency.

**Why not build-time flag?** Two builds from one source adds CI
complexity and makes it impossible to test cloud routes locally by just
setting an env var.

### AD-2: Router layout

**Local mode** (`wallfacer run`):
- `/` renders the task board (kanban).
- `/plan`, `/plan/:specPath` render the planning view.
- No landing page, no `/docs`, no `/pricing`.
- Deep links: `/task/:id`, `/task/:id/diff`, `/task/:id/oversight`, etc.

**Cloud mode** (`wallfacer web`):
- `/` renders the marketing landing page.
- `/docs`, `/docs/:slug`, `/pricing`, `/install` render the cloud site pages.
- `/dashboard` renders the task board.
- `/dashboard/plan`, `/dashboard/plan/:specPath` render planning.
- `/dashboard/task/:id` for task deep links.

The router uses `boot.mode` to register the appropriate route set at
app initialization, not lazy guards. This keeps the route table clean
in each mode.

**Hash link migration**: the old UI uses `#<uuid>` and `#plan/<path>`.
The new router uses history mode. A one-time redirect handler in
`App.vue` checks `window.location.hash` on mount and navigates to the
equivalent history-mode route, preserving existing bookmarks.

### AD-3: Auth bootstrap

**Local mode**: the Go server injects `serverApiKey` into
`window.__WALLFACER__`. The API client reads it from the boot store and
sends it as `Authorization: Bearer <key>` on every request. Same
mechanism as today (meta tag), different delivery.

**Cloud mode**: session-based auth via HTTP-only cookies, same as the
sandbox project. `serverApiKey` is empty. The API client sends requests
without a bearer token; the server validates the session cookie. The
existing `stores/auth.ts` (Pinia) handles `/api/auth/me` checks.

### AD-4: Migration strategy (parallel build with cutover)

Both the old `ui/` and the new Vue board exist simultaneously during
migration. An env var (`WALLFACER_VUE_UI=true|false`, default `false`)
controls which frontend the Go server serves:

- `false` (default): serves `ui/` via Go templates, as today.
- `true`: serves the Vue `dist/` with `window.__WALLFACER__` injection.

This lets development proceed page by page without breaking the
production UI. When the Vue board reaches feature parity, flip the
default to `true`, then remove `ui/` entirely.

During the parallel phase:
- `make build` continues to build both (old `ui/` pipeline + Vue `dist/`).
- `make test` runs both test suites.
- CI validates both paths.

### AD-5: Test strategy

The 152 existing Vitest tests in `ui/js/tests/` use `vm.runInContext`
to load transpiled JS into a sandboxed V8 context. This technique does
not survive the Vue migration.

Tests are rewritten page-by-page alongside component migration:
- Component tests using `@vue/test-utils` + Vitest + `happy-dom`.
- Composable tests (SSE, WebSocket, drag-and-drop) as unit tests.
- API client tests (mock fetch).
- Pinia store tests.

Coverage target: maintain or exceed the coverage percentage of the
modules being migrated. Old tests for un-migrated modules stay in
`ui/js/tests/` until their module migrates.

### AD-6: CSS approach

Migrate to Vue scoped styles + shared design tokens:

- Extract `ui/css/tokens.css` into `frontend/src/styles/tokens.css`
  (already partially exists).
- Per-component styles use `<style scoped>` in `.vue` files.
- Tailwind utility classes carry over (add Tailwind to the Vite build).
- Global base styles (reset, typography) in `frontend/src/styles/base.css`.
- Delete the 27 module CSS files from `ui/css/` as components migrate.

### AD-7: Frontend directory stays `frontend/`

The directory keeps its name. Its scope expands from "cloud marketing
site" to "unified Vue frontend". The `package.json` name changes from
`wallfacer-web` to `wallfacer-frontend`. No rename churn.

## Migration Order

### Phase 0: Infrastructure

Set up the parallel-build mechanism and mode switching before migrating
any UI pages.

1. Add `window.__WALLFACER__` injection to the Go server's SPA handler.
2. Add `stores/boot.ts` Pinia store.
3. Add mode-aware router (`router.ts` branches on `boot.mode`).
4. Add the `WALLFACER_VUE_UI` cutover flag to `internal/cli/server.go`.
5. Wire `frontend/dist/` embedding into the main `wallfacer run` binary
   (second `//go:embed` or conditional serve).
6. Add Vite proxy rules for all `/api/*` routes the task board uses.
7. Verify: `wallfacer run` with `WALLFACER_VUE_UI=true` serves the Vue
   app, which renders a placeholder "board coming soon" page at `/`.

### Phase 1: Shared primitives

Port the reusable infrastructure that every page depends on.

- **API client** (`stores/api.ts` or `composables/useApi.ts`): typed
  fetch wrapper with bearer-token and SSE support.
- **SSE composable** (`composables/useSse.ts`): reactive EventSource
  with reconnect, replacing `api.js` SSE logic.
- **WebSocket composable** (`composables/useWebSocket.ts`): for terminal.
- **State store** (`stores/tasks.ts`): Pinia store replacing `state.js`
  globals.
- **Design tokens**: merge `ui/css/tokens.css` into
  `frontend/src/styles/tokens.css`.
- **Modal system**: Vue modal component replacing `modal-controller.ts`.
- **Markdown rendering**: composable wrapping marked.js (already exists
  in `frontend/src/lib/markdown.ts`, extend for task content).

### Phase 2: Board shell and kanban

The core task board experience.

- Board layout component (three-column kanban).
- Task card component (drag-and-drop via Sortable.js or Vue Draggable).
- Task detail panel (sidebar or full view).
- Board composer (task creation form).
- Status bar, sidebar navigation.

### Phase 3: Task detail views

- Task event timeline.
- Diff viewer.
- Oversight panel.
- Log streaming (SSE).
- Turn usage / cost breakdown.
- Feedback form (waiting state).

### Phase 4: Secondary pages

- Settings (env config, sandbox config, appearance).
- Planning chat (spec mode + task mode).
- File explorer.
- Terminal (xterm.js integration).
- Agents & Flows management.
- Routines management.

### Phase 5: Polish and cutover

- Port remaining edge cases (command palette, keyboard shortcuts,
  BroadcastChannel tab leader, pixel office).
- Achieve test parity.
- Flip `WALLFACER_VUE_UI` default to `true`.
- Remove `ui/` directory, old `//go:embed ui`, Go template rendering,
  `make ui-ts`, `make ui-css`, `make typecheck-js`, `scripts/build-ts.mjs`.
- Update Wails desktop build to use Vue `dist/`.
- Update all three CI workflows.

### Phase 6: Cleanup

- Archive `specs/local/typescript-migration.md` and
  `specs/local/typed-dom-hooks.md`.
- Remove esbuild, old tsconfig, `ui/types/globals.d.ts`.
- Update CLAUDE.md, docs, and README to reflect the new frontend stack.

Each phase is a set of independently shippable PRs that keep both
`WALLFACER_VUE_UI=false` (old) and `WALLFACER_VUE_UI=true` (new) green.

## Build Pipeline Changes

### Makefile

```makefile
# Build Vue frontend for embedding.
frontend-build:
	cd frontend && npm ci && npm run build

# Full gate now includes Vue build.
build: fmt lint frontend-build build-binary pull-images

# Dev mode: Vite on :5173, Go server on :8080.
dev-frontend:
	cd frontend && npm run dev
```

`make ui-ts` and `make ui-css` remain during the parallel phase for the
old UI, removed in Phase 5.

### Go embed

During the parallel phase, both embed directives coexist:

```go
//go:embed ui
var uiFiles embed.FS           // old UI (default)

//go:embed frontend/dist
var vueDist embed.FS            // new Vue UI (WALLFACER_VUE_UI=true)
```

After cutover, only `vueDist` remains (possibly moved to
`internal/webserver/spa/` like the cloud site currently does).

### CI workflows

All three workflows (`release-binary.yml`, `release-desktop.yml`,
`test.yml`) add a step before `go build` / `wails build`:

```yaml
- uses: actions/setup-node@v4
  with: { node-version: '22', cache: 'npm', cache-dependency-path: frontend/package-lock.json }
- run: cd frontend && npm ci && npm run build
```

### Vite config

Extend `frontend/vite.config.ts` proxy to cover all API routes the task
board uses:

```typescript
proxy: {
  '/api':      { target: 'http://localhost:8080', changeOrigin: true },
  '/login':    'http://localhost:8080',
  '/callback': 'http://localhost:8080',
  '/logout':   'http://localhost:8080',
},
```

These already exist. No change needed unless new routes are added.

### Wails desktop

`make build-desktop` currently embeds `ui/`. After cutover it embeds
`frontend/dist/`. The Wails config (`wails.json` or build flags) updates
to point at the Vue dist.

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| 80K LOC is too large to migrate in one pass | Parallel build with cutover flag. Old UI stays functional throughout. Each phase ships independently. |
| Drag-and-drop (Sortable.js) hard to port | Use `vue-draggable-plus` or wrap Sortable.js in a Vue directive. Prototype in Phase 2 before committing. |
| xterm.js integration complex in Vue | xterm.js works fine in Vue via refs. The sandbox project already does this. |
| SSE leader election (BroadcastChannel) | Port as a Vue composable. The pattern is framework-agnostic; just wrap in `onMounted`/`onUnmounted`. |
| Test coverage drops during migration | Tests rewritten alongside each component. Coverage tracked per-phase. Old tests remain for un-migrated modules. |
| Dev workflow disrupted | `npm run dev` (Vite on :5173) + `wallfacer run` (Go on :8080) is the proven dev loop from `frontend/`. No new tooling to learn. |
| Binary size increase from embedding both UIs | Temporary during parallel phase. After cutover, old `ui/` removed. |
| Go template features lost (ServerAPIKey injection) | Replaced by `window.__WALLFACER__` injection at serve time. Same capability, different mechanism. |
| Hash-link bookmarks break | `App.vue` redirect handler maps `#<uuid>` to `/task/<uuid>` on first load. |

## Acceptance Criteria

- `wallfacer run` serves the Vue SPA by default (after cutover).
- `wallfacer web` serves the same SPA with cloud routes enabled.
- Local mode: `/` renders the kanban board directly.
- Cloud mode: `/` renders the landing page; `/dashboard` renders the board.
- All task board features work: kanban drag-and-drop, task creation,
  detail views, log streaming, diff viewer, planning chat, file explorer,
  terminal, settings, agents/flows, routines, command palette.
- `make build` produces a working binary embedding the Vue dist.
- `make test` passes (Vue component tests + Go backend tests).
- Desktop app (Wails) embeds and serves the Vue dist.
- CI workflows build the Vue frontend before Go/Wails compilation.
- `ui/` directory removed. No Go template rendering for the board.
- Old hash-link bookmarks redirect to history-mode equivalents.
- `vue-tsc --noEmit` passes with zero errors.
- CLAUDE.md and docs updated to reflect the new frontend stack.

## Superseded Specs

- **`specs/local/typescript-migration.md`** — archived. The JS-to-TS
  in-place migration with esbuild is superseded by the full Vue+TS
  rewrite. Pilot work (clipboard.ts, build directory, CI steps) will be
  removed in Phase 5.
- **`specs/local/typed-dom-hooks.md`** — archived. The DOM hook safety
  problem (renaming `id`/`data-js-*` breaks selectors) disappears under
  Vue's template compiler, which catches invalid refs at build time.
