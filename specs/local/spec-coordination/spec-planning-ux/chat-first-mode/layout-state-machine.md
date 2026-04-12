---
title: Layout state machine — chat-first vs three-pane
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/chat-first-mode/spec-tree-index-endpoint.md
affects:
  - ui/js/spec-mode.js
  - ui/css/spec-mode.css
  - ui/partials/spec-mode.html
effort: medium
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Layout state machine — chat-first vs three-pane

## Goal

Make the Plan-mode layout respond to spec tree state: `ChatFirst` (no specs and no index) renders chat-only; `ThreeIndex`/`ThreeSpec` renders the existing three-pane with the explorer pinned entry or a focused spec. Transitions are driven by `/api/specs/stream` SSE events and animate with CSS transitions.

## What to do

1. In `ui/partials/spec-mode.html`, set a `data-layout` attribute on the spec-mode container. Initial value `three-pane` to preserve current rendering; `spec-mode.js` will update it after the first `/api/specs/tree` fetch.
2. In `ui/css/spec-mode.css`, pin the explorer and focused-view widths as CSS custom properties and add transition rules:
   ```css
   .spec-mode-container {
     --explorer-width: 220px;
     --focused-view-width: 1fr;
   }
   [data-layout="chat-first"] .spec-explorer,
   [data-layout="chat-first"] .spec-focused-view {
     width: 0;
     opacity: 0;
     transform: translateX(-12px);
     overflow: hidden;
     pointer-events: none;
   }
   [data-layout="three-pane"] .spec-explorer {
     width: var(--explorer-width);
     opacity: 1;
     transform: none;
   }
   /* etc. for focused-view, chat-stream flex */
   .spec-explorer, .spec-focused-view, .spec-chat-stream {
     transition:
       width 260ms cubic-bezier(0.2, 0, 0, 1),
       opacity 260ms cubic-bezier(0.2, 0, 0, 1),
       transform 260ms cubic-bezier(0.2, 0, 0, 1);
   }
   [data-layout="chat-first"] {
     transition-timing-function: cubic-bezier(0.3, 0, 0.8, 0.15);
     transition-duration: 200ms;
   }
   @media (prefers-reduced-motion: reduce) {
     .spec-explorer, .spec-focused-view, .spec-chat-stream {
       transition-duration: 0s;
     }
   }
   ```
3. In `ui/js/spec-mode.js`, add a function `_applyLayout()` that:
   - Reads `specModeState.tree` (non-empty?) and `specModeState.index` (non-null?) — both populated from the `/api/specs/tree` endpoint and SSE stream.
   - Sets `data-layout="chat-first"` iff `tree` is empty AND `index` is null; else `three-pane`.
   - Calls itself on SSE update and on workspace group switch.
4. In the `C` keyboard shortcut handler, add an early-return when `data-layout === "chat-first"` — the chat pane is the only visible pane, so hiding it is a no-op.
5. The resize handle (`#spec-chat-resize`) between the focused view and chat stream must be hidden in chat-first layout (`[data-layout="chat-first"] #spec-chat-resize { display: none; }`).

## Tests

- `ui/js/tests/spec-mode-layout.test.js` (new):
  - `TestLayout_EmptyTreeNullIndex_RendersChatFirst`: stub tree endpoint returns `{tree: [], index: null}`. After init, container has `data-layout="chat-first"`.
  - `TestLayout_NonEmptyTree_RendersThreePane`: stub returns `{tree: [{...}], index: null}`. Container has `data-layout="three-pane"`.
  - `TestLayout_IndexOnlyNoSpecs_RendersThreePane`: `{tree: [], index: {...}}` → `three-pane`.
  - `TestLayout_TransitionOnSSEUpdate`: initial `chat-first`; simulate an SSE payload carrying `index: {...}`; `data-layout` flips to `three-pane`.
  - `TestLayout_TransitionReverseOnEmpty`: initial `three-pane`; SSE reports tree empty and index null; layout flips to `chat-first`.
  - `TestLayout_CIsNoOpInChatFirst`: simulate `C` keydown when `data-layout="chat-first"`; chat pane visibility is unchanged.
  - `TestLayout_ReducedMotionHonored`: stub `matchMedia('(prefers-reduced-motion: reduce)')`, check computed `transition-duration` on the explorer is 0s.

## Boundaries

- **Do NOT** implement the focused-view crossfade or the spec-only-affordance slide-out — that's the follow-up `focused-view-crossfade.md` task.
- **Do NOT** fetch markdown or render the focused view for the index — that's the `explorer-roadmap-entry.md` task.
- **Do NOT** animate the chat pane's internal contents (messages area). Tab-bar crossfade is a separate concern.
- **Do NOT** persist the layout state in localStorage — it's derived from server state, not user preference.
