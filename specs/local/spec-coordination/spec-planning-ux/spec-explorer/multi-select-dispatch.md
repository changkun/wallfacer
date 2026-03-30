---
title: Multi-select for batch dispatch
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/spec-explorer/spec-tree-renderer.md
affects:
  - ui/js/spec-explorer.js
effort: small
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Multi-select for batch dispatch

## Goal

Add checkbox multi-select to leaf specs in the spec explorer for batch dispatch. When multiple validated leaf specs are selected, a "Dispatch Selected" button appears that dispatches all selected specs as kanban tasks in one batch.

## What to do

1. In `ui/js/spec-explorer.js`, add a selection state:
   ```javascript
   let _selectedSpecPaths = new Set();
   ```

2. When rendering leaf spec nodes, add a checkbox:
   ```javascript
   if (node.is_leaf && node.spec.status === "validated") {
     const checkbox = document.createElement("input");
     checkbox.type = "checkbox";
     checkbox.className = "spec-select-checkbox";
     checkbox.checked = _selectedSpecPaths.has(node.path);
     checkbox.addEventListener("change", function() {
       if (this.checked) _selectedSpecPaths.add(node.path);
       else _selectedSpecPaths.delete(node.path);
       updateDispatchSelectedButton();
     });
     nodeEl.prepend(checkbox);
   }
   ```

3. Add a "Dispatch Selected" button in the spec explorer header:
   ```html
   <button id="spec-dispatch-selected-btn" class="spec-dispatch-selected-btn hidden"
           onclick="dispatchSelectedSpecs()">Dispatch Selected (0)</button>
   ```

4. Implement `updateDispatchSelectedButton()`:
   - Show the button when `_selectedSpecPaths.size > 0`.
   - Update the button text with the count.
   - Hide when 0 selected.

5. Implement `dispatchSelectedSpecs()` as a stub that logs the selected paths. The actual dispatch logic (calling `POST /api/tasks/batch`) will be wired by the dispatch-workflow spec.

6. Support shift-click for range selection:
   - Track the last-clicked checkbox index.
   - On shift-click, select/deselect all checkboxes between last click and current click.

7. Clear selection when the spec tree refreshes (re-render preserves checked state via `_selectedSpecPaths`).

## Tests

- `TestCheckboxOnLeafOnly`: Checkboxes appear only on leaf specs with `validated` status. Non-leaf and non-validated specs have no checkbox.
- `TestSelectionCountUpdates`: Checking 3 boxes shows "Dispatch Selected (3)". Unchecking one shows "(2)".
- `TestDispatchButtonHiddenWhenNone`: Button is hidden when no specs are selected.
- `TestShiftClickRangeSelect`: Clicking checkbox A, then shift-clicking checkbox C selects A, B, and C.
- `TestSelectionSurvivesRefresh`: After spec tree re-render (polling), previously selected paths remain checked.

## Boundaries

- Do NOT implement the actual dispatch API call — `dispatchSelectedSpecs()` is a stub.
- Do NOT add "select all" or "deselect all" buttons — keep it simple.
- Do NOT show checkboxes for non-validated or non-leaf specs.
