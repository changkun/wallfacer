---
title: "Expose Per-Group Task Counts in Config API"
status: complete
track: foundations
depends_on:
  - specs/foundations/multi-workspace-groups/task-01-active-groups-map.md
affects:
  - internal/handler/config.go
effort: small
created: 2026-03-27
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 9: Expose Per-Group Task Counts in Config API

## Goal

Add `active_groups` to the config API response with per-group task status
counts, so the frontend can show how many tasks are in progress and waiting
for each workspace group — even groups that are not currently viewed.

## What to do

1. In `internal/handler/config.go`, add a helper that queries all active
   stores for task counts:

   ```go
   type activeGroupInfo struct {
       Key        string `json:"key"`
       InProgress int    `json:"in_progress"`
       Waiting    int    `json:"waiting"`
   }

   func (h *Handler) activeGroupInfos(ctx context.Context) []activeGroupInfo {
       var infos []activeGroupInfo
       if h.workspace == nil {
           return infos
       }
       for _, snap := range h.workspace.AllActiveSnapshots() {
           info := activeGroupInfo{Key: snap.Key}
           if snap.Store != nil {
               if tasks, err := snap.Store.ListTasks(ctx, false); err == nil {
                   for _, t := range tasks {
                       switch t.Status {
                       case store.TaskStatusInProgress, store.TaskStatusCommitting:
                           info.InProgress++
                       case store.TaskStatusWaiting:
                           info.Waiting++
                       }
                   }
               }
           }
           infos = append(infos, info)
       }
       return infos
   }
   ```

2. In `buildConfigResponse()`, add:

   ```go
   "active_groups": h.activeGroupInfos(ctx),
   ```

   This replaces the originally planned `active_group_keys` field with a
   richer structure that includes per-status counts.

## Tests

- `TestConfigResponseIncludesActiveGroups` — create manager with two
  active groups, add tasks with different statuses, call
  `buildConfigResponse`, verify the field contains both groups with
  correct counts.
- `TestActiveGroupInfosEmptyManager` — nil workspace manager returns
  empty slice.

## Boundaries

- Frontend usage is in task-10.
