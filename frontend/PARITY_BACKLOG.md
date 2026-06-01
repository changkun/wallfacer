# Vue Frontend Parity Backlog (vs legacy ui/)

Exhaustive behavior-parity gaps from the 2026-06-01 fan-out audit (8 parallel
auditors over all 52 ui/js modules, deduped). Clearing this list is the
precondition for deleting `ui/`. Check items off as they land; each should be
one scoped commit.

Legend: `[ ]` open · `[x]` done · `[~]` intentionally won't-do (note why).

## Broken (wrong behavior shipping today)
- [x] modal-logs — "Events" tab renders only usage/timeline token stats, never the actual event stream (state_change/output/feedback/error/system) with conflict/rebase detail. Mislabeled. (ui/js/modal-core.js:1223-1429 → TaskDetail.vue:749-789)

## Missing (no frontend equivalent)
- [x] modal-core — "Blocked by" dependencies panel (list + status badges + clickable links + "Waiting on X of Y")
- [x] render — dependency badges on cards (blocked / deps-met / dep-cancelled with blocking names)
- [x] render — cost progress bar (green/yellow/red, spent vs max_cost_usd) on in_progress/waiting cards
- [x] render — failure-category friendly labels (Timeout/Budget/Crash/…) on failed cards
- [ ] status-bar — agent presence list (active agents + signed-in user, status dots)
- [~] status-bar — sign-in badge + org switcher (CLOUD-ONLY; needs /api/auth/orgs; deferred)
- [x] modal-core — retry history section (past attempts: status/time/cost/turns, expandable)
- [x] modal-core — usage breakdown table (per-agent token/cost: impl/test/refinement/oversight)
- [x] modal-core — prompt history section (#1/#2 prior iterations)
- [ ] modal-core — backlog spec Edit/Preview tabs (textarea vs rendered markdown)
- [ ] tasks — modal-based backlog editing (debounced PATCH of prompt/timeout/sandbox/deps/tags/budgets/scheduled_at/model)
- [ ] modal-logs — test-phase log streaming + test oversight (/oversight/test), parallel Testing tab
- [x] depgraph/unified-graph — Map view integrated (vendored; MapPage works)
- [ ] modal-flamegraph — span timeline detail table + cumulative cost chart (/turn-usage) + oversight phase bands
- [x] modal-stats — Top Tasks rows clickable to open task
- [ ] modal-logs — 8MB truncation banner + "Download full log" link
- [ ] modal-core — task environment aside provenance (container digest, instructions hash, API endpoint, ts)
- [ ] command-palette — spec rows (fuzzy match on spec title/path, Plan section)
- [ ] command-palette — "Sync with default" action for waiting/failed
- [ ] templates — searchable anchored template picker (live filter, body preview, Esc/outside-click)
- [ ] tasks — dependency picker search dropdown (auto-focus, filter, outside-click close)
- [x] tasks — tag input: Backspace removes last tag on empty; comma commits
- [x] tasks — flow-select updates composer placeholder/data-task-flow
- [ ] workspace — browser folder create (/api/workspaces/mkdir) + rename (/api/workspaces/rename)
- [x] workspace — per-group max-parallel override (done earlier in SettingsTabWorkspace)
- [ ] render — hide system routines (kind=routine + system:* tag) from board
- [x] render — scheduled badge with relative time on backlog cards
- [ ] render — forked-task ancestry badge (parent id, click to open)
- [ ] render — brainstorm category tag badges (BRAINSTORM_CATEGORIES from /api/config)
- [ ] board-composer — empty-board composer dismiss-for-session persistence
- [ ] board-composer — composer "@" action button (insert @ + fire mention)
- [x] sidebar-badge — board unread dot
- [ ] planning-chat — queue item double-click edit mode
- [x] planning-chat — send-mode toggle button (already present PlanningChatPanel.vue:45-61)
- [ ] spec-mode — sidebar collapse/expand persisted on boot
- [ ] search — `<mark>` highlight markup in results
- [ ] search — mode-aware filter routing (spec/depgraph/board)
- [ ] docs — prev/next ordered-doc nav links
- [ ] docs — floating table-of-contents sidebar
- [ ] docs — markdown link enhancement (linkHandler:'docs')
- [ ] containers — task status badge in monitor cell
- [~] terminal — Wails desktop PTY discovery (/api/desktop-port) (desktop-only; defer)
- [ ] flows — parallel-step grouping viz ("‖" chips)
- [ ] flows — step input_from relationship shown
- [x] envconfig — first-launch "no credentials" alert banner
- [x] trash-bin — restore-success toast
- [ ] markdown — modal/card markdown helper actions (toggle section, copy)
- [x] state — pendingCancel "cancelling" indicator (done earlier in TaskDetail)
- [ ] state — active-group badge tracking from SSE
- [ ] task-stream — waitForTaskTitle resilience (poll until non-empty)
- [ ] dispatch-toast — dispatched-task pulse highlight on board
- [ ] utils — mobile column nav (IntersectionObserver pill)
- [ ] events — keyboard shortcuts e/c/d/b (explorer/chat/dispatch/breakdown)

## Weaker (present but degraded)
- [ ] tasks — dependency picker chips w/ remove buttons (vs plain select multiple)
- [ ] modal-diff — highlight.js syntax coloring in diffs
- [ ] modal-results — collapse older turns in <details>
- [ ] modal-logs — impl-vs-test phase log separation
- [ ] git — 409 conflict shows blocking_tasks list, not generic alert
- [ ] command-palette — context actions: tab-switch jumps (testing/changes/flamegraph/timeline)
- [ ] command-palette — recent-tasks fallback when palette opens empty
- [x] search — multi-tag AND + text combination (DONE: lib/taskFilter)
- [ ] modal-ndjson — thinking blocks inline-expandable (>5 lines "+N lines")
- [x] explorer — unsaved-changes dirty-confirm on close
- [ ] explorer — 30+ extension/special-file semantic icon map (vs emoji)
- [x] explorer — Tab→indent in textarea edit
- [x] terminal — container picker uses /api/debug/health (DONE earlier; per-state icons still minor)
- [ ] tasks — brainstorm flow empty-prompt allowance + placeholder
- [ ] tasks — routine creation interval/repeat controls detail
- [ ] command-palette — doc search title-prefix bonus scoring
- [ ] command-palette — remote/local result distinction + server snippet field
- [ ] mention — file ranking path-substring boost
- [ ] status-bar — terminal panel height persisted
- [x] explorer — Task Prompts relative updated_at timestamp
- [x] explorer — md rendered/raw toggle (DONE earlier)
- [ ] envconfig — OAuth button visibility reactive to base-URL
- [ ] instructions — preloadedContent for re-init-from-template
- [ ] events — visibilitychange→fetchTasks on tab refocus
- [ ] task-stream — waitForTaskDelta SSE-resolve optimization
- [ ] workspace — group popover active/switching state (partial in Sidebar)
- [ ] spec-mode — focused-view crossfade epoch-guard against click-spam
- [ ] spec-explorer — Task Prompts SSE subscription to stay fresh

## Counts at audit time: 1 broken · 53 missing · 29 weaker
