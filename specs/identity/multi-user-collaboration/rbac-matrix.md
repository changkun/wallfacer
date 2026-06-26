---
title: RBAC Scope Matrix
status: stale
depends_on:
  - specs/identity/multi-user-collaboration.md
  - specs/identity/authentication.md
affects:
  - internal/auth/authorize.go
  - internal/cli/server.go
  - internal/apicontract/routes.go
  - internal/handler/
effort: medium
created: 2026-06-14
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# RBAC Scope Matrix

The lead child of [multi-user-collaboration.md](../multi-user-collaboration.md). The collaboration parent depends on a working authorization surface before presence, audit, optimistic concurrency, or private threads are meaningful: without role gating, every signed-in org member can mutate every task. This spec defines the canonical permission matrix and wires it onto the routes, on top of the already-shipped primitives.

## Current State (shipped)

- `internal/auth/authorize.go`: `RequireSuperadmin(next)` returns 403 when the caller is not a superadmin; `RequireScope(scope)` returns a handler-wrapper factory that 403s when the caller lacks the scope. Both read the principal from the request context.
- `authkit.Identity` exposes `IsSuperadmin bool` and `Scopes []string`. There is **no** `roles[]` claim. So the admin/editor/viewer model maps onto **scopes**, not onto a role claim that does not exist.
- `internal/store/principal.go`: tasks are already org/owner-scoped for *visibility* (`Principal.CanSee`, `OrgID` / `CreatedBy`). RBAC adds *mutation* gating on top of visibility.
- Anonymous/local mode installs no auth middleware, so the wrappers are simply absent (everyone is effectively admin). This spec must preserve that: no regression for self-hosted anonymous use.

## Goal

Define the canonical scope-to-permission mapping (the matrix), pick the wallfacer scope names, and apply `RequireScope` / `RequireSuperadmin` to the mutating routes so an org's members get admin / editor / viewer behavior. Visibility stays owned by `Principal.CanSee`; this spec is about who may *act*.

## Design

### Conceptual roles to scopes

Three conceptual roles, each a set of scopes the platform issues in `Identity.Scopes`:

| Role | Scopes (proposed names) | Can |
|------|------|-----|
| viewer | `wallfacer:read` | See boards/specs/tasks they already have visibility to; no mutation. |
| editor | `wallfacer:read`, `wallfacer:write` | Create/dispatch/cancel/feedback tasks, edit specs and planning, run flows. |
| admin | `wallfacer:read`, `wallfacer:write`, `wallfacer:admin` | Editor plus org-level settings, force-archive, manage routines/agents, and destructive actions. |

Superadmin (`Identity.IsSuperadmin`) stays orthogonal and above the matrix (cross-org operational access), enforced by the existing `RequireSuperadmin`.

### The matrix

A single table maps each mutating route group to the minimum scope. The canonical list lives in this spec and is mirrored as wiring in `internal/cli/server.go`. Sketch:

| Route group | Min scope |
|-------------|-----------|
| `GET /api/**` (reads) | `wallfacer:read` |
| task create / dispatch / PATCH (cancel, feedback, archive) | `wallfacer:write` |
| spec transition / write / planning send | `wallfacer:write` |
| routines & agents/flows mutation | `wallfacer:write` |
| org settings, member management, destructive purge | `wallfacer:admin` |

Reads require `wallfacer:read`; `CanSee` still filters which rows come back.

### Wiring

Apply the wrappers at the router in `internal/cli/server.go`, grouping routes by required scope rather than annotating each handler, mirroring how `RequireSuperadmin` is already attached (`server.go:920`). When no auth provider is configured, the wrappers are not installed (anonymous mode unchanged).

## Acceptance Criteria

- A canonical scope-to-permission matrix is documented here and reflected 1:1 in the router wiring.
- A viewer (`wallfacer:read` only) gets 403 on every mutating route and 200 on reads (filtered by `CanSee`).
- An editor can create/dispatch/feedback/cancel and edit specs/planning; an admin additionally passes the admin-scoped routes.
- Anonymous / local mode is byte-identical to today (no wrappers installed).
- Scope names are confirmed against what the platform actually issues in `Identity.Scopes` (coordinate with `latere.ai/x/auth`).

## Open Questions

- Final scope names: `wallfacer:{read,write,admin}` vs reusing platform-wide scope names. Gated on what `latere.ai/x/auth` issues.
- Whether `wallfacer:admin` is per-org or global. Lean per-org, carried with `org_id`.

## Non-Goals

- Presence, audit log, optimistic concurrency, private threads (separate children of the parent).
- A custom role-claim in the JWT; this spec deliberately maps onto existing scopes.
