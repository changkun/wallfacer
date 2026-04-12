---
title: "Archival: Focused view read-only banner and archive/unarchive actions"
status: validated
depends_on:
  - specs/local/spec-coordination/spec-archival/archive-api.md
  - specs/local/spec-coordination/spec-archival/explorer-ux.md
affects:
  - ui/js/spec-mode.js
  - ui/css/spec-mode.css
  - internal/planner/spec.go
effort: medium
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Archival: Focused view read-only banner and archive/unarchive actions

## Goal

Update `ui/js/spec-mode.js` so that opening an archived spec shows a read-only
banner, hides all mutation actions (dispatch, breakdown, refine, edit), and adds
Archive / Unarchive toolbar buttons that call the new HTTP endpoints. Update the
planning chat agent's system prompt to refuse writes on archived specs.

## What to do

### `ui/js/spec-mode.js`

1. **Detect archived status** — in the spec-load handler (where `parseSpecFrontmatter()`
   is called, around line 400), extract `parsed.frontmatter.status` and compute:
   ```js
   var isArchived = parsed.frontmatter.status === "archived";
   ```

2. **Read-only banner** — insert a banner element at the top of the focused view
   content area when `isArchived`:
   ```js
   var bannerEl = document.getElementById("spec-archived-banner");
   if (bannerEl) {
     bannerEl.classList.toggle("hidden", !isArchived);
   }
   ```
   The banner HTML (add to the relevant partial template):
   ```html
   <div id="spec-archived-banner" class="spec-archived-banner hidden">
     ⊘ Archived — read-only. Hidden from the live graph and drift checks.
     <button id="spec-unarchive-btn" class="spec-btn spec-btn--secondary">Unarchive</button>
   </div>
   ```

3. **Hide mutation actions** — when `isArchived`, hide the dispatch button, breakdown
   button, and refine action. These use `classList.toggle("hidden", condition)` already;
   extend the conditions:
   ```js
   // Dispatch button (around line 321):
   dispatchBtn.classList.toggle("hidden", !(isValidated && specIsLeaf) || isArchived);
   // Breakdown button (around line 342):
   breakdownBtn.classList.toggle("hidden", !isDraftedOrValidated || isArchived);
   // Refine action (wherever it is wired):
   refineBtn.classList.toggle("hidden", isArchived);
   ```
   Markdown edit affordance (if any inline edit exists): disable or hide similarly.

4. **Archive / Unarchive toolbar actions** — add two buttons to the focused view
   toolbar. Show/hide based on lifecycle eligibility:
   - **Archive button**: visible when `status` is `drafted`, `complete`, or `stale`;
     hidden when `archived`, `vague`, `validated`
   - **Unarchive button**: visible only when `status === "archived"`
     (also rendered in the banner per step 2)

   Wire both buttons to call the new endpoints:
   ```js
   function _archiveSpec(relPath) {
     fetch("/api/specs/archive", {
       method: "POST",
       headers: {"Content-Type": "application/json"},
       body: JSON.stringify({path: relPath}),
     }).then(function(res) {
       if (res.ok) { _reloadSpec(relPath); }
       else { res.text().then(function(t) { _showError(t); }); }
     });
   }
   function _unarchiveSpec(relPath) {
     fetch("/api/specs/unarchive", {
       method: "POST",
       headers: {"Content-Type": "application/json"},
       body: JSON.stringify({path: relPath}),
     }).then(function(res) {
       if (res.ok) { _reloadSpec(relPath); }
       else { res.text().then(function(t) { _showError(t); }); }
     });
   }
   ```

5. **Non-leaf archival warning** — before calling `_archiveSpec`, if the spec has
   children (check the spec tree data), show a confirmation dialog:
   ```js
   if (specHasChildren && !confirm(
     "Archiving will hide " + childCount + " descendant spec(s). Continue?"
   )) { return; }
   ```

6. **Undo** — implement lightweight browser-side undo for the last archive/unarchive.
   Store `{action, path}` in a module-level variable after each call:
   ```js
   var _lastArchiveAction = null;  // {action: "archive"|"unarchive", path}
   ```
   Wire an "Undo" button (shown in a dismissable toast/notification after archive or
   unarchive) that calls the reverse endpoint and then re-renders. The toast auto-dismisses
   after 8 seconds; dismissal clears `_lastArchiveAction`. This avoids server-side state.

### `internal/planner/spec.go`

7. **Chat agent guard rails** — in the function that assembles the planning agent's
   system prompt context (the function that reads and formats the focused spec for
   the agent), add a guard when the spec's status is `archived`:
   ```go
   if s.Status == spec.StatusArchived {
       // Prepend an instruction to the system context
       return "⚠ This spec is archived (read-only). Do NOT write to or modify " +
           "this spec. If the user requests changes, tell them to unarchive " +
           "the spec first using the Unarchive button in the focused view.\n\n" + baseContext
   }
   ```
   Identify the exact function name by reading `internal/planner/spec.go` before
   implementing; the function likely returns a string used as part of the system prompt.

### `ui/css/spec-mode.css`

8. **Archived banner styles** — add:
   ```css
   .spec-archived-banner {
     background-color: #f8f9fa;
     border: 1px solid #dee2e6;
     border-left: 4px solid #6c757d;
     padding: 0.5rem 0.75rem;
     margin-bottom: 0.75rem;
     display: flex;
     align-items: center;
     gap: 0.5rem;
     color: #6c757d;
     font-size: 0.875rem;
   }
   ```

## Tests

Manual verification:
- Opening an archived spec shows the banner and hides dispatch/breakdown/refine buttons
- Archive button visible for `drafted`/`complete`/`stale` specs; hidden for `vague`,
  `validated`, `archived`
- Clicking Archive → confirmation if non-leaf → calls `POST /api/specs/archive` → spec reloads
- Clicking Unarchive → calls `POST /api/specs/unarchive` → spec reloads as `drafted`
- Undo toast appears after archive/unarchive; clicking Undo reverses the action
- Chat agent receives archived guard context: asking to edit an archived spec returns
  the "unarchive first" response

## Boundaries

- Do NOT implement multi-select archival in this task (single spec at a time only)
- Do NOT change the backend planner sandbox image selection or container spec
- Do NOT modify `specs_dispatch.go` or `specs.go` API endpoints
- Explorer UX (tree filter, "Show archived" toggle) is in `explorer-ux.md` — do not touch
  `spec-explorer.js` here
