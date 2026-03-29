# Task 14: Polish — Preferences, Minimap, Accessibility

**Status:** Done
**Depends on:** Task 5 (Renderer + view toggle), Task 12 (Interaction)
**Phase:** Phase 6 (Polish and Persistence)
**Effort:** Medium

## Goal

Add quality-of-life features: remember view mode preference, render a
minimap for large offices, smooth camera follow on selection, and basic
accessibility attributes.

## What to do

1. **View preference persistence** in `office.js`:
   - On toggle: `localStorage.setItem("wallfacer-office-view", "true"/"false")`
   - On page load: if stored preference is "true" and assets available,
     auto-show office view instead of board
   - Key: `wallfacer-office-view`

2. **Minimap** — create `ui/js/office/minimap.js`:
   - Shown when office has >20 desks (configurable threshold)
   - Renders in a small fixed-size canvas (e.g., 150×100 px) in bottom-right
     corner of the office view
   - Draws a scaled-down version of the full tile grid:
     - Floor tiles as light dots
     - Furniture as dark dots
     - Characters as colored dots (color matches their sprite palette)
     - Viewport rectangle outline showing current camera view
   - Click on minimap → pan camera to that position
   - Update minimap every ~500ms (not every frame) for performance

3. **Camera follow** in `camera.js`:
   - `followTarget(worldX, worldY)` — smoothly pan toward target position
     using lerp: `camera.x += (targetX - camera.x) * 0.1` per frame
   - Triggered when selecting a character: camera pans to center on them
   - Disabled when user manually pans (any pointer-drag cancels follow)

4. **Accessibility** in `office.js`:
   - Set `aria-label="Pixel office view showing task agent characters"` on canvas
   - Set `role="img"` on canvas (it's a visual representation, not interactive
     in the accessibility tree — interactions are via pointer events)
   - Add a visually-hidden `<div>` below canvas that lists current characters
     and their states as screen-reader-accessible text:
     ```html
     <div class="sr-only" id="office-sr-summary" aria-live="polite">
       3 tasks: "Auth refactor" working, "Fix bug" waiting, "Add tests" idle
     </div>
     ```
   - Update this text whenever character states change (debounced to ~2s)

5. Add `minimap.js` to `scripts.html` after `interaction.js`
6. Wire minimap creation and updates in `office.js`

## Tests

- `polish.test.js`:
  - View preference: setting stored and retrieved from localStorage correctly
  - View preference: "true" triggers `showOffice()` on init
  - Camera follow: after `followTarget(100, 100)`, camera position moves
    toward (100, 100) over multiple `update()` calls
  - Camera follow: manual pan cancels follow
  - Minimap: not created when desk count ≤ 20
  - Minimap: created when desk count > 20
  - SR summary: text updates when character states change
  - SR summary: debounced (multiple rapid changes produce one update)

## Boundaries

- Do NOT change the rendering pipeline or character system
- Do NOT add new task states or backend changes
- Do NOT implement sound
- Do NOT add settings UI for office preferences (just localStorage)
