---
title: Crossfade focused view on index ↔ spec switch
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/chat-first-mode/layout-state-machine.md
  - specs/local/spec-coordination/spec-planning-ux/chat-first-mode/explorer-roadmap-entry.md
affects:
  - ui/js/spec-mode.js
  - ui/css/spec-mode.css
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Crossfade focused view on index ↔ spec switch

## Goal

When the focused entry changes (index ↔ spec, spec ↔ different spec), the focused view content should crossfade rather than hard-replace. The spec-only affordances (status chip, dispatch, archive) have their own opacity + translateY transition so they clearly belong to specs and are absent for the index.

## What to do

1. In `ui/js/spec-mode.js`, wrap the focused-view content-swap logic:
   - Before mounting new content, set the outgoing content node's opacity to 0 over 140ms using `cubic-bezier(0.3, 0, 0.8, 0.15)` (emphasized accelerate).
   - 40ms into the outgoing fade, insert the incoming content at `opacity: 0`, then fade it to 1 over 180ms with `cubic-bezier(0.2, 0, 0, 1)` (emphasized decelerate).
   - Small queue to absorb click-spam: if a new swap starts while the previous incoming content is still fading in, finalize the previous to `opacity: 1` instantly before starting the new swap.
2. Compose with the existing `_switchEpoch` guard from `planning-chat-threads` (if it applies here) so stale fetches never paint.
3. Spec-only affordances (status chip, dispatch button, archive button, `depends_on` indicator): add a transition on `opacity` and `transform: translateY(6px)` over 220ms. Toggled by the `spec-focused-view--index` class from the explorer-roadmap-entry task.
4. Respect `prefers-reduced-motion: reduce`: crossfade durations collapse to 0; final state still applied.
5. The chat-pane messages area crossfade for tab switching (from the spec's Animations section) lives here as a minor addition: on `_switchToThread`, fade `#spec-chat-messages` to opacity 0 (120ms), swap content, fade back to 1. Ensure the epoch guard from `planning-chat-threads:00ffdc17` still prevents stale painting.

## Tests

- `ui/js/tests/spec-mode-animation.test.js` (new):
  - `TestFocusedViewCrossfade_FadesOutOld`: simulate a focus-change; during the transition window, the outgoing content's opacity approaches 0 over 140ms.
  - `TestFocusedViewCrossfade_FadesInNew`: incoming content is mounted at opacity 0, reaches 1 within 180ms.
  - `TestFocusedViewCrossfade_ClickSpam`: three rapid focus-change events result in the last content rendered, no earlier content stuck on screen.
  - `TestAffordances_HiddenOnIndex`: focusing the index applies `spec-focused-view--index`; the dispatch button's opacity is 0.
  - `TestAffordances_AppearOnSpec`: focusing a spec removes the index class; dispatch button opacity is 1.
  - `TestCrossfade_ReducedMotion`: under `prefers-reduced-motion: reduce`, the crossfade resolves synchronously and final state is correct.
- `ui/js/tests/planning-chat.test.js` (extend):
  - `TestChatMessages_CrossfadeOnThreadSwitch`: `_switchToThread` triggers an opacity-0 → opacity-1 transition on `#spec-chat-messages`, with the epoch guard preventing stale paints.

## Boundaries

- **Do NOT** move the explorer or chat pane during content swaps — those are layout-state transitions, not focus-change transitions.
- **Do NOT** animate markdown content reflow inside the focused view. Only the container's opacity changes.
- **Do NOT** add scroll restoration as part of this task — the existing per-thread scroll tracking covers it.
- **Do NOT** introduce a JS animation library. CSS transitions only; JS only toggles classes and timing.
