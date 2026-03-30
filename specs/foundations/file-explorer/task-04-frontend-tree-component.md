---
title: "Frontend Tree Component"
status: complete
depends_on:
  - specs/foundations/file-explorer/task-01-backend-path-validation-and-tree-listing.md
  - specs/foundations/file-explorer/task-03-frontend-panel-shell.md
affects:
  - ui/js/explorer.js
effort: large
created: 2026-03-22
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 4: Frontend Tree Component

## Goal

Implement the lazy-loading directory tree that fetches and renders workspace contents one level at a time. This is the core navigation component of the file explorer.

## What to do

1. Add to `ui/js/explorer.js`:

   a. State management:
      ```javascript
      let _explorerRoots = [];    // one root per active workspace
      let _explorerOpen = false;  // panel visibility (from Task 3)
      let _explorerWidth = 260;   // persisted width (from Task 3)
      ```
      Each node: `{ path, name, type, expanded, children, loading }`

   b. `_loadExplorerRoots()`:
      - Get active workspaces from the config (use `Routes.config.get()` or the existing config state)
      - For each workspace, create a root node with `name` = directory basename, `path` = workspace path, `type` = "dir"
      - Call `_renderTree()` to display roots

   c. `_expandNode(node)`:
      - Set `node.loading = true`, re-render the node's loading indicator
      - Fetch `Routes.explorer.tree({path: node.path, workspace: <root workspace>})`
      - Parse response entries into child nodes
      - Set `node.children = childNodes`, `node.expanded = true`, `node.loading = false`
      - Re-render subtree

   d. `_collapseNode(node)`:
      - Set `node.expanded = false`, `node.children = null` (discard to keep memory lean)
      - Re-render subtree

   e. `_renderTree()`:
      - Clear `#explorer-tree` container
      - For each root, recursively render visible nodes
      - Each node is a `<div>` with:
        - Disclosure triangle (▶/▼) for directories
        - File/folder icon
        - Name text
        - `data-path` attribute for identification
        - Depth-based indentation via `padding-left: ${depth * 16}px`
        - Click handler: directories toggle expand/collapse, files call `_openFilePreview(node)` (Task 5)
      - Directories first, then files (already sorted by backend)
      - Hidden entries (`.`-prefixed) get `.explorer-node--hidden` class for dimmed styling

   f. `_renderNode(node, depth, container)`:
      - Create DOM element for single node
      - If `node.loading`, show a small spinner or "..." indicator
      - If `node.expanded && node.children`, recursively render children at `depth + 1`

   g. Keyboard navigation within the tree:
      - Arrow Up/Down: move focus between visible nodes
      - Arrow Right: expand collapsed directory
      - Arrow Left: collapse expanded directory, or move to parent
      - Enter: expand/collapse directory, or open file preview
      - Track focused node index; use `tabindex` and `aria-` attributes for accessibility
      - Focus management: clicking a node focuses it; keyboard nav updates focus

   h. Update `_initExplorer()` (from Task 3) to call `_loadExplorerRoots()` when panel opens for the first time.

2. Pass the workspace root path through the node hierarchy so each node knows which workspace it belongs to (needed for the `workspace` query param in API calls).

## Tests

Testable logic (pure functions) to add to `ui/js/tests/explorer.test.js`:

- `TestSortNodes` — if any client-side sorting is added, verify dirs-first + case-insensitive alphabetical
- `TestNodeStateManagement` — expand sets children + expanded flag, collapse clears children

Note: Most tree logic involves DOM manipulation and fetch calls, which are tested via the backend integration. The Vitest VM pattern can test pure state management functions if extracted.

## Boundaries

- Do NOT implement the file preview modal (Task 5) — `_openFilePreview()` can be a stub or empty function
- Do NOT implement the Ctrl+E keyboard shortcut (Task 6)
- Do NOT implement file editing
- Keep tree state in memory only — no persistence of expanded nodes across page reloads

## Implementation notes

- The spec suggested separate `let` declarations for `_explorerOpen` and `_explorerWidth` state vars, but these were already managed by Task 3 via localStorage and panel DOM state, so no duplicate vars were needed.
- `_openFilePreview()` is a stub as specified — will be implemented in Task 5.
- Tree state uses a simple object graph (`_explorerRoots` array of node objects) rather than a Map/WeakMap, matching the vanilla JS style of the rest of the codebase.
- `reloadExplorerTree()` is exposed globally so workspace.js can call it when the active workspace changes.
- Pure helper functions (`_buildChildNodes`, `_getVisibleNodes`, `_findParent`, `_basename`) are exposed on `window` for testability.
