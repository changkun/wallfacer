---
title: "Frontend Per-Group Task Count Badges"
status: complete
track: foundations
depends_on:
  - specs/foundations/multi-workspace-groups/task-09-config-active-groups.md
affects:
  - ui/js/workspace.js
effort: small
created: 2026-03-27
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 10: Frontend Per-Group Task Count Badges

## Goal

Show per-group task count badges on workspace group tabs, so users can see
at a glance how many tasks are in progress and waiting in each group —
including groups that are not currently viewed.

## What to do

1. In `ui/js/state.js`, add:

   ```js
   activeGroups: [],  // [{key, in_progress, waiting}, ...]
   ```

2. In `ui/js/workspace.js`, update `fetchConfig()` to populate
   `state.activeGroups` from the config response's `active_groups` field.

3. In `renderWorkspaceGroups()` and `renderHeaderWorkspaceGroupTabs()`:
   - Look up the group's key in `state.activeGroups`.
   - If `in_progress > 0` or `waiting > 0`, render a compact badge next
     to the group name/tab showing the counts. For example:
     - `3 running` (in_progress count, shown when > 0)
     - `1 waiting` (waiting count, shown when > 0)
   - Use distinct colors: e.g. blue/indigo for running, amber/yellow for
     waiting.
   - For the currently viewed group, show the badge too (it's useful info).
   - Keep it subtle — small text or pill badges, not intrusive.

4. Style the badges with Tailwind classes. Example:

   ```html
   <span class="text-xs text-blue-400">3 running</span>
   <span class="text-xs text-amber-400">1 waiting</span>
   ```

## Tests

- `ui/js/__tests__/workspace.test.js` — verify that
  `renderWorkspaceGroups` renders count badges when a group has
  `in_progress > 0` or `waiting > 0`, and omits them when both are 0.

## Boundaries

- No backend changes.
- No changes to workspace switching behavior.
