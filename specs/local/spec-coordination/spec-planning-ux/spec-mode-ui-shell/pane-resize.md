---
title: Spec mode pane resize handle
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/spec-mode-ui-shell/spec-mode-layout.md
affects:
  - ui/js/spec-mode.js
  - ui/css/
effort: small
created: 2026-03-30
updated: 2026-03-31
author: changkun
dispatched_task_id: null
---

# Spec mode pane resize handle

## Goal

Add a draggable resize handle between the focused markdown view and the chat stream pane, following the same pattern as the existing explorer and terminal panel resize handles.

## What to do

1. In `ui/js/spec-mode.js`, add `_initSpecChatResize()` following the pattern from `_initExplorerResize()` in `explorer.js` and `_initPanelResize()` in `status-bar.js`:
   ```javascript
   var _specChatMinWidth = 280;
   var _specChatMaxWidthFraction = 0.5;
   var _specChatStorageKey = "wallfacer-spec-chat-width";

   function _initSpecChatResize() {
     var handle = document.getElementById("spec-chat-resize");
     var chatPane = document.getElementById("spec-chat-stream");
     if (!handle || !chatPane) return;

     // Restore persisted width
     var stored = localStorage.getItem(_specChatStorageKey);
     if (stored) {
       var w = parseInt(stored, 10);
       if (w >= _specChatMinWidth) chatPane.style.width = w + "px";
     }

     handle.addEventListener("mousedown", function(e) {
       e.preventDefault();
       var startX = e.clientX;
       var startW = chatPane.offsetWidth;
       document.body.style.userSelect = "none";
       document.body.style.cursor = "col-resize";

       function onMouseMove(ev) {
         // Chat is on the right, so dragging left increases width
         var delta = startX - ev.clientX;
         var maxW = Math.floor(window.innerWidth * _specChatMaxWidthFraction);
         var newW = Math.min(maxW, Math.max(_specChatMinWidth, startW + delta));
         chatPane.style.width = newW + "px";
       }

       function onMouseUp() {
         document.removeEventListener("mousemove", onMouseMove);
         document.removeEventListener("mouseup", onMouseUp);
         document.body.style.userSelect = "";
         document.body.style.cursor = "";
         localStorage.setItem(_specChatStorageKey, parseInt(chatPane.style.width, 10));
       }

       document.addEventListener("mousemove", onMouseMove);
       document.addEventListener("mouseup", onMouseUp);
     });
   }
   ```

2. Add CSS for the resize handle:
   ```css
   .spec-chat-resize {
     width: 4px;
     cursor: col-resize;
     background: transparent;
     transition: background 0.15s;
   }
   .spec-chat-resize:hover {
     background: var(--accent-color);
   }
   ```

3. Call `_initSpecChatResize()` on DOMContentLoaded.

## Tests

- `TestChatPaneResizePersistence`: Dragging the handle changes chat pane width. The width is persisted to localStorage and restored on reload.
- `TestChatPaneMinWidth`: Cannot resize below 280px.
- `TestChatPaneMaxWidth`: Cannot resize beyond 50% of window width.

## Boundaries

- Do NOT modify the explorer resize handle — it already works.
- Do NOT add touch event handling — mouse-only, same as existing resize handles.
