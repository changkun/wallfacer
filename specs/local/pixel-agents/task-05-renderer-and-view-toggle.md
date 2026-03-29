---
title: Renderer and View Toggle
status: complete
track: local
depends_on:
  - specs/local/pixel-agents/task-02-tilemap-and-layout.md
  - specs/local/pixel-agents/task-03-sprite-cache.md
  - specs/local/pixel-agents/task-04-camera.md
affects: []
effort: large
created: 2026-03-28
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 5: Renderer and View Toggle

## Goal

Build the canvas rendering pipeline and wire it into the existing board UI
as a toggleable secondary view. After this task, users can toggle to see an
empty office with floor tiles, walls, and furniture (no characters yet).

## What to do

1. Create `ui/js/office/renderer.js` with:
   - `OfficeRenderer` constructor: takes canvas element, TileMap, SpriteCache, Camera
   - `start()` — begins `requestAnimationFrame` loop
   - `stop()` — cancels animation frame
   - `render(timestamp)` — single frame:
     a. Clear canvas
     b. Apply camera transform (`ctx.save()`, `ctx.translate()`, `ctx.scale()`)
     c. Draw floor layer (cached to offscreen canvas, redrawn on layout change)
     d. Collect furniture into `drawables[]` array
     e. Z-sort drawables by Y coordinate (bottom edge)
     f. Draw each drawable via SpriteCache
     g. `ctx.restore()`
   - `setLayout(tileMap, furniture, seats)` — update when task count changes
   - `invalidateFloorCache()` — force floor redraw
   - Set `ctx.imageSmoothingEnabled = false` once on init

2. Create `ui/js/office/office.js` as the top-level coordinator:
   - `initOffice()` — called once on page load:
     a. Create canvas element, append to `#office-container`
     b. Instantiate TileMap, SpriteCache, Camera, OfficeRenderer
     c. Attach camera input handlers to canvas
     d. Handle window resize (update canvas size, camera, floor cache)
   - `showOffice()` / `hideOffice()` — toggle visibility:
     a. `#board` gets `display: none`, `#office-container` gets `display: block` (and vice versa)
     b. Start/stop the render loop to avoid wasting cycles when hidden
   - `isOfficeVisible()` — returns current state

3. Modify `ui/index.html` (or the relevant partial):
   - Add `<div id="office-container" class="hidden"><canvas id="office-canvas"></canvas></div>`
     adjacent to the `<main id="board">` element
   - Add toggle button in the board header area (near existing controls):
     ```html
     <button id="office-toggle" class="hidden" title="Toggle office view">
       <!-- small pixel-art icon or text label -->
       Office
     </button>
     ```
   - Button hidden by default; shown only when `assetAvailable()` is true
     OR when placeholder mode is acceptable (controlled by a flag)

4. Add `<script src="/js/office/tileMap.js"></script>` and siblings to
   `scripts.html`, after existing scripts. Order:
   `tileMap.js`, `spriteCache.js`, `camera.js`, `renderer.js`, `office.js`

5. Wire toggle button click handler in `office.js`:
   - On click: swap board/office visibility
   - Update button label/icon to indicate current mode

## Tests

- `renderer.test.js`:
  - `OfficeRenderer` can be instantiated with mock canvas context
  - `setLayout` updates internal state
  - `render` calls `ctx.clearRect`, `ctx.save`, `ctx.scale`, `ctx.restore`
  - Z-sort orders drawables by Y ascending
  - Floor cache: second render reuses offscreen canvas (no redraw)
- `office.test.js`:
  - `showOffice()` sets board hidden, office-container visible
  - `hideOffice()` reverses
  - Toggle button click alternates between views

## Boundaries

- Do NOT implement characters — the office is empty furniture only
- Do NOT implement SSE integration (that's task 9)
- Do NOT implement click-to-select interaction (that's task 12)
- Do NOT add minimap (that's task 14)
