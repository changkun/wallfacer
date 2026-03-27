# Task 10: Frontend Activity Indicator for Workspace Groups

**Status:** Todo
**Depends on:** Task 9
**Phase:** 4 (Frontend)
**Effort:** Small

## Goal

Show a visual indicator next to workspace groups that have running tasks
in the background, so users know which groups are active even when not
viewed.

## What to do

1. In `ui/js/state.js`, add:

   ```js
   activeGroupKeys: [],
   ```

2. In `ui/js/workspace.js`, update `fetchConfig()` to populate
   `state.activeGroupKeys` from the config response's
   `active_group_keys` field.

3. In `renderWorkspaceGroups()` (lines 273-336) and
   `renderHeaderWorkspaceGroupTabs()` (lines 340-414):
   - Check if a group's key is in `state.activeGroupKeys`.
   - If yes and it's not the currently viewed group, show a small activity
     dot (e.g., a colored circle or pulsing indicator) next to the group
     name/tab.

4. Style the indicator with Tailwind classes. Keep it subtle — a small
   dot is sufficient.

## Tests

- `ui/js/__tests__/workspace.test.js` — verify that
  `renderWorkspaceGroups` adds the indicator class when a group's key
  is in `activeGroupKeys` and omits it otherwise.

## Boundaries

- No backend changes.
- No changes to workspace switching behavior.
