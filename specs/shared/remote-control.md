---
title: Remote control of signed-in local wallfacer instances
status: vague
depends_on:
  - specs/shared/authentication.md
affects:
  - internal/auth/
  - internal/handler/
effort: xlarge
created: 2026-04-19
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Remote Control

## Problem

A locally-running wallfacer that has signed in to latere.ai is already
**linked** to a user's account — the session cookie and the
`CreatedBy` / `OrgID` fields tie every local record to a latere.ai
principal. That identity link is the foundation for remote control:
the latere.ai web UI (or a mobile client) observing and operating a
user's local wallfacer instances without exposing the machine to the
public internet.

The current sign-in flow makes the link but does not expose it. There
is no way for latere.ai to reach a signed-in local instance, and no
way for a signed-in local instance to announce itself to latere.ai.

## What this spec does

Build the wire between a signed-in local wallfacer and the latere.ai
control plane so that a user with a browser on their phone can see
their desktop's board, dispatch a task, or cancel a running one.

## What this spec does NOT do

Explicit non-goals, cribbed from the parent and re-stated so nobody
scope-creeps this spec back into them:

- **Paid-features gate.** Remote control is identity, not billing.
  Whether it is free or paid is a pricing decision that lives
  elsewhere.
- **Session sync.** The local instance stays the source of truth for
  its own task data. latere.ai is a relay, not a mirror.
- **Billing hook.** Usage on the local instance still produces cost on
  the user's local credentials; latere.ai does not charge anything
  just because the user looked at their board from a phone.

Just: identity on both ends of the wire, so a latere.ai control plane
can route a web-UI action to the right local instance.

## Design space

Two obvious shapes; both solve the NAT traversal problem:

1. **Long-lived outbound connection.** Local wallfacer opens a
   WebSocket (or gRPC-over-HTTP/2 stream) to latere.ai on sign-in,
   keeps it warm, and latere.ai sends commands down the pipe.
   Upside: real-time, low latency. Downside: server cost scales with
   active instances; NATs occasionally drop idle connections.
2. **Periodic heartbeat + pull.** Local wallfacer polls latere.ai on
   a short interval; queued commands ride the poll response.
   Upside: cheap on the server, survives NAT drops. Downside: latency
   floor equals the poll interval; the poll itself is measurable load.

Either path needs:

- A latere.ai-side registry keyed by principal: `principal_id →
  [instances]`, with each instance entry carrying hostname, OS,
  last-seen, wallfacer version, and a capability manifest (which API
  routes the instance actually exposes).
- Request routing: a remote action addressed to `principal_id +
  instance_id` needs to reach that specific instance, not just any
  instance the user is signed in on.
- Command authorization: the local wallfacer must verify every
  incoming action against the same JWT path regular API calls go
  through. Remote-control is not a bypass.
- An audit record of every remote action, stamped with the calling
  principal + the source IP of the control-plane client. Hooks into
  [`observability/audit-log.md`](../observability/audit-log.md) when that ships.

## What Phase 2 already delivered

The only commitment Phase 2 had to make for this spec: when sign-in
happens, wallfacer records enough identity info to register later.
That is done:

- `*jwtauth.Claims` on every authenticated request.
- `Task.CreatedBy` + `Task.OrgID` persisted.
- `workspace.Group.CreatedBy` + `workspace.Group.OrgID` persisted.

A registration call that sends "I am principal X, wallfacer version Y,
running on host Z" is now a straightforward handler-side change, not
an identity overhaul.

## Open questions

- **Discovery.** Does the local instance push its public endpoint (in
  the rare case it has one) or is every instance strictly outbound?
  Outbound-only is simpler and more portable.
- **Multiple instances per principal.** A user with a desktop + a
  laptop signs in on both. The UI must let the user pick which
  instance a remote action targets. How does that UI look?
- **Offline instances.** A remote action against a laptop that's
  closed — queue until it wakes, return 503, or surface
  "instance offline"? Probably a bounded queue with a timeout.
- **Security boundary.** Remote control widens the attack surface of a
  local machine. Which scopes (`scp`) gate what? `remote-control`
  probably wants to be its own scope so a user can opt out even after
  sign-in.

## Out of scope for this spec

- The wire protocol (protobuf schema, WebSocket framing). That lands
  when the design shape is picked.
- The latere.ai-side control UI. That lives with the cloud product.
- Any mobile client code. Same reason.

## Dependencies

- Authentication Phase 2 (done): principal on every request.
- Likely benefits from: [`audit-log.md`](../audit-log.md) for
  per-action attribution, but can ship without it.
- Does not gate: cloud multi-tenant or any local-track work.
