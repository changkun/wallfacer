# Task 6: Pathfinding

**Status:** Todo
**Depends on:** Task 2 (TileMap)
**Phase:** Phase 2 (Character System)
**Effort:** Small

## Goal

Implement BFS pathfinding on the tile grid so characters can navigate
between their desk and wander destinations.

## What to do

1. Create `ui/js/office/pathfinding.js` with:
   - `findPath(tileMap, startX, startY, goalX, goalY, extraPassable)`:
     - BFS on 4-connected grid (up, down, left, right — no diagonals)
     - `extraPassable` is an optional `Set` of `"x,y"` strings that should
       be treated as passable even if blocked (used for character's own chair)
     - Returns array of `{x, y}` from start to goal (inclusive), or `null`
       if no path exists
     - Uses `tileMap.isPassable(x, y)` for collision checks
   - `randomPassableTile(tileMap)`:
     - Returns a random `{x, y}` that is passable floor (for wander targets)
     - Avoids wall, void, and furniture tiles

## Tests

- `pathfinding.test.js`:
  - Path from (1,1) to (3,1) on open 5×5 grid: returns 3-step path
  - Path around an obstacle: correctly routes around blocked tile
  - No path exists (fully blocked): returns null
  - `extraPassable` allows routing through a normally blocked tile
  - Path includes both start and goal positions
  - Path has no diagonal moves (each step differs by exactly 1 in x or y)
  - `randomPassableTile` returns a tile where `isPassable` is true

## Boundaries

- Do NOT implement walk animation or movement — just path computation
- Do NOT modify the TileMap (pathfinding is read-only)
- Do NOT add to `scripts.html` (done when renderer integrates characters)
