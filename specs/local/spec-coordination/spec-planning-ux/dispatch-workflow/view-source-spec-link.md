---
title: View Source Spec Link in Task Modal
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/dispatch-workflow/task-spec-source-field.md
affects:
  - ui/js/modal-core.js
  - ui/partials/task-detail-modal.html
effort: small
created: 2026-04-04
updated: 2026-04-04
author: changkun
dispatched_task_id: null
---

# View Source Spec Link in Task Modal

## Goal

Add a "View Source Spec" link in the task detail modal when the task has `spec_source_path` metadata. Clicking the link closes the modal, switches to spec mode, and focuses the source spec.

## What to do

1. In `ui/partials/task-detail-modal.html`, add a "Source Spec" element near the task ID display (around line 14-17 in the header area):
   - Add a hidden-by-default element: `<span id="modal-spec-source" class="hidden"><a href="#" id="modal-spec-source-link" class="text-indigo-400 hover:text-indigo-300 text-xs"></a></span>`
   - Position it after the task ID, before the close button

2. In `ui/js/modal-core.js`, when populating the modal with task data:
   - Check if `task.spec_source_path` is truthy
   - If so, show `#modal-spec-source`, set the link text to the spec filename (last path segment without `.md`), and set a data attribute with the full path
   - If not, hide `#modal-spec-source`

3. Add click handler for `#modal-spec-source-link`:
   - Close the task modal
   - Switch to spec mode via `switchToSpecMode()`
   - Focus the spec via `focusSpec(specPath, workspace)`

## Tests

- Frontend test: verify modal shows spec source link when task has `spec_source_path`, hides it when empty, and click handler triggers spec mode navigation

## Boundaries

- Do NOT modify the task card rendering (separate task)
- Do NOT add any backend changes
- Do NOT modify the task modal's other sections
