---
title: Spawn and Despawn Effects
status: complete
depends_on:
  - specs/local/pixel-agents/task-07-character-state-machine.md
affects: []
effort: small
created: 2026-03-28
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 10: Spawn and Despawn Effects

## Goal

Implement the Matrix-style digital rain effect for character spawn (reveal)
and despawn (dissolve), adding visual flair to task creation and removal.

## What to do

1. Create `ui/js/office/effects.js` with:
   - `MatrixEffect` constructor: `new MatrixEffect(type, spriteWidth, spriteHeight)`
     - `type`: `"spawn"` or `"despawn"`
     - Duration: ~0.5s (30 frames at 60fps)
   - `update(dt)` — advance the effect timer
   - `isComplete()` — returns true when animation finished
   - `getAlphaMask(col, row)` — returns alpha (0–1) for each pixel position:
     - **Spawn**: bright sweep moves top-to-bottom per column with staggered
       timing. Pixels above the sweep are fully revealed (alpha=1), below
       are hidden (alpha=0). The sweep position is offset per column using
       a simple hash for stagger.
     - **Despawn**: reverse — sweep moves top-to-bottom, pixels above the
       sweep are hidden (alpha=0).
   - `getTrailColor(col, row)` — returns a green tint `rgba(0, 255, 0, a)`
     for the trail behind the sweep head, fading with distance.

2. Integrate into renderer:
   - When a character is in SPAWN or DESPAWN state, the renderer:
     a. Rasterizes the character sprite to a temporary canvas
     b. Applies the `MatrixEffect` alpha mask pixel-by-pixel (or via
        `globalCompositeOperation` with a mask canvas)
     c. Draws the green trail overlay
   - Performance: the effect is per-character and short-lived (~0.5s),
     so pixel-level processing is acceptable

3. Wire into Character:
   - Character SPAWN state creates a `MatrixEffect("spawn", ...)`
   - Character DESPAWN state creates a `MatrixEffect("despawn", ...)`
   - `character.getDrawInfo()` includes `effect` reference when active

## Tests

- `effects.test.js`:
  - Spawn effect: at t=0, `getAlphaMask(0, bottom)` ≈ 0 (hidden)
  - Spawn effect: at t=0.5s, `getAlphaMask(0, bottom)` ≈ 1 (revealed)
  - Despawn effect: at t=0, `getAlphaMask(0, 0)` ≈ 1 (still visible)
  - Despawn effect: at t=0.5s, `getAlphaMask(0, 0)` ≈ 0 (dissolved)
  - `isComplete()` returns false during animation, true after
  - Column stagger: different columns have different sweep positions at same time
  - Trail color: green with fading alpha

## Boundaries

- Do NOT implement speech bubbles (that's task 11)
- Do NOT modify the character state machine transitions
- Do NOT add new character states — SPAWN and DESPAWN already exist
