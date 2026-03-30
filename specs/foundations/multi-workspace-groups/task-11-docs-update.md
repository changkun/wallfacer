---
title: "Documentation Update"
status: complete
depends_on:
  - specs/foundations/multi-workspace-groups/task-01-active-groups-map.md
  - specs/foundations/multi-workspace-groups/task-02-modify-switch.md
  - specs/foundations/multi-workspace-groups/task-03-runner-task-ws-key.md
  - specs/foundations/multi-workspace-groups/task-04-runbackground-lifecycle.md
  - specs/foundations/multi-workspace-groups/task-05-run-uses-taskstore.md
  - specs/foundations/multi-workspace-groups/task-06-remove-409-guard.md
  - specs/foundations/multi-workspace-groups/task-07-watcher-resubscribe.md
  - specs/foundations/multi-workspace-groups/task-08-watcher-multi-store-iteration.md
  - specs/foundations/multi-workspace-groups/task-09-config-active-groups.md
  - specs/foundations/multi-workspace-groups/task-10-frontend-activity-indicator.md
affects: []
effort: small
created: 2026-03-27
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 11: Documentation Update

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
