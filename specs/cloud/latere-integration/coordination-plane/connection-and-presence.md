---
title: Coordination Connection and Presence
status: stale
depends_on:
  - specs/cloud/latere-integration/coordination-plane.md
affects:
  - internal/cli/web.go
  - internal/auth/
  - internal/handler/
  - frontend/src/
effort: xlarge
created: 2026-06-14
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# Coordination Connection and Presence

Phase 1 (lead child) of the [coordination plane](../coordination-plane.md). It
builds the **one outbound connection** every coordination capability rides, the
coordinator-side **registry** that connection feeds, and the first capability on
it: **org-wide presence**. Remote control, metadata projection, and spec
comments are later children that reuse this connection unchanged; they are out
of scope here except where they constrain the wire shape.

This spec settles the anchor's open question 1 (connection shape) as: long-lived
WSS, because presence needs push. It does not re-decide anything else; the
relay-not-mirror, coordinator-as-role-of-wallfacerd, opt-in, and git-remote
identity decisions are inherited.

## The two transports, kept separate

The single most load-bearing distinction in this spec. There are **two** wires
and they do not merge:

1. **Browser to local instance: unchanged.** The board still talks to its own
   serving instance over the existing SSE stream (`internal/handler/stream.go`,
   `GET /api/tasks/stream`) plus `POST /api/presence/focus`. [multi-user-collaboration](../../../identity/multi-user-collaboration.md)
   said "No new SSE endpoint. No WebSocket" for presence, and that stays true for
   this browser leg. The browser-facing `event: presence` payload
   `{present: [{id, name, avatar, role, focus}]}` does **not** change shape.
2. **Instance to coordinator: new.** A signed-in, opted-in instance holds one
   long-lived outbound WSS to the coordinator. This is the new wire. It uses
   `github.com/coder/websocket` (already a dependency, used by
   `internal/handler/terminal.go`), outbound-only so it traverses NAT with no
   inbound exposure.

Only the **source** of each browser's `present[]` list changes: from a
process-local map to a coordinator-aggregated one. The UI is re-homed, not
rewritten.

## 1. Connection lifecycle

### Where the code lives

- **Coordinator (accept side):** a new package `internal/coordinator/`, mounted
  on the wf.latere.ai front-end server (`internal/cli/web.go`, `RunWeb`, service
  `wallfacer-web`, which already does OIDC login, `/api/me`, and serves the
  SPA). The coordinator adds one handler, `GET /api/coordination/ws`, behind the
  same auth path as the rest of that server.
- **Client (dial side):** a connector started from the local instance's existing
  cloud-mode block in `internal/cli/server.go` (the `cloudMode` /
  `jwtValidator` / `authClient` wiring around line 189). Place it in
  `internal/coordinator/client` (same package tree, client subpackage) so the
  wire types are shared with the accept side. A hosted-shared `wallfacerd` runs
  **both**: it serves browsers (server.go) and dials the coordinator (client).

### Open

The connector opens when **both** hold:

- The instance is signed in (a valid token exists, the same
  `authkit`/`internal/auth` token the API uses, loaded as in
  `internal/cli/auth.go`).
- Coordination is explicitly **opted in** (the anchor's gate; one switch, see
  open question 4 there). Anonymous instances and signed-in-but-opted-out
  instances open nothing and phone home nothing. Local-anonymous behavior is
  byte-identical.

On dial the connector sends the principal JWT (WSS handshake `Authorization`
header). The coordinator validates it on the **same** `internal/auth` path as
every API request (`auth.Validator`, the `PrincipalFromContext` principal). A
failed validation closes the socket; this is not an auth bypass.

### Manifest registration

First frame after the handshake is the **manifest** (instance to coordinator):

```json
{
  "type": "manifest",
  "principal": "u_01J8B…",          // Identity.Sub
  "org": "org_01J…",                // Identity.OrgID
  "instance_id": "inst_<persisted>",// stable per data-dir, survives restart
  "host_label": "changkun-mbp",     // human label for the instance picker
  "version": "wallfacer/<semver>",
  "workspaces": [                    // cross-machine identities served now
    {"remote": "github.com/latere-ai/wallfacer", "local_key": "<GroupKey>"}
  ],
  "capabilities": ["presence", "focus", "projection", "remote-control", "comments"]
}
```

`workspaces[].remote` is the **canonical git remote URL** (cross-machine join
key, below). `local_key` is the existing per-machine `GroupKey`
(`internal/workspace/groups.go`), sent for the instance's own routing only; the
coordinator never joins on it. `capabilities` lets later children negotiate
without a protocol bump. The manifest is re-sent on any workspace-set change
(workspace switch) so the registry tracks what each instance currently serves.

### Heartbeat / liveness

The connection itself is the liveness signal. The connector sends a ping frame
every 20 s; the coordinator clears the instance from the registry on socket
close or **60 s** of silence (the same liveness window multi-user uses for
SSE-based presence). No separate heartbeat endpoint.

### Reconnect / backoff

On any drop the connector reconnects with exponential backoff (1 s, capped at
30 s, full jitter) while signed-in and opted-in. Each reconnect **re-registers
the full manifest**; the registry is rebuilt from reconnects and never
durable (anchor decision). The browser SSE leg is independent: a coordinator
outage degrades cross-instance presence to process-local without dropping the
board.

### Teardown

Clean teardown on sign-out, coordination opt-out, or process exit: the
connector sends a close frame and cancels backoff. The coordinator drops the
instance immediately on close, which clears its presence rows and re-broadcasts
to peers (no 60 s wait on a graceful close).

## 2. Coordinator-side registry

In-memory, ephemeral, rebuilt from reconnects. Never persisted (anchor: "lost
on restart, rebuilt from reconnects"). Two indices, both keyed off the validated
principal, both guarded by one mutex:

- `principal -> []instance` : every live connection for a `Sub` (one person,
  many machines). Each entry carries the manifest plus the live socket handle
  for the command router (phase 3) and projection (phase 2) to reuse.
- `org -> set<principal>` : derived membership of who is present in an org,
  recomputed as instances join and leave.

A third index, `remote-url -> set<instance>`, joins instances on the
cross-machine workspace key for collaboration (phase 4); it is built here so the
registry shape is settled, even though presence aggregates per org, not per
workspace.

The registry exposes a narrow interface (Join, Leave, manifest update, snapshot,
subscribe) so each capability child consumes it without touching socket
plumbing. Restart of wf.latere.ai empties it; instances reconnect and rebuild it
within one backoff cycle.

## 3. Org-wide presence

### Goal (re-homed unchanged)

Every member who opens a board served by a connected, opted-in instance appears
in one org-wide presence list, even though each teammate runs their own instance
on their own machine. multi-user's presence/focus/typing UI (the header avatar
stack, the per-card viewer avatars, the "X is typing" indicator) ships
**unchanged**. Only the list's source moves from process-local to
coordinator-aggregated. multi-user's RBAC, audit, attribution, and
optimistic-concurrency are unaffected and not touched here.

### The path of a focus hint

The browser still does what multi-user specified; the new hops are appended on
the instance-to-coordinator wire:

1. Browser to its instance: `POST /api/presence/focus` with
   `{task_id, thread_id, editing}`, unchanged semantics, server-side rate limit
   **500 ms** per client, typing TTL **3 s** (multi-user's numbers, reused).
2. Instance to coordinator: the instance relays the focus/typing hint up the WSS
   as a coordinator-wire delta (below). This is the "coordinator path" the
   browser endpoint feeds; the endpoint contract to the browser does not change.
3. Coordinator aggregates the hint into the principal's registry entry and fans
   it to **peer instances** in the same org (and, phase 4, the same remote URL).
4. Each peer instance emits the merged list on its **own** browser SSE as the
   existing `event: presence`. The browser never learns there was a second wire.

### Coordinator-wire shapes (new, distinct from the browser event)

Instance to coordinator (presence delta):

```json
{
  "type": "presence",
  "instance_id": "inst_…",
  "principal": "u_…",
  "focus": {"remote": "github.com/…", "task_id": "…", "thread_id": "…",
            "editing": "task:<id>.prompt", "ttl_ms": 3000}
}
```

Coordinator to instance (org aggregate, fan-out):

```json
{
  "type": "presence-snapshot",
  "org": "org_…",
  "present": [
    {"sub": "u_…", "host_labels": ["changkun-mbp"],
     "focus": {"remote": "…", "task_id": "…", "thread_id": "…", "editing": "…"}}
  ]
}
```

The coordinator carries **only** the allow-listed presence hints (sub, host
label, focus anchors, typing target). No source, no diff, no prompt text, no
repo path. The receiving instance hydrates each `sub` into the browser
`{id, name, avatar, role, focus}` shape from its own org-member cache
(`GET /api/org/members`, multi-user), so PII expansion happens instance-side,
not on the wire. The fan-out is full-snapshot on every change (membership is
small, dozens per org, the same fan-out multi-user already accepts).

## 4. Two-deployment reconciliation

Both topologies are clients of the one coordinator (anchor decision). The hard
case is hosted-shared, where one wallfacerd has presence from **two** sources at
once:

- **Direct browsers** on that hosted instance, tracked in its **process-local
  Presence map** (multi-user's design: an active SSE subscription means present).
- **Its own outbound WSS** to the coordinator, which carries cross-instance
  presence from every other instance in the org (teammates' laptops, other
  hosted replicas).

Reconciliation is a **merge keyed on `Sub`**, performed at the single point
where each browser's `present[]` is rendered for the SSE emit:

1. Take the process-local present set (direct browsers on this instance).
2. Take the coordinator `presence-snapshot` for this org.
3. Union by `Sub`. A member who is present via a direct browser session **and**
   via their own laptop instance collapses to **one** row. Multiple instances or
   sessions for the same `Sub` flatten into one entry; their focus hints merge
   (most recent wins per anchor, the same last-write semantics multi-user uses
   for fuzzy state).

Because the merge happens at the emit point feeding the unchanged
`event: presence`, the browser UI is identical across both deployment models: it
always renders one de-duplicated org list. Local-first sync (N laptops, no
direct browsers on any) is just the degenerate case where the process-local set
is empty and the merge is the coordinator snapshot alone.

## Cross-machine workspace identity

The local key stays the local key. `GroupKey`
(`internal/workspace/groups.go`, SHA-256 of sorted local paths) differs per
machine and cannot mean "same workspace" to two teammates, so it stays the
instance-local routing key only. A **new** normalized-origin-URL function (strip
scheme/credentials/`.git` suffix, lowercase host, canonical
`host/owner/repo`) derives the **cross-machine join key** from `git remote get-url
origin` (the git helpers already live in `internal/handler/git.go`). A workspace
with no remote is local-only: it never registers a remote URL, never joins org
presence or collaboration, and stays invisible to the coordinator.

## Data boundary

Lands alongside the anchor's phase 5 widening of
[data-boundary-enforcement](../../data-boundary-enforcement.md): this connection
is the first governed phone-home channel.

- **Opt-in gate.** Connection requires sign-in **and** explicit coordination
  opt-in. Off by default. A regression test asserts that an anonymous instance
  and a signed-in-but-opted-out instance open no outbound socket and emit no
  frame.
- **Allow-list as the boundary.** Only the enumerated presence fields (sub, host
  label, focus/typing anchors, capability list, workspace remote URLs) cross the
  WSS. Source, diffs, agent output, secrets, env vars, and **local repo paths**
  never do. The allow-list has a regression test, the same shape as the RUM
  scrubber. The manifest's `local_key` is the one local-derived value sent, and
  it is an opaque hash, not a path.

## Non-goals

- **Remote control, projection, comments.** Later children on this same
  connection. Here we only reserve their capability strings and the registry
  indices they reuse.
- **Durable presence or registry.** Ephemeral by decision; no database.
- **A new browser transport.** The browser keeps SSE + `POST /api/presence/focus`
  untouched. The WSS is instance-to-coordinator only.
- **CRDT / live cursors.** Presence is awareness, not co-editing (inherited
  non-goal).

## Dispatch split

This child is `xlarge`; it dispatches as two leaves, the second building on the
first:

- [connection.md](connection-and-presence/connection.md) (foundation, large): the
  outbound WSS, opt-in gate, JWT auth, manifest (persisted instance id +
  git-remote identity), heartbeat, reconnect, teardown, and the coordinator
  registry with the narrow interface the capability leaves consume. No presence.
- [presence.md](connection-and-presence/presence.md) (large): org-wide presence on
  top of the registry, the coordinator-wire deltas, focus/typing relay, the
  browser-SSE re-home (event shape unchanged), and the two-deployment merge.

The acceptance criteria and open questions below are the umbrella view; each leaf
carries its own slice.

## Acceptance criteria

1. Two teammates, each on their own machine, each `wallfacer run` signed in and
   opted in, see each other in the org presence list within 2 s of either
   opening a board; either going offline clears the other's view within 60 s
   (immediately on clean sign-out).
2. A signed-in instance that has **not** opted in opens no outbound socket and
   sends zero frames (test asserts on the connector and on a wire capture).
3. Killing wf.latere.ai drops cross-instance presence but every board stays
   live; instances reconnect with backoff and rebuild the registry, presence
   reappears within one backoff cycle, no manual action.
4. In hosted-shared mode, a member present via a direct browser **and** via
   their own laptop instance appears as exactly **one** row in the presence list
   (merge by `Sub`).
5. Two teammates on the same repo by **git remote URL** (different local paths,
   different `GroupKey`) join the same workspace presence; a teammate on a
   workspace with no remote never appears in org collaboration.
6. The allow-list regression test fails if any field outside the enumerated set
   (notably a local path or diff) is added to a coordinator-bound frame.
7. A focus change made in one teammate's browser surfaces on a peer's card
   viewer avatar within the existing rate-limit window, with the browser
   `event: presence` payload shape unchanged from multi-user.

## Instance identity (decided)

`instance_id` is a **persisted per-data-dir id**, not per-process, so a restart
re-takes the same registry slot and does not cause a presence flap or a dangling
remote-control target. It is generated once and stored at
`<configDir>/instance-id`, the same load-or-create pattern as the public-client
cookie key (`loadOrCreateCookieKey`, `internal/cli/server.go`). The `host_label`
travels alongside for the picker UI but is not the identity.

The coordinator keys the registry on `(principal, instance_id)`. On a new
connection whose `instance_id` already has a (stale) socket registered, the
coordinator **replaces** the old entry rather than adding a second, so a restart
that reconnects before the 60 s liveness timeout does not briefly show two
instances. Two instances on one machine with **different** `--data` dirs get
different ids (each has its own `instance-id` file), which is the correct
disambiguation; two instances sharing a data dir is unsupported (they would also
race the store, already disallowed).

## Open questions

1. **Opt-in surface.** Where the coordination switch lives (a settings toggle in
   `frontend/src/` plus a server-side default) and whether it is org-policy
   enforced or per-instance. Anchor open question 4 keeps it one switch; this
   spec needs the concrete UI affordance and the server default (off).
