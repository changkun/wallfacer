---
title: SpriteCache with Placeholder Rendering
status: complete
track: local
depends_on:
  - specs/local/pixel-agents/task-01-asset-scaffolding.md
affects: []
effort: medium
created: 2026-03-28
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 3: SpriteCache with Placeholder Rendering

## Goal

Implement sprite loading, frame slicing, and zoom-level caching. When real
PNG assets are missing, render colored rectangles as placeholders so the
office view is functional without purchased art.

## What to do

1. Create `ui/js/office/spriteCache.js` with:
   - `SpriteCache` constructor: holds a Map of `key → OffscreenCanvas`
   - `loadSpriteSheet(url, frameWidth, frameHeight)` → returns Promise that
     resolves to a `SpriteSheet` object with `.frame(index)` accessor
   - `SpriteSheet.frame(index)` returns `{ sx, sy, sw, sh }` source rect
   - `getCached(key, zoom)` → returns OffscreenCanvas at requested zoom or null
   - `cache(key, zoom, canvas)` — stores an OffscreenCanvas
   - `invalidateZoom()` — clears all cached canvases (called on zoom change)
2. Implement `rasterizeFrame(spriteSheet, frameIndex, zoom)`:
   - Creates OffscreenCanvas at `frameWidth * zoom × frameHeight * zoom`
   - Draws with `imageSmoothingEnabled = false` for crisp pixel art
   - Returns the OffscreenCanvas
3. Implement placeholder fallback in `loadSpriteSheet`:
   - If `Image.onerror` fires (asset missing), create a `PlaceholderSheet`
   - `PlaceholderSheet` generates colored rectangles:
     - Characters: small colored humanoid silhouette (solid color block)
     - Furniture: grey rectangles with type label
     - Tiles: floor = beige, wall = dark grey
   - Color chosen by hashing the sprite key
4. Export `assetAvailable()` — returns true if at least one character PNG
   loaded successfully. Used by the view toggle to show/hide the office button.

## Tests

- `spriteCache.test.js`:
  - `SpriteSheet.frame(0)` returns correct `{sx, sy, sw, sh}` for first frame
  - `SpriteSheet.frame(n)` computes correct row/col from sheet dimensions
  - `getCached` returns null for uncached keys
  - `cache` + `getCached` round-trips correctly
  - `invalidateZoom` clears all entries
  - Placeholder fallback: when image load fails, `PlaceholderSheet` is returned
    (mock Image.onerror)
  - `assetAvailable()` returns false when no PNGs loaded

## Boundaries

- Do NOT implement character animation logic (that's task 7)
- Do NOT define LimeZu-specific slicing coordinates yet (that's task 13)
- Do NOT add to `scripts.html` yet (that's task 5)
