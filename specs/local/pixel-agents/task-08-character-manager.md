---
title: CharacterManager and Desk Persistence
status: archived
depends_on:
  - specs/local/pixel-agents/task-02-tilemap-and-layout.md
  - specs/local/pixel-agents/task-07-character-state-machine.md
affects: []
effort: medium
created: 2026-03-28
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---


# Task 8: CharacterManager and Desk Persistence

## Goal

Implement the CharacterManager that maps tasks to characters, assigns desks,
persists desk assignments in localStorage, and integrates characters into
the renderer's draw pipeline.

## What to do

1. Create `ui/js/office/characterManager.js` with:
   - `CharacterManager` constructor: takes TileMap reference
   - `_characters` — Map of taskId → Character
   - `_deskAssignments` — Map of taskId → deskIndex (persisted)
   - `syncTasks(tasks)` — given an array of task objects:
     a. For each new task (not in `_characters`): assign desk, create Character
     b. For each existing task: call `character.setTaskStatus(task.status)`
     c. For each removed task: trigger DESPAWN on character
     d. Remove dead characters (despawn complete)
   - `assignDesk(taskId)`:
     a. Check localStorage for existing assignment
     b. If not found, pick first unassigned seat (by creation order)
     c. Save to localStorage: key `wallfacer-office-desks`
     d. Return seat `{x, y, direction, deskIndex}`
   - `pruneStaleAssignments(taskIds)` — remove localStorage entries for
     tasks not in the current task list
   - `getDrawables()` — returns array of draw info from all living characters
     (for the renderer to Z-sort and draw)
   - `updateAll(dt)` — calls `character.update(dt)` on each character
   - `characterAt(worldX, worldY)` — hit test: returns Character whose
     bounding box contains the point, or null (for interaction layer)
   - `getCharacterByTaskId(taskId)` — direct lookup

2. Integrate into renderer (`renderer.js` update):
   - `OfficeRenderer` accepts a `CharacterManager` reference
   - In `render()`, after furniture drawables, add character drawables
     from `characterManager.getDrawables()`
   - Characters participate in Z-sort with furniture

3. Integrate into office.js:
   - `initOffice()` creates CharacterManager
   - Pass to renderer

4. Add `characterManager.js` and `character.js` and `pathfinding.js` to
   `scripts.html` (after tileMap.js, before office.js)

## Tests

- `characterManager.test.js`:
  - `syncTasks([{id: "a", status: "backlog"}])` creates one character
  - Second call with same task: updates status, does not create new character
  - Removing a task from the list triggers DESPAWN
  - Desk assignment: first task gets desk 0, second gets desk 1
  - localStorage round-trip: assignments survive clear + re-sync
  - `pruneStaleAssignments` removes entries for unknown task IDs
  - `characterAt` returns correct character for position within bounds
  - `characterAt` returns null for empty space
  - Layout expansion: when taskCount > seats, `syncTasks` triggers layout
    regeneration with more desks

## Boundaries

- Do NOT implement SSE subscription — just accept task arrays via `syncTasks()`
- Do NOT implement click/selection handlers (that's task 12)
- Do NOT implement spawn/despawn visual effects (that's task 10)
