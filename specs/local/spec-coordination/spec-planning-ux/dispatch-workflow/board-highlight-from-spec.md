---
title: Board Highlight from Spec Context
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/dispatch-workflow/spec-badge-on-cards.md
affects:
  - ui/js/spec-mode.js
  - ui/js/render.js
effort: small
created: 2026-04-04
updated: 2026-04-04
author: changkun
dispatched_task_id: null
---

# Board Highlight from Spec Context

## Goal

When viewing a spec in focused mode, highlight its dispatched task(s) on the board. When switching to board mode from a focused spec, scroll to and visually emphasize the relevant tasks. This maintains context across mode switches and helps users track spec-to-task relationships.

## What to do

1. In `ui/js/spec-mode.js`, when the user switches from spec mode to board mode (in the mode switching logic):
   - If a spec is focused and has a `dispatched_task_id`, store the task ID in a transient variable (e.g., `_highlightTaskId`)
   - After mode switch completes and board renders, find the task card with that ID and:
     - Scroll it into view (`element.scrollIntoView({behavior: 'smooth', block: 'center'})`)
     - Add a temporary highlight class (e.g., `card-highlight`) with a CSS animation that fades out after 2 seconds
   - Clear `_highlightTaskId` after highlighting

2. In `ui/js/render.js`, add CSS class support:
   - Add `.card-highlight` CSS class with a brief glow/pulse animation (use existing card styling colors, e.g., indigo border glow)
   - The animation should auto-remove after completion (use `animationend` event to remove the class)

3. Handle edge case: if the task card isn't visible (e.g., in a collapsed column or filtered out), skip the highlight gracefully.

## Tests

- Frontend test: verify that switching from spec mode to board mode with a focused spec applies the highlight class to the correct task card

## Boundaries

- Do NOT add board-to-spec navigation (that's handled by the spec badge click handler in the separate task)
- Do NOT add filtering/search integration (future enhancement)
- Do NOT modify the board's column layout or task ordering
