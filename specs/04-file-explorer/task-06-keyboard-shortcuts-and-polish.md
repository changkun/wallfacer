# Task 6: Keyboard Shortcuts and Polish

**Status:** Done
**Depends on:** Task 4, Task 5
**Phase:** Phase 1 — Read-Only Browsing + Preview
**Effort:** Small

## Goal

Add the global keyboard shortcut for toggling the explorer panel, and ensure all keyboard interactions work correctly together.

## Implementation notes

- The shortcut was changed from `Ctrl+E` / `Cmd+E` to bare `E` key because `Ctrl+E` conflicts with browser address bar focus. This is consistent with the other bare-key shortcuts: `N` (new task) and `?` (help).
- Registered in `ui/js/events.js` alongside the existing `n` and `?` handlers. Suppressed when focus is in `<input>`, `<textarea>`, or `<select>`, and when a modal is open.
- Button title and keyboard shortcuts modal updated to show `E` instead of `Ctrl+E`.
- See commit `2ca952f`.
