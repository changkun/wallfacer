---
title: Art Integration and Asset Detection
status: archived
depends_on:
  - specs/local/pixel-agents/task-03-sprite-cache.md
affects: []
effort: medium
created: 2026-03-28
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---


# Task 13: Art Integration and Asset Detection

## Goal

Define the LimeZu-specific sprite slicing coordinates, implement graceful
asset detection, and wire the toggle button visibility to asset availability.

## What to do

1. Update `ui/js/office/spriteCache.js` with LimeZu sprite definitions:
   - **Character sheets** (896×656 px each, 16×16 frame grid = 56 cols × 41 rows):
     - Each `char_NN.png` is a full animation sheet from Modern Interiors
       Premade Characters. Layout documented in pack's
       `Spritesheet_animations_GUIDE.png`.
     - Define `CHARACTER_ANIMS` mapping animation name → row ranges and
       frame counts per direction (down, left, right, up):
       - `idle`: row 0, 1 frame per direction
       - `walk`: rows 1–2, 6 frames per direction
       - `sit`: row 3, transition frames
       - `sitting_idle`: rows 5–6
       - `typing`: rows 7–10 (PC work animations)
     - Exact row/column offsets must be verified against the guide PNG and
       a sample character sheet — the guide shows frame positions visually.
   - **Furniture sheet** (`office_sheet.png`, 256×848 px, 16×53 tile grid):
     - All furniture is in one sheet, not individual files.
     - Define `FURNITURE_DEFS` mapping type → `{ sx, sy, sw, sh, frames }`:
       - Desk, chair, PC (on/off), monitor, whiteboard, bookshelf, sofa,
         plant, coffee machine — identify pixel regions in the sheet.
     - PC/monitor items have 2+ frames (off, on states) in adjacent cells.
   - **Tile sheets** (from Modern Interiors Room Builder subfiles):
     - `floor.png` (240×640): 5 columns of floor styles, each 3 tiles
       wide (48px) with multiple pattern rows. Pick one office-appropriate
       style and define its region.
     - `wall.png` (512×640): Wall auto-tile column groups. Each group has
       all edge/corner/fill variants. Define which sub-region to use for
       each wall configuration (top, bottom, left, right, corners, fills).

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

## Tests

- `spriteCache-art.test.js`:
  - `CHARACTER_ANIMS.walk` defines frames for 4 directions
  - `CHARACTER_ANIMS.typing` defines frames (rows 7–10 region)
  - `FURNITURE_DEFS.PC.frames` >= 2 (off/on states)
  - `FURNITURE_DEFS.DESK` has `sx, sy, sw, sh` within 256×848 bounds
  - `detectAssets()` with mock Image success → `assetAvailable()` returns true
  - `detectAssets()` with mock Image error → `assetAvailable()` returns false
  - Placeholder mode: all sprites render without errors when assets missing

## Boundaries

- Do NOT modify the rendering pipeline — just update sprite definitions
- Do NOT create or modify actual PNG files — assets are full sheets placed manually
- Do NOT change the character state machine or animation logic
