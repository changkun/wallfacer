---
title: UI Message Queue and Interrupt
status: validated
track: local
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-chat-agent/ui-chat-send-stream.md
affects:
  - ui/js/planning-chat.js
  - ui/js/tests/planning-chat.test.js
  - internal/handler/planning.go
  - internal/planner/planner.go
effort: medium
created: 2026-04-03
updated: 2026-04-03
author: changkun
dispatched_task_id: null
---

# UI Message Queue and Interrupt

## Goal

Add client-side message queuing so the user can keep typing while the agent
responds, and an interrupt button that cancels the current agent turn while
preserving session context for the next message.

## What to do

### Backend: Interrupt endpoint

1. Add route to `internal/apicontract/routes.go`:
   ```go
   {POST, "/api/planning/messages/interrupt", "InterruptPlanningMessage", "interruptMessage", "Interrupt the current agent turn.", {"planning"}},
   ```

2. Implement `Handler.InterruptPlanningMessage(w, r)`:
   - Call `h.planner.Interrupt()` (new method)
   - Return `{status: "interrupted"}` or 409 if not busy

3. Add `Planner.Interrupt()` method:
   - Call `p.handle.Kill()` on the current handle
   - Clear the busy flag
   - Do NOT clear the session ID — the next `--resume` should still work
   - Close the live log so SSE stream ends

### Frontend: Message queue

4. Add queue state to `planning-chat.js`:
   ```js
   let _queue = [];  // Array of {id, text, element}
   ```

5. Modify `sendMessage(text)`:
   - If `_streaming`, push message to `_queue` instead of POSTing
   - Render queued message as a styled chip/pill below the input box
   - Each queued message has edit and remove buttons

6. Implement queue rendering:
   - Queued messages appear in a horizontal or vertical stack below the
     input field
   - Click a queued message to open inline editing (replace chip with
     an input field pre-filled with the text)
   - Dismiss button (x) removes the message from the queue
   - Messages drain from the front of the queue automatically

7. Implement auto-drain:
   - When streaming ends (SSE `done` event or interrupt), check `_queue`
   - If non-empty, shift the first message and call `sendMessage()` with it
   - This creates a chain: user types multiple messages, agent processes
     them one at a time

8. Implement interrupt button:
   - Show an "Interrupt" button next to the input while `_streaming`
   - On click: POST to `/api/planning/messages/interrupt`
   - The interrupted response (whatever was streamed so far) is kept in
     the chat as a truncated message with a visual indicator (e.g.,
     italic "interrupted" label)
   - After interrupt completes, auto-drain the queue

### Integration

9. Run `make api-contract` to regenerate routes.

## Tests

### Backend
- `TestInterruptPlanningMessage_NotBusy` — returns 409 when no exec
- `TestInterruptPlanningMessage_Busy` — mock active handle, verify Kill()
  called and busy cleared

### Frontend
- `TestMessageQueue_AddToQueue` — send while streaming, verify message
  appears in queue UI
- `TestMessageQueue_EditQueued` — click queued message, verify edit mode
  activates with input pre-filled
- `TestMessageQueue_RemoveQueued` — click dismiss on queued message,
  verify removed from queue
- `TestMessageQueue_AutoDrain` — queue 2 messages, mock stream completion,
  verify first queued message sent automatically
- `TestMessageQueue_Interrupt` — click interrupt, verify POST sent and
  truncated message shown in chat

## Boundaries

- Do NOT implement message reordering in the queue — messages drain FIFO
- Do NOT persist the queue — it's ephemeral (page reload clears it)
- Do NOT modify the conversation store — interrupted responses are still
  appended to messages.jsonl by the backend goroutine (with whatever
  partial output was collected before the kill)
