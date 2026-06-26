---
title: Coordination Presence
status: stale
depends_on:
  - specs/cloud/latere-integration/coordination-plane/connection-and-presence/connection.md
affects:
  - internal/handler/
  - internal/cli/web.go
  - frontend/src/
effort: large
created: 2026-06-14
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# Coordination Presence

Dispatchable leaf 2 of [connection-and-presence](../connection-and-presence.md),
built on [connection](connection.md). It delivers **org-wide presence**: teammates
each running their own local instance see one presence list, and focus/typing
awareness crosses machines. It consumes the connection and the registry interface
from leaf 1; it adds no transport of its own. multi-user-collaboration's presence
UI (header avatar stack, per-card viewer avatars, "X is typing") ships unchanged;
only the **source** of the list moves to the coordinator.

## What leaf 1 provides

- The live WSS per instance and the `presence` frame type reserved on it.
- The **`Directory`** seam: `Snapshot(org)` / `InstancesForRemote(remote)` (which
  read across **all replicas**, via Valkey under multi-replica) and the local
  socket table (deliver to a socket this replica terminates). Presence MUST query
  the `Directory`, never the local table, or each replica shows a partial org
  list.
- Cross-replica fan-out: `Directory.Publish` to the org channel and
  `Subscribe(replicaID)` for inbound, so a focus hint reaches peers whose home
  replica differs.
- The validated `(Sub, OrgID)` per connection and each instance's served
  `remote` workspaces.

This leaf adds the presence aggregation on top, the coordinator-wire deltas, and
the browser re-home.

## The two transports stay separate

Restating the parent's load-bearing rule, because this leaf touches both:

1. **Browser to its instance: unchanged.** The board keeps the existing SSE
   (`internal/handler/stream.go`, `GET /api/tasks/stream`) and
   `POST /api/presence/focus`. The browser-facing `event: presence` payload
   `{present: [{id, name, avatar, role, focus}]}` does **not** change shape.
2. **Instance to coordinator: the new presence frames** on leaf 1's WSS.

The browser never learns there is a second wire.

## Path of a focus hint

1. **Browser to its instance:** `POST /api/presence/focus`
   `{task_id, thread_id, editing}`, unchanged semantics, server rate limit
   **500 ms** per client, typing TTL **3 s** (multi-user's numbers).
2. **Instance to coordinator:** the instance relays the hint up the WSS as a
   `presence` delta.
3. **Coordinator:** folds the hint into the principal's registry-adjacent presence
   state and fans the merged org snapshot to **peer instances** in the same org
   (and, for the comments leaf, the same `remote`).
4. **Each peer instance:** emits the merged list on its **own** browser SSE as the
   existing `event: presence`.

## Coordinator-wire shapes

Instance to coordinator (presence delta):

```json
{
  "type": "presence",
  "instance_id": "inst_…",
  "focus": {"remote": "github.com/…", "task_id": "…", "thread_id": "…",
            "editing": "task:<id>.prompt", "ttl_ms": 3000}
}
```

`principal`/`org` are taken from the connection's validated JWT, not the frame.

Coordinator to instance (org aggregate, full snapshot on each change):

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

The wire carries **only** allow-listed presence hints (sub, host label, focus
anchors, typing target). No source, diff, prompt text, or repo path. Membership is
small (dozens per org), so full-snapshot fan-out on each change is fine, the same
fan-out multi-user already accepts.

## PII expansion is instance-side

The coordinator never holds names, emails, or avatars. The receiving instance
hydrates each `sub` into the browser `{id, name, avatar, role, focus}` shape from
its **own** org-member cache (`GET /api/org/members`, multi-user), so PII
expansion happens instance-side, not on the wire. An unknown `sub` renders the
muted chip.

## Two-deployment reconciliation

A hosted-shared `wallfacerd` has presence from two sources at once and must merge
them; local-first is the degenerate case where one source is empty.

- **Direct browsers** on a hosted instance, tracked in its **process-local
  presence map** (multi-user: an active SSE subscription means present).
- **Its own outbound WSS** `presence-snapshot`, carrying every other instance in
  the org.

Merge **by `Sub`** at the single point where each browser's `present[]` is
rendered for the SSE emit:

1. Take the process-local present set (direct browsers here).
2. Take the coordinator `presence-snapshot` for this org.
3. Union by `Sub`. A member present via a direct browser **and** their own laptop
   collapses to **one** row; multiple instances/sessions for one `Sub` flatten;
   focus hints merge, most-recent wins (multi-user's last-write for fuzzy state).

Because the merge is at the emit point feeding the unchanged `event: presence`,
the browser UI is identical across both deployment models.

## Liveness and decay

- A `presence` hint with no refresh decays per its `ttl_ms` (typing) or is cleared
  when the instance leaves the registry (leaf 1's 60 s idle / immediate graceful
  close). The coordinator re-broadcasts the org snapshot on any change.
- A coordinator outage degrades cross-instance presence to process-local without
  dropping the board (leaf 1 reconnects; presence reappears within one backoff
  cycle).

## Non-goals

- The connection, registry, manifest, heartbeat: leaf 1.
- Remote control, projection, comments: later capability leaves (they reuse this
  leaf's `remote`-scoped fan-out for comments).
- CRDT / live cursors. Presence is awareness, not co-editing.

## Acceptance criteria

1. Two teammates, each on their own machine, signed in and opted in, see each
   other in the org presence list within 2 s of either opening a board; either
   going offline clears the other's view within 60 s (immediately on clean
   sign-out).
2. A focus change in one teammate's browser surfaces on a peer's card viewer
   avatar within the existing rate-limit window, with the browser `event: presence`
   payload shape unchanged from multi-user.
3. A typing hint clears on the peer within its 3 s TTL after the typist stops.
4. In hosted-shared mode, a member present via a direct browser **and** their own
   laptop instance appears as exactly **one** row (merge by `Sub`).
5. The coordinator wire carries no name/email/avatar/path: a wire capture shows
   only sub + host label + focus anchors; PII appears only after instance-side
   hydration.
6. Killing the coordinator drops cross-instance presence but every board stays
   live; presence reappears within one backoff cycle, no manual action.

## Test plan

- Unit: merge-by-`Sub` collapses duplicate sessions/instances to one row and
  merges focus most-recent-wins.
- Unit: presence-snapshot hydration maps `sub` to the member-cache entry; unknown
  sub yields the muted chip.
- Integration: two in-process instances on one coordinator exchange presence;
  focus relay round-trips browser to peer; typing TTL decays.
- Boundary: a wire capture asserts no name/email/avatar/repo-path field on any
  `presence` frame.
