# Task 9: Expose Active Group Keys in Config API

**Status:** Todo
**Depends on:** Task 1
**Phase:** 3 (Handler Changes)
**Effort:** Small

## Goal

Add `active_group_keys` to the config API response so the frontend knows
which workspace groups have open stores (running tasks).

## What to do

1. In `internal/handler/config.go`, add to `buildConfigResponse()`:

   ```go
   "active_group_keys": h.workspace.ActiveGroupKeys(),
   ```

2. Verify `ActiveGroupKeys()` was added in task-01.

## Tests

- `TestConfigResponseIncludesActiveGroupKeys` — create manager with
  two active groups, call `buildConfigResponse`, verify the field is
  present and contains both keys.

## Boundaries

- Frontend usage is in task-10.
