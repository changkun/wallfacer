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

## The registry

In-memory, ephemeral, rebuilt from reconnects, never persisted. One mutex guards
three indices, all derived from the validated principal:

- `principal -> []instance` : every live connection for a `Sub` (one person, many
  machines). Each entry holds the manifest plus the live socket handle, so the
  capability leaves (presence, projection, remote control, comments) reuse the
  same connection without their own plumbing.
- `org -> set<principal>` : present principals per org, recomputed on join/leave.
- `remote-url -> set<instance>` : instances serving a given cross-machine
  workspace, for collaboration fan-out (comments leaf). Built here so the registry
  shape is settled even though presence aggregates per org.

### Interface (the contract the capability leaves consume)

```go
type Registry interface {
    Join(p Principal, inst Instance) // on manifest
    Leave(instanceID string)         // on close/timeout
    UpdateManifest(instanceID string, m Manifest)
    Snapshot(org string) []Instance  // current instances in an org
    InstancesForRemote(remote string) []Instance
    Subscribe() (<-chan RegistryEvent, func()) // join/leave/manifest deltas
}
```

`Subscribe` is how presence and remote-control learn of membership changes
without touching socket code. The registry carries only registration metadata
(principal, org, instance id, host label, version, workspace remotes,
capabilities), never task or content data.

## Non-goals

- Presence aggregation, focus/typing, the browser-SSE re-home: leaf 2
  ([presence](presence.md)).
- Projection, remote control, comments: later capability leaves.
- Durable registry or any database. Ephemeral by decision.

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

## Test plan

- Unit: manifest validation drops body-supplied principal/org; uses JWT.
- Unit: instance-id load-or-create is stable across simulated restart.
- Unit: git-remote normalize maps `git@github.com:Org/Repo.git`,
  `https://github.com/Org/Repo`, and `https://github.com/Org/Repo.git` to one key.
- Integration: connect, manifest, heartbeat, idle-timeout eviction, graceful
  close, reconnect-resumes-same-slot, against an in-process coordinator.
- Boundary: opted-out and anonymous emit zero frames (mirrors the RUM gate test).

## Open questions

1. **Opt-in surface.** The concrete UI affordance (a settings toggle in
   `frontend/src/`) and whether the default is per-instance or org-policy
   enforced. Server default is off; the UI is the open part (parent open
   question 1).
