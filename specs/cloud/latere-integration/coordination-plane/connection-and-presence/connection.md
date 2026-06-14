---
title: Coordination Connection and Registry
status: drafted
depends_on:
  - specs/cloud/latere-integration/coordination-plane/connection-and-presence.md
affects:
  - internal/cli/web.go
  - internal/cli/server.go
  - internal/auth/
effort: large
created: 2026-06-14
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Coordination Connection and Registry

Dispatchable leaf 1 (foundation) of [connection-and-presence](../connection-and-presence.md).
It builds **only** the wire and the registry it feeds: the outbound WSS, the
opt-in gate, JWT auth, manifest registration, heartbeat, reconnect, teardown, and
the coordinator-side registry with the narrow interface the capability leaves
consume. **No presence, projection, remote control, or comments here**: those are
later leaves that read the registry this one defines. The parent settled the
shape decisions (long-lived WSS, persisted instance id, git-remote identity,
relay-not-mirror); this leaf is their implementation contract.

## Package layout

- `internal/coordinator/` (new): the accept side, mounted on the wf.latere.ai
  front-end server (`internal/cli/web.go`, `RunWeb`, service `wallfacer-web`). It
  adds one handler, `GET /api/coordination/ws`, behind that server's existing
  auth path. It owns the registry (below) and a wire codec shared with the client.
- `internal/coordinator/client` (new subpackage): the dial side, started from the
  local instance's cloud-mode block (`internal/cli/server.go`, around the
  `cloudMode` / `authClient` wiring near line 189). Shares the wire types with the
  accept side. A hosted-shared `wallfacerd` runs both (serves browsers via
  server.go, dials the coordinator via client).

## Opt-in gate and open precondition

The connector dials only when **both** hold:

1. The instance is signed in (a valid `authkit`/`internal/auth` token exists, the
   same one the API uses, loaded as in `internal/cli/auth.go`).
2. Coordination is explicitly **opted in** (the parent's gate; one switch, server
   default **off**). Anonymous instances and signed-in-but-opted-out instances
   dial nothing and emit nothing; local-anonymous behavior is byte-identical.

A regression test asserts zero outbound socket and zero frames when either
condition is false (the data-boundary gate, realized here).

## Handshake and auth

On dial the connector sets `Authorization: Bearer <jwt>` on the WSS handshake.
The coordinator validates it on the **same** `internal/auth` path as every API
request (`auth.Validator`, `PrincipalFromContext`). Validation failure closes the
socket with a 4401 application close code; this is not an auth bypass. The
validated principal (`Sub`, `OrgID`) is the registry key, never a value taken
from the manifest body.

## Manifest registration

First frame after the handshake (instance to coordinator):

```json
{
  "type": "manifest",
  "instance_id": "inst_<persisted>",
  "host_label": "changkun-mbp",
  "version": "wallfacer/<semver>",
  "workspaces": [
    {"remote": "github.com/latere-ai/wallfacer", "local_key": "<GroupKey>"}
  ],
  "capabilities": ["presence", "focus", "projection", "remote-control", "comments"]
}
```

`principal` and `org` are **not** in the manifest body: the coordinator takes them
from the validated JWT, so a client cannot claim another principal. The manifest
is re-sent on any workspace-set change (workspace switch) so the registry tracks
what each instance currently serves.

### Instance id (persisted)

`instance_id` is generated once and stored at `<configDir>/instance-id`, the same
load-or-create pattern as `loadOrCreateCookieKey` (`internal/cli/server.go`). It
survives restart, so a reconnect re-takes the same registry slot. Two instances
with different `--data` dirs get different ids (correct disambiguation); two
sharing a data dir is unsupported (already disallowed by the store).

### Git-remote workspace identity

`workspaces[].remote` is the **canonical git remote URL**, the cross-machine join
key. A new normalize function (strip scheme/credentials/`.git`, lowercase host,
canonical `host/owner/repo`) derives it from `git remote get-url origin` (git
helpers in `internal/handler/git.go`). `local_key` is the per-machine `GroupKey`
(`internal/workspace/groups.go`), an opaque hash sent for the instance's own
routing only; the coordinator never joins on it and it is the one local-derived
value on the wire (a hash, not a path). A workspace with no remote registers no
remote URL and never joins org collaboration.

## Heartbeat, reconnect, teardown

- **Heartbeat.** The connector sends a WSS ping every 20 s. The coordinator drops
  the instance on socket close or **60 s** of silence (the parent's liveness
  window). No separate heartbeat endpoint.
- **Reconnect.** On any drop the connector reconnects with exponential backoff
  (1 s base, 30 s cap, full jitter) while signed-in and opted-in, re-registering
  the full manifest each time. The registry is rebuilt from reconnects, never
  durable.
- **Teardown.** Clean close on sign-out, opt-out, or process exit: send a close
  frame, cancel backoff. The coordinator drops the instance immediately on a
  graceful close (no 60 s wait) and notifies subscribers.

## Wire framing

Every frame is a JSON envelope with a `type` discriminator (`manifest`, and the
capability frame types reserved by later leaves: `presence`, `projection`,
`command`, `spec-comment`). The codec is shared by both sides in
`internal/coordinator`. Unknown `type` is ignored with a logged warning so a
newer peer does not break an older one (forward-compatible).

## Two registry roles (the seam that survives multiple replicas)

wallfacerd runs **more than one replica**. A local instance's WSS load-balances
to **one** replica (its home replica) and stays there for the connection's life
(a single long-lived socket needs no session affinity); only that replica can
`Send` down that socket. So the registry splits into two roles that must not be
conflated, or every cross-replica query returns a partial view:

1. **Local socket table (per replica, in-memory).** Answers only "is this
   `instance_id` terminated **here**, and give me its `Sender`." Used for
   delivery. Each replica has its own; it knows nothing about sockets on other
   replicas. This is `internal/coordinator`'s in-memory table.
2. **Directory (cross-replica query + fan-out).** Answers "who is in this org",
   "which instances serve this `remote`", and routes a frame to an instance whose
   home replica may be a different pod. This is an **interface** with two impls:

```go
type Directory interface {
    Join(p Principal, m Manifest, replicaID string) // register (own replica)
    Leave(instanceID string)
    UpdateManifest(instanceID string, m Manifest)
    Snapshot(org string) []InstanceMeta          // ALL replicas
    InstancesForRemote(remote string) []InstanceMeta
    Publish(target Route, frame []byte)          // cross-replica fan-out
    Subscribe(replicaID string) (<-chan Inbound, func())
}
```

- **`memDirectory`** (single replica / local dev / `replicas: 1`): backed by the
  local table; `Snapshot` is just the local instances. Selected when no Redis is
  configured.
- **`redisDirectory`** (multiple replicas): backed by Valkey (below). `Snapshot`
  reads the shared index; `Publish` fans out cross-replica.

The capability leaves (presence, projection, remote control, comments) consume
`Directory` for queries/fan-out and the local table only for "deliver to a socket
I terminate." The directory carries only registration metadata (principal, org,
instance id, host label, version, workspace remotes, capabilities), never task or
content data.

## Horizontal scaling (multiple replicas)

Cross-replica state reuses the **shared managed Valkey** (`latere-valkey`,
Redis-compatible, TLS `rediss://`), the same cluster lux and sandboxd already
co-tenant; wallfacer adds its own `kubernetes_secret` and namespaces every key
under `wf:coord:*`. Config-gated by `WALLFACER_REDIS_URL`: **absent** selects
`memDirectory` (local dev, current `replicas: 1`, byte-identical to single
process); **present** selects `redisDirectory`. Parsed with `redis.ParseURL`
(TLS auto), matching lux/sandboxd.

### Shared index (Valkey)

Each replica writes the instances **it terminates** into Valkey and refreshes
them by heartbeat; it never writes another replica's instances.

- `wf:coord:inst:<instance_id>` -> hash {principal, org, host_label, version,
  remotes, capabilities, **home_replica**}, `EXPIRE 90s`.
- `wf:coord:org:<org_id>` -> set of `instance_id`, members `EXPIRE`-aligned.
- `wf:coord:ws:<remote>` -> set of `instance_id` serving that workspace.

TTL is the crash-safety net: 60 s liveness, ~90 s key TTL, refreshed on the 20 s
heartbeat; a clean disconnect deletes the keys immediately. A replica that dies
lets its entries expire, so the org view self-corrects within the TTL window
without any cleanup job.

### Cross-replica fan-out (Valkey pub/sub)

A frame whose target socket lives on another replica is published, not sent
directly:

- `wf:coord:ch:org:<org_id>` : presence updates for an org. Each replica
  subscribes for the orgs it serves and re-emits to its local sockets.
- `wf:coord:ch:replica:<replica_id>` : directed messages (remote-control routes a
  command to the replica that owns the target `instance_id`, looked up in the
  index).
- `wf:coord:ch:ws:<remote>` : comment fan-out to instances serving a workspace.

`replica_id` is a per-process random id (Deployment, not StatefulSet); a stale
channel after a pod dies is harmless (no subscribers). Pub/sub is at-most-once,
which is fine: presence carries the full org snapshot (self-healing on the next
update), projection is idempotent on `(instance_id, task_id, seq)`, comments are
ULID-keyed. The message body is the snapshot itself (simplest at dozens per org),
with the Valkey index as the source of truth on reconnect.

### Storage tiering (cache vs system of record)

Valkey is a **cache**, eviction-unsafe under memory pressure, so it holds only
**ephemeral** state: the live index, presence, routing, and pub/sub. **Durable,
authoritative** data does **not** live in Valkey:

- **Spec comments** are cloud-authoritative (the one relay-not-mirror exception),
  so their system of record is **Postgres** (`latere-pg`), not the cache. The
  `wallfacer` database and `WALLFACER_DATABASE_URL` are provisioned (see
  [spec-comments](../spec-comments.md)). Connection and presence themselves are
  Valkey-only and need no Postgres.
- **Metadata projection** live read-model may sit in Valkey (it is a cache of
  record, regenerable by replay); only its long-retention rollups want Postgres
  (see [metadata-projection](../metadata-projection.md)).

### Redis-outage degradation

Extending the relay-not-mirror ethos: if Valkey is unreachable, cross-replica
coordination degrades (presence falls back to each replica's local sockets,
fan-out stops) but **every local board keeps working** and each replica still
serves its own connected instances. Coordination is best-effort infrastructure,
never on the critical path of a local task.

## Non-goals

- Presence aggregation, focus/typing, the browser-SSE re-home: leaf 2
  ([presence](presence.md)).
- Projection, remote control, comments: later capability leaves.
- A durable system of record in this leaf. The Valkey index is ephemeral and
  TTL-expiring (a cache, not a database); authoritative durable data (spec
  comments, projection rollups) lives in Postgres, owned by those leaves.
- Session affinity / sticky load balancing. A single long-lived socket stays on
  its home replica naturally; no LB stickiness config is required.

## Acceptance criteria

1. A signed-in, opted-in `wallfacer run` dials the coordinator, registers its
   manifest, and appears in the registry within 2 s; a signed-in-but-opted-out
   instance dials nothing (test asserts on the connector and a wire capture).
2. A manifest cannot set `principal`/`org`; the coordinator derives them from the
   JWT. A forged manifest principal is ignored (test).
3. Killing the connection drops the registry entry within 60 s (idle) or
   immediately (graceful close); reconnect re-registers and re-takes the **same**
   `instance_id` slot, no duplicate entry.
4. Restarting the local process reuses the persisted `instance_id`; the registry
   shows one instance, not a new one (no flap).
5. Two instances on the same repo by **git remote URL** (different local paths,
   different `GroupKey`) register the same `remote` and both appear under that
   `remote-url` index; an instance on a remote-less workspace registers no remote.
6. The wire codec ignores an unknown frame `type` without dropping the connection.
7. **Multi-replica.** Two instances connected to **different** replicas appear in
   each other's org `Snapshot` (via the Valkey index), and a frame targeting an
   instance whose home replica differs is delivered via pub/sub. With no
   `WALLFACER_REDIS_URL`, the same scenario falls back to single-replica
   (`memDirectory`) and a single process behaves identically.
8. **Replica crash.** When a replica dies, its instances' Valkey keys expire
   within the TTL window and drop out of the org `Snapshot` with no cleanup job.

## Test plan

- Unit: manifest validation drops body-supplied principal/org; uses JWT.
- Unit: instance-id load-or-create is stable across simulated restart.
- Unit: git-remote normalize maps `git@github.com:Org/Repo.git`,
  `https://github.com/Org/Repo`, and `https://github.com/Org/Repo.git` to one key.
- Unit: `memDirectory` and `redisDirectory` pass the same `Directory` contract
  test (table-driven against both impls).
- Integration: connect, manifest, heartbeat, idle-timeout eviction, graceful
  close, reconnect-resumes-same-slot, against an in-process coordinator.
- Integration (multi-replica): two coordinator instances sharing one Valkey (a
  test container or miniredis) see each other's instances in `Snapshot`; a
  pub/sub frame routes to the instance on the other replica.
- Boundary: opted-out and anonymous emit zero frames (mirrors the RUM gate test).

## Open questions

1. **Opt-in surface.** The concrete UI affordance (a settings toggle in
   `frontend/src/`) and whether the default is per-instance or org-policy
   enforced. Server default is off; the UI is the open part (parent open
   question 1).
