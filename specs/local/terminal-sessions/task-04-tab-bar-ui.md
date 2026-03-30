---
title: Tab Bar UI
status: complete
depends_on: []
affects: []
effort: medium
created: 2026-03-28
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 4: Tab Bar UI

## Goal

Add a tab bar above the xterm.js canvas in the terminal panel. This task builds the visual component and local tab state management, but does not wire it to WebSocket session messages (Task 5 does that).

## What to do

1. In `ui/partials/status-bar.html`, add a tab bar container inside `#status-bar-panel`, above where xterm.js mounts:
   ```html
   <div id="terminal-tab-bar" class="..." hidden>
     <!-- tabs inserted here by JS -->
     <button id="terminal-tab-add" title="New session" aria-label="New terminal session">+</button>
   </div>
   <div id="terminal-canvas"></div>
   ```
   Move xterm.js mounting target from `#status-bar-panel` to `#terminal-canvas`.

2. In `ui/js/terminal.js`, update `initTerminal()` to mount xterm into `#terminal-canvas` instead of `#status-bar-panel`.

3. Add tab management functions in `ui/js/terminal.js`:
   - `_addTab(sessionId, label)` — creates a tab element in `#terminal-tab-bar`, sets click handler.
   - `_removeTab(sessionId)` — removes a tab element.
   - `_activateTab(sessionId)` — visually highlights the active tab (add/remove CSS class).
   - `_renameTab(sessionId, label)` — updates the tab label text.

4. Each tab element should have:
   - A label (default: "Shell N" where N is a counter).
   - A close button (×) that fires a callback (wired in Task 5).
   - An `aria-selected` attribute for accessibility.
   - A `data-session-id` attribute for lookup.

5. Style the tab bar with Tailwind classes matching the existing status bar aesthetic:
   - Small, compact tabs (similar to VS Code terminal tabs).
   - Active tab distinguished by background color using existing CSS variables.
   - Tab bar hidden when only one session exists (show only when ≥2 sessions); the "+" button is always visible in the tab bar area.

6. Show `#terminal-tab-bar` when the terminal panel is visible.

7. Update `ui/css/style.css` if needed for tab-specific styles that Tailwind utilities cannot express.

## Tests

- `TestTerminalTabBar_AddTab` — call `_addTab()`, verify DOM element created with correct attributes.
- `TestTerminalTabBar_RemoveTab` — add then remove a tab, verify DOM element gone.
- `TestTerminalTabBar_ActivateTab` — add two tabs, activate second, verify `aria-selected` and CSS class.
- `TestTerminalTabBar_HiddenWithOneTab` — verify tab bar hidden with one tab, shown with two.
- Run `make test-frontend` to verify existing terminal tests still pass.

## Boundaries

- Do NOT send WebSocket messages from tab clicks (Task 5 wires that).
- Do NOT manage multiple xterm instances — there's still one xterm instance that gets reattached (Task 5 handles session output buffering).
- Do NOT change the backend.
- Keep the tab bar purely visual and locally stateful in this task.
