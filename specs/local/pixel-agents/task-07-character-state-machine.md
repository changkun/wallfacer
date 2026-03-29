---
title: Character State Machine and Animation
status: complete
track: local
depends_on:
  - specs/local/pixel-agents/task-03-sprite-cache.md
  - specs/local/pixel-agents/task-06-pathfinding.md
affects: []
effort: medium
created: 2026-03-28
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 7: Character State Machine and Animation

## Goal

Implement the Character class with its state machine, animation controller,
and walk movement. Each character represents one task and transitions
between states based on task status changes.

## What to do

1. Create `ui/js/office/character.js` with:
   - State constants: `SPAWN`, `WALK_TO_DESK`, `WORKING`, `SPEECH_BUBBLE`,
     `IDLE`, `WANDER`, `DESPAWN`
   - Direction constants: `DOWN`, `LEFT`, `RIGHT`, `UP` (0–3)
   - `Character` constructor: `new Character(id, spriteIndex, seat)`
     - `id` — task UUID
     - `spriteIndex` — which character sprite sheet (0–19)
     - `seat` — `{x, y, direction}` assigned desk position
     - Initial state: `SPAWN`
   - `update(dt)` — advance per frame:
     - **SPAWN**: run spawn timer (~0.5s), then transition to IDLE
     - **IDLE**: stand at current position, periodically pick a wander target
     - **WANDER**: follow path from pathfinding; on arrival, pause, then IDLE
     - **WALK_TO_DESK**: follow path to seat; on arrival, transition to WORKING
     - **WORKING**: play typing/reading animation at seat
     - **SPEECH_BUBBLE**: remain at seat, display bubble type
     - **DESPAWN**: run despawn timer (~0.5s), then mark as `dead`
   - `setTaskStatus(status)` — maps task status to character state:
     - `backlog` → IDLE
     - `in_progress` / `committing` → WALK_TO_DESK (if not at desk) or WORKING
     - `waiting` → SPEECH_BUBBLE (amber)
     - `failed` → SPEECH_BUBBLE (red)
     - `done` → IDLE
     - `cancelled` → DESPAWN
   - `getDrawInfo()` → `{ x, y, spriteIndex, frameIndex, direction, state }`
     for the renderer

2. Animation controller (within Character):
   - `_animFrame` — current frame index
   - `_animTimer` — accumulator, advances frame every N ms
   - Frame counts: typing=2, reading=2, walk=4, idle=1
   - Walk animation: cycle through 4 frames while moving
   - Typing/reading: alternate 2 frames while seated

3. Walk movement:
   - Store current path as array of `{x, y}` from pathfinding
   - Walk speed: 2 tiles/second
   - Linear interpolation between tile centers:
     `pos += direction * speed * dt`
   - On reaching next tile, shift path array, update direction
   - If path is empty, arrival → trigger state transition

## Tests

- `character.test.js`:
  - New character starts in SPAWN state
  - After 0.5s of `update(dt)`, transitions to IDLE
  - `setTaskStatus("in_progress")` on IDLE character → WALK_TO_DESK
  - `setTaskStatus("waiting")` → SPEECH_BUBBLE
  - `setTaskStatus("cancelled")` → DESPAWN
  - Walk movement: character at (0,0) with path to (2,0) reaches (2,0)
    after sufficient `update()` calls
  - Animation frame cycles: typing alternates 0/1, walk cycles 0/1/2/3
  - `getDrawInfo()` returns correct spriteIndex and current frame
  - DESPAWN: after timer expires, `character.dead` is true

## Boundaries

- Do NOT manage character lifecycle (creation/destruction) — that's task 8
- Do NOT render characters to canvas — just produce draw info
- Do NOT implement spawn/despawn visual effects (that's task 10)
- Do NOT implement speech bubble rendering (that's task 11)
