---
title: Interaction and Modal Integration
status: archived
depends_on:
  - specs/local/pixel-agents/task-05-renderer-and-view-toggle.md
  - specs/local/pixel-agents/task-08-character-manager.md
affects: []
effort: medium
created: 2026-03-28
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---


# Task 12: Interaction and Modal Integration

## Goal

Implement click, hover, and keyboard interactions that let users select
characters, view task details, and respond to speech bubbles — bridging
the office view back to the existing task management UI.

## What to do

1. Create `ui/js/office/interaction.js` with:
   - `OfficeInteraction` constructor: takes canvas, Camera, CharacterManager
   - Hit testing:
     - On `pointerdown`/`pointerup` (distinguish from pan via movement threshold):
       convert screen coords to world via `camera.screenToWorld()`
     - Check `characterManager.characterAt(wx, wy)` for character hit
     - Check bubble bounding box separately (bubbles float above characters)
   - **Tap/click character**: select character
     - Set `_selectedCharacterId`
     - Renderer draws white 1px outline around selected character sprite
       (precomputed by dilating sprite bounds by 1px)
     - Show tooltip with task title near character
   - **Double-tap/double-click character**: open task detail
     - Call existing `openModal(taskId)` from `modal-core.js`
   - **Tap/click speech bubble**:
     - Waiting bubble: call existing feedback submission flow
       (`openModal(taskId)` which shows the feedback tab)
     - Failed bubble: call `openModal(taskId)`
   - **Hover** (desktop only, `pointermove`):
     - Show tooltip `<div>` positioned near cursor with task title + status
     - Hide on `pointerleave`
   - **Long-press** (touch, >500ms hold):
     - Show same tooltip
   - **Keyboard**:
     - `Escape` — deselect character
     - `Tab` — cycle selection through characters (by desk order)
     - `Enter` — open modal for selected character

2. Tooltip rendering:
   - Create a `<div id="office-tooltip">` positioned absolutely over canvas
   - Content: task title (truncated to ~30 chars) + status badge
   - Style: dark background, light text, small font, rounded corners
   - Position: offset from character screen position via `worldToScreen()`

3. Selection outline in renderer:
   - When `_selectedCharacterId` is set, draw a white 1px outline around
     the character sprite after the main draw pass
   - Outline computed by drawing the sprite silhouette expanded by 1px
     with white color, then drawing the actual sprite on top

4. Add `interaction.js` to `scripts.html` after `office.js`
5. Wire `OfficeInteraction` creation in `initOffice()`

## Tests

- `interaction.test.js`:
  - Click at character position → `_selectedCharacterId` set to character's taskId
  - Click empty space → `_selectedCharacterId` cleared
  - Double-click character → `openModal` called with correct taskId
  - Bubble click on waiting character → `openModal` called
  - Escape key → selection cleared
  - Tab key → cycles to next character
  - `pointerdown` + significant `pointermove` = pan (no selection)
  - `pointerdown` + minimal move + `pointerup` = click (selection)

## Boundaries

- Do NOT implement drag-to-reassign desks — assignments are automatic
- Do NOT modify existing modal code — just call `openModal(taskId)`
- Do NOT implement minimap interaction (that's task 14)
