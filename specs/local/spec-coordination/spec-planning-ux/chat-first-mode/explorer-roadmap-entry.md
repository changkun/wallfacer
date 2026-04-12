---
title: Render pinned Roadmap entry in the spec explorer
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/chat-first-mode/spec-tree-index-endpoint.md
affects:
  - ui/js/spec-explorer.js
  - ui/css/spec-mode.css
  - ui/js/spec-mode.js
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Render pinned Roadmap entry in the spec explorer

## Goal

Show `specs/README.md` as a pinned `📋 Roadmap` entry at the top of the spec explorer when `index` is non-null. Clicking it focuses the index and renders its markdown in the existing focused-view pipeline; spec-only affordances (status chip, dispatch, archive) are hidden when the index is focused.

## What to do

1. In `ui/js/spec-explorer.js`, before rendering the tree, render a pinned list item when `index !== null`:
   - Class: `spec-explorer-pinned spec-explorer-item--roadmap`
   - Label: literal `📋 Roadmap` (not the backend-provided title — keeps the visual anchor stable across renames).
   - `data-entry="index"` attribute so click/keyboard handlers can distinguish it from regular spec nodes.
2. Click handler: dispatch a focus-index event / call the focused-view loader with the index metadata. Reuse the existing focus machinery in `ui/js/spec-mode.js`.
3. Keyboard navigation (`j`/`k` or arrow keys) includes the pinned entry as the first item. Enter on the pinned entry focuses it. Dispatch/archive/rename keyboard shortcuts are no-ops when the pinned entry is focused.
4. In `ui/js/spec-mode.js`'s focused-view rendering path, when the focused entry is the index:
   - Fetch the markdown via existing `/api/explorer/file` endpoint (`path=specs/README.md`, workspace from the `index.workspace` field).
   - Render with the existing markdown pipeline.
   - Hide the status chip, dispatch button, archive button, and `depends_on` indicator elements (add/remove a class `spec-focused-view--index` that CSS uses to toggle visibility).
   - Chat header label reads `"Roadmap"` instead of a spec title.
5. In `ui/css/spec-mode.css`, add:
   ```css
   .spec-explorer-pinned {
     /* Same visual shell as tree nodes, plus a subtle top-of-list accent (border-bottom / background) */
   }
   .spec-focused-view--index .spec-focused-status-chip,
   .spec-focused-view--index .spec-dispatch-btn,
   .spec-focused-view--index .spec-archive-btn,
   .spec-focused-view--index .spec-depends-on {
     display: none;
   }
   ```

## Tests

- `ui/js/tests/spec-explorer.test.js` (extend):
  - `TestExplorer_PinnedRoadmap_RendersWhenIndexPresent`: given a mocked tree payload with `index: {...}`, the first child of the explorer DOM has class `spec-explorer-pinned` and text `📋 Roadmap`.
  - `TestExplorer_PinnedRoadmap_HiddenWhenIndexNull`: `index: null` → no pinned node.
  - `TestExplorer_PinnedRoadmap_KeyboardNav`: first `j` press from the header focuses the pinned entry.
  - `TestExplorer_PinnedRoadmap_ClickFocusesIndex`: click fires the focus handler with `data-entry="index"`.
- `ui/js/tests/spec-mode.test.js` (or create a focused-view subset):
  - `TestFocusedView_IndexHidesSpecAffordances`: focusing the index applies `spec-focused-view--index` class; status chip / dispatch / archive are not visible.
  - `TestFocusedView_IndexRendersMarkdown`: the fetched README body is rendered via the markdown pipeline.

## Boundaries

- **Do NOT** modify the pinned entry's displayed label based on the file's H1 — always render `📋 Roadmap`.
- **Do NOT** add a "create Roadmap" affordance for repos that don't have one. That's implicit in the first-scaffold task (`readme-autocreate.md`).
- **Do NOT** change how non-index nodes render. The pinned entry is an addition, not a refactor.
- **Do NOT** make the pinned entry draggable, right-clickable for a context menu, or anything beyond "click / keyboard focus." Keep the surface minimal.
