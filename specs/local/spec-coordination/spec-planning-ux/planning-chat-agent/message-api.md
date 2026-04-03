---
title: Message API Endpoints
status: validated
track: local
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-chat-agent/conversation-store.md
  - specs/local/spec-coordination/spec-planning-ux/planning-chat-agent/planning-prompt.md
affects:
  - internal/handler/planning.go
  - internal/handler/planning_test.go
  - internal/apicontract/routes.go
  - internal/planner/planner.go
  - internal/cli/server.go
effort: medium
created: 2026-04-03
updated: 2026-04-03
author: changkun
dispatched_task_id: null
---

# Message API Endpoints

## Goal

Add HTTP endpoints for sending messages to the planning agent, retrieving
conversation history, and clearing the conversation. This is the core API
that connects the UI chat pane to the planning container.

## What to do

1. Add routes to `internal/apicontract/routes.go`:
   ```go
   {GET,    "/api/planning/messages",        "GetPlanningMessages",   "messages",      "Retrieve conversation history.", {"planning"}},
   {POST,   "/api/planning/messages",        "SendPlanningMessage",   "sendMessage",   "Send a user message, triggers agent exec.", {"planning"}},
   {DELETE, "/api/planning/messages",        "ClearPlanningMessages", "clearMessages", "Clear conversation history.", {"planning"}},
   ```

2. Implement `Handler.GetPlanningMessages(w, r)` in `internal/handler/planning.go`:
   - Call `h.planner.Conversation().Messages()`
   - Support optional `?before=<RFC3339 timestamp>` for pagination (filter
     messages with timestamp before the given value)
   - Return JSON array of messages

3. Implement `Handler.SendPlanningMessage(w, r)` in `internal/handler/planning.go`:
   - Decode JSON body: `{message: string, focused_spec: string}`
   - Return **409 Conflict** if an exec is already in flight (check
     `Planner.IsBusy()` — new method, see step 5)
   - Append user message to conversation store
   - Build exec args: `-p <message> --output-format stream-json`
     plus `--resume <session-id>` if session exists
   - Prepend focused spec context to the message if `focused_spec` is set:
     `"[Focused spec: <path>]\n\n<user message>"`
   - Call `Planner.Exec(ctx, cmd)` in a background goroutine
   - Return **202 Accepted** with `{status: "accepted"}` immediately
   - The goroutine: reads Handle.Stdout() to completion, extracts session ID
     via `extractSessionID()`, parses final output via `parseOutput()`,
     appends assistant message to conversation store, saves session info,
     clears busy flag

4. Implement `Handler.ClearPlanningMessages(w, r)`:
   - Call `h.planner.Conversation().Clear()`
   - Return `{status: "cleared"}`

5. Add `IsBusy() bool` and busy tracking to `Planner`:
   - Add `busy bool` field protected by the existing mutex
   - Set `busy = true` before Exec, clear after the goroutine finishes
   - `IsBusy()` returns the flag under lock

6. Register handlers in `internal/cli/server.go` `BuildMux()` handlers map.

7. Run `make api-contract` to regenerate `ui/js/generated/routes.js` and
   `docs/internals/api-contract.json`.

## Tests

- `TestGetPlanningMessages_Empty` — returns empty array when no messages
- `TestGetPlanningMessages_WithHistory` — seed messages, verify returned
- `TestGetPlanningMessages_Pagination` — seed messages, filter with `?before`
- `TestSendPlanningMessage_NotRunning` — planner not started, returns error
- `TestSendPlanningMessage_Busy` — send while exec in flight, returns 409
- `TestSendPlanningMessage_Accepted` — mock backend, verify 202 returned
  and message appended to store
- `TestClearPlanningMessages` — seed messages, clear, verify empty

## Boundaries

- Do NOT implement SSE streaming here — that's message-stream
- Do NOT implement slash command expansion here — that's slash-command-registry
- Do NOT modify the UI — backend API only
- The background goroutine does NOT stream to the client — it just collects
  output for persistence. Streaming is handled by the message-stream task.
