---
title: "Frontend File Preview Modal"
status: complete
track: foundations
depends_on:
  - specs/foundations/file-explorer/task-02-backend-file-content-reading.md
  - specs/foundations/file-explorer/task-04-frontend-tree-component.md
affects:
  - ui/js/explorer.js
effort: medium
created: 2026-03-22
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 5: Frontend File Preview Modal

## Goal

Implement the file preview modal that displays syntax-highlighted file contents when a file is clicked in the explorer tree. Handles text files, binary files, and oversized files gracefully.

## What to do

1. Add to `ui/js/explorer.js` (or create a new `ui/js/modal-explorer.js` if it grows large):

   a. `_openFilePreview(node)`:
      - Construct fetch URL: `Routes.explorer.file({path: node.path, workspace: node.workspace})`
      - Show a modal overlay using the existing modal pattern (reuse modal backdrop + container structure)
      - Display loading indicator while fetching

   b. Handle response cases:
      - **413 (too large)**: Parse error JSON, show message: "File too large to preview (X MB, max 2 MB)."
      - **Binary** (`X-File-Binary: true` header or JSON with `binary: true`): Show "Binary file (X KB)" placeholder
      - **Text**: Render with syntax highlighting

   c. Text file rendering:
      - Get language from `extToLang(node.name)` (imported/reused from `modal-diff.js`)
      - If language found: use `hljs.highlight(content, {language}).value`
      - If no language: use `hljs.highlightAuto(content).value` or display as plain text
      - Split into lines via `splitHighlightedLines()` from `modal-diff.js`
      - Render as numbered lines in a `<pre><code>` block with line numbers in a gutter column
      - Apply existing diff modal CSS patterns for line numbering

   d. Modal structure:
      ```html
      <div class="explorer-modal-backdrop" onclick="closeExplorerPreview()">
        <div class="explorer-modal" onclick="event.stopPropagation()">
          <div class="explorer-modal__header">
            <span class="explorer-modal__path">src/internal/handler/config.go</span>
            <button class="explorer-modal__close" onclick="closeExplorerPreview()">&times;</button>
          </div>
          <div class="explorer-modal__content">
            <!-- Highlighted code with line numbers -->
          </div>
        </div>
      </div>
      ```

   e. `closeExplorerPreview()`:
      - Remove/hide the modal
      - Return focus to the tree node that opened it

   f. Close on Escape key (integrate with existing keyboard handling or add listener on modal open).

2. Add explorer modal styles to `ui/css/explorer.css`:
   - Backdrop: fixed position, semi-transparent background (match existing modal backdrop)
   - Modal: centered, max-width ~80vw, max-height ~80vh, scrollable content area
   - Header: file path display, close button
   - Content: monospace font, line numbers gutter, horizontal scroll for long lines
   - Match existing modal z-index layering

3. Ensure `extToLang()` and `splitHighlightedLines()` are accessible from explorer.js. They are currently in `modal-diff.js` as top-level functions (global scope), so they should be directly callable.

## Tests

Add to `ui/js/tests/explorer.test.js`:

- `TestFilePreviewBinaryDetection` — verify that binary response triggers placeholder rendering logic
- `TestFilePreviewLargeFileMessage` — verify 413 response triggers appropriate message

(Most modal rendering is DOM-dependent; test the decision logic, not the DOM manipulation.)

## Boundaries

- Do NOT implement edit mode (Task 8)
- Do NOT add "Edit" button to the modal yet
- Do NOT modify `modal-diff.js` — reuse its global functions as-is
- Keep the modal independent from the task detail modal (`openModal`/`closeModal`) — this is a separate modal for file preview

## Implementation notes

- Used raw `fetch()` instead of `api()` because the readFile endpoint returns `text/plain` for text files, not JSON, and `api()` always calls `.json()`.
- Extracted `_classifyFileResponse()` as a pure testable helper for response classification logic.
- Added `_relativePath()` helper to strip workspace prefix for cleaner path display in the modal header.
- The modal DOM is created dynamically on first open and reused (innerHTML replaced) for subsequent opens, rather than a static HTML partial — matching the depgraph panel pattern.
- Escape key integration uses the existing `closeFirstVisibleModal` pattern in events.js.
