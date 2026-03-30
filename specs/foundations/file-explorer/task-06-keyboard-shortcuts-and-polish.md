---
title: "Keyboard Shortcuts and Polish"
status: complete
depends_on:
  - specs/foundations/file-explorer/task-04-frontend-tree-component.md
  - specs/foundations/file-explorer/task-05-frontend-file-preview-modal.md
affects:
  - ui/js/events.js
effort: small
created: 2026-03-22
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 6: Keyboard Shortcuts and Polish

## Goal

Add the global keyboard shortcut for toggling the explorer panel, and ensure all keyboard interactions work correctly together.

## Implementation notes

- The shortcut was changed from `Ctrl+E` / `Cmd+E` to bare `E` key because `Ctrl+E` conflicts with browser address bar focus. This is consistent with the other bare-key shortcuts: `N` (new task) and `?` (help).
- Registered in `ui/js/events.js` alongside the existing `n` and `?` handlers. Suppressed when focus is in `<input>`, `<textarea>`, or `<select>`, and when a modal is open.
- Button title and keyboard shortcuts modal updated to show `E` instead of `Ctrl+E`.
- See commit `2ca952f`.
