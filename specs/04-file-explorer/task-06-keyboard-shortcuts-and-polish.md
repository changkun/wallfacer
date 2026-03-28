# Task 6: Keyboard Shortcuts and Polish

**Status:** Todo
**Depends on:** Task 4, Task 5
**Phase:** Phase 1 — Read-Only Browsing + Preview
**Effort:** Small

## Goal

Add the global `Ctrl+E` / `Cmd+E` keyboard shortcut for toggling the explorer panel, and ensure all keyboard interactions work correctly together.

## What to do

1. Register the `Ctrl+E` / `Cmd+E` shortcut:
   - Add to the existing keyboard shortcut handling in `ui/js/keyboard-shortcuts.js` or `ui/js/explorer.js`
   - Check for `(e.ctrlKey || e.metaKey) && e.key === 'e'`
   - Call `toggleExplorer()`
   - `e.preventDefault()` to suppress browser's default behavior (e.g., search bar in some browsers)
   - Do NOT trigger when focus is inside a `<textarea>` or `<input>` (check `e.target.tagName`)

2. Verify keyboard interaction matrix:
   - When explorer is focused: arrow keys navigate tree, Enter opens/expands, Escape does nothing special
   - When file preview modal is open: Escape closes modal, other keys don't affect tree
   - When neither is focused: Ctrl+E toggles panel
   - When a task modal is open: Ctrl+E should NOT toggle explorer (modal has priority)

3. Add the shortcut to the command palette if one exists (check `ui/js/command-palette.js` for a registration pattern).

4. Update the toggle button's `title` attribute to include the shortcut hint: `"Toggle Explorer (Ctrl+E)"`.

## Tests

No additional tests needed — keyboard shortcuts are DOM-event-dependent and covered by manual testing.

## Boundaries

- Do NOT add new keyboard shortcuts beyond Ctrl+E
- Do NOT modify existing shortcut handlers for other features
- Do NOT change tree keyboard navigation (already implemented in Task 4)
