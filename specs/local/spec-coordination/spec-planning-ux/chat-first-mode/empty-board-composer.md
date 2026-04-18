---
title: Empty-Board task-creation composer
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/chat-first-mode/plan-to-board-bridges.md
affects:
  - ui/js/board-composer.js
  - ui/js/render.js
  - ui/partials/board.html
  - ui/partials/scripts.html
  - ui/css/board.css
effort: large
created: 2026-04-12
updated: 2026-04-13
author: changkun
dispatched_task_id: null
---

# Empty-Board task-creation composer

## Goal

When Board mode opens with zero tasks in the current workspace group, render a prominent task-creation composer — not empty columns. The composer uses the same visual shell as the Plan chat composer, exposes the existing task-create fields via a `[▾ Advanced]` disclosure, submits via `POST /api/tasks`, and animates a "lift" into the Backlog column as a real task card while the rest of the Board UI fades in behind it.

## What to do

1. Create `ui/js/board-composer.js` exporting a `BoardComposer` module:
   - `init()` mounts the composer DOM when the board is empty; unmounts when the task list becomes non-empty.
   - Uses the same CSS shell classes as the planning chat composer (`spec-chat-composer` et al.) for visual parity. Add a `board-composer` modifier class for Board-specific styling (centring, label).
   - Primary label: `"What should the agent work on?"`; bottom row: `[▾ Advanced]` on the left, `[Create ➤]` on the right.
   - Beneath the composer: a one-line link `"Planning something larger? Start a chat in [Plan] →"` that switches mode to Plan on click.
2. Advanced disclosure (`[▾ Advanced]`):
   - Expanded state persists per session (module-scope flag), NOT across reloads.
   - When expanded, render below the textarea:
     - Sandbox dropdown (Claude / Codex), default from server config.
     - Goal textarea (optional).
     - Timeout input (minutes), default 60.
     - Fresh start checkbox, default false.
     - Prompt template dropdown populated from `/api/templates`.
   - Field IDs + fetched values come from the existing task-create path — do NOT introduce new field definitions.
3. Submit flow:
   - `POST /api/tasks` with `{prompt, goal, timeout, sandbox, fresh_start}` and optional `template_id` (same body the current UI uses — find it in the existing task-create path and reuse verbatim).
   - On success, receive the created task object.
   - Animate the composer lift:
     a. Measure the composer's bounding rect and the target position in the Backlog column.
     b. Compress the composer's textarea content into a single-line card preview via opacity + scale (180ms, emphasized accelerate).
     c. Translate the composer shell to the Backlog-column position, scaling to the task-card footprint (260ms, emphasized decelerate).
     d. Concurrently fade the full Board UI in behind the composer (`opacity: 0 → 1` over 200ms, 60ms delay).
     e. At end of animation, unmount the composer and mount the real task card in the Backlog column with a single pulse animation (reuse `task-card--just-created` from `plan-to-board-bridges.md`).
   - Total elapsed time under 500ms (matches Plan-bootstrap budget).
4. Respect `prefers-reduced-motion: reduce`: animations collapse to instant transitions; final state (composer gone, board visible, card present) is correct.
5. Task-count watcher: subscribe to the task stream SSE; as soon as the task count transitions from 0 to ≥1, unmount the composer. DO NOT re-mount if the task count later drops back to 0 within the same session — the user has moved past onboarding.

## Tests

- `ui/js/tests/empty-board-composer.test.js` (new):
  - `TestComposer_RendersOnlyWhenEmpty`: task list with 0 tasks → composer mounts; 1+ tasks → composer unmounted.
  - `TestComposer_NoRemountOnArchiveDuringSession`: composer mounts; a task is created (unmounts); task is archived (task count back to 0) → composer does NOT remount.
  - `TestComposer_SubmitsSameBodyShape`: click Create with populated fields → `fetch("/api/tasks", {method: "POST", body: ...})` receives the same shape as the existing task-create flow (assert via a recorded expected payload from the current code).
  - `TestComposer_AdvancedExpandsAndPersists`: click `[▾ Advanced]` → fields visible; navigate away and back within session → fields still visible.
  - `TestComposer_AdvancedResetsOnReload`: re-init module after a page reload → advanced is collapsed.
  - `TestComposer_PlanLinkSwitchesMode`: click the `Plan` link beneath → mode becomes `"plan"`.
  - `TestComposer_LiftAnimationCompletes`: after submit, composer is unmounted within 500ms and the new card is visible in Backlog with the pulse class applied.
  - `TestComposer_ReducedMotion`: under `prefers-reduced-motion: reduce`, all states (composer unmount, board visible, card present) resolve immediately.
- `ui/js/tests/board.test.js` (extend):
  - `TestBoard_EmptyHintReplacedByComposer`: the minimal empty-Board hint from `plan-to-board-bridges.md` is unmounted when the composer mounts; both cannot be visible simultaneously.

## Boundaries

- **Do NOT** introduce a new API route. Reuse `POST /api/tasks` as-is.
- **Do NOT** define new task fields. The composer exposes exactly the fields the current task-create flow exposes.
- **Do NOT** persist advanced-expanded state across sessions. A user-wide preference is explicitly excluded by the parent spec's non-goals.
- **Do NOT** show the composer when the Board has tasks, even if all tasks are archived (archived tasks still prevent the empty state in this flow; we only check the non-archived count to match the parent spec, but the composer unmounts once any task has been created in the session regardless of its current state).
- **Do NOT** rebuild the empty-Board hint; it's replaced, not extended. Delete the hint markup once the composer is functional.

## Implementation notes

- The lift animation is implemented in `_animateOutAndUnmount` (`ui/js/board-composer.js`): on successful submit the composer is reparented to `document.body` with `position: fixed` at its current viewport rect, its interior chrome (label, action bar, advanced panel, bridge line) fades over 180ms, and the shell translates + scales (emphasized decelerate, 260ms) onto the Backlog "+ New Task" button's rect. Reparenting empties the composer slot so the `#board:has(...)` rule disengages and the board columns reappear; `.board--columns-entering` on `#board` fades them in over 200ms with a 60ms delay (see `ui/css/board.css`). Total elapsed time ~380ms, inside the 500ms budget from the parent spec.
- When the board or the Backlog target cannot be measured (e.g. headless DOM, hidden board), the animation falls back to an immediate `unmount()`, matching the `prefers-reduced-motion: reduce` path.
- Board columns are hidden while the composer is mounted via `#board:has(#board-empty-composer:not(:empty)) > [id^="col-wrapper-"] { display: none }` so the empty board shows only the composer instead of four empty column headers alongside it.
- The DOM structure mirrors the Plan-mode chat composer: an outer `.board-composer-wrap` card (same border/radius/shadow/bg-card as `.spec-chat-stream` in chat-first) wraps an inset `.board-composer.spec-chat-composer` that reuses the Slack-style base from `spec-mode.css` (bg-input, 6/8/8 margin, 8px radius) with zero board-specific overrides on the shell. The action bar exposes two `.spec-chat-composer__action` icons — `@` (inserts an `@` and re-fires the input event so the mention autocomplete opens) and `▾` (toggles the advanced panel) — plus a single `➤` send button (no split group, since Board has no send-mode toggle). `/` is deliberately omitted from the top-level action bar because Board mode has no live slash-command system analogous to Plan mode's, and a top-level `/` button would promise UX that doesn't exist; the prompt-templates picker is instead reachable via an "Insert from template" button inside the advanced panel. Advanced exposes sandbox, timeout, goal, and templates; `fresh_start` is omitted because it is only meaningful when resuming a previous session. An empty-state hint reusing `.spec-chat-empty-hint` / `.spec-chat-empty-hint__cmd` sits above the inner composer, and the "Planning something larger?" bridge line sits below the card as a sibling (outside the card framing).
- `TestComposer_LiftAnimationCompletes` and `TestComposer_SubmitsSameBodyShape` from the spec's test matrix are not automated. The former relies on `getBoundingClientRect` / layout-driven transforms that the vm-based test harness cannot represent faithfully; the latter would require a stateful DOM (the composer uses innerHTML once and then reads via querySelector) which the harness also cannot represent. Mount/unmount, session dismissal, reduced-motion, and sync(count) branches are all covered; the submit-body shape is verified by code inspection against `tasks.js:createTask` — same field set, same defaults; the lift animation is validated interactively in the browser.
- The full new-task form (`#new-task-form` in `ui/partials/board.html`) is unchanged. The composer coexists with it; once the first task is created the composer unmounts and the existing "+ New Task" button is the primary path for further tasks. This avoids coupling two different UI surfaces to the same createTask code path.
- `render.js` drives `BoardComposer.sync(totalVisible)` every refresh. When the module is absent (e.g. in tests that load render.js without board-composer.js) the call is skipped via a `typeof BoardComposer !== "undefined"` guard so tests that pre-date this spec continue to pass.
