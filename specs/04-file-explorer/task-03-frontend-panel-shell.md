# Task 3: Frontend Panel Shell

**Status:** Done
**Depends on:** Task 1 (routes must exist in generated routes.js)
**Phase:** Phase 1 — Read-Only Browsing + Preview
**Effort:** Medium

## Goal

Create the explorer side panel structure: HTML partial, CSS styles, header toggle button, and drag-to-resize behavior. This provides the container that the tree component (Task 4) will populate.

## What to do

1. Create `ui/partials/explorer-panel.html`:
   ```html
   <aside id="explorer-panel" class="explorer-panel" style="display: none;">
     <div class="explorer-panel__header">
       <span class="explorer-panel__title">Explorer</span>
     </div>
     <div id="explorer-tree" class="explorer-panel__tree">
       <!-- Tree nodes rendered by JS -->
     </div>
     <div class="explorer-panel__resize-handle" id="explorer-resize-handle"></div>
   </aside>
   ```

2. Create `ui/css/explorer.css` with styles for:
   - `.explorer-panel` — fixed-width left panel, flex column, border-right, uses `--bg`, `--border` CSS vars
   - `.explorer-panel__header` — panel title bar with padding
   - `.explorer-panel__tree` — scrollable tree container (`overflow-y: auto`, `flex: 1`)
   - `.explorer-panel__resize-handle` — vertical drag handle on right edge (4-6px wide, cursor: `col-resize`)
   - `.explorer-panel__resize-handle:hover` — visual highlight on hover
   - Tree node styles (prepared for Task 4): `.explorer-node`, `.explorer-node--dir`, `.explorer-node--file`, `.explorer-node--hidden` (dimmed opacity for dot-prefixed)
   - Indentation via `padding-left` scaled by depth level

3. Add `@import "explorer.css";` to `ui/css/styles.css`.

4. Update `ui/partials/initial-layout.html`:
   - Add explorer toggle button in the header actions area (before existing buttons)
   - Button uses a file-tree SVG icon, `onclick="toggleExplorer()"`, `title="Toggle Explorer (Ctrl+E)"`

5. Update `ui/partials/board.html`:
   - Wrap the existing `board-grid` and the explorer panel in a flex container so they sit side by side
   - Explorer panel on left, board-grid takes remaining space with `flex: 1`

6. Include the explorer-panel partial in `initial-layout.html` or board layout area.

7. Add `<script src="/js/explorer.js"></script>` to `ui/partials/scripts.html` (after status-bar.js).

8. Create `ui/js/explorer.js` with initial panel management functions:
   - `toggleExplorer()` — show/hide panel, persist state to localStorage key `"wallfacer-explorer-open"`
   - `_initExplorerResize()` — drag handle logic following `_initPanelResize()` pattern from status-bar.js:
     - Min width: 200px, max width: 50% of viewport
     - Persist width to localStorage key `"wallfacer-explorer-width"`
     - Restore width on page load
     - Double-click handle resets to default 260px
   - `_initExplorer()` — called on page load, restores panel state and width from localStorage
   - Board grid adjusts in real time during resize (no snap-on-release)

## Tests

No frontend tests needed for this task — the resize/toggle behavior is DOM-dependent. Tests for the tree logic come in Task 7.

## Boundaries

- Do NOT implement tree rendering (Task 4)
- Do NOT implement file preview modal (Task 5)
- Do NOT implement keyboard shortcuts beyond the toggle button click handler (Task 6)
- The explorer-panel should show an empty tree container — Task 4 fills it

## Implementation notes

- The spec called for wrapping board-grid inside `board.html`. Instead, the flex wrapper `<div class="board-with-explorer">` is placed in `ui/index.html` around both the `explorer-panel.html` and `board.html` template includes. This avoids modifying `board.html` and keeps the wrapper at the composition layer.
- The `Ctrl+E` shortcut is listed in the keyboard shortcuts modal for discoverability, but the actual keydown handler is deferred to Task 6 per the boundary rules.
