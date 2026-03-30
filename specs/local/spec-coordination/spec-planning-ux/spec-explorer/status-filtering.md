---
title: Spec status filtering
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

# Spec status filtering

## Goal

Add a status filter dropdown to the spec explorer header that shows only specs matching the selected status(es). This helps users focus on actionable specs when the tree grows large.

## What to do

1. In the explorer panel header (when in spec mode), add a filter dropdown:
   ```html
   <select id="spec-status-filter" onchange="filterSpecTree(this.value)">
     <option value="all">All statuses</option>
     <option value="drafted">Drafted</option>
     <option value="validated">Validated</option>
     <option value="stale">Stale</option>
     <option value="complete">Complete</option>
     <option value="incomplete">Not complete</option>
   </select>
   ```

2. In `ui/js/spec-explorer.js`, implement `filterSpecTree(filter)`:
   - Re-render the spec tree, hiding nodes that don't match the filter.
   - For non-leaf nodes: show if any descendant leaf matches the filter (don't hide a parent when a child matches).
   - For `"incomplete"`: show everything except `complete` nodes.
   - Persist the selected filter in localStorage (`wallfacer-spec-filter`).

3. Restore the filter selection on page load from localStorage.

## Tests

- `TestFilterByDrafted`: Only specs with `status: drafted` (and their ancestors) are visible.
- `TestFilterIncomplete`: All specs except `complete` are visible.
- `TestFilterAll`: All specs are visible.
- `TestFilterPersistence`: Selected filter persists in localStorage and restores on reload.
- `TestAncestorVisibility`: A non-leaf spec is visible if any descendant matches the filter, even if the non-leaf itself doesn't match.

## Boundaries

- Do NOT add full-text search within specs — this is status filtering only.
- Do NOT modify the spec tree API response — filtering is purely client-side.
