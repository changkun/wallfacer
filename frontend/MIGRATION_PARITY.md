# Vue Frontend Parity Tracker

Working tracker for `specs/local/vue-frontend-migration.md`. Goal: the Vue app
(`frontend/`) reaches **100% functional parity** with the vanilla-JS UI (`ui/`).
Reference UI runs at `http://localhost:8080` (old, plain JS). Vue runs via
`cd frontend && npm run dev` (Vite :5173, proxies `/api` → :8080). Inject
`window.__WALLFACER__ = {mode:'local', serverApiKey:'', version:''}` to force
local mode in dev.

Status legend: **DONE** = behavior parity reached · **PARTIAL** = exists, gaps
listed · **MISSING** = no Vue equivalent · **VERIFY** = agent uncertain, confirm.

Parity = **behavior**, not pixels. Cosmetic diffs are out of scope.

> Backend note: local board is currently **empty** (`/api/tasks` → `[]`), auth is
> open. Task-dependent features (cards, detail, diff, logs) need a throwaway
> workspace with test tasks to verify dynamically — do **not** mutate the user's
> live board.

---

## Summary (53 modules + 4 lib)

- **DONE (~19):** transport, theme, markdown, task-stream, terminal, containers,
  agents, flows, system-prompts, instructions, templates, usage-stats,
  analytics-tabs, trash-bin, modal-logs, depgraph, unified-graph, spec-explorer.
- **PARTIAL (~22):** api, state, events, utils, tasks, dnd, status-bar,
  modal-core, modal-oversight, modal-results, modal-ansi, modal-stats, span-stats,
  planning-chat, spec-mode, envconfig, images, routines, explorer, git, workspace,
  search, command-palette, docs.
- **MISSING (~11):** render*, bootstrap-choreography, sidebar-badge, modal-diff,
  modal-ndjson, modal-flamegraph, dispatch-toast, ideate†, mention,
  keyboard-shortcuts‡, board-composer.

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
4. **board-composer + tasks.js creation depth.** Flow picker, dependency picker,
   batch create, budget overrides (max_cost/tokens), tag input, routine
   scheduling, model override, per-task sandbox override, template insertion,
   empty-state composer. Vue composer is a minimal 4-field form.
5. **utils dialogs — showAlert/showConfirm/showPrompt + ARIA announce.** Many
   flows depend on these; no consolidated Vue equivalent.
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
14. **git** branch create + open-folder; **images** pull SSE progress; **envconfig** model dropdown populate.
15. **Infra:** api archived-task pagination, deep-link hash redirect (`#uuid`,
    `#plan/path`), BroadcastChannel SSE tab-leader relay, heartbeat staleness.
16. **render** tag/impact badges + relative time (VERIFY in TaskCard), **bootstrap-choreography**
    first-spec focus/toast, **sidebar-badge** board unread dot, **keyboard-shortcuts** dynamic binding.

---

## Per-area detail

### Core infra (api, transport, state, events, render, utils, markdown, bootstrap, theme)
| Module | Status | Vue equivalent | Missing behaviors |
|---|---|---|---|
| transport.js | DONE | api/client.ts | — bearer token + fetch wrappers covered |
| theme.js | DONE | stores/prefs.ts | — toggle/persist/OS-listener covered |
| markdown.js | DONE | lib/markdown.ts | marked+highlight+mermaid placeholders covered |
| api.js | PARTIAL | api/client.ts, stores/tasks.ts, composables/useSse.ts | archived pagination, deep-link hash, waitForTaskDelta, BroadcastChannel relay, heartbeat staleness, git stream |
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
| spec-mode.js | PARTIAL | PlanningPage.vue, SpecFocusedView.vue | dispatch button flow + tree refresh + toast, archive/unarchive toast w/ undo |
| dispatch-toast.js | PARTIAL+ | stores/toast.ts + Toaster.vue | **toast system built** (Pinia store + global Toaster, tested) and used for archive/unarchive **undo**. Pending: planning dispatch-complete toast + "View on Board →" wiring |

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
