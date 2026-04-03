---
title: UI Chat Send and Stream
status: validated
track: local
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-chat-agent/message-stream.md
affects:
  - ui/js/spec-mode.js
  - ui/js/planning-chat.js
  - ui/js/tests/planning-chat.test.js
effort: medium
created: 2026-04-03
updated: 2026-04-03
author: changkun
dispatched_task_id: null
---

# UI Chat Send and Stream

## Goal

Wire up the spec mode chat pane to send messages to the planning agent and
stream responses in real-time. This makes the chat pane functional for basic
back-and-forth conversation.

## What to do

1. Create `ui/js/planning-chat.js` as the chat module:

   ```js
   // State
   let _streaming = false;
   let _eventSource = null;

   // Public API
   export function initPlanningChat() { ... }
   export function sendMessage(text) { ... }
   export function isStreaming() { return _streaming; }
   ```

2. Implement `initPlanningChat()`:
   - Find DOM elements: `#spec-chat-input`, `#spec-chat-stream`
   - Load conversation history via `GET /api/planning/messages` and render
     existing messages into the stream container
   - Attach Enter key handler on the input field to call `sendMessage()`
   - Call from `spec-mode.js` when spec mode is activated

3. Implement `sendMessage(text)`:
   - If `_streaming`, add to local queue (see ui-message-queue task) — for
     this task, simply disable input while streaming
   - POST to `/api/planning/messages` with `{message: text, focused_spec: currentFocusedSpec}`
   - Append user message bubble to `#spec-chat-stream`
   - On 202 response, start SSE: `new EventSource(routes.planning.messageStream)`
   - On 409 response, show "Agent is busy" indicator

4. Implement SSE consumption:
   - Create `EventSource` pointing to `/api/planning/messages/stream`
   - On `message` event: append text chunk to the current assistant bubble
     (create bubble on first chunk)
   - On `done` event: mark streaming complete, re-enable input, close
     EventSource
   - On `error` event: show error state, re-enable input
   - Auto-scroll the stream container to bottom on each chunk

5. Implement message rendering:
   - User messages: right-aligned bubble with user styling
   - Assistant messages: left-aligned bubble, render markdown via the
     existing `renderMarkdown()` function from `ui/js/markdown.js`
   - Timestamp shown below each message

6. Implement slash command autocomplete:
   - On `/` typed as first character, fetch `GET /api/planning/commands`
   - Show dropdown menu above input with matching commands
   - Arrow keys to navigate, Enter/Tab to select
   - Selection fills the input with the command and positions cursor
     after it for argument entry

7. Wire into `spec-mode.js`:
   - Import and call `initPlanningChat()` in spec mode initialization
   - Update `breakDownFocusedSpec()` to call `sendMessage("/break-down")`
     instead of pre-filling the input

## Tests

- `TestPlanningChat_SendMessage` — mock fetch, verify POST body contains
  message and focused_spec
- `TestPlanningChat_RenderUserMessage` — verify user message appears in
  stream container with correct styling
- `TestPlanningChat_RenderAssistantMessage` — verify assistant message
  renders markdown
- `TestPlanningChat_HistoryLoad` — mock GET response with messages, verify
  they render on init
- `TestPlanningChat_SlashAutocomplete` — type `/sum`, verify dropdown shows
  `/summarize`
- `TestPlanningChat_StreamingDisablesInput` — verify input disabled during
  streaming

## Boundaries

- Do NOT implement the message queue (edit/remove queued messages) — that's
  ui-message-queue
- Do NOT implement the interrupt button — that's ui-message-queue
- For this task, simply disable the input while the agent is responding
- Do NOT modify backend code — this is frontend only
