---
title: Spec tree renderer with status badges
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/spec-explorer/spec-tree-api.md
  - specs/local/spec-coordination/spec-planning-ux/spec-mode-ui-shell/mode-state-and-switching.md
affects:
  - ui/js/
effort: medium
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Spec tree renderer with status badges

## Goal

Render the spec tree from the `GET /api/specs/tree` API response in the explorer panel when in spec mode. Each node shows a status icon, title, and recursive progress count (e.g., "4/6") for non-leaf specs. Clicking a node focuses it in the markdown view.

## What to do

1. Create `ui/js/spec-explorer.js` with the spec tree rendering logic:
   ```javascript
   let _specTreeData = null;     // cached API response
   let _specTreeTimer = null;    // refresh polling timer
   let _specExpandedPaths = new Set(JSON.parse(localStorage.getItem("wallfacer-spec-expanded") || "[]"));

   function loadSpecTree() {
     fetch(Routes.specs.tree(), { headers: withBearerHeaders() })
       .then(r => r.json())
       .then(data => {
         _specTreeData = data;
         renderSpecTree();
       });
   }
   ```

2. Implement `renderSpecTree()`:
   - Build a tree structure from the flat `nodes` array (using path components).
   - Group by workspace (top level) for multi-repo forests.
   - For each node, render:
     - **Status icon**: map `status` to icons (`complete` → `✅`, `validated` → `✔`, `drafted` → `📝`, `vague` → `💭`, `stale` → `⚠️`)
     - **Title**: from `spec.title` frontmatter
     - **Progress badge**: for non-leaf nodes, show `done/total` from the progress map (e.g., `4/6`)
     - **Collapsible subtree**: click to expand/collapse, persisted in `_specExpandedPaths`
   - Render into the `explorer-tree` container (same element the file explorer uses).

3. Implement `switchExplorerRoot(mode)` — called by `switchMode()`:
   - When mode is `"specs"`: clear `explorer-tree` innerHTML, call `loadSpecTree()`, start 3-second polling.
   - When mode is `"workspace"`: clear `explorer-tree` innerHTML, call `_loadExplorerRoots()` (existing function), start explorer refresh poll.
   - Store which mode is active to avoid double-loading.

4. Wire click events: clicking a spec node calls `focusSpec(path, workspace)` (from `spec-mode.js`). Non-spec directories (when "Show workspace files" is on) use the existing explorer file click behavior.

5. Add a "Show workspace files" checkbox toggle in the explorer header:
   ```html
   <label id="spec-explorer-workspace-toggle" class="hidden">
     <input type="checkbox" onchange="toggleSpecWorkspaceFiles(this.checked)"> Show workspace files
   </label>
   ```
   When checked, below the spec tree, render the full workspace file tree using the existing `_loadExplorerRoots()` mechanism (in a separate section). When unchecked, hide the workspace files section.

6. Include `spec-explorer.js` in `ui/index.html` script list.

## Tests

- `TestRenderSpecTree`: Given a mock API response with 3 nodes (1 non-leaf, 2 leaves), verify the explorer-tree container has 3 rendered nodes with correct titles.
- `TestStatusIcons`: Each status value maps to the correct icon character.
- `TestProgressBadge`: Non-leaf node with progress {done:4, total:6} shows "4/6" badge. Leaf nodes show no badge.
- `TestCollapseExpand`: Clicking a non-leaf node toggles its children visibility. Expanded state persists in localStorage.
- `TestClickFocusesSpec`: Clicking a leaf node calls `focusSpec()` with the correct path.
- `TestSwitchExplorerRoot`: `switchExplorerRoot("specs")` renders spec tree. `switchExplorerRoot("workspace")` renders file explorer. Both clear previous content.
- `TestShowWorkspaceFilesToggle`: Checking the toggle shows workspace files below spec tree. Unchecking hides them.

## Boundaries

- Do NOT implement status filtering (dropdown) — that's the `status-filtering` task.
- Do NOT implement multi-select for batch dispatch — that's the `multi-select` task.
- Do NOT implement the dependency minimap — that's the `dependency-minimap` task.
- Do NOT modify the existing file explorer logic in `explorer.js` beyond adding the `switchExplorerRoot` hook.
