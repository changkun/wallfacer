---
title: Audit Log
status: drafted
depends_on:
  - specs/identity/authentication.md
affects:
  - internal/store/
  - internal/handler/
  - internal/workspace/
  - ui/
effort: large
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Audit Log

## Problem

Wallfacer has no cross-entity record of *who did what, when*. Phase 2 of
`identity/authentication.md` delivers `CreatedBy` + `OrgID` on task and
workspace records, which answers *who originated this record* but not:

- Who transitioned task T from `in_progress` → `cancelled` at 14:02?
- Who edited the workspace-level `AGENTS.md` between dispatches?
- Who changed sandbox routing in `/api/env`?
- Who rebuilt the search index, triggered a refinement, or dispatched a spec?

These answers matter for three audiences:

1. **Cloud multi-user operators**, the `identity/multi-user-collaboration.md`
   spec already flags an audit log as an expected deliverable; teams need
   attribution for coordination and dispute resolution.
2. **Compliance-interested single-host deployments**, regulated users
   running wallfacer on a VPS want a tamper-evident trail of mutating
   actions.
3. **Debugging**, "the task changed hands and I don't know when" is a
   recurring class of support question.

This spec is flagged as **future effort**, not blocking the cloud move.
Phase 2 of authentication gives audit-log the principal context it needs;
this spec sits one layer above.

## Scope

### In scope

- A uniform structured record, **audit event**, stamped on every
  mutating operation that a handler processes.
- A write surface the store, workspace manager, and handlers can call with
  a single line: `audit.Record(ctx, op, target, delta)`.
- Per-workspace-group append-only storage, readable by record ID and by
  actor.
- Minimal read API: `GET /api/audit?target=<id>` and
  `GET /api/audit?actor=<sub>`, paginated, cloud-gated.
- Retention policy: configurable via `WALLFACER_AUDIT_RETENTION_DAYS`;
  default 365 in cloud mode, disabled in local-anonymous mode.

### Out of scope

- **Cross-entity query surface** (e.g. "show me everything Bob did across
  all workspaces"). The MVP indexes per target and per actor within a
  workspace group; fleet-wide search belongs in a later spec.
- **Tamper-evident chaining** (hash-linked append-only log à la Merkle).
  The MVP is append-only on disk; cryptographic chaining is a follow-up
  only if a compliance user explicitly asks.
- **Compliance certification** (SOC 2, HIPAA). Out of scope; this spec
  delivers the substrate, not the certification.
- **Real-time streaming UI** (live activity feed). The read API is enough
  for a later UI spec to build on.
- **Audit of read-only actions.** `GET /api/tasks` is not recorded. Only
  operations that mutate persisted state are audited.
- **PII-stripping / redaction.** The audit entry stores the principal
  sub, not email or display name. Resolution is a read-path concern.

## Prerequisites

- `identity/authentication.md` **Phase 2 complete**, handlers must have
  `*jwtauth.Claims` in `ctx` to stamp actors. Before Phase 2 lands, the
  `actor_sub` field of an audit event is either empty (anonymous local)
  or populated from the API key path (`"apikey"` sentinel).
- `identity/data-boundary-enforcement.md`, if that ships first, the cloud
  path for audit events inherits its boundary rules. Not a hard
  dependency; this spec can ship in either order.

## Design Sketch

### Event shape

```go
// internal/store/audit.go
type AuditEvent struct {
    ID         string    `json:"id"`          // UUID
    Timestamp  time.Time `json:"ts"`
    ActorSub   string    `json:"actor_sub"`   // claims.Sub or "apikey" or ""
    ActorType  string    `json:"actor_type"`  // "user" | "service" | "apikey" | "anonymous"
    OrgID      string    `json:"org_id,omitempty"`
    Operation  string    `json:"op"`          // "task.update", "workspace.instructions.write", ...
    TargetKind string    `json:"target_kind"` // "task" | "workspace" | "config" | ...
    TargetID   string    `json:"target_id"`
    Delta      any       `json:"delta,omitempty"` // optional before/after snippet
    RequestID  string    `json:"request_id"`  // correlate with access log
}
```

### Write surface

```go
// internal/audit/audit.go
//
// Record stamps a mutation event. No-op when auditing is disabled.
// Always safe to call, never panics, never fails the caller's write.
// Errors are logged, not propagated.
func Record(ctx context.Context, op, targetKind, targetID string, delta any)
```

Handlers call `audit.Record` at the *end* of a successful mutation. The
helper pulls the principal from `auth.PrincipalFromContext(ctx)`, handles
the missing-claims cases, and appends to the workspace group's audit log.

### Storage

- One append-only JSONL file per workspace group under
  `data/<group>/audit.log`.
- Rotated daily once the file crosses 50 MB (mirror the existing task
  trace rotation pattern).
- Indexed lazily on read (grep-and-paginate); no secondary index in the
  MVP. Cloud deployments that need query-at-scale route audit events into
  the telemetry pipeline in `observability/telemetry-observability.md` instead
  of reading the local file.

### Operation catalog (initial set)

| Operation | Target | Where stamped |
|-----------|--------|---------------|
| `task.create` | task | `handler.createTask` |
| `task.update` | task | `handler.patchTask` (per changed field) |
| `task.cancel` | task | `handler.cancelTask` |
| `task.delete` | task | `handler.deleteTask` |
| `task.resume` | task | `handler.resumeTask` |
| `task.retry` | task | `handler.patchTask` when status → backlog |
| `workspace.switch` | group | `workspace.Manager.Switch` |
| `workspace.instructions.write` | group | `handler.putInstructions` |
| `config.env.update` | config | `handler.putEnv` |
| `config.system-prompt.override` | prompt | `handler.putSystemPrompt` |
| `admin.rebuild-index` | index | `handler.rebuildIndex` |
| `auth.org-switch` | principal | `handler.switchOrg` (Phase 2) |
| `spec.dispatch` | spec | `handler.dispatchSpec` |

The catalog is extensible. New mutating handlers add an `audit.Record`
call at the success point; the op name is a bare constant added next to
the handler.

### Read API (cloud-gated)

- `GET /api/audit?target=<id>`, events for one target, newest first.
- `GET /api/audit?actor=<sub>`, events by one principal.
- `GET /api/audit?op=<name>`, events of a given operation type.
- Pagination via `cursor` (last-seen event ID).
- Requires claims in context; `RequireScope("audit:read")` gates it.
  `RequireSuperadmin` is sufficient for the initial cut.

Local anonymous mode doesn't mount this route.

## Integration with existing event trace

The per-task event trace in `internal/store/` (`state_change`, `feedback`,
`error`, etc.) is a *task-scoped* log and stays where it is. The audit log
is *cross-entity*, it covers workspaces, config, admin actions, specs,
and things the task trace has never tracked.

Overlap on task mutations is intentional:
- The task trace answers "what is the timeline of this task?"
- The audit log answers "who did what to this task, and to what else?"

Phase 2 of authentication will add an `actor_sub` field to the existing
task event shape so the task trace carries the same attribution as the
audit log for its subset of operations. That's a one-field addition in
the authentication data-model spec; the audit log spec does not duplicate
it.

## UI (deferred)

The MVP is an API only. A later UI spec will build the activity panel on
top of the read endpoints. Keeping UI out of this spec is deliberate,
the cloud-track work that cares about audit (multi-user collaboration)
will drive the UX from a collaborative-workflow angle, not a
compliance-dashboard angle.

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `WALLFACER_AUDIT_ENABLED` | Master switch | `true` in cloud mode, `false` in local anonymous |
| `WALLFACER_AUDIT_RETENTION_DAYS` | Days of audit events to keep on disk | `365` |
| `WALLFACER_AUDIT_DIR` | Override on-disk location | `data/<group>/audit.log` |

## Task Breakdown (future)

Not broken down yet. A rough decomposition, when this spec activates:

1. Event shape + `audit.Record` helper + storage.
2. Stamp the initial operation catalog in existing handlers.
3. Read API + superadmin gate.
4. Retention / rotation.
5. Telemetry pipeline integration for cloud scale.
6. Activity panel UI (separate spec).

## Explicitly deferred

- Cryptographic chaining.
- Cross-workspace / fleet-wide queries.
- Compliance certification work.
- Real-time streaming UI.
- Audit of read actions.
- Redaction / PII controls beyond "we store sub, not email".
