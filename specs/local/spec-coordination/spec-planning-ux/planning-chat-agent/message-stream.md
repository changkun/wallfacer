---
title: Message Stream SSE Endpoint
status: complete
track: local
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/planning-chat-agent/message-api.md
affects:
  - internal/handler/planning.go
  - internal/handler/planning_test.go
  - internal/apicontract/routes.go
  - internal/planner/planner.go
effort: medium
created: 2026-04-03
updated: 2026-04-03
author: changkun
dispatched_task_id: null
---

# Message Stream SSE Endpoint

## Goal

Add an SSE endpoint that streams the planning agent's response in real-time
as it executes. This allows the UI to show the agent's output token-by-token
rather than waiting for the full response.

## What to do

1. Add route to `internal/apicontract/routes.go`:
   ```go
   {GET, "/api/planning/messages/stream", "StreamPlanningMessages", "messageStream", "Stream the agent's response tokens.", {"planning"}},
   ```

2. Add a `LiveLog` mechanism to Planner for streaming the current exec's
   stdout. Use the same `internal/runner/livelog.go` pattern:
   - When `SendPlanningMessage` starts a background exec goroutine, it
     creates a new `liveLog` and tees `Handle.Stdout()` into it while
     also collecting the full output for parsing
   - Add `Planner.LogReader() *LiveLogReader` that returns a reader for
     the current exec's live log, or nil if not busy
   - The live log is closed when the exec goroutine finishes

3. Implement `Handler.StreamPlanningMessages(w, r)`:
   - Set SSE headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`
   - Get `h.planner.LogReader()` — if nil (not busy), return 204 No Content
   - Loop: call `reader.ReadChunk(ctx)`, write each chunk as raw SSE data,
     flush. Follow the same pattern as `streamLiveLog` in `stream.go`.
   - Send keepalive comments every `constants.SSEKeepaliveInterval`
   - On `io.EOF` (exec finished), send a final `event: done` with the
     parsed assistant message as JSON data, then close

4. Update the `SendPlanningMessage` goroutine (from message-api task) to
   create and manage the live log:
   - Before reading Handle.Stdout(), create a `liveLog`
   - Use `io.TeeReader` to copy stdout into both the live log buffer and
     the output collector
   - Close the live log when the exec finishes

5. Register handler in `BuildMux()` and run `make api-contract`.

## Tests

- `TestStreamPlanningMessages_NotBusy` �� returns 204 when no exec in flight
- `TestStreamPlanningMessages_LiveData` — mock a planner exec, verify SSE
  chunks arrive in order and end with `event: done`
- `TestStreamPlanningMessages_Keepalive` — verify keepalive sent when no
  data flowing (use a slow mock exec)

## Boundaries

- Do NOT implement the UI SSE consumer — that's ui-chat-send-stream
- Do NOT add message queue or interrupt logic — that's ui-message-queue
- Reuse the existing `liveLog` / `LiveLogReader` types from
  `internal/runner/livelog.go` — do NOT duplicate them. If they need to
  be extracted to a shared package, do that as part of this task.
