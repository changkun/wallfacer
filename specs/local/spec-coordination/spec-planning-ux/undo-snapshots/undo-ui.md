---
title: UI Undo Button
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/undo-snapshots/undo-api.md
affects:
  - ui/partials/spec-mode.html
  - ui/js/planning-chat.js
effort: small
created: 2026-04-04
updated: 2026-04-04
author: changkun
dispatched_task_id: null
---

# UI Undo Button

## Goal

Add a single "Undo" button to the planning chat header in `ui/partials/spec-mode.html`
that calls `POST /api/planning/undo`, removes the last message pair from the DOM, and
refreshes the spec tree. The button is disabled when no undoable rounds exist.

## What to do

1. **Add the button to `ui/partials/spec-mode.html`** in the `spec-chat-stream__header`
   div, before the Close button:

   ```html
   <div class="spec-chat-stream__header">
     <span>Planning Chat</span>
     <button id="spec-chat-undo" title="Undo last planning round" disabled>Undo</button>
     <button id="spec-chat-clear">Clear</button>
     <button onclick="toggleSpecChat()">✕</button>
   </div>
   ```

2. **Wire the button in `ui/js/planning-chat.js`**:

   a. In `init()`, add alongside existing button refs:
   ```js
   this._undoBtn = document.getElementById('spec-chat-undo');
   this._undoBtn.addEventListener('click', () => this._onUndo());
   ```

   b. Add `_updateUndoBtn()` — enables button when the message list has at least one
   assistant message, disables otherwise:
   ```js
   _updateUndoBtn() {
     if (!this._undoBtn) return;
     const hasRounds = this._messagesEl.querySelectorAll(
       '.bubble--assistant').length > 0;
     this._undoBtn.disabled = !hasRounds || this._streaming;
   }
   ```

   c. Call `_updateUndoBtn()` at the end of:
   - `_loadHistory()` — after messages are rendered
   - `_appendMessageBubble()` / `_appendMessageBubbleWithActivity()` — after appending
   - `_stopStreaming()` — after streaming ends
   - After `clearHistory()` resolves

   d. In `_startStreaming()`, also call `_updateUndoBtn()` to disable during streaming
   (identical to how the send button is disabled).

   e. Add `async _onUndo()`:
   ```js
   async _onUndo() {
     this._undoBtn.disabled = true;
     this._undoBtn.textContent = 'Undoing…';
     try {
       const res = await fetch('/api/planning/undo', { method: 'POST' });
       if (!res.ok) {
         const body = await res.json().catch(() => ({}));
         this._showError(body.error || 'Undo failed');
         return;
       }
       // Remove the last user+assistant message pair from the DOM
       const bubbles = this._messagesEl.querySelectorAll('.bubble');
       // Walk backwards: remove the last assistant bubble and its preceding user bubble
       for (let i = bubbles.length - 1; i >= 0; i--) {
         const b = bubbles[i];
         b.remove();
         if (b.classList.contains('bubble--user')) break;
       }
       // Refresh spec tree so frontmatter changes are reflected
       if (typeof reloadSpecTree === 'function') reloadSpecTree();
     } catch (e) {
       this._showError('Undo failed: ' + e.message);
     } finally {
       this._undoBtn.textContent = 'Undo';
       this._updateUndoBtn();
     }
   }
   ```

   f. Add `_showError(msg)` if not already present — inserts a temporary error notice
   into the messages area that auto-dismisses after 4 seconds.

3. **Keyboard shortcut** — in `ui/js/events.js`, add `u` key in spec mode to trigger
   `PlanningChat.undo()` (expose `_onUndo` as a public `undo()` method). This mirrors
   how `c` toggles chat and `d` dispatches.

## Tests

- `test_undo_button_disabled_on_empty_history` — init chat with no messages, verify
  `#spec-chat-undo` has `disabled` attribute
- `test_undo_button_enabled_after_message_append` — append an assistant bubble, verify
  button is enabled
- `test_undo_button_disabled_during_streaming` — call `_startStreaming()`, verify button
  is disabled; call `_stopStreaming()`, verify re-enabled
- `test_undo_removes_last_message_pair` — stub `fetch` to return 200, call `_onUndo()`,
  verify the last user+assistant bubble pair is removed from DOM
- `test_undo_shows_error_on_conflict` — stub `fetch` to return 409
  `{"error":"no planning commits to undo"}`, verify error notice appears and button
  state is restored

## Boundaries

- Do NOT add per-message undo buttons — one global button in the header only
- Do NOT implement redo
- Do NOT remove messages from the server-side conversation store (the undo button
  only updates the DOM; the server conversation log is NOT purged)
- Do NOT change the behavior of the Clear, Send, or Interrupt buttons
- The `reloadSpecTree()` call is best-effort — if the function is not available in the
  current scope, skip it silently
