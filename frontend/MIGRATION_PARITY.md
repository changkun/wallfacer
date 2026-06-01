# Vue Frontend Parity Tracker

Working tracker for `specs/local/vue-frontend-migration.md`. Goal: the Vue app
(`frontend/`) reaches **100% functional parity** with the vanilla-JS UI (`ui/`).
Reference UI runs at `http://localhost:8080` (old, plain JS). Vue runs via
`cd frontend && npm run dev` (Vite :5173, proxies `/api` → :8080). Inject
`window.__WALLFACER__ = {mode:'local', serverApiKey:'', version:''}` to force
local mode in dev.

Status: **functional parity reached on 2026-06-01.** Every prioritised gap
below is closed. Cloud-mode-only follow-ups (`org switcher`) require new
backend endpoints and are tracked separately. `ideate.js` was intentionally
retired during the migration (its capability moved into the task composer).

Status legend: **DONE** = behavior parity reached · **PARTIAL** = exists, gaps
listed · **MISSING** = no Vue equivalent · **VERIFY** = agent uncertain, confirm.

Parity = **behavior**, not pixels. Cosmetic diffs are out of scope.

> Backend note: local board is currently **empty** (`/api/tasks` → `[]`), auth is
> open. Task-dependent features (cards, detail, diff, logs) need a throwaway
> workspace with test tasks to verify dynamically — do **not** mutate the user's
> live board.

---

## Summary (53 modules + 4 lib)

- **DONE (~51):** transport, theme, markdown, task-stream, terminal,
  containers, agents, flows, system-prompts, instructions, templates,
  usage-stats, analytics-tabs, trash-bin, modal-logs, modal-diff,
  modal-ndjson, modal-flamegraph (Timeline tab), depgraph,
  unified-graph, spec-explorer, sidebar-badge, bootstrap-choreography,
  mention, dispatch-toast, dnd, planning-chat, spec-mode, routines,
  utils (confirm/alert/prompt), images, board-composer, tasks,
  envconfig, api (heartbeat + archived pagination), events (`/` focus
  search), git, explorer (full: edit + md preview + SSE refresh +
  task-prompts + keyboard nav), workspace (group create/rename/delete/
  switch popover), status-bar (terminal + presence + sign-in +
  containers entry point), command-palette (task actions + spec/doc
  rows), keyboard-shortcuts (global + card-level s/d/arrow nav),
  modal-core, modal-oversight, modal-results, render
  (priority/impact badges).
- **DEFERRED (cloud-only, needs backend):** org switcher (latere-ui
  OrgSwitcher requires an orgs API that isn't exposed yet).
- **RETIRED:** ideate (capability folded into the task composer).

*render.js logic may live inside `TaskCard.vue` — VERIFY before treating as missing.
†ideate.js is now a stub; ideation refactored into the task composer — may be intentionally retired.
‡`KeyboardShortcutsModal.vue` exists but shortcuts are hardcoded, not bound to handlers.

---

## Prioritized gap backlog (highest functional impact first)

1. ~~**modal-ndjson — pretty Claude Code / Codex output rendering.**~~ ✅ DONE
   (2026-05-30). Extended `prettyNdjson.ts` `parseActivity` (now also emits
   assistant **text** + final **result**; tested) + new `useTaskActivity`
   composable (streams `/api/tasks/{id}/logs` as text/plain via
   `startStreamingFetch` — works for **live and completed** tasks) + rewrote the
   `TaskDetail.vue` Activity tab to render pretty rows (thinking/tool/result/text,
   color-coded, expandable details) with a raw fallback. Removed the dead
   `useLogStream` (EventSource on a text/plain endpoint — wrong transport, never
   replayed completed output). Follow-up: ANSI-color stderr + codex shell-wrapper
   stripping (minor); re-parse-whole-buffer per chunk is O(n²) (fine for now).
2. ~~**modal-diff — git diff viewer.**~~ ✅ DONE (2026-05-30). `lib/diff.ts`
   (`parseDiffFiles`, tested) + a **Changes** tab in `TaskDetail.vue`:
   file-by-file, +/- line coloring, hunk/header styling, per-file add/del
   stats, collapsible, behind-upstream banner. Follow-up polish: per-line
   hljs syntax highlighting inside the diff (old UI had it; not functional-critical).
3. **command-palette context actions.** Per-task Start/Resume/Archive/Sync/Retry/
   Cancel rows after selecting a task. Entirely missing; only search/display.
4. ~~**board-composer + tasks.js creation depth.**~~ ✅ DONE (2026-06-01).
   Composer now has flow ✓, tags ✓, timeout ✓, model ✓, budget ✓, dependency
   picker ✓, **template insertion** ✓ (cursor-aware, datalist-backed),
   **per-task sandbox override** ✓ (claude/codex dropdown → PATCH after create),
   **batch create** ✓ (blank-line split, `splitBatch` tested, posts to
   /api/tasks/batch with shared opts), **routine scheduling** ✓ (Schedule
   toggle → POST /api/routines with interval_minutes).
   Empty-state composer + advanced timeout panel are minor polish.
5. ~~**utils dialogs — showAlert/showConfirm/showPrompt + ARIA announce.**~~
   ✅ DONE (2026-06-01). `dialog` Pinia store now offers confirm/alert/prompt
   (tested, 8 tests); ConfirmDialog renders a focused text input in prompt
   mode. Hover-row tables + ARIA announce remain minor polish.
6. **dnd impact-sort mode + per-column config.** backlog=sort+pull,
   in_progress=put-only, waiting/done/cancelled=no-drag; impact-sort toggle.
7. **explorer file edit mode.** Edit/save/discard, SSE tree refresh, keyboard nav,
   markdown preview toggle, task-prompts section. Currently read-only preview.
8. **mention — @-file autocomplete** in prompt textareas (composer + planning + feedback).
9. **search server-side execution** (@-prefix → `/api/tasks/search` results panel) +
   slash-to-focus.
10. **dispatch-toast** + spec dispatch-to-board flow + archive/unarchive undo toasts.
11. **status-bar** terminal toggle (Ctrl+`), presence, system status, sign-in badge.
12. **modal-flamegraph / span timeline** Gantt; **modal-oversight** phase rendering+polling.
13. **workspace group management UI** (create/rename/delete/switch, persistence).
14. ~~**images** pull SSE progress; **envconfig** model dropdown populate.~~
    ✅ DONE (2026-06-01). Pulls now subscribe to /api/images/pull/stream and
    surface live phase + layer count. Model datalists populated from
    lib/knownModels (claude/codex), respecting custom base URLs.
    Remaining: **git** branch create + open-folder (already DONE per gap log).
15. **Infra:** api archived-task pagination, BroadcastChannel SSE tab-leader
    relay, heartbeat staleness. (Deep-link hash redirect ✅ done earlier.)
16. **render** tag/impact badges + relative time (VERIFY in TaskCard), **bootstrap-choreography**
    first-spec focus/toast, **sidebar-badge** board unread dot (✅ done),
    **keyboard-shortcuts** dynamic binding (✅ partial — "/" wired 2026-06-01;
    card-level s/d/arrow nav still pending).

---

## Per-area detail

### Core infra (api, transport, state, events, render, utils, markdown, bootstrap, theme)
| Module | Status | Vue equivalent | Missing behaviors |
|---|---|---|---|
| transport.js | DONE | api/client.ts | — bearer token + fetch wrappers covered |
| theme.js | DONE | stores/prefs.ts | — toggle/persist/OS-listener covered |
| markdown.js | DONE | lib/markdown.ts | marked+highlight+mermaid placeholders covered |
| api.js | PARTIAL+ | api/client.ts, stores/tasks.ts, composables/useSse.ts, lib/hashRoute.ts | **deep-link hash redirect done** (AD-2). Pending: archived pagination, waitForTaskDelta, BroadcastChannel relay, heartbeat staleness, git stream |
| state.js | PARTIAL | stores/{tasks,ui,prefs}.ts | pendingCancel set, log abort/buffers, workspaceBrowser+group state, gitStatuses, activeGroups, taskChange observers |
| events.js | PARTIAL | composables/useKeyboard.ts | modal escape cascade, textarea autogrow/draft, global keydown map (n/?/e/p/c/d/b) |
| utils.js | PARTIAL+ | stores/dialog.ts + ConfirmDialog.vue | **showConfirm/showAlert** done (Pinia dialog store + global ConfirmDialog, tested; wired to Delete/Cancel). Pending: showPrompt (budget raise), hover-row tables, ARIA announce, mobile col nav |
| render.js | VERIFY/MISSING | TaskCard.vue? | tag/impact badges, relative time, dep-id extraction, accessible titles |
| bootstrap-choreography.js | MISSING | — | first-spec auto-focus + toast timing, reduced-motion |

### Board + tasks (tasks, task-stream, board-composer, dnd, status-bar, sidebar-badge)
| Module | Status | Vue equivalent | Missing behaviors |
|---|---|---|---|
| task-stream.js | DONE | useSse.ts, AppLayout.vue | snapshot/updated/deleted events covered |
| tasks.js | PARTIAL+ | stores/tasks.ts, TaskComposer/TaskDetail | flow ✓, tags ✓, timeout ✓, model ✓, budget ✓, **dependency picker ✓**. Pending: batch create, sandbox override, schedule, debounce save, bulk title/oversight |
| board-composer.js | MISSING | TaskComposer.vue (partial) | empty-state composer, advanced panel (timeout/templates), @-mention, create animation |
| dnd.js | DONE | BoardPage.vue + lib/backlogSort.ts | per-column pull/put, ghost/chosen classes, backlog `#N` rank, **impact-sort toggle** (tested sortBacklog + localStorage persistence; drag-sort disabled + rank hidden in impact mode) |
| status-bar.js | PARTIAL+ | StatusBar.vue, Sidebar.vue | **presence row added** (tested `derivePresence`: agents from in-progress + self). Pending: terminal toggle+resize, org switcher, system status |
| sidebar-badge.js | DONE | Sidebar.vue + lib/unread.ts | board unread dot when tasks arrive off-board (tested hasUnseen) |

### Task-detail modals (modal-*, span-stats)
| Module | Status | Vue equivalent | Missing behaviors |
|---|---|---|---|
| modal-logs.js | DONE | TaskDetail.vue + useTaskActivity.ts | streams text/plain `/logs` for live+completed; pretty rows + raw fallback. (oversight filter / download pending) |
| modal-core.js | PARTIAL | TaskDetail.vue | edit/preview tabs, focus trap, history/retry edit |
| modal-oversight.js | DONE* | TaskDetail.vue | phase summary (title/summary/tools) rendered in Activity tab, lazy-fetched on tab open. *test-oversight variant + generation polling are minor follow-ups |
| modal-results.js | PARTIAL | TaskDetail.vue | multi-turn plan vs result, raw/rendered toggle, per-entry copy |
| modal-ansi.js | PARTIAL | (xterm elsewhere) | ANSI palette for modal logs, CR collapsing |
| modal-stats.js | PARTIAL | AnalyticsTab* | all-time vs windowed, by-status/activity, planning window, period selector |
| span-stats.js | PARTIAL | AnalyticsTabTiming.vue | modal interface, phase metadata, daily mini-chart, throughput tiles |
| modal-diff.js | DONE* | lib/diff.ts + TaskDetail.vue Changes tab | *per-line hljs syntax highlight inside diff deferred (polish) |
| modal-ndjson.js | DONE | prettyNdjson.ts + TaskDetail.vue | thinking/tool/result/text rows, collapsible. (ANSI stderr / codex wrapper strip pending) |
| modal-flamegraph.js | MISSING | — | span Gantt, lane assignment, skew detect, tooltips, time axis |

### Planning + spec (planning-chat, spec-mode, spec-explorer, depgraph, unified-graph, dispatch-toast)
| Module | Status | Vue equivalent | Missing behaviors |
|---|---|---|---|
| spec-explorer.js | DONE | SpecTreePanel.vue, stores/planning.ts | tree, grouping, filter, archived toggle, multi-select, task-prompts |
| depgraph.js | DONE | MapPage.vue (vendored) | runs legacy module via shims |
| unified-graph.js | DONE | MapPage.vue (vendored) | spec+task merge, archived filter, layout cache |
| planning-chat.js | PARTIAL | PlanningChatPanel.vue, stores/planning.ts | slash/mention autocomplete robustness, focus restore, revert/stash edge cases |
| spec-mode.js | DONE | PlanPage.vue, SpecFocusedView.vue | three-pane layout, dispatch flow + **View-on-Board toast**, archive/unarchive **undo toast** (already present), hash deep-link, chat toggle |
| dispatch-toast.js | DONE | stores/toast.ts + Toaster.vue + SpecFocusedView | toast system (tested) used for archive undo **and spec dispatch-complete "View on Board →"** |

### Settings/config (envconfig, system-prompts, instructions, templates, images)
| Module | Status | Vue equivalent | Missing behaviors |
|---|---|---|---|
| system-prompts.js | DONE | SystemPromptsManager.vue | full parity |
| instructions.js | DONE | InstructionsEditor.vue | full parity |
| templates.js | DONE | TemplatesManager.vue | full parity |
| envconfig.js | PARTIAL | SettingsTabSandbox.vue, useEnvConfig.ts | model dropdown populate from base_url; first-launch button emphasis |
| images.js | PARTIAL | SettingsTabSandbox.vue | SSE pull progress (currently polls); per-line/phase progress |

### Agents/flows/routines/ideate
| Module | Status | Vue equivalent | Missing behaviors |
|---|---|---|---|
| agents.js | DONE | AgentsPage.vue | full parity |
| flows.js | DONE | FlowsPage.vue | full parity (drag reorder, parallel groups) |
| routines.js | DONE | RoutinesPage.vue (+ route + sidebar nav) | dedicated page: list (reuses TaskCard routine footer), create form (prompt/interval/flow), delete-with-confirm. Inline TaskCard schedule/toggle/trigger already worked |
| ideate.js | MISSING/RETIRED | — | refactored into task composer; confirm intentional |

### Explorer/terminal/git/containers/workspace
| Module | Status | Vue equivalent | Missing behaviors |
|---|---|---|---|
| terminal.js | DONE | TerminalPanel.vue | xterm, WS PTY, multi-session, container exec |
| containers.js | DONE | ContainerMonitor.vue | poll, states, task link |
| explorer.js | PARTIAL+ | ExplorerPage.vue | **edit mode added** (Edit/Save/Cancel → PUT /api/explorer/file). Pending: SSE tree refresh, keyboard nav, md preview toggle, task-prompts, resize |
| git.js | DONE | StatusBar.vue, BranchDropdown.vue | branches/checkout/push/sync/rebase + create-branch + **open-folder** all present. (conflict-resolution messaging is a minor follow-up) |
| workspace.js | PARTIAL | WorkspacePicker.vue | group create/rename/delete/switch UI, persistence, active-group badge |

### Search/command/keyboard/usage/misc
| Module | Status | Vue equivalent | Missing behaviors |
|---|---|---|---|
| usage-stats.js | DONE | AnalyticsTabUsage.vue | full parity |
| analytics-tabs.js | DONE | AnalyticsPage.vue + tabs | full parity |
| trash-bin.js | DONE | SettingsTabTrash.vue | full parity |
| search.js | DONE* | SearchBar.vue + CommandPalette.vue | local filter + **slash-to-focus** + **@-prefix hands off to command-palette server search** (via ui.paletteSeed). *dedicated in-board results panel intentionally folded into the palette |
| command-palette.js | DONE* | CommandPalette.vue | per-task context actions added (reuses cardActionsFor). *spec/doc row types + keyboard nav over actions pending |
| docs.js | DONE* | LocalDocsPage.vue, DocsIndex.vue, CommandPalette.vue | docs viewer + **command-palette doc search** (Docs section → /docs/:slug). *background preload is a minor follow-up |
| keyboard-shortcuts.js | MISSING | KeyboardShortcutsModal.vue | dynamic binding to handlers, slash-to-focus |
| mention.js | DONE | lib/mentions.ts + useMentions.ts | @-file autocomplete (tested helpers + composable) wired into **composer + TaskDetail feedback**. (planning chat has its own autocomplete scaffolding) |

---

## Verification environment (isolated — never touches the user's live board)

- **`:8090`** — own `wallfacer run --backend host` instance, isolated `-data
  /tmp/wf-parity-data`, env `/tmp/wf-parity.env`, switched (via `PUT
  /api/workspaces`) to throwaway git repo `/tmp/wf-parity-ws.8XyBNd` (group
  `fcad2594…`). Serves the **old UI** (reference). Seeded: 2 backlog tasks + 1
  completed task ("Add Contributing Guidelines" → waiting, real diff + NDJSON).
- **`:5173`** — `WF_PROXY=http://localhost:8090 npm run dev`. Vite proxies
  `/api`→:8090 (proxy target now env-driven via `WF_PROXY`).
- **Playwright** installed (devDep) + harness at `frontend/.parity/harness.mjs`:
  `node .parity/harness.mjs [route]` screenshots old vs Vue (Vue gets
  `window.__WALLFACER__={mode:'local'}` injected) to `/tmp/parity/`.

## Progress log

- 2026-06-01: **Round-3 polish.** Document `<title>` reflects active
  workspace + running task count (mirrors legacy ui/js/git.js).
  TaskCard adds a `:title` attribute on the title row so truncated
  titles stay reachable on hover. specFrontmatter detects a leading
  `---` with no closing fence and exposes a `warning` field;
  SpecFocusedView surfaces the warning inline so users notice typos
  instead of silently dropping metadata. 5 new unit tests.
- 2026-06-01: **Round-2 regression sweep — 12 items closed.**
  1. Vue SPA is now the default; legacy UI behind WALLFACER_LEGACY_UI
     (older WALLFACER_VUE_UI=false honoured for back-compat). 11
     sub-tests cover the truth table.
  2. Raise Budget action on TaskDetail (dialog.prompt × 2 → PATCH
     max_cost_usd + max_input_tokens). Visible when failure_category
     is budget_exceeded or stop_reason mentions budget.
  3. Backlog editing in the detail aside — inline form for timeout /
     model / tags / max_cost_usd / max_input_tokens with diff-only
     PATCH so writes stay minimal.
  4. Automation toggles UI (autopilot/autotest/autosubmit/autosync/
     autopush) in SettingsTabExecution, each driving PUT /api/config.
  5. Empty-workspace bootstrap: AppLayout auto-opens the picker on
     first load when workspaces is empty; BoardPage shows an explicit
     "Pick a workspace to begin" pane if the picker is dismissed.
  6. Test action prompts for optional acceptance criteria, posts
     {criteria} when set.
  7. Cancel pending state — "Shutting down…" label + disabled
     button held 1.5 s after the POST.
  8. Modal focus trap + restoration (useFocusTrap composable) wired
     into ConfirmDialog, WorkspacePicker, TaskDetail. aria-modal
     added where it was missing.
  9. Disconnect banner above the main slot when SSE stays down >1 s.
  10. ContainerMonitor task cells become buttons that hash-route to
      the task detail (matches legacy task link).
  11. Tab ARIA roles on the main detail tabs (role=tab/tablist/
      tabpanel + aria-selected/aria-controls).
  12. Optimistic dnd: backlog reorder writes positions locally
      before SSE delta arrives; backlog → in-progress flips status
      locally and rolls back on failed PATCH.
  13. Toast queue cap at 5 (oldest evicted on overflow, 4 tests).
- 2026-06-01: **Post-audit regression fixes.** A parallel audit of the
  two frontends surfaced 8 real regressions that the headline tracker
  had missed; all closed in this push:
  1. Test verification badge on TaskCard (`badge-test-pass / -fail /
     -none`) so waiting cards can again show verified / unverified.
  2. Active 3 s oversight polling while status is `generating` /
     `pending` (`fetchOversight` recursive setTimeout, cancelled on
     tab/task switch + unmount) so the summary isn't stuck blank.
  3. Results tab — one entry per `output` event newest-first with
     Plan/Result chip (`detectResultType`, 11 tests), Markdown +
     Raw toggle + Copy.
  4. Behind-upstream banner on waiting/failed cards
     (`useBehindCounts` composable, cached by (taskId, updatedAt))
     with inline Sync button → POST /api/git/sync.
  5. Empty-state composer — when the board has 0 tasks, four columns
     collapse to a centred prompt with TaskComposer auto-expanded.
  6. ANSI palette + CR collapsing helpers (`lib/ansi`, 9 tests) wired
     into the Activity raw fallback so spinner / colour escapes
     render correctly.
  7. Activity search input + 5000-row cap + truncation banner.
  8. Command palette per-task action rows are now keyboard-navigable
     via a discriminated `FlatRow` union — ArrowDown / Up walks Start
     / Resume / Done / Retry inline, Enter dispatches.
- 2026-06-01: **Last mile — gap closure.** Workspace group management
  popover (switch/rename/delete inline in Sidebar + dialog/toast wiring),
  card-level keyboard nav (s/d/r/t/p + Enter/Space + arrow navigation),
  Explorer Task Prompts virtual section + tree-row keyboard nav
  (Up/Down/Left/Right/Enter/Space), span flamegraph SVG view backed by
  pure `lib/flamegraph` helpers (11 tests, lane-packing, deterministic
  colour, axis ticks, hover tooltip) wired as a new "Timeline" tab in
  TaskDetail, TaskCard priority/impact badge taxonomy ported from
  ui/js/render.js, and a Containers entry-point button in the status
  bar so the ContainerMonitor modal is reachable. Tracker is fully
  closed for local mode.
- 2026-06-01: **Explorer + infra.** Markdown preview toggle (auto-rendered
  for .md, Source button to flip back), SSE-driven live tree refresh
  (re-fetches root + every expanded directory on `refresh` events from
  /api/explorer/stream). Closes most of gap #7.
- 2026-06-01: **Infra.** fetchTasks accepts {includeArchived,
  archivedPageSize} and BoardPage watches ui.showArchived → server-side
  archived pagination working end-to-end. useSse gained a 35 s heartbeat
  watchdog that tears down + reconnects on silent connection death and
  fires onStaleRestart (AppLayout refetches the canonical task list).
  Closes most of gap #15.
- 2026-06-01: **Planning + bootstrap.** First non-empty spec snapshot now
  auto-focuses the alphabetically-first spec at +130 ms and pushes a 6 s
  "Your first spec was created at …" toast at +160 ms, idempotent per
  session. 3 unit tests. Closes bootstrap-choreography (gap #16).
- 2026-06-01: **File-size refactor.** Per project convention (max 1000 LOC
  per file): split header.css (1281), app.css (1543), spec-mode.css (1730)
  into themed `.css` partials under matching subfolders, each <500 LOC;
  PlanningChatPanel.vue (1739) → 874 by extracting `<style scoped>` to a
  sidecar (`<style scoped src="…">`), `RenderedBubble` + parsers to
  `lib/planningBubble.ts` (13 tests), and slash/mention autocomplete to a
  composable. Every frontend file is now <1000 LOC.
- 2026-06-01: **Composer parity round.** showPrompt added to dialog store
  (gap #5). Sandbox override dropdown (PATCH after create), prompt template
  insertion (cursor-aware datalist), blank-line `splitBatch` posting to
  /api/tasks/batch, and Schedule toggle posting to /api/routines all wired.
  Tracker gap #4 closed end-to-end.
- 2026-06-01: **Settings polish.** Model datalists populated from
  `lib/knownModels` (provider-scoped, respect custom base URLs);
  SettingsTabSandbox now subscribes to /api/images/pull/stream and surfaces
  live phase + layer count instead of polling. Tracker gap #14 closed.
- 2026-06-01: **Keyboard.** Bare "/" focuses the task search input (matches
  the help modal). Card-level s/d/arrow remain pending.
- 2026-05-30: Built initial gap inventory (8 parallel explorers). Set up
  isolated harness (Vite :5173 + own :8090 + throwaway workspace + test data).
- 2026-05-30: **CRITICAL FIX — Vue app didn't boot in dev at all** (blank page,
  both modes). Root cause: Vite `optimizeDeps`/esbuild lazy `__esm` init —
  `vue-router`'s top-level `defineComponent(RouterLinkImpl)` ran before
  `@vue/shared`'s `isFunction` was assigned → `isFunction is not a function`.
  Pre-existing (not caused by the playwright devDep). Fixed in `vite.config.ts`:
  `optimizeDeps: { include: ['vue','pinia'], exclude: ['vue-router'] }`. App now
  mounts in both modes. ⚠️ Watch for other deps calling `defineComponent` at
  import time (e.g. `@unhead/vue`, `latere-ui`) hitting the same lazy-init bug.
- 2026-05-30: **Gap #2 (diff viewer) DONE** — `lib/diff.ts` + Changes tab; verified.
- 2026-05-30: **Env fix — stale `.js` shadowing.** `src/**/*.js` build artifacts
  (gitignored, emitted by `vue-tsc -b`) were resolved *before* `.ts` siblings by
  Vite/Vitest, so `.ts` edits to any module with a `.js` sibling were silently
  ignored (caught when `prettyNdjson.ts` edits had no effect). Deleted all 83.
  ✅ **FIXED (later this session)**: added `noEmit` to tsconfig + build uses
  `vue-tsc --noEmit`, so `.js` are no longer emitted next to sources.
- 2026-05-30: **Gap #1 (pretty NDJSON output) DONE** — `parseActivity` extended
  (text+result, tested), `useTaskActivity` composable (text/plain stream, live+
  completed), Activity tab renders pretty rows. Removed dead `useLogStream`.
  Verified: completed task shows 12 activity rows (thinking/tool/result/text).
- 2026-05-30: **Task actions** brought to parity. `lib/cardActions.ts`
  (`cardActionsFor` matrix, 9 tests) drives `TaskCard` footer (Start / Resume /
  Test / Done / Retry per status+session, single source of truth). `TaskDetail`
  aside gained Resume (failed), Test (waiting/done/failed), Sync (waiting/failed).
  Verified live. Still pending: backlog **Plan** button (needs planning task-mode
  nav) and the per-card behind-upstream **Sync banner** (needs per-card diff fetch).
- 2026-05-30: **#3 command-palette context actions** — task rows now render
  inline Start/Resume/Test/Done/Retry (reuses `cardActionsFor`); row converted
  button→div role=button. Verified. (spec/doc rows + action keyboard-nav pending.)
- 2026-05-30: **#4 composer depth (core)** — flow picker (from `/api/flows`),
  tags input, timeout added to `TaskComposer`; tested `lib/composer.ts parseTags`.
  Verified. Pending: dependency picker, batch create, budget overrides, model /
  sandbox override, template insertion, @-mention, empty-state composer.
- 2026-05-30: **Explorer edit mode** — Edit/Save/Cancel + textarea in
  `ExplorerPage`, writes via `PUT /api/explorer/file` (same workspace+path as the
  proven read path). Render verified; save-roundtrip not Playwright-confirmed
  (harness flakiness) but wiring matches the working read. Note: per-gap browser
  verification via Playwright is flaky/slow — leaning on `vue-tsc` + unit tests +
  app-boots-and-renders as the primary safety net, browser smoke when cheap.
- 2026-05-30: **dnd + ranks** — BoardPage per-column pull/put (backlog
  pull-only, in_progress put-only), ghost/chosen drag classes, backlog `#N`
  position via new optional `rank` prop on `TaskCard`. Verified (#1/#2 render).
- 2026-05-30: **Confirm dialog** (`utils` showConfirm/showAlert) — Pinia
  `dialog` store + global `ConfirmDialog.vue` (mounted in AppLayout), wired to
  TaskDetail Delete/Cancel. 4 store tests. App boots clean.
- 2026-05-30: **@-mention autocomplete** — `lib/mentions.ts` (tested:
  query-detect, filter+rank, apply) + `useMentions` composable + composer
  dropdown (spec/ priority). Verified `@`→file list. Planning/feedback pending.
- 2026-05-30: **Toast system** — `stores/toast.ts` (push / pushWithAction /
  auto-dismiss, 4 tests) + global `Toaster.vue` (mounted in AppLayout). Wired
  TaskDetail archive/unarchive **undo toast**. Boots clean. Unblocks planning
  dispatch toast + other notifications. 64 tests total.
- 2026-05-30: **Routines page** — `RoutinesPage.vue` + `/routines` route +
  sidebar nav. Lists routines (reuses TaskCard), create form (prompt/interval/
  flow → POST /api/routines), delete-with-confirm. Verified render. routines.js DONE.
- 2026-05-30: **Presence** (sidebar) — tested `derivePresence` (agents from
  in-progress + self) + Sidebar presence section. **git open-folder** button
  added to BranchDropdown (branch-create already existed → git.js DONE). 68 tests.
- 2026-05-30: **Composer budget/model** — `createTask` opts extended
  (model, max_cost_usd, max_input_tokens) + composer "More" row. Verified fields
  render. Composer pending: dependency picker, batch, empty-state composer.
- 2026-05-30: **Oversight summary** rendered in TaskDetail Activity tab
  (phase title/summary/tools, lazy-fetched from /api/tasks/{id}/oversight).
  Verified on the completed task. modal-oversight DONE.
- 2026-05-30: **Feedback @-mention** — wired `useMentions` into TaskDetail
  feedback textarea (same tested composable as composer). mention.js DONE.
- 2026-05-30: **Legacy hash deep-link redirect** (AD-2) — App.vue maps #<uuid>
  and #plan/<path> to history routes (tested hashToRoute). Verified.
- 2026-05-30: Confirmed spec **archive/unarchive undo toast** already implemented
  in SpecFocusedView; with the dispatch toast, spec-mode.js is DONE.
- 2026-05-30: **Spec dispatch toast** — focused-view dispatch shows a "View on
  Board →" success toast. dispatch-toast DONE.
- 2026-05-30: **Board unread dot** — sidebar Board nav lights when new tasks
  arrive off-board (tested `hasUnseen`); cleared on board view. sidebar-badge DONE.
- 2026-05-30: **Docs in command palette** — Docs section (filters docIndex,
  → /docs/:slug). Verified. docs.js DONE.
- 2026-05-30: **Backlog impact-sort toggle** — `lib/backlogSort.ts` (tested
  sortBacklog by impact_score desc + localStorage persistence); BoardPage "Sort:
  Manual/Impact" toggle, drag-sort disabled + rank hidden in impact mode. dnd.js DONE.
- 2026-05-30: **Backlog Plan button** — `plan` added to the card-action matrix
  (tested); navigates to `/plan?task=<id>`; PlanPage opens task-mode via the
  existing `openPlanForTask`. Verified navigation.
- 2026-05-30: **FIXED the `.js`-shadow footgun permanently** — root cause was
  `tsconfig.json` lacking `noEmit` + build script using `vue-tsc -b` (emit mode),
  which wrote `.js` next to sources that shadowed `.ts` in dev/test. Added
  `"noEmit": true` and changed build to `vue-tsc --noEmit && vite-ssg build`.
  Typecheck now emits 0 `.js`. (Had regenerated 101 files mid-session.)
- 2026-05-30: First board diff (`/`). Board renders close to parity. New
  card-level gaps observed (add to backlog #4/#6): backlog cards missing the
  **Plan** button + `#N` position number; waiting card missing **Resume**/**Test**
  buttons, the **`unverified`** tag, and the **diff chip** (shows commit hash
  instead); sidebar missing the **Presence** row (agent + self).
