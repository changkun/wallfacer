---
title: "Archival: Explorer tree — Show archived toggle and muted rendering"
status: validated
depends_on:
  - specs/local/spec-coordination/spec-archival/core-model.md
affects:
  - ui/js/spec-explorer.js
  - ui/css/spec-mode.css
effort: medium
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Archival: Explorer tree — Show archived toggle and muted rendering

## Goal

Update `ui/js/spec-explorer.js` and `ui/css/spec-mode.css` so that archived specs
are hidden from the tree by default, revealed by a "Show archived" checkbox (persisted
to `localStorage`), rendered in a muted style when visible, auto-collapsed on reveal,
and excluded from the status filter dropdown unless "Show archived" is on. Update the
dependency minimap to render archived nodes with dashed outlines.

## What to do

### `ui/js/spec-explorer.js`

1. **Status icon** — add `archived` to `_specStatusIcons` (around line 20):
   ```js
   var _specStatusIcons = {
     complete: "✅", validated: "✔", drafted: "📝",
     vague: "💭", stale: "⚠️", archived: "📦",
   };
   ```

2. **"Show archived" toggle state** — add a localStorage-backed variable alongside
   the existing filter variables (around line 14):
   ```js
   var _showArchived = localStorage.getItem("wallfacer-spec-show-archived") === "true";
   ```

3. **Filter node logic** — in `_nodeMatchesFilter()`, add an early-return guard so
   archived specs are excluded unless `_showArchived` is set:
   ```js
   function _nodeMatchesFilter(node) {
     if (node.status === "archived" && !_showArchived) return false;
     // ... existing filter logic
   }
   ```

4. **Status filter dropdown** — add `archived` as a filter option in the dropdown
   HTML/render. Disable the `archived` filter option when `_showArchived` is false:
   ```js
   // When building the filter dropdown:
   var archivedOpt = document.createElement("option");
   archivedOpt.value = "archived";
   archivedOpt.textContent = "archived";
   archivedOpt.disabled = !_showArchived;
   archivedOpt.title = _showArchived ? "" : "Enable 'Show archived' to filter by this status";
   ```

5. **"Show archived" checkbox** — in the explorer header (alongside the existing
   "Show workspace files" checkbox), add:
   ```html
   <label>
     <input type="checkbox" id="spec-show-archived"> Show archived
   </label>
   ```
   Wire it in JS:
   ```js
   document.getElementById("spec-show-archived").addEventListener("change", function(e) {
     _showArchived = e.target.checked;
     localStorage.setItem("wallfacer-spec-show-archived", _showArchived);
     _renderSpecTree();  // re-render; archived filter option toggled
   });
   ```
   Initialize the checkbox state from `_showArchived` on load.

6. **Muted rendering** — in `_renderSpecNode()` (around line 364), add
   `spec-node--archived` class when spec status is archived:
   ```js
   var cls = "spec-node";
   if (node.isLeaf) cls += " spec-node--leaf";
   if (node.status === "archived") cls += " spec-node--archived";
   ```

7. **Auto-collapse on reveal** — when `_showArchived` is toggled on, force-collapse
   any archived parent nodes that become visible. In the toggle handler (after setting
   `_showArchived = true`), iterate `_specTree` and collapse nodes whose status is
   `"archived"`:
   ```js
   if (_showArchived) {
     _forceCollapseArchived(_specTree);
   }
   ```
   Implement `_forceCollapseArchived(tree)` to remove archived node paths from
   `_expandedPaths` (the localStorage-backed expanded set).

8. **Dependency minimap** — in the minimap render function, when building node elements:
   add class `spec-minimap__node--archived` if `node.status === "archived"`. Respect
   `_showArchived` to determine whether to include archived nodes at all. For edges
   to/from archived nodes, add class `spec-minimap__edge--archived`.

9. **Multi-select dispatch list** — in the checkbox render logic (around line 394-410),
   archived specs must not get a checkbox regardless of status. The existing
   `status === "validated"` guard already excludes them; no change needed there.

10. **`"incomplete"` filter** — the existing `"incomplete"` option uses
    `status !== "complete"`. Archived specs should NOT be classified as `"incomplete"`:
    update the matching logic:
    ```js
    case "incomplete":
      return node.status !== "complete" && node.status !== "archived";
    ```

### `ui/css/spec-mode.css`

11. **Status badge** — add `.spec-status--archived` after the stale rule (around line 121):
    ```css
    .spec-status--archived {
      background-color: #e2e3e5;
      color: #6c757d;
    }
    ```

12. **Muted tree node** — add a rule for archived nodes in the tree:
    ```css
    .spec-node--archived {
      opacity: 0.6;
      color: #6c757d;
    }
    ```

13. **Minimap dashed nodes and edges** — add:
    ```css
    .spec-minimap__node--archived {
      outline: 2px dashed #adb5bd;
      opacity: 0.6;
    }
    .spec-minimap__edge--archived {
      stroke-dasharray: 4 3;
      opacity: 0.5;
    }
    ```

## Tests

Manual verification steps (no automated JS tests currently exist for this module):
- With `_showArchived = false` (default): a spec with `status: archived` is absent
  from the tree render; the archived filter option in the dropdown is disabled
- With `_showArchived = true`: archived specs appear with `spec-node--archived` class
  (muted), auto-collapsed when first revealed
- The `"incomplete"` filter excludes archived specs
- `localStorage["wallfacer-spec-show-archived"]` persists across page reloads

## Boundaries

- Do NOT modify the backend or Go handler code
- Do NOT change `ui/js/spec-mode.js` (that is `focused-view-ux.md`)
- Do NOT change the explorer's progress computation — archived exclusion is handled
  server-side in `progress.go` (task `impact-progress.md`)
- The "Show archived" toggle does NOT affect the focused spec view rendering
