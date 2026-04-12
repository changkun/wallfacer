---
title: Render pinned Roadmap entry in the spec explorer
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/chat-first-mode/spec-tree-index-endpoint.md
affects:
  - ui/js/spec-explorer.js
  - ui/css/spec-mode.css
  - ui/js/spec-mode.js
effort: small
created: 2026-04-12
updated: 2026-04-13
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

## Implementation notes

- The CSS selectors in the spec sample (`.spec-focused-status-chip`, `.spec-depends-on`) do not exist in the actual markup. The real elements are `#spec-focused-status`, `#spec-focused-kind`, `#spec-focused-effort`, `#spec-focused-meta`, and the button IDs (`#spec-dispatch-btn` / `#spec-summarize-btn` / `#spec-archive-btn` / `#spec-unarchive-btn` / `#spec-archived-banner`). The implementation hides those actual selectors; there is no separate "depends_on indicator" element in the focused view today.
- The spec described `j`/`k`/arrow keyboard navigation as a test case (`TestExplorer_PinnedRoadmap_KeyboardNav`), but the explorer does not currently implement tree-level keyboard navigation. The pinned entry supports Enter/Space to focus itself (via `tabindex="0"` + `_onSpecIndexKeydown`), which matches the spec's "Enter on the pinned entry focuses it" requirement, but the broader j/k listing navigation is deferred to a future spec if it is added at all.
- The spec mentioned a "Chat header label reads Roadmap" requirement. The planning chat pane always says "Planning Chat"; there is no per-spec title in that header. The focused-view title bar (`#spec-focused-title`) is set to the literal `Roadmap`, which is what the user-visible label in the focus area looks like, so the intent ("the focused-view title reads Roadmap") is honoured.
- `focusRoadmapIndex` reuses `_focusedSpecPath` / `_focusedSpecWorkspace` rather than introducing separate state, with a `_focusedIsIndex` flag to disambiguate. This keeps downstream consumers (hash deeplink, mode switches, explorer highlight) working without special-casing.
