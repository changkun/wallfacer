---
title: TileMap and Layout Algorithm
status: archived
depends_on: []
affects: []
effort: medium
created: 2026-03-28
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---


# Task 2: TileMap and Layout Algorithm

## Goal

Implement the tile grid data structure and the auto-layout algorithm that
generates an office floor plan from a task count. This is pure data logic
with no rendering — it produces a 2D grid of tile types and furniture
placements that the renderer will consume.

## What to do

1. Create `ui/js/office/tileMap.js` with:
   - Tile type constants: `VOID`, `FLOOR`, `WALL`
   - `TileMap` constructor: `new TileMap(width, height)` → 2D array of tile types
   - `tileAt(x, y)` — returns tile type
   - `isPassable(x, y)` — returns true if floor and not blocked by furniture
   - `furnitureAt(x, y)` — returns furniture descriptor or null
   - `seatPositions()` — returns array of `{x, y, direction}` for all chair tiles
2. Implement `generateOfficeLayout(taskCount)`:
   - Compute `N = max(taskCount, 6)` desks
   - Arrange desks in rows of 4 (2 facing 2) with aisle gaps
   - Each desk cluster: 2 desks (2×1) facing each other, chair behind each, PC on desk
   - Place common area (sofa, plant, coffee machine, bookshelf) at bottom
   - Generate walls around bounding rectangle
   - Fill interior with floor tiles
   - Return `{ tileMap, furniture, seats }` where furniture is an array of
     `{ type, x, y, width, height, state }` and seats is an array of
     `{ x, y, direction, deskIndex }`
3. Furniture types as constants: `DESK`, `CHAIR`, `PC`, `SOFA`, `PLANT`,
   `COFFEE`, `WHITEBOARD`, `BOOKSHELF`

## Tests

- `tileMap.test.js`:
  - `generateOfficeLayout(1)` produces at least 6 seats (minimum)
  - `generateOfficeLayout(10)` produces at least 10 seats
  - All seats are on passable floor tiles
  - All furniture tiles are marked impassable (except chairs which are conditionally passable)
  - Walls form a closed perimeter (no gaps)
  - Common area furniture is placed (at least sofa + plant)
  - `isPassable` returns false for wall tiles and furniture, true for empty floor

## Boundaries

- Do NOT render anything to canvas — this is data-only
- Do NOT add to `ui/index.html` or `scripts.html`
- Do NOT implement pathfinding (that's task 6)
