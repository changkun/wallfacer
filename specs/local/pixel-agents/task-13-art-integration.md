# Task 13: Art Integration and Asset Detection

**Status:** Todo
**Depends on:** Task 3 (SpriteCache)
**Phase:** Phase 5 (Art Integration)
**Effort:** Medium

## Goal

Define the LimeZu-specific sprite slicing coordinates, implement graceful
asset detection, and wire the toggle button visibility to asset availability.

## What to do

1. Update `ui/js/office/spriteCache.js` with LimeZu sprite definitions:
   - Character sprite sheets (from Modern Interiors Character Generator):
     - Sheet layout: 4 rows (down, left, right, up) × N columns per animation
     - Walk: 4 frames per direction
     - Idle: 1 frame per direction (first walk frame)
     - Define `CHARACTER_FRAMES` mapping: `{ walk: [0,1,2,3], idle: [0] }`
     - Frame size: 16×16 px (or 16×24 if the generator outputs taller sprites —
       verify after purchase and update accordingly)
   - Furniture sprites (from Modern Office):
     - `FURNITURE_DEFS` mapping type → `{ file, frames, frameSize }`
     - PC: 2 frames (off, on) at 16×16
     - Desk: single frame at 32×16 (2 tiles wide)
     - Chair: single frame at 16×16
     - Other items: single frames at their tile sizes
   - Tile sprites (from Modern Interiors):
     - Floor: tile variants (4–8 slight variations for visual interest)
     - Wall: auto-tile set (define which sub-sprites to use for each
       wall configuration: top, bottom, left, right, corners)

2. Implement `detectAssets()` in `spriteCache.js`:
   - Attempt to load `ui/assets/office/characters/char_00.png` via `Image()`
   - On success: set `_assetsAvailable = true`, proceed to load all sheets
   - On error: set `_assetsAvailable = false`, use placeholder rendering
   - `assetAvailable()` returns `_assetsAvailable`

3. Wire toggle button visibility in `office.js`:
   - On init, call `detectAssets()`. On completion:
     - If assets available: show `#office-toggle` button, load all sprites
     - If assets not available but dev mode (e.g., URL param `?office=dev`):
       show button anyway with placeholder sprites
     - If assets not available and no dev flag: keep button hidden

4. Update `ui/assets/office/README.md` with exact file naming and placement
   instructions now that sprite definitions are finalized.

## Tests

- `spriteCache-art.test.js`:
  - `CHARACTER_FRAMES.walk` has 4 entries
  - `FURNITURE_DEFS.PC.frames` equals 2
  - `FURNITURE_DEFS.DESK.frameSize` equals `{w: 32, h: 16}`
  - `detectAssets()` with mock Image success → `assetAvailable()` returns true
  - `detectAssets()` with mock Image error → `assetAvailable()` returns false
  - Placeholder mode: all sprites render without errors when assets missing

## Boundaries

- Do NOT modify the rendering pipeline — just update sprite definitions
- Do NOT create or modify actual PNG files (that's manual art work)
- Do NOT change the character state machine or animation logic
