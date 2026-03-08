# Frontend Modernization Spec

## Status: Draft

## Context

The Wallfacer frontend is ~3,200 lines of vanilla JavaScript across 15 modules, served as static files from the Go backend. It uses Tailwind CSS (CDN), Sortable.js, and marked.js as its only dependencies with no build step.

The current architecture works for its scope today, but several patterns are showing scaling friction:

- **`modal.js` is 1,039 lines** — a single file managing task details, live log streaming (3 modes), diff rendering, dual-agent monitoring, usage stats, feedback, retry, and resume. Adding any new modal section means hunting for the right insertion point in a dense imperative file.
- **Global mutable state** in `state.js` is shared across all modules without any encapsulation. The list of globals has grown organically; reasoning about what can mutate `tasks`, `logsAbort`, or `oversightData` requires reading every file.
- **DOM is the state** in many places — conditional show/hide logic is scattered across `modal.js`, `render.js`, and `tasks.js`, each using raw `classList.toggle('hidden', ...)`. When task status adds a new transition, all three files need updating.
- **No type safety** — the `Task`, `TaskEvent`, `TaskUsage` shapes exist only in the Go backend; the frontend infers them from usage.
- **Ad-hoc template strings with `escapeHtml()`** for dynamic content inside modal sections. This pattern is error-prone, hard to review for XSS completeness, and makes refactoring difficult.
- **No component boundary** between the card, the modal header, and the modal body — state that only concerns the modal (log stream mode, oversight cache, test log state) bleeds into module-level globals.
- **No testability** — the current code has no unit or integration tests because every function reads from DOM IDs and global state.

## Goals

1. Preserve the current no-heavy-runtime philosophy — the bundle should be small and fast.
2. Enable component-level encapsulation so `modal.js` can be broken into focused units without cross-module coupling.
3. Add TypeScript so the Go API's data shapes are reflected at compile time.
4. Add a lightweight build step (no complex webpack config) so we can bundle, type-check, and tree-shake.
5. Make the application testable (at least at the component level).
6. Maintain zero backend changes — the Go server continues to serve `ui/` as static files.

## Non-Goals

- SSR / server-side rendering (Go backend stays authoritative, no Node.js in the runtime path).
- A full SPA router — the app is a single board page; navigation is modal-based, not URL-based.
- Replacing Tailwind CSS — keep it; it is working well.
- Replacing Sortable.js or marked.js.
- Rewriting business logic — the API calls, SSE handling, and task lifecycle semantics stay the same.

## Options Considered

### Option A: Keep vanilla JS, extract sub-modules

Extract `modal.js` into ~6 focused files (`modal-backlog.js`, `modal-logs.js`, `modal-diff.js`, etc.) and introduce a thin reactive state object to replace the scattered globals.

**Pros:** No build step, no new dependency, lowest risk.
**Cons:** Does not solve the template-string XSS pattern or the lack of type safety. Still hard to test. The root problem (imperative DOM updates) persists. This is an incremental improvement, not a path to scaling further.

### Option B: Alpine.js

A progressive-enhancement library (~15 KB) that adds reactivity via HTML attributes (`x-data`, `x-bind`, `x-on`). No build step required.

**Pros:** Nearly zero migration cost; add it to `index.html` and gradually replace imperative DOM calls.
**Cons:** State lives in the HTML template, which makes complex state (SSE streams, multi-mode log viewer) awkward. No TypeScript support without a build step. Testing Alpine components is non-trivial. Not suited for deeply dynamic content like the log stream renderer or diff viewer.

### Option C: Vue 3 + Vite + TypeScript

Vue 3's Composition API with Single File Components (`.vue`), bundled with Vite. Pinia for cross-component state.

**Pros:**
- SFCs collocate template, logic, and styles — a natural fit for the card/modal boundary problem.
- The Composition API (`ref`, `computed`, `watch`) maps cleanly onto the current imperative update patterns.
- Vite provides fast HMR, TypeScript, and produces a small, tree-shaken bundle.
- Pinia gives the current global state an explicit, testable shape (typed stores).
- Large ecosystem; Vue DevTools for debugging reactive state.
- Progressive migration is possible (mount Vue on a subtree while leaving other parts in vanilla JS).

**Cons:**
- Introduces a build step and `node_modules`.
- Team needs familiarity with SFC syntax and `<script setup>`.
- Vue's reactivity system adds a small conceptual overhead for contributors new to it.

### Option D: Svelte + Vite + TypeScript

Svelte compiles components to vanilla JS at build time — no virtual DOM, no runtime framework overhead.

**Pros:**
- Smallest runtime of any option (~0 KB framework runtime).
- Svelte's reactive declarations (`$:`) are arguably more readable than Vue's `ref`/`computed`.
- SFCs with `<script lang="ts">` support TypeScript natively.
- Svelte stores (`writable`, `derived`) map cleanly onto the current global state pattern.
- Excellent Vite integration.

**Cons:**
- Smaller ecosystem than Vue/React; fewer off-the-shelf component libraries (not needed here, but worth noting).
- Svelte's template compilation can produce surprising output in edge cases (ANSI rendering, raw `innerHTML`).
- Fewer contributors know Svelte vs Vue/React.
- Less mature TypeScript story than Vue 3 (improving in Svelte 5).

### Option E: React + Vite + TypeScript

The industry default. Largest ecosystem, most contributors likely familiar.

**Pros:** Ubiquitous knowledge, massive ecosystem, excellent TypeScript support, Zustand/Jotai for state.
**Cons:** React's virtual DOM and JSX add more overhead than Svelte or Vue for an app of this size. React's component model (hooks rules, strict mode caveats) is heavier than needed. Bundle size is larger (~45 KB gzipped for React + ReactDOM vs ~6 KB for Svelte runtime or ~23 KB for Vue 3 core).

## Recommendation: Vue 3 + Vite + TypeScript

Vue 3 is the strongest fit for this project because:

1. **The Composition API matches the existing mental model.** The current code already separates concerns into focused files (`render.js`, `modal.js`, `api.js`). Moving each to a `<script setup>` SFC requires minimal conceptual shift — functions become `ref`/`computed`/`watch`, and templates replace `innerHTML` strings.

2. **Pinia stores replace `state.js` cleanly.** Each logical domain (task list, modal state, git status, log streaming) maps to a typed Pinia store. The stores are independently testable with Vitest.

3. **Progressive migration is practical.** Vue 3 can be mounted on a `<div id="app">` while the rest of the page remains as-is. Cards, modals, and the board can migrate file by file without a flag day.

4. **Vite is the right build tool.** Near-instant HMR, TypeScript support, small production bundles (`rollup` under the hood), and zero config for the common case. The Go server can serve the `dist/` output as before.

5. **No behavioral changes are needed.** SSE handling, API calls, drag-and-drop (Sortable.js adapters exist for Vue), and markdown rendering all survive the migration.

Svelte was a close second and would produce a slightly smaller bundle. The tiebreaker is contributor familiarity and the maturity of Vue 3's TypeScript tooling.

## Architecture After Migration

### Directory Layout

```
ui/
  src/
    main.ts              # mount App, create pinia
    App.vue              # root: header + board + modals
    types/
      task.ts            # Task, TaskEvent, TaskUsage, TaskStatus interfaces
      git.ts             # WorkspaceStatus, Branch interfaces
      config.ts          # ServerConfig, EnvConfig interfaces
    stores/
      tasks.ts           # task list, SSE stream, fetchTasks, render state
      modal.ts           # currentTaskId, modal open/close
      git.ts             # workspace git status, SSE stream
      logs.ts            # log streaming state, mode, buffer
      config.ts          # autopilot, models, max parallel
    api/
      client.ts          # fetch wrapper (same logic as current api.js)
      tasks.ts           # task CRUD, status transitions
      git.ts             # push, sync, branch ops
      env.ts             # env config read/write
      instructions.ts    # CLAUDE.md read/write
    components/
      board/
        Board.vue          # 4-column task board grid
        Column.vue         # single column with header
        TaskCard.vue       # draggable card (current createCard/updateCard)
        NewTaskForm.vue    # collapsible create form
      modal/
        TaskModal.vue      # outer shell (overlay, split layout, open/close)
        BacklogView.vue    # prompt editor, settings, refinement chat
        ProgressView.vue   # live logs panel
        WaitingView.vue    # feedback form + test section
        DoneView.vue       # diff, usage stats, retry/archive
        FailedView.vue     # resume, sync, retry
        LogViewer.vue      # oversight / pretty / raw tabs + stream
        DiffViewer.vue     # file-by-file collapsible diff
        EventTimeline.vue  # event list
        UsageStats.vue     # per-agent cost breakdown
      git/
        WorkspaceBar.vue   # workspace badges + branch dropdown
        BranchDropdown.vue # search/create/switch branches
      settings/
        SettingsModal.vue  # outer shell
        ApiConfigForm.vue  # tokens, base URL, models
        InstructionsEditor.vue
        ContainerMonitor.vue
      common/
        AlertModal.vue
        MarkdownView.vue   # rendered/raw toggle
        AnsiText.vue       # ANSI escape → styled spans
    composables/
      useSSE.ts          # generic reconnecting EventSource composable
      useDiff.ts         # diff fetch + cache logic
      useLogStream.ts    # log streaming with abort, mode toggle
      useDebounce.ts     # debounced reactive value
  index.html             # single entry point, mounts <div id="app">
  vite.config.ts
  tsconfig.json
```

### Type Definitions

Define the Go model types explicitly in `types/task.ts`:

```typescript
export type TaskStatus =
  | 'backlog' | 'in_progress' | 'waiting' | 'done'
  | 'failed' | 'cancelled' | 'archived';

export interface Task {
  id: string;
  prompt: string;
  title: string;
  status: TaskStatus;
  position: number;
  created_at: string;
  updated_at: string;
  timeout: number;
  model: string;
  session_id?: string;
  worktrees: Worktree[];
  usage?: TaskUsage;
  last_test_result?: TestResult;
  // ... full shape from store/task.go
}
```

These types become the contract between the API layer and the UI, caught at compile time when the backend model changes.

### State Management (Pinia)

Replace `state.js` globals with typed stores:

```typescript
// stores/tasks.ts
export const useTasksStore = defineStore('tasks', () => {
  const tasks = ref<Task[]>([]);
  const showArchived = ref(false);
  const sseSource = ref<EventSource | null>(null);

  async function fetchTasks() { /* ... */ }
  function startStream() { /* useSSE composable */ }

  const visibleTasks = computed(() =>
    tasks.value.filter(t => showArchived.value || t.status !== 'archived')
  );

  return { tasks, showArchived, visibleTasks, fetchTasks, startStream };
});
```

The modal store owns `currentTaskId` and exposes `openModal` / `closeModal`, making it trivial to test whether the modal opened on a given action.

### SSE Composable

The current reconnect logic is duplicated across `api.js` (tasks stream) and `git.js` (git stream). Extract to a reusable composable:

```typescript
// composables/useSSE.ts
export function useSSE<T>(url: Ref<string>, onMessage: (data: T) => void) {
  const connected = ref(false);
  let retryDelay = 1000;
  // exponential backoff reconnect, cleanup on unmount
  onUnmounted(() => source?.close());
  return { connected };
}
```

### Render Strategy

Replace the manual reconciliation loop in `render.js` with Vue's virtual DOM. The `v-for` on `<TaskCard>` handles insertion/removal; `:key="task.id"` preserves identity across updates. The `updateCard()` merge logic disappears — Vue updates only what changed via reactivity.

### LogViewer Component

The most complex subsystem (`modal.js:400–900`) becomes `LogViewer.vue` + `useLogStream.ts`. The composable owns the `AbortController`, raw buffer accumulation, NDJSON parsing, and pretty/raw/oversight mode. The component renders the result. Both are independently testable.

### AnsiText Component

`ansiToHtml()` (currently inline in `modal.js`) becomes a `<AnsiText :text="rawLine" />` component that emits `<span>` elements with Tailwind-compatible color classes. This can be unit-tested against the 16-color ANSI table.

### DiffViewer Component

`parseDiffByFile()` and `renderDiffFiles()` become a component receiving a `diff: string` prop. The file collapse/expand state is local reactive state inside the component, not a DOM class toggle.

## Migration Strategy

The migration should be incremental and never break the running application. This avoids a big-bang rewrite risk.

### Phase 0 — Build tooling only (no behavioral change)

1. Add `package.json` with `vite`, `vue`, `@vitejs/plugin-vue`, `typescript`, `pinia`.
2. Configure Vite to output to `ui/dist/` and serve `index.html` as entry.
3. Copy current JS files verbatim into `src/legacy/`, rename to `.js`.
4. Mount a stub `App.vue` that renders nothing but confirms Vite + Vue boot correctly.
5. Update Go server to serve `ui/dist/` in production, `vite dev` in development (proxy `/api/*` to `:8080`).
6. **Checkpoint**: app behaves identically; bundle is served from Vite.

### Phase 1 — Types and API layer

1. Define all `types/` interfaces matching current Go models.
2. Rewrite `api.js` → `api/client.ts` as a typed fetch wrapper.
3. Rewrite `api/tasks.ts`, `api/git.ts`, `api/env.ts`, `api/instructions.ts`.
4. Legacy JS files import from the new typed API layer.
5. **Checkpoint**: all API calls go through typed functions; no behavioral change.

### Phase 2 — Pinia stores replace global state

1. Create `stores/tasks.ts` — move `tasks`, `showArchived`, SSE logic from `state.js` and `api.js`.
2. Create `stores/git.ts` — move git SSE state from `git.js`.
3. Create `stores/config.ts` — move autopilot, models, max parallel.
4. Legacy JS files call store actions instead of mutating globals.
5. Delete `state.js`.
6. **Checkpoint**: state is centralized; reactive updates work through Pinia.

### Phase 3 — Board and card components

1. Convert `render.js` → `Board.vue` + `Column.vue` + `TaskCard.vue`.
2. Wire Sortable.js via `useSortable` composable (vue-draggable-plus or thin wrapper).
3. `NewTaskForm.vue` replaces the inline form.
4. **Checkpoint**: board renders from store; drag-and-drop works.

### Phase 4 — Modal shell and status views

1. Create `TaskModal.vue` with open/close controlled by `stores/modal.ts`.
2. Split into `BacklogView.vue`, `ProgressView.vue`, `WaitingView.vue`, `DoneView.vue`, `FailedView.vue`.
3. Each view receives the `Task` object as a prop; internal state is local.
4. **Checkpoint**: all task status views work; `modal.js` is deleted.

### Phase 5 — LogViewer and DiffViewer

1. Extract `useLogStream.ts` composable.
2. Create `LogViewer.vue` (oversight/pretty/raw tabs).
3. Create `AnsiText.vue`.
4. Create `DiffViewer.vue`.
5. **Checkpoint**: log streaming and diff display work; related code in modal.js/render.js is deleted.

### Phase 6 — Settings and auxiliary modals

1. Convert `envconfig.js` → `ApiConfigForm.vue`
2. Convert `instructions.js` → `InstructionsEditor.vue`
3. Convert `containers.js` → `ContainerMonitor.vue`
4. Convert `theme.js` → `useTheme.ts` composable
5. Delete legacy files.
6. **Checkpoint**: settings modals work; `index.html` has no inline JS.

### Phase 7 — Tests

1. Add Vitest + `@vue/test-utils`.
2. Unit tests for `AnsiText.vue`, `DiffViewer.vue`, `useSSE.ts`, `useLogStream.ts`.
3. Store tests for `tasks.ts`, `modal.ts` stores.
4. Smoke tests for `TaskCard.vue` rendering across all task statuses.
5. **Checkpoint**: CI runs `vitest` on every PR.

## Build Integration

```
# Development
npm run dev    # vite dev server on :5173, proxies /api/* to :8080

# Production (embedded in Go server)
npm run build  # outputs to ui/dist/
go build -o wallfacer .  # serves ui/dist/ via embed or filepath
```

The Makefile gains a `make ui` target that runs `npm run build` before `go build`.

For the Go server, `ui/dist/` is served as static files — no change to the server routing logic.

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| ANSI rendering differences after component rewrite | Unit-test `AnsiText` against the existing color mapping table before switching over |
| SSE reconnect behavior changes | Port the exact exponential backoff logic into `useSSE.ts` and verify with integration tests |
| Sortable.js integration breaking DnD | Use thin wrapper composable; test backlog reorder and in-progress drop in Phase 3 before proceeding |
| Build step added to CI | Makefile `make ui` target; CI installs Node only for the build stage |
| Contributors unfamiliar with Vue | Architecture stays close to current module structure; composables mirror the current function-per-concern pattern |

## Out of Scope (Future)

- Vue Router for URL-based task navigation (current modal-only UX works well)
- Storybook for component documentation
- E2E tests with Playwright (valuable but separate from this spec)
- Replacing Tailwind with CSS Modules or styled-components
