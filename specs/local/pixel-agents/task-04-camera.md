# Task 4: Camera

**Status:** Done
**Depends on:** None
**Phase:** Phase 1 (Rendering Foundation)
**Effort:** Small

## Goal

Implement the viewport camera with pan and zoom, using PointerEvent for
unified mouse/touch input. The camera transforms world coordinates to
screen coordinates for the renderer.

## What to do

1. Create `ui/js/office/camera.js` with:
   - `Camera` constructor: `new Camera(canvasWidth, canvasHeight)`
   - Properties: `x`, `y` (world offset), `zoom` (integer, 2–6, default 3)
   - `worldToScreen(wx, wy)` → `{sx, sy}` applying zoom and offset
   - `screenToWorld(sx, sy)` → `{wx, wy}` inverse transform
   - `setZoom(level)` — clamp to 2–6 integer range, zoom toward center
   - `pan(dx, dy)` — shift offset by screen-space delta
   - `clamp(worldWidth, worldHeight)` — prevent panning beyond world bounds
   - `resize(canvasWidth, canvasHeight)` — update on window resize
2. Implement `attachInputHandlers(canvas, camera)`:
   - `pointerdown` + `pointermove` + `pointerup` for pan (single pointer)
   - `wheel` for zoom (deltaY → zoom in/out by 1 step)
   - Pinch-to-zoom: track two pointers, compute distance delta, map to zoom
   - Enforce minimum 3× zoom on touch devices (`pointerType === "touch"`)
   - Call `canvas.setPointerCapture(pointerId)` on down for reliable tracking

## Tests

- `camera.test.js`:
  - `worldToScreen` at zoom=3 with offset (0,0): world (10,10) → screen (30,30)
  - `worldToScreen` with offset (5,0): world (10,10) → screen (15,30)
  - `screenToWorld` is inverse of `worldToScreen`
  - `setZoom` clamps: setZoom(1) → 2, setZoom(7) → 6
  - `pan` shifts offset correctly
  - `clamp` prevents negative offset and beyond world bounds
  - Pinch zoom: simulated two-pointer events change zoom level

## Boundaries

- Do NOT render anything — camera is a pure transform + input module
- Do NOT handle click/selection (that's task 12)
- Do NOT persist zoom/pan state to localStorage (that's task 14)
