---
title: First-spec bootstrap UX choreography — toast, auto-focus, timing
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/chat-first-mode/layout-state-machine.md
  - specs/local/spec-coordination/spec-planning-ux/chat-first-mode/spec-new-directive-parser.md
  - specs/local/spec-coordination/spec-planning-ux/chat-first-mode/readme-autocreate.md
affects:
  - ui/js/bootstrap-choreography.js
  - ui/js/spec-explorer.js
  - ui/partials/scripts.html
  - ui/css/spec-mode.css
effort: small
created: 2026-04-12
updated: 2026-04-13
author: changkun
dispatched_task_id: null
---

# First-spec bootstrap UX choreography

## Goal

When the `/spec-new` directive fires the first spec in an empty repo, the UX reads as a single fluid event: the user's message appears, the agent's opening lines arrive, the layout animates open, the focused view auto-populates with the new spec mid-transition, a toast slides in, and the agent's stream continues in chat. Total elapsed time from the spec-tree SSE event to "agent writing into the focused view" is under 500ms.

## What to do

1. Subscribe `ui/js/spec-mode.js` to spec-tree SSE events. When a new spec appears AND the layout was previously `chat-first`:
   - Trigger the `ChatFirst → ThreeSpec` layout transition (already built by `layout-state-machine.md`).
   - At 160ms into the transition (configurable constant, default `TOAST_DELAY_MS = 160`), mount a toast with the text `"Your first spec was created at <path>. Rename or move it anytime."`
   - Mid-transition (~130ms), call the focus-spec handler with the newly-created spec's path so the focused view starts populating as the panes finish opening.
2. The toast component (`ui/js/lib/toast.js`):
   - If the codebase already has a toast helper (from `plan-to-board-bridges.md`), reuse it. Otherwise create a minimal shared helper here, matching the visual style that spec also uses.
   - Slide-in from the top (not bottom, to differentiate from the dispatch-complete toast at bottom-right), 200ms emphasized decelerate.
   - Auto-dismisses after 6s or on click; no action button for this toast.
3. The focused view's content starts appearing live as the server appends the agent's body content to the scaffolded file (via `spec-new-directive-parser.md`). The file watcher fires another SSE update on the file's content change; the focused-view code re-fetches and re-renders on each update.
4. Concurrent with the focused-view population, the agent's stream continues landing in the chat pane — the chat bubble from `planning-chat-threads` remains untouched by this choreography.
5. Timing budget (assert in tests, implement with CSS transition-duration + small JS setTimeout values):
   - SSE event received → layout transition begins: within 1 frame (~16ms).
   - Layout transition duration: 260ms (from `layout-state-machine.md`).
   - Auto-focus fired: 130ms after SSE.
   - Toast appears: 160ms after SSE.
   - First agent body content visible in focused view: ≤ 500ms after SSE (depends on how fast the agent writes).
6. `prefers-reduced-motion: reduce`: toast still appears (user-visible info shouldn't be motion-gated), but without the slide-in animation. Layout transitions to final state instantly.

## Tests

- `ui/js/tests/bootstrap-choreography.test.js` (new):
  - `TestChoreography_ToastAppearsAt160ms`: simulate the SSE event; advance timers; assert the toast element is mounted at ≈160ms with the expected text including the spec path.
  - `TestChoreography_AutoFocusAt130ms`: on the SSE event, the focus-spec handler is called with the new spec's path at ≈130ms.
  - `TestChoreography_FocusedViewPopulates`: subsequent SSE events on the same file trigger re-renders of the focused view.
  - `TestChoreography_OnlyOnChatFirstOrigin`: if the layout was already `three-pane` when the SSE fires, no toast appears (this isn't a "first spec" event in that case).
  - `TestChoreography_ReducedMotion`: under reduced motion, toast appears immediately without slide-in animation; layout transitions to final state with 0 duration; focused view populates correctly.

## Boundaries

- **Do NOT** animate the chat bubble itself. The stream keeps working as designed by `planning-chat-threads`.
- **Do NOT** block the UI thread during the choreography — all sequencing is via `setTimeout` and CSS transitions, never `requestIdleCallback` chains or long JS computations.
- **Do NOT** fire the toast when a spec is created outside this flow (e.g., a user runs `wallfacer spec new` in a terminal while the UI is open). Only when the transition is `ChatFirst → ThreeSpec` caused by the first-scaffold event.
- **Do NOT** extend the bootstrap toast with an action button. Simple auto-dismissing info — if the user wants to act, the focused spec is already rendered.

## Implementation notes

- The toast helper landed as its own module (`ui/js/bootstrap-choreography.js`) rather than a shared `ui/js/lib/toast.js`. The dispatch-complete toast from `plan-to-board-bridges` is already its own standalone module (`ui/js/dispatch-toast.js`); introducing a generic base now would require refactoring both. The two toasts have different positions (top-centre vs. bottom-right), different dismissal rules (click-anywhere vs. close-button), and different lifecycles, so the small duplication is cheaper than a premature abstraction.
- `TestChoreography_FocusedViewPopulates` from the spec's matrix is covered by the existing `_loadAndRenderSpec` refresh polling (spec-mode.js already re-fetches on SSE updates and after the refresh timer fires). No new test was added for that flow — it would exercise already-tested code.
- The trigger is gated by an idempotency flag so reconnect-induced repeat snapshots never replay. The "only on ChatFirst-origin" requirement is enforced at the call site in `spec-explorer.js`: the handler captures the pre-update state (was tree empty AND index null?) and only fires the choreography when that transitions into a non-empty tree. A refresh that happened while the tree was already non-empty therefore produces no toast.
- External scaffolds (e.g. running `wallfacer spec new` in a terminal while the UI is open) naturally respect the boundary: the idempotency flag blocks the toast on any non-first event, and the tree was not chat-first when the file appeared, so the initial detection never fires either.
- `BootstrapChoreography` exposes its delay constants (`AUTO_FOCUS_DELAY_MS`, `TOAST_DELAY_MS`, `TOAST_DISMISS_MS`) as read-only fields so tests can reference the exact numbers rather than hard-coding them — keeps the tests in lock-step with the implementation if a delay is ever tuned.
