# Task 11: Speech Bubbles

**Status:** Done
**Depends on:** Task 7 (Character)
**Phase:** Phase 3 (Task State Integration)
**Effort:** Small

## Goal

Implement speech bubble overlays that float above characters to indicate
actionable states: waiting (amber "..."), failed (red "!"), and committing
(green spinner).

## What to do

1. Create `ui/js/office/bubbles.js` with:
   - Bubble type constants: `BUBBLE_WAITING`, `BUBBLE_FAILED`, `BUBBLE_COMMITTING`
   - `SpeechBubble` constructor: `new SpeechBubble(type)`
   - `update(dt)` — animate:
     - Waiting: "..." dots cycle (3-frame animation)
     - Failed: static "!" with slight pulse (scale oscillation)
     - Committing: spinning indicator (4-frame rotation)
   - `getDrawInfo()` → `{ type, frameIndex, visible }`
   - `dismiss()` — trigger fade-out (quick 0.2s alpha transition)

2. Bubble rendering (pixel art, drawn programmatically):
   - Background: rounded rectangle, 11×13 px at 1× zoom
   - Colors: amber (#F59E0B) for waiting, red (#EF4444) for failed,
     green (#22C55E) for committing
   - Symbols drawn as pixel patterns (no font rendering):
     - "..." — three 1px dots spaced 2px apart
     - "!" — vertical line 1px wide, 5px tall, with 1px dot below
     - Spinner — 4 rotated states of a simple arc
   - Small triangle pointer at bottom center pointing down toward character
   - If `bubbles.png` exists in assets, use it instead of programmatic drawing

3. Position bubbles relative to character:
   - Float above character head: `characterY - bubbleHeight - 4px` (at world scale)
   - Follow character position (including during walk)
   - When character is seated, adjust for sitting offset

4. Wire into Character:
   - SPEECH_BUBBLE state sets `character.bubble` to appropriate type
   - Exiting SPEECH_BUBBLE dismisses the bubble
   - Renderer draws bubbles in the overlay pass (after Z-sorted drawables,
     so bubbles always render on top)

## Tests

- `bubbles.test.js`:
  - `new SpeechBubble(BUBBLE_WAITING)` has type "waiting"
  - `update(dt)` advances animation frame
  - `dismiss()` starts fade, after 0.2s `visible` becomes false
  - Waiting bubble cycles through 3 frames
  - Committing bubble cycles through 4 frames
  - Failed bubble is static (1 frame) with pulse

## Boundaries

- Do NOT implement click-to-interact with bubbles (that's task 12)
- Do NOT change character state machine — just read the state
- Do NOT implement sound effects
