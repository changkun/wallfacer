---
title: Guided path from Plan to Board — toast, badge, empty-board hint
status: validated
depends_on: []
affects:
  - ui/js/tasks.js
  - ui/js/spec-mode.js
  - ui/partials/sidebar.html
  - ui/partials/board.html
  - ui/css/
effort: medium
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Guided path from Plan to Board

## Goal

Three affordances that bridge users from Plan to Board at the right moment: (1) a dispatch-complete toast with a "View on Board →" button, (2) a subtle unread dot on the sidebar Board nav button when tasks exist the user hasn't seen, (3) a one-line empty-Board hint pointing back to Plan.

## What to do

### 1. Dispatch-complete toast

1. In `ui/js/tasks.js` (or the dispatch handler for `/api/specs/dispatch`), on a successful response, show a toast:
   - Content: `"Dispatched N task(s) to the Board."`
   - Action button: `[View on Board →]`
   - Slide-in animation from bottom-right, 200ms emphasized decelerate.
   - Auto-dismiss after 8s OR on button click. Do not auto-dismiss faster.
2. On button click:
   - Switch mode to `"board"` (same path as clicking the nav button).
   - Do NOT write `wallfacer-mode` to localStorage (see `default-mode-resolution.md`).
   - Scroll the Backlog column to show the new task(s) and apply a one-cycle pulse animation (CSS class `task-card--just-created`, 1200ms total) to highlight them.
3. Reuse any existing toast infrastructure (`ui/js/lib/` may have one). If not, create a minimal toast element appended to `document.body` with an absolute-positioned container at bottom-right.

### 2. Sidebar Board unread dot

1. In `ui/partials/sidebar.html`, add a `<span class="sidebar-nav__unread-dot" hidden>` inside the Board nav button.
2. In `ui/js/tasks.js` on the task stream handler, track a per-session set of task IDs the user has "seen" (toggled to true when Board mode is active and the task is visible in the viewport). New tasks not in the seen-set activate the dot.
3. The dot clears the moment Board mode is entered (any task visible in the Board is implicitly "seen" for the purpose of dismissing the dot).
4. CSS: same visual style as the `spec-chat-tab__unread` dot from `planning-chat-threads` for consistency — 6px circle, accent color.

### 3. Empty-Board hint

1. In `ui/partials/board.html`, add an empty-state element rendered only when the Board has zero tasks. For the scope of THIS task, the empty state is a one-line hint:
   - `"Nothing to execute yet. Start a chat in [Plan] to draft your first spec, or drop a task directly into the backlog →"`
   - `[Plan]` is a clickable element that triggers the mode switcher.
2. (The full empty-Board composer is implemented in `empty-board-composer.md` and replaces this minimal hint. Keep the hint simple here; the composer task will swap it out.)

## Tests

- `ui/js/tests/dispatch-toast.test.js` (new):
  - `TestToast_RendersOnDispatchSuccess`: mocked dispatch response → toast element appears with correct text.
  - `TestToast_ViewOnBoardSwitchesMode`: click the action button → mode becomes `"board"`.
  - `TestToast_DoesNotPersistPreference`: click the action button → `localStorage.wallfacer-mode` unchanged.
  - `TestToast_PulsesNewTasks`: after mode switch, task cards with IDs from the dispatch response have the `task-card--just-created` class for the animation duration.
  - `TestToast_AutoDismissAfter8s`: advance timers 8000ms → toast is removed.
- `ui/js/tests/sidebar-badge.test.js` (new):
  - `TestBadge_AppearsOnNewTask`: simulate a task-created SSE event while in Plan mode → dot is visible.
  - `TestBadge_ClearsOnBoardMode`: enter Board mode → dot is hidden.
  - `TestBadge_PersistsAcrossModeSwitches`: new task arrives in Plan → dot on; switch to Plan (again) → dot still on; switch to Board → dot off.
- `ui/js/tests/board.test.js` (extend):
  - `TestEmptyBoardHint_RendersWhenZeroTasks`: empty task list → hint element is rendered with the clickable `[Plan]` link.
  - `TestEmptyBoardHint_HiddenWhenNonEmpty`: at least one task → hint is not in the DOM.
  - `TestEmptyBoardHint_PlanLinkSwitchesMode`: click the `[Plan]` link → mode becomes `"plan"`.

## Boundaries

- **Do NOT** implement the empty-Board composer in this task — it's scoped to `empty-board-composer.md` and will replace the minimal hint.
- **Do NOT** show the unread dot on nav buttons other than Board. This is a Plan → Board bridge, not a general notification system.
- **Do NOT** persist the "seen" set across reloads. A reload resets to "all existing tasks are seen" (avoids a stale dot showing on every cold open).
- **Do NOT** use the dispatch toast for any other purpose. If future flows want a toast, they add their own — this is not a general toast framework.
