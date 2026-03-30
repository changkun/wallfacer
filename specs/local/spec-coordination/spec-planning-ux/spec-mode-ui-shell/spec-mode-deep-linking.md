---
title: Spec mode deep-linking and keyboard shortcuts
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/spec-mode-ui-shell/focused-markdown-view.md
affects:
  - ui/js/spec-mode.js
  - ui/js/api.js
  - ui/js/events.js
effort: small
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Spec mode deep-linking and keyboard shortcuts

## Goal

Add hash-based deep-linking for spec mode (`#spec/<path>`) so spec URLs are shareable and bookmarkable. Add spec-mode-specific keyboard shortcuts (`Enter` to open, `D` to dispatch, `B` to break down).

## What to do

1. In `ui/js/api.js`, extend the `_handleInitialHash()` function to handle spec deep-links:
   ```javascript
   // After the existing task UUID regex check:
   const specMatch = location.hash.match(/^#spec\/(.+)$/);
   if (specMatch) {
     const specPath = decodeURIComponent(specMatch[1]);
     switchMode("spec");
     // Focus the spec after the spec tree is loaded
     focusSpec(specPath, activeWorkspaces[0]);
     return;
   }
   ```

2. In `ui/js/spec-mode.js`, update `focusSpec()` to set the hash:
   ```javascript
   history.replaceState(null, "", "#spec/" + encodeURIComponent(specPath));
   ```
   When switching to board mode, clear the spec hash:
   ```javascript
   if (location.hash.startsWith("#spec/")) {
     history.replaceState(null, "", location.pathname);
   }
   ```

3. In `ui/js/events.js`, add spec-mode-specific shortcuts to the keydown handler:
   ```javascript
   // Only active when in spec mode
   if (getCurrentMode() === "spec") {
     if (e.key === "Enter") {
       // Open selected spec in focused view (if explorer has a selected node)
       e.preventDefault();
       openSelectedSpec();
     }
     if (e.key === "d" || e.key === "D") {
       // Dispatch current leaf spec
       e.preventDefault();
       dispatchFocusedSpec();
     }
     if (e.key === "b" || e.key === "B") {
       // Break down — focus chat with pre-filled message
       e.preventDefault();
       breakDownFocusedSpec();
     }
   }
   ```
   Use the same input/modal guards as existing shortcuts.

4. Implement stub functions in `spec-mode.js`:
   - `openSelectedSpec()` — reads the explorer's selected node and calls `focusSpec()`. No-op until spec explorer is wired.
   - `dispatchFocusedSpec()` — no-op stub, wired by dispatch-workflow spec.
   - `breakDownFocusedSpec()` — pre-fills chat input with "Break this into sub-specs" and focuses it. Chat sending is wired by planning-chat-agent spec.

## Tests

- `TestSpecDeepLinkParsing`: Hash `#spec/specs/local/foo.md` correctly switches to spec mode and focuses the spec path.
- `TestSpecDeepLinkUpdatesOnFocus`: Focusing a spec via click updates `location.hash` to `#spec/<path>`.
- `TestSpecDeepLinkClearedOnBoardMode`: Switching to board mode removes the `#spec/` hash.
- `TestSKeyboardShortcutNotInInput`: `S` key does not toggle mode when typing in the chat input textarea.
- `TestDKeyDispatchGuard`: `D` key only fires in spec mode, not in board mode.

## Boundaries

- Do NOT implement actual dispatch logic — `dispatchFocusedSpec()` is a stub.
- Do NOT implement actual chat message sending — `breakDownFocusedSpec()` only pre-fills the input.
- Do NOT implement `hashchange` event listening — use the initial hash check pattern only (consistent with existing approach).
