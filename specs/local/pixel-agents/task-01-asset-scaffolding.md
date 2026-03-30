---
title: Asset Scaffolding
status: complete
depends_on: []
affects: []
effort: small
created: 2026-03-28
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 1: Asset Scaffolding

## Goal

Set up the directory structure, .gitignore rules, and asset README so that
subsequent tasks have a place to load sprites from, and contributors know
how to obtain the LimeZu packs.

## What to do

1. Create `ui/assets/office/README.md` with:
   - Purchase links for LimeZu Modern Office ($2.50) and Modern Interiors (~$5-10)
   - Directory layout showing where to place extracted sprites
   - File naming conventions (char_00.png–char_19.png, desk.png, etc.)
2. Create empty subdirectories: `ui/assets/office/characters/`,
   `ui/assets/office/furniture/`, `ui/assets/office/tiles/`,
   `ui/assets/office/effects/`.
3. Add `.gitkeep` in each subdirectory so git tracks the structure.
4. Add to the project `.gitignore`:
   ```
   # Pixel office assets (licensed, not redistributable)
   ui/assets/office/characters/*.png
   ui/assets/office/furniture/*.png
   ui/assets/office/tiles/*.png
   !ui/assets/office/effects/bubbles.png
   !ui/assets/office/README.md
   ```
5. Create a minimal `ui/assets/office/effects/bubbles.png` placeholder
   (1×1 transparent PNG) so the committed path exists.

## Tests

- No code tests needed — this is pure scaffolding.
- Verify manually: `git status` shows README.md and .gitkeep files tracked,
  PNG paths in characters/furniture/tiles are ignored.

## Boundaries

- Do NOT create any JS files yet
- Do NOT modify `ui/index.html` or any existing JS
- Do NOT modify `main.go` or Go embed directives
