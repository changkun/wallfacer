---
title: Cloud Coordination Plane
status: drafted
depends_on:
  - specs/identity/auth-by-default.md
affects:
  - internal/cli/web.go
  - internal/auth/
  - internal/handler/
  - internal/store/
  - frontend/src/
effort: xlarge
created: 2026-06-14
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Cloud Coordination Plane

Anchor spec for **Cloud v1 (metadata coordination)**, the lead axis of the
[latere-integration](../latere-integration.md) track. It resolves that
umbrella's open question ("does cloud metadata coordination live in a separate
Latere service, or as a `wallfacer cloud` server mode?") in favor of a
coordinator **role on the existing wallfacer cloud server** (`wallfacerd` at
wf.latere.ai), and the [remote-control](../../identity/remote-control.md) open
question ("how does a signed-in local instance announce itself?") in favor of a
single outbound connection that every coordination capability rides.

## Problem

Auth-by-default shipped: a plain `wallfacer run` signs in to auth.latere.ai and
every local record is now tied to a principal (`CreatedBy` / `OrgID`). That
identity link is **made but not used**. Nothing crosses between a signed-in
local instance and the rest of the user's org:

- No one can see who else on their team is online (no presence).
- The user cannot see their own board from another device (no remote control).
- Org-level history / usage / team visibility has nowhere to aggregate.
- Two teammates working the same repo cannot see each other's spec comments,
  because there is no channel between their two local instances.

Each capability above needs the *same primitive*: a live, authenticated channel
from a local instance to a cloud coordinator keyed by principal + org. This spec
defines that channel once, and the capabilities that ride it.

## The two axes (context)

The cloud track has two independent axes. This spec is **Axis A** only.

| Axis | What | Crosses the data boundary? | Status |
|------|------|----------------------------|--------|
| **A. Coordination plane** (this spec) | Presence, remote control, metadata projection, collaboration relay. Local stays source of truth. | Metadata + presence + comment anchors only (allow-listed) | Cloud v1, lead |
| **B. Remote execution** | Dispatch agent *runs* to Cella / Topos / Managed Agents / Antigravity. | Yes, source + worktree leave deliberately | Cloud v2+, demand-gated |

Axis B ([cella-runtime](cella-runtime.md), [topos](topos-remote-executor.md),
etc.) is unchanged and out of scope here. The two share only the auth principal.

## Core decision: relay + projection, never mirror

The coordinator is **never authoritative for local task data**. This preserves
the invariant remote-control already committed to ("latere.ai is a relay, not a
mirror; the local instance stays the source of truth"). The coordinator holds
exactly two kinds of state:

1. **Ephemeral routing + presence state** (in memory, lost on restart, rebuilt
   from reconnects): which principals are online, on which instances, focused on
   what. Never durable.
2. **A derived, allow-listed metadata *projection*** pushed from each instance's
   `store.TaskEvent` stream: counts, titles, statuses, actor subs, timestamps,
   usage totals. A read-model for org history / usage / team dashboards. It is a
   *projection of* local truth, regenerable by replay; it is never written back
   to an instance and never the system of record.

What the coordinator is **not**: a task store, a spec store, a credential store,
or a place local data is backed up. Pull the plug on wf.latere.ai and every
local instance keeps working exactly as today; only cross-instance features go
dark.

**One scoped exception:** inline spec comments are cloud-resident in v1 (see
capability 4). The coordinator is authoritative for that one collaboration
artifact, never for local task or spec *data*. The exception is bounded to
comments and is paid down by a planned git-export path.

### Why this is not "absorbing"

Consume-don't-absorb forbids wallfacer reimplementing Cella (runtime), FS (file
plane), Identity (auth), Lux (keys). The coordinator does none of those. It
coordinates **wallfacer's own domain** (specs, tasks, board presence, the
project graph) across a user's own instances. Coordinating your own concepts is
wallfacer-owned work, the same way the local server already coordinates one
machine's tasks. It is a *role of wallfacerd*, the cloud server that already
runs at wf.latere.ai, not a new platform service.

## Architecture

```
 local wallfacer (laptop)  ──┐
 local wallfacer (desktop) ──┼──▶  outbound WSS to wf.latere.ai
 local wallfacer (teammate)──┘        (wallfacerd coordinator role)
                                        ├─ presence registry (ephemeral)
                                        ├─ command router (remote control)
                                        ├─ metadata projection (per org)
                                        └─ collaboration relay (comments)
```

### The connection

On sign-in, a local instance opens **one** authenticated, long-lived outbound
connection to the coordinator (WebSocket over TLS; outbound-only, so it works
behind NAT with no inbound exposure). It carries the principal JWT; the
coordinator validates it on the same `internal/auth` path as every API request
(remote control is not an auth bypass). The instance registers a manifest:
principal, org, instance id, host label, wallfacer version, the workspace
identities it currently serves (see below), and a capability list.

The connection is the liveness signal (heartbeat). Drop or 60 s silence ⇒ the
instance's presence is cleared. This generalizes remote-control's "outbound
connection" design shape so it is built **once** and shared by all four
capabilities, rather than per-feature.

### Cross-machine workspace identity

The existing per-machine workspace key is a SHA-256 of sorted **local** paths,
which differs across machines for the same repo and therefore cannot mean "the
same workspace" to two teammates. Cross-machine identity uses the **canonical
git remote URL** (normalized `origin`) as the shared key, registered with the
org in the coordinator. The local-path fingerprint stays the *local* key; the
git-remote identity is the *cross-machine* key the coordinator joins on. A
workspace with no remote is local-only and never appears in org collaboration.

### Multiple replicas

wallfacerd runs more than one replica, so an instance's WSS lands on one replica
(its home) while presence and routing must span all of them. Cross-replica state
reuses the shared managed **Valkey** (`latere-valkey`, the cache lux/sandboxd
already co-tenant): a TTL-refreshed instance index plus pub/sub fan-out, namespaced
`wf:coord:*`, config-gated by `WALLFACER_REDIS_URL` with an in-memory
single-replica fallback for local dev. Valkey is a cache and holds only ephemeral
coordination state; **durable, authoritative** data (spec comments, the projection
long-tail rollups) lives in **Postgres** (`latere-pg`), a new infra dependency
since wallfacer is filesystem-storage today. The full design is in
[connection](latere-integration/coordination-plane/connection-and-presence/connection.md);
the durable-tier provisioning is an open decision (see Open questions).

## Capabilities riding the plane

Each is a thin feature on the one connection; each becomes a child spec.

### 1. Presence (Cloud v1, lead)

Every member opening a board served by an instance connected to the coordinator
appears in an org-wide presence list, even though each runs their own instance
on their own machine. Re-homes [multi-user-collaboration](../../identity/multi-user-collaboration.md)'s
presence design from "process-local within one hosted instance" to
"coordinator-aggregated across instances." The avatar-stack / focus / typing UI
from that spec is unchanged; only the source of the presence list moves.

#### Two deployment models, one coordinator

Both topologies are supported and are just different clients of the same
coordinator (decided: keep both as parallel deployments):

- **Local-first sync (primary).** N teammates each run their own local
  `wallfacer`, each connected outbound to the coordinator. Presence and
  collaboration aggregate across instances. This is the lead model.
- **Hosted shared (alternative).** One wallfacerd serves an org and members
  point browsers at it (the instance-per-org model in multi-user-collaboration).
  That hosted instance is itself one more coordinator client; its in-process
  presence and the coordinator's cross-instance presence reconcile to one list.

multi-user-collaboration's RBAC matrix, audit log, attribution, and
optimistic-concurrency are valid under **both** models and are re-homed, not
rewritten; only the *source* of the presence/collaboration feed moves to the
coordinator.

### 2. Remote control

The user's browser on wf.latere.ai (or a phone) lists their online instances and
routes an action (view board, dispatch, cancel) to a chosen instance over the
connection. Supersedes the transport design in
[remote-control](../../identity/remote-control.md); that spec's registry,
routing, per-action audit, and `remote-control` opt-out scope all land here as
the command-router capability rather than a separate wire.

### 3. Metadata projection

Each instance pushes the allow-listed projection (above) so the org gets
history / usage / team-visibility dashboards without any instance exposing
source or diffs. This is the umbrella's line-77 "tap TaskEvent, redact to
allow-list, push" seam, now with a defined destination.

### 4. Collaboration: inline spec comments

Teammates on the same workspace (joined by git-remote identity) comment on spec
lines, see who commented, and resolve. Storage is **hybrid: cloud-now,
git-export later** (decided).

- **v1, cloud-resident.** Comment threads live in the coordinator, authoritative
  there, pushed to connected peers in real time. This ships fastest and works
  even when teammates have no shared git remote yet. It is the **one scoped
  exception** to relay-not-mirror: the coordinator is authoritative for *spec
  comments* (a new collaboration artifact), but never for local task or spec
  *data*, which stays projection-only. The exception is bounded to this one
  type and is paid down by:
- **later, git-export.** A follow-up adds export/import so comment threads can be
  materialized into the repo (e.g. `.wallfacer/comments/<spec>.ndjson`) and
  travel with the project, restoring portability and offline access. Specced as
  its own leaf; the v1 schema is designed export-friendly from the start (stable
  ids, content-hash anchors) so the later path is not a rewrite.

Rationale for cloud-now: real-time visibility and "works without a shared
remote" are the collaboration value users actually asked for; git-resident-first
would gate the feature on every team having a shared remote and on git sync
cadence. The lock-in risk is contained by committing to the export path and an
export-friendly schema up front. Comments are attributed through the same actor
model (`ActorSub`) as task events.

## Data boundary

This plane is the first channel by which a **local instance phones home**, which
reverses the prior assumption in
[data-boundary-enforcement](../data-boundary-enforcement.md) ("local instances
do not phone home"). That spec must be widened from "browser RUM only" to "the
governed coordination channel," with:

- **Opt-in.** The connection is gated on sign-in **and** an explicit
  coordination opt-in. Anonymous and signed-in-but-opted-out instances phone
  home nothing. Local-anonymous behavior stays byte-identical.
- **Allow-list as the gate.** Only the enumerated projection fields, presence
  hints, and comment anchors cross. Source, diffs, agent output, secrets, env
  vars, and repo paths never do. The allow-list is the boundary control and has
  a regression test, the same shape as the RUM scrubber.

## Non-goals

- **Cloud as system of record for local task/spec data.** Never. Relay +
  projection only. (Spec comments are the one scoped, paid-down exception.)
- **Remote execution** (Axis B): agent runs going to Cella/Topos live in those
  specs.
- **A new Latere service.** The coordinator is a role of wallfacerd, not a new
  standalone product, and does not absorb Cella/FS/Identity/Lux.
- **CRDT / live-cursor co-editing of specs or prompts.** Comments are
  append-and-resolve, not collaborative text editing (see multi-user non-goals).
- **Backing up local task data to the cloud.** Out of scope and counter to the
  relay-not-mirror decision.

## Phasing

1. **The connection** + presence (lead): one outbound WSS, registry, heartbeat,
   org-wide presence list. Delivers visible value first.
2. **Metadata projection**: allow-listed push + org dashboards.
3. **Remote control**: command router + instance picker UI (absorbs
   remote-control).
4. **Spec comments**: git-resident store + coordinator relay + the comment UI.
5. **Data-boundary widening**: opt-in gate + allow-list test (lands alongside 1).

## Child breakdown

- `coordination-plane/connection-and-presence.md` (phase 1 transport + registry
  + heartbeat + org-wide presence; the lead child)
- `coordination-plane/metadata-projection.md` (phase 2 allow-listed push + org
  dashboards + the coordinator read-model store)
- re-home `identity/remote-control.md` as the command-router capability on this
  connection (phase 3)
- `coordination-plane/spec-comments.md` (phase 4 cloud-resident threads + relay;
  export-friendly schema; git-export is a follow-up leaf)
- widen `cloud/data-boundary-enforcement.md` to govern the phone-home channel
  (phase 5, lands with phase 1)

## Open questions

1. **Connection shape.** Long-lived WSS (real-time, server cost scales with live
   instances) vs heartbeat-poll (cheap, latency floor). Remote-control's design
   space already framed this; WSS is the lead because presence needs push.
2. **Durable store for authoritative data (blocks comments + projection
   rollups).** Ephemeral coordination lives in Valkey, decided. But spec comments
   are cloud-authoritative and the projection long-tail wants durability, and a
   cache is eviction-unsafe for a system of record. wallfacer has **no database on
   `latere-pg`** today (it is filesystem-storage). The open decision: provision a
   `wallfacer` Postgres database on `latere-pg` (the pattern every other service
   uses), or revisit the cloud-now comment-storage choice. Does **not** block
   phase-1 connection + presence, which are Valkey-only.
3. **Comment anchoring across spec edits.** A comment pins to a spec
   line/section; the underlying spec changes in git. Anchor on content hash +
   fuzzy reposition, or section id? Decided in `spec-comments.md`.
4. **Opt-in granularity.** One coordination switch, or per-capability (presence
   on, projection off)? Start with one switch; revisit if users want finer
   control.
5. **Workspaces without a remote.** Collaboration requires a shared git remote.
   Confirm the UX when a workspace has none (local-only, no presence join).
