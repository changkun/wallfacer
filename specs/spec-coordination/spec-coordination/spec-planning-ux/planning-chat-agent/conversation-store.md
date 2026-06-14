---
title: Conversation Store
status: complete
track: local
depends_on: []
affects:
  - internal/planner/conversation.go
  - internal/planner/conversation_test.go
effort: medium
created: 2026-04-03
updated: 2026-04-03
author: changkun
dispatched_task_id: null
---

# Conversation Store

## Goal

Implement server-side conversation persistence for the planning chat so that
the UI can replay prior messages after a browser reload or application restart.
This is the data layer that the message API handlers will build on.

## What to do

1. Create `internal/planner/conversation.go` with a `ConversationStore` type:

   ```go
   type Message struct {
       Role        string    `json:"role"`      // "user" or "assistant"
       Content     string    `json:"content"`
       Timestamp   time.Time `json:"timestamp"`
       FocusedSpec string    `json:"focused_spec,omitempty"`
   }

   type SessionInfo struct {
       SessionID   string    `json:"session_id"`
       LastActive  time.Time `json:"last_active"`
       FocusedSpec string    `json:"focused_spec,omitempty"`
   }

   type ConversationStore struct {
       dir string // ~/.wallfacer/planning/<fingerprint>/
       mu  sync.Mutex
   }
   ```

2. Implement `NewConversationStore(configDir, fingerprint string)` that creates
   `~/.wallfacer/planning/<fingerprint>/` if it doesn't exist.

3. Implement `AppendMessage(msg Message) error` — append a JSON line to
   `messages.jsonl` using atomic write pattern (write to temp, fsync, rename
   is not needed for append — just open with O_APPEND|O_CREATE|O_WRONLY and
   write a single line).

4. Implement `Messages() ([]Message, error)` — read `messages.jsonl` and
   return all messages in order. Skip malformed lines with a log warning.

5. Implement `Clear() error` — remove `messages.jsonl` and `session.json`.

6. Implement `SaveSession(info SessionInfo) error` — write `session.json`
   atomically (temp file + rename, same pattern as `internal/store/`).

7. Implement `LoadSession() (SessionInfo, error)` — read `session.json`.
   Return zero value if file doesn't exist.

8. Wire `ConversationStore` into the `Planner` struct — add a
   `Conversation() *ConversationStore` accessor. Create the store in
   `planner.New()` using a new `ConfigDir` field on `planner.Config`.

## Tests

- `TestConversationStore_AppendAndRead` — append 3 messages, read them back,
  verify order and content
- `TestConversationStore_AppendConcurrent` — append from 10 goroutines,
  verify all messages present (no corruption)
- `TestConversationStore_Clear` — append messages, clear, verify empty
- `TestConversationStore_SessionRoundTrip` — save session info, load it back,
  verify fields match
- `TestConversationStore_LoadSession_Missing` — load from empty dir, verify
  zero value returned without error
- `TestConversationStore_MalformedLines` — write a file with some bad JSON
  lines, verify they're skipped and valid lines still returned

## Boundaries

- Do NOT add HTTP handlers here — that's the message-api task
- Do NOT implement SSE streaming — that's the message-stream task
- Do NOT integrate with the UI — backend only
- Do NOT modify `Planner.Exec()` — this task is purely about persistence
