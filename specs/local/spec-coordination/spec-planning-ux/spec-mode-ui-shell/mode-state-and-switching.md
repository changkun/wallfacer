---
title: Add mode state and header mode tabs
status: validated
depends_on: []
affects:
  - ui/js/state.js
  - ui/js/events.js
  - ui/index.html
effort: small
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Add mode state and header mode tabs

## Goal

Introduce a `currentMode` state variable (`"board"` | `"spec"`) and add `[Board]` / `[Specs]` mode tabs to the header. Clicking a tab or pressing `S` switches modes. This is the foundation all other spec mode tasks build on.

## What to do

1. In `ui/js/state.js`, add:
   ```javascript
   let currentMode = localStorage.getItem("wallfacer-mode") || "board";
   ```
   Export getter/setter functions:
   ```javascript
   function getCurrentMode() { return currentMode; }
   function setCurrentMode(mode) {
     currentMode = mode;
     localStorage.setItem("wallfacer-mode", mode);
   }
   ```

2. In `ui/index.html` (the `initial-layout.html` partial), add two mode tab buttons inside `app-header__primary`, between the identity div and `workspace-group-tabs`:
   ```html
   <div id="mode-tabs" class="mode-tabs">
     <button id="mode-tab-board" class="mode-tab active" onclick="switchMode('board')">Board</button>
     <button id="mode-tab-spec" class="mode-tab" onclick="switchMode('spec')">Specs</button>
   </div>
   ```

3. Create `ui/js/spec-mode.js` with the mode switching orchestrator:
   ```javascript
   function switchMode(mode) {
     if (mode === getCurrentMode()) return;
     setCurrentMode(mode);
     // Toggle active class on tabs
     document.getElementById("mode-tab-board").classList.toggle("active", mode === "board");
     document.getElementById("mode-tab-spec").classList.toggle("active", mode === "spec");
     // Toggle visibility of board vs spec containers (containers added in layout task)
     var board = document.getElementById("board");
     var specView = document.getElementById("spec-mode-container");
     if (board) board.style.display = mode === "board" ? "" : "none";
     if (specView) specView.style.display = mode === "spec" ? "" : "none";
     // Switch explorer root when containers exist
     if (typeof switchExplorerRoot === "function") {
       switchExplorerRoot(mode === "spec" ? "specs" : "workspace");
     }
   }
   ```

4. In `ui/js/events.js`, add the `S` keyboard shortcut to the existing keydown handler:
   ```javascript
   if (e.key === "s" || e.key === "S") {
     switchMode(getCurrentMode() === "board" ? "spec" : "board");
   }
   ```
   Add the same input/modal guards that `n`, `?`, and `e` already use.

5. In `ui/index.html`, include `spec-mode.js` in the script list after `state.js`.

6. On page load (DOMContentLoaded), call `switchMode(getCurrentMode())` to restore persisted mode.

7. Add CSS for `.mode-tabs` and `.mode-tab` in `ui/css/` or inline in index.html:
   - Horizontal button group, same visual style as workspace-group tabs
   - `.mode-tab.active` has an underline or filled background to indicate current mode

## Tests

- `TestModeStateDefault`: `getCurrentMode()` returns `"board"` when no localStorage value exists.
- `TestModeStatePersistence`: `setCurrentMode("spec")` writes to localStorage; `getCurrentMode()` returns `"spec"` after page reload simulation.
- `TestSwitchModeTogglesActiveClass`: Calling `switchMode("spec")` adds `active` to spec tab, removes from board tab. Vice versa.
- `TestSwitchModeIdempotent`: Calling `switchMode("board")` when already in board mode is a no-op (no errors, no flicker).
- `TestSKeyboardShortcut`: Simulating `S` keypress toggles mode. Does not trigger when an input/textarea is focused or a modal is open.

## Boundaries

- Do NOT create the spec mode three-pane layout containers — that's the `spec-mode-layout` task.
- Do NOT implement explorer root switching logic — that's the spec-explorer spec. The `switchExplorerRoot` call is a forward reference that's a no-op until wired.
- Do NOT implement the focused markdown view or chat stream.
- Do NOT change the kanban board rendering logic.
