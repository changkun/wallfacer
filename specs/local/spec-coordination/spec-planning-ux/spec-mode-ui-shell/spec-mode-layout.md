---
title: Spec mode three-pane layout
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/spec-mode-ui-shell/mode-state-and-switching.md
affects:
  - ui/index.html
  - ui/js/spec-mode.js
  - ui/css/
effort: medium
created: 2026-03-30
updated: 2026-03-31
author: changkun
dispatched_task_id: null
---

# Spec mode three-pane layout

## Goal

Add the HTML containers and CSS layout for the spec mode three-pane view (explorer + focused markdown view + chat stream). The layout is hidden when in board mode and shown when in spec mode. The board container and spec mode container are siblings that swap visibility.

## What to do

1. In `ui/index.html`, restructure the `board-with-explorer` div to contain both views:
   ```html
   <div class="board-with-explorer">
     {{template "explorer-panel.html"}}
     <main class="board-grid flex-1 min-h-0" id="board" tabindex="-1">
       <!-- existing board columns -->
     </main>
     <div id="spec-mode-container" class="spec-mode-container" style="display: none">
       <div id="spec-focused-view" class="spec-focused-view">
         <div class="spec-focused-view__header">
           <span id="spec-focused-title" class="spec-focused-view__title">Select a spec</span>
           <span id="spec-focused-status" class="spec-focused-view__status"></span>
           <button id="spec-dispatch-btn" class="spec-dispatch-btn hidden">Dispatch</button>
           <button id="spec-summarize-btn" class="spec-summarize-btn hidden">Summarize</button>
         </div>
         <div id="spec-focused-body" class="spec-focused-view__body prose-content">
           <!-- rendered markdown goes here -->
         </div>
       </div>
       <div id="spec-chat-resize" class="spec-chat-resize" role="separator" aria-orientation="horizontal"></div>
       <div id="spec-chat-stream" class="spec-chat-stream">
         <div id="spec-chat-messages" class="spec-chat-stream__messages">
           <!-- chat messages rendered here -->
         </div>
         <div class="spec-chat-stream__input">
           <textarea id="spec-chat-input" class="spec-chat-input" placeholder="Type a directive..." rows="2"></textarea>
           <button id="spec-chat-send" class="spec-chat-send-btn" onclick="sendSpecChatMessage()">Send</button>
         </div>
       </div>
     </div>
   </div>
   ```

2. Add CSS for the spec mode layout:
   ```css
   .spec-mode-container {
     display: flex;
     flex: 1;
     min-height: 0;
     gap: 0;
   }
   .spec-focused-view {
     flex: 1;
     display: flex;
     flex-direction: column;
     min-width: 0;
     overflow: hidden;
   }
   .spec-focused-view__header {
     /* compact header bar with title, status badge, action buttons */
   }
   .spec-focused-view__body {
     flex: 1;
     overflow-y: auto;
     padding: 1rem;
   }
   .spec-chat-stream {
     width: 360px;
     min-width: 280px;
     display: flex;
     flex-direction: column;
     border-left: 1px solid var(--border-color);
   }
   .spec-chat-stream__messages {
     flex: 1;
     overflow-y: auto;
     padding: 0.5rem;
   }
   .spec-chat-stream__input {
     /* input area pinned to bottom */
   }
   .spec-chat-resize {
     /* vertical resize handle between focused view and chat */
   }
   ```

3. Update `switchMode()` in `ui/js/spec-mode.js` to toggle the new containers:
   - Board mode: `board.style.display = ""`, `spec-mode-container.style.display = "none"`
   - Spec mode: `board.style.display = "none"`, `spec-mode-container.style.display = ""`

4. Ensure the bottom panels (terminal, dep graph, office) work in both modes — they sit below `board-with-explorer` and are unaffected by the content swap.

5. Ensure the existing explorer panel's resize handle works correctly when the spec mode container is the adjacent sibling instead of the board.

## Tests

- `TestSpecModeLayoutVisible`: Switching to spec mode shows `spec-mode-container` and hides `board`.
- `TestBoardModeLayoutVisible`: Switching to board mode shows `board` and hides `spec-mode-container`.
- `TestBottomPanelsWorkInSpecMode`: Terminal panel toggle works when in spec mode (Ctrl+` cycles panels).
- `TestSpecFocusedViewScrollable`: The `spec-focused-view__body` container is scrollable when content overflows.
- `TestChatStreamLayout`: The chat stream has a fixed width, messages area scrolls, input stays at bottom.

## Boundaries

- Do NOT implement markdown rendering in the focused view — that's the `focused-markdown-view` task.
- Do NOT implement chat message sending or receiving — that's the planning-chat-agent spec.
- Do NOT implement the resize handle between focused view and chat — that's the `pane-resize` task.
- Do NOT implement the dispatch or summarize button behavior — those are in other specs. Just add the placeholder HTML elements.
