# Task 11: Documentation Update

**Status:** Todo
**Depends on:** Tasks 1-10
**Phase:** 5 (Documentation)
**Effort:** Small

## Goal

Update user-facing and internal documentation to reflect multi-workspace
group support.

## What to do

1. **`docs/guide/workspaces.md`** — Update the workspace groups section:
   - Explain that switching groups no longer stops running tasks.
   - Describe the activity indicator on group tabs.
   - Remove any mention of the 409 error when switching during task
     execution.

2. **`docs/internals/internals.md`** or relevant internals doc:
   - Document the `activeGroups` map and store lifecycle rule.
   - Explain how task count reference counting keeps stores alive.
   - Document `taskStore()` resolution in the Runner.

3. **`CLAUDE.md`** — If workspace behavior is mentioned, update to reflect
   that multiple groups can run concurrently.

4. **`docs/guide/configuration.md`** — No new env vars, but if the config
   API response is documented, mention `active_group_keys`.

## Boundaries

- Documentation only. No code changes.
