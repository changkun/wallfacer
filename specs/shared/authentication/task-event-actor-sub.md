---
title: Stamp actor principal on task event trace
status: validated
depends_on:
  - specs/shared/authentication/jwt-middleware.md
affects:
  - internal/store/
  - internal/handler/
effort: small
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Stamp actor principal on task event trace

## Goal

When a handler writes an event to a task's trace (`state_change`, `feedback`,
`error`, `system`, etc.), record *who* caused the event. This gives Phase 2
task-scoped attribution out of the box and provides the hook that
`shared/audit-log.md` will later extend to cross-entity mutation history.

Local anonymous mode writes an empty actor string, which is the same
attribution today's events carry, so no behavior change.

## What to do

1. **Event model**, extend the existing event type in `internal/store/`
   (grep for `type TaskEvent` or the event append path). Add:
   ```go
   ActorSub  string `json:"actor_sub,omitempty"`  // claims.Sub or "apikey" or ""
   ActorType string `json:"actor_type,omitempty"` // "user" | "service" | "apikey" | "anonymous"
   ```
   Both are `omitempty` so legacy events round-trip as empty strings.
2. **Population**, in every handler path that appends an event, resolve
   the actor from `ctx` via `auth.PrincipalFromContext` (delivered by
   `jwt-middleware.md`). If no claims, check whether the request was
   gated by `WALLFACER_SERVER_API_KEY` and stamp `"apikey"` /
   `"service"`; otherwise stamp `""` / `"anonymous"`.
3. **Centralize**, to avoid threading `ctx` through every event-append
   callsite, add a helper in `internal/store/`:
   ```go
   // AppendEvent records an event, resolving the actor from ctx when
   // possible. ctx may be nil for non-request-scoped writes (e.g. the
   // runner goroutine writing state transitions); those stamp actor
   // as "system" / "service".
   func (s *Store) AppendEvent(ctx context.Context, taskID string, ev TaskEvent) error
   ```
   Migrate existing appenders to the new signature. Update the runner's
   background writes to pass `context.Background()` (resolves to the
   "system" actor).
4. **No new API surface**. `GET /api/tasks/{id}/events` already returns
   the event list; the two new fields flow through the existing JSON
   response untouched thanks to `omitempty`.

## Tests

- `internal/store/task_event_actor_test.go`:
  - Event appended with user claims in ctx → `actor_sub == claims.Sub`,
    `actor_type == "user"`.
  - Event appended with no claims, API key gated request → `actor_type == "apikey"`.
  - Event appended from a background goroutine (ctx = `context.Background()`)
    → `actor_type == "system"`.
  - Legacy on-disk events (missing both fields) round-trip as empty
    strings and deserialize without error.
- `internal/handler/tasks_test.go`:
  - State transition from PATCH by an authenticated user stamps that user
    as the event actor.

## Boundaries

- Do not change the shape of existing event types (state_change, feedback,
  etc.). Only add fields.
- Do not persist email or display name on the event. Only the principal
  sub. Display-name resolution is a read-path concern.
- Do not fan out to workspaces, config, or admin actions, that is
  [`shared/audit-log.md`](../audit-log.md)'s job. This spec covers only
  the per-task event trace.
- Do not add a read filter by actor. Cross-entity queries belong in
  the audit-log spec.