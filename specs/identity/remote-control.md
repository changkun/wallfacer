---
title: Remote control of signed-in local wallfacer instances
status: drafted
depends_on:
  - specs/cloud/latere-integration/coordination-plane.md
affects:
  - internal/auth/
  - internal/handler/
effort: large
created: 2026-04-19
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Remote Control

## Problem

A locally-running wallfacer signed in to latere.ai is already **linked** to a
user's account: `CreatedBy` / `OrgID` tie every local record to a principal.
That link is the foundation for remote control, the latere.ai web UI (or a
phone browser) observing and operating a user's local instances without
exposing the machine to the public internet.

The link is made but not exposed. There is no way for latere.ai to reach a
signed-in local instance.

## The wire moved

This spec no longer designs its own transport. The
[coordination plane](../cloud/latere-integration/coordination-plane.md) and its
[connection-and-presence](../cloud/latere-integration/coordination-plane/connection-and-presence.md)
child are the single source of truth for:

- the **one** long-lived outbound WSS every signed-in instance holds,
- the coordinator **instance registry** (`principal_id -> [instances]`),
- **routing** a command to a specific `instance_id`,
- the instance **announce / manifest** (host label, OS, version, capability
  list, served workspaces),
- the **heartbeat** and presence liveness,
- **connection-level JWT** validation on the `internal/auth` path.

Remote control is the **command-router capability** that rides that shared
connection. It is not a separate wire. What stays here is everything specific
to routing a UI action to a chosen instance and running it safely: the control
UI, the instance picker, offline handling, per-action authorization, per-action
audit, the action set, and the `remote-control` opt-out scope.

## Control UI (wf.latere.ai)

A signed-in user opens wf.latere.ai in a desktop or phone browser and sees,
per org, the instances the coordinator reports online for their principal.

- **Instance picker.** A user with a desktop and a laptop signed in on both
  sees both, labeled by host + OS + last-seen. Every remote action targets one
  chosen `instance_id`, never "any instance the user is signed in on." The
  picker is the routing address.
- **Offline instances.** An action against an instance the coordinator has no
  live connection for (laptop closed, NAT drop) is handled as: a bounded queue
  with a short timeout for transient drops, a `503` on timeout, and an explicit
  "instance offline" state in the picker so the user is not waiting blind.
  Long-offline instances are not queued.

The cloud-side implementation of this UI lives with the cloud product; this
spec fixes the UX contract (picker, target-by-instance, offline states) the
command router must support.

## Remote actions

Each action is a routed command that the instance executes by invoking its own
existing local API route, through the normal handler path, exactly as a local
browser would. There is no special remote entry point.

| Action | Instance-side route |
|--------|---------------------|
| View board (snapshot + live) | `GET /api/tasks`, `GET /api/tasks/stream` |
| Inspect a task | `GET /api/tasks/{id}/events`, `GET /api/tasks/{id}/diff` |
| Dispatch a task | `POST /api/tasks` (and `POST /api/specs/transition` for spec dispatch) |
| Cancel a running task | `PATCH /api/tasks/{id}` with `status=cancelled` |
| Answer a waiting task | `POST /api/tasks/{id}/feedback`, `POST /api/tasks/{id}/done` |
| Resume a failed task | `POST /api/tasks/{id}/resume` |

The action set is deliberately the routes a user would reach from the local
board. Adding an action means mapping it to an existing route, not widening the
API.

## Authorization: not a bypass

Connection-level auth happens upstream (the JWT on the WSS, validated by the
coordinator). That is necessary but not sufficient. When a routed command
arrives, the instance invokes the target route through its **own** handler
chain, so the same per-route JWT and scope checks a local request goes through
run again, instance-side. Remote control adds no authenticated path that
skips them. The principal is already on every request (`auth.Claims`), so
per-action authorization needs no new identity work.

## Audit

Every remote action writes an audit record stamped with the calling principal
and the source IP of the control-plane client, distinct from a local-board
action. The record names the action and the target `instance_id`. This is the
command-router's responsibility, recorded instance-side at invocation.

## Opt-out scope

`remote-control` is its own scope (`scp`), separate from sign-in. A user who is
signed in (so presence and projection may be enabled) can still refuse remote
command execution. With the scope absent, the instance accepts no routed
actions: it answers the connection, but the command router rejects every
action. Opt-out is per-instance, evaluated instance-side.

## Design space

The connection shape is decided upstream: one long-lived outbound WSS per
signed-in instance (outbound-only, NAT-friendly), shared by all coordination
capabilities. Remote control rides it; it picks no transport of its own.

## Non-goals

Restated so this does not creep back into them:

- **Paid-features gate.** Remote control is identity, not billing.
- **Session sync / mirror.** The local instance stays source of truth for its
  task data. The coordinator relays; it does not mirror.
- **Billing hook.** Remote viewing produces no charge of its own; local runs
  still cost on local credentials.

## Out of scope for this spec

- The transport, registry, routing, announce, and heartbeat: owned by the
  coordination plane and its connection-and-presence child.
- The cloud-side control UI implementation and any phone client code: cloud
  product.

## Dependencies

- The coordination plane connection (the wire, registry, routing, heartbeat).
- Auth (done): principal on every request, which per-action authorization and
  audit reuse. The dependency chains through the coordination plane.
