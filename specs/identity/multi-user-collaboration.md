---
title: Multi-User Collaboration (Org-Scoped Board)
status: drafted
depends_on:
  - specs/identity/authentication.md
  - specs/cloud/multi-tenant.md
  - specs/foundations/storage-backends.md
affects:
  - internal/store/
  - internal/handler/
  - internal/runner/
  - internal/workspace/
  - internal/planner/
  - internal/apicontract/
  - ui/
  - docs/internals/
  - docs/guide/
effort: xlarge
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---

# Multi-User Collaboration (Org-Scoped Board)

## Problem

Wallfacer today is a **single-user, single-process server**. Every request is anonymous once the shared bearer token matches, every in-memory map is process-global, and every state-changing action — creating a task, dragging it to *In Progress*, submitting feedback, editing the workspace `AGENTS.md`, toggling autopilot, rotating the Anthropic API key — is indistinguishable from any other. There is no concept of "who did this."

This blocks the cloud movement in ways the existing `cloud/multi-tenant.md` spec does **not** solve:

1. **Teams are the unit that buys software, not individuals.** A team on `wallfacer.latere.ai` wants a **shared board** — multiple engineers seeing the same task list, same planning threads, same oversight history — not N isolated per-user instances that only communicate through GitHub PRs.
2. **No attribution.** Today a task card's timeline says *"moved to In Progress at 14:02"*. In a shared board it must say *"Alice moved to In Progress"* — and the store has no actor field to carry that.
3. **No presence.** Two engineers dragging the same card, editing the same spec, or chatting in the same planning thread have no idea the other is there. They step on each other's work silently.
4. **No permission boundaries.** Everything is admin-equivalent. A junior contributor can rotate the org API key, disable autopilot, delete production tasks, or edit the shared `AGENTS.md` template — no different from the team lead who pays the bill.
5. **Automation loops have no identity.** Autopilot promotion, auto-retry, title generation, oversight runs all happen "somewhere in the server." In an audit-conscious multi-user setup these need a clearly-named service actor, and the toggle that enables them needs an admin gate.

Until identity, attribution, presence, and RBAC exist in the data model and the wire protocol, *"wallfacer runs in the cloud"* only means *"each user runs their own private wallfacer in the cloud."* That is a hosted single-tenant product, not a collaboration platform. This spec defines the work needed to close that gap.

---

## Relationship to `cloud/multi-tenant.md`

`cloud/multi-tenant.md` explicitly **rejects** the shared-multi-user-server design, citing the cost of per-user scoping every in-memory map, workspace manager, automation loop, SSE stream, and circuit breaker. Its answer is **one wallfacer instance per user**, hibernated when idle.

This spec **reframes the tenant unit**: from *user* to *organization* (team). It does not overturn the instance-per-tenant decision; it redefines what a tenant is.

| Dimension | `multi-tenant.md` (original) | This spec (org-scoped) |
|---|---|---|
| Tenant = | One user | One org (≥1 user) |
| Instances per user | 1 dedicated | 0 — user logs into an org instance |
| Instances per org | n/a | 1 shared instance, hibernated when all members idle |
| Data isolation boundary | User | Org |
| In-memory scoping | Not needed (process is single-user) | Not needed (process is single-org) |
| Collaboration within a tenant | n/a | **Yes — primary goal** |
| Cross-tenant access | Forbidden (infrastructure layer) | Forbidden (infrastructure layer) |

The benefit is that `cloud/multi-tenant.md`'s architectural simplicity survives: the wallfacer process still serves **one tenant**, so we do not rewrite every store to be keyed by `tenant_id`. But within that one tenant, there can be many users, each authenticated, each attributed, each with a role.

The spec maintains three deployment modes from `multi-tenant.md` and adds collaboration as a new axis:

| Mode | Runs on | Tenant | Users | Needs this spec? |
|---|---|---|---|---|
| **Local anonymous** | User machine | — | 1 anonymous | No |
| **Local authenticated** | User machine | — | 1 authenticated (just attribution) | Partial — just actor fields |
| **Cloud hosted (personal)** | latere.ai K8s | Personal org of 1 | 1 | Partial — same code path as team, one member |
| **Cloud hosted (team)** | latere.ai K8s | Org of N | 1..N concurrent | Full — this spec |

Local authenticated benefits from the identity plumbing (event timeline shows *"Alice moved to In Progress"* even though there is only ever one Alice on the machine). Cloud personal is architecturally identical to cloud team with a membership of one. There is no fork in the code path.

**Alternatives considered and rejected:**

- **(a) Reverse the `multi-tenant.md` decision** — make a single process multi-tenant, keyed by `tenant_id` everywhere. Paid for once, serves both intra-org and cross-org. Rejected because `multi-tenant.md` already analyzed this cost and the argument is still correct — the refactor touches the hot path of the runner, the store, the workspace manager, every SSE subscriber, and every automation loop, with no product benefit that the org-scoped instance model does not already deliver.
- **(b) Read-only observers only** — other users can watch the board but only the owner can mutate. Rejected because this is not "collaboration" in any useful sense; it is *pair-programming-by-screensharing-over-HTTPS*. The user's explicit ask includes "who operated what," "multiple users in the same chat," and "role-based automation permissions," none of which a read-only observer mode addresses.

---

## Identity Model

Wallfacer consumes identity produced by the `auth.latere.ai` auth service (`specs/identity/authentication.md`). The JWT claims — `sub`, `org_id`, `roles[]`, `email`, `principal_type`, `is_superadmin` — are already defined there. This spec is **purely a consumer** of those claims; it adds no user model, no org model, no password storage, no invite flow. All of that is owned by the auth service.

### Identity types wallfacer persists

| Type | Source | Purpose | Example |
|---|---|---|---|
| `PrincipalID` | JWT `sub` | Actor on every mutation | `u_01J8B…` |
| `OrgID` | JWT `org_id` | Tenant scoping (cloud only) | `org_01J…` |
| `ServiceActor` | Constant sentinel | Automation-loop mutations | `service:autopilot`, `service:auto-retry`, `service:title-gen`, `service:oversight`, `service:auto-test` |
| `PrincipalKind` | JWT `principal_type` | Distinguish user vs service vs agent | `"user"`, `"service"`, `"agent"` |

A `ServiceActor` is a reserved `principal_id` namespace (`service:<name>`) used when wallfacer's own automation mutates state on nobody's behalf. It is not minted by the auth service; it is a local constant. The UI renders service actors with a distinct chip color so the timeline is unambiguous: *"autopilot moved task X to In Progress"*.

### Roles

Role names are issued by the auth service in the JWT `roles[]` claim. Wallfacer **does not define** the universe of role names, but it **does define** what each known role is authorized to do within the wallfacer surface. The minimum viable role set wallfacer recognizes:

| Role | Within wallfacer |
|---|---|
| `admin` | All editor permissions + settings, env, API keys, sandbox routing, autopilot toggles, instruction templates, member removal (via auth service), task force-delete, audit log access |
| `editor` | Create/update/cancel/feedback/delete-own tasks; edit planning specs; edit instructions; send planning chat; dispatch specs |
| `viewer` | Read-only: board, task events, diffs, oversight, usage, planning chat history, spec tree |

Users with no known role (or with roles outside `{admin, editor, viewer}`) default to `viewer`. `is_superadmin=true` in the JWT overrides all per-role checks (used for latere.ai operations, not for regular members).

### Fallback: local / anonymous modes

In deployments where `AUTH_URL` is unset, wallfacer continues to run in single-user anonymous mode. In that mode every mutation is attributed to a sentinel `principal:local` actor and the RBAC layer is bypassed (equivalent to everyone being `admin`). This preserves the current behavior exactly — no regression for self-hosted anonymous use.

---

## Data Model Changes

The single largest migration in this spec. Every record that represents a user action must gain an actor field. Records that represent org-scoped data (cloud mode only) must gain an org field; in local/anonymous mode the org field is always null.

### Fields added

The canonical shape is `Actor { ID: string, Kind: PrincipalKind }`. It is a small inline struct (not a pointer to a separate table) so it travels with every snapshot.

| Store record | File | New fields | Meaning |
|---|---|---|---|
| `Task` | `internal/store/models.go` | `CreatedBy Actor`, `UpdatedBy Actor`, `OrgID *string` | Task creator and last mutator |
| `TaskEvent` | `internal/store/models.go` | `Actor Actor` | Who caused this event — critical for the timeline |
| `Tombstone` | `internal/store/models.go` | `DeletedBy Actor` | Who soft-deleted |
| `TaskFeedback` (event payload) | `internal/store/models.go` | `Author Actor` | Waiting-task feedback author |
| `RefinementSession` / `RefinementJob` | `internal/store/models.go` | `StartedBy Actor` | Who requested refinement |
| `PlanningMessage` | `internal/planner/conversation.go` | `Author Actor` | Who wrote this chat turn (user vs agent vs another user) |
| `PlanningThread` | `internal/planner/conversation.go` | `CreatedBy Actor`, `Visibility {private, shared}` | Private = only creator can see; shared = any org member |
| `WorkspaceInstructions` (AGENTS.md edit log) | `internal/prompts/` | `UpdatedBy Actor`, `UpdatedAt time.Time` | Who last edited the shared instruction template |
| `EnvEdit` (new, audit-log entry) | `internal/store/audit.go` (new) | `Actor`, `Diff []string` (field names only, never values) | Admin edit of `.env` |
| `ConfigEdit` (new, audit-log entry) | `internal/store/audit.go` (new) | `Actor`, `Field`, `Old`, `New` | Admin toggle of autopilot/automation |
| `APIKeyAction` (new, audit-log entry) | `internal/store/audit.go` (new) | `Actor`, `Action {rotate, reveal, test}` | Sensitive env access |
| `SpecDispatch` event | `internal/spec/` | `Actor` | Which team member dispatched this spec |
| `SystemPromptOverride` | existing | `UpdatedBy Actor` | Who edited the built-in prompt template |
| `PromptTemplate` | existing | `CreatedBy Actor`, `Visibility {private, shared}` | Private-to-user vs shared-with-org |

### On-disk layout changes

Per-task directories (`data/<task-uuid>/`) gain `actors.json`, an append-only log redundant with the events stream but cheaper to read for the explorer/diff views. Its primary value is recovery: if the events file is corrupted on a workspace switch, `actors.json` still answers "who created this task."

Audit log (`data/.audit/`) is a new directory with one append-only file per day (`data/.audit/2026-04-18.ndjson`). Separate from task events because it lives at the workspace/org level, not the task level, and retention is different (audit retention is configurable per org; task retention is not).

### Migration

Existing per-task files lack actor fields. On first load after upgrade:

1. `CreatedBy` and `UpdatedBy` default to `Actor{ID: "principal:local", Kind: "user"}` — preserves a plausible actor without pretending we know who did it.
2. `OrgID` stays null (local mode) until the workspace is migrated into an org on the cloud.
3. `TaskEvent` entries without `Actor` render as `"unknown"` in the UI with a muted chip.
4. Audit log files only exist from the upgrade date forward.

The migration is non-destructive: files are rewritten atomically (temp + rename) only when the task is next mutated. This matches the existing `internal/store/migrate.go` pattern.

### Workspace key: still fingerprint-based

The existing workspace key is a SHA-256 of sorted workspace paths. Cloud mode introduces a second dimension — the org — but the path fingerprint is still what identifies a *workspace group*. Cloud path is `orgs/<org_id>/workspaces/<fingerprint>/`. Local path is unchanged (`~/.wallfacer/<fingerprint>/`). The in-process store sees only one workspace at a time, so it does not need org-aware keying; the per-instance wallfacer serves one org exclusively.

---

## Real-Time Collaboration

Four concerns, in order of complexity:

### 1. Presence (who is here)

**Goal.** Every member opening the board sees a list of other members who are currently connected.

**Mechanism.** The existing `/api/tasks/stream` SSE connection is the liveness signal. A user is *present* iff they have an active SSE subscription. On connect: `store.Presence.Join(PrincipalID, connectionID)`. On disconnect (or 60 s silence): `Leave`. The presence map is process-local (no persistence needed).

A new delta event `event: presence` rides the existing tasks-stream with payload `{present: [{id, name, avatar, focus}]}`. Clients render a stack of avatar chips in the header. On any presence change the server broadcasts the full list to all current subscribers (fan-out is cheap; membership is small, max ~50 per org).

No new SSE endpoint. No WebSocket. The single bidirectional surface we already have (`/api/terminal/ws`) stays scoped to terminal.

### 2. Activity focus (what are they looking at)

**Goal.** Show which card other members currently have open. A small avatar appears on the top-right of the card when someone is viewing it.

**Mechanism.** The client sends focus changes via a cheap `POST /api/presence/focus` (not SSE — it is a write). Payload: `{task_id: "…" | null, thread_id: "…" | null}`. The server updates the presence entry and broadcasts. Focus changes are rate-limited per client to one every 500 ms (server-side, to guard against a rogue client flapping).

### 3. Typing / live-edit awareness (are they composing)

**Goal.** In planning chat, a "X is typing…" indicator. In task prompt editing, a lock icon while someone is mid-edit.

**Mechanism.** Same focus endpoint carries an optional `editing: "task:<id>.prompt" | "thread:<id>.compose" | null`. TTL is 3 seconds from last heartbeat; client refreshes while the field is focused. On lapse the server clears. This is *awareness*, not *locking* — two users can type into the same field simultaneously; the conflict resolution lives in section 4.

### 4. Concurrent mutation conflicts

**Goal.** Prevent silent clobber when two users mutate the same resource.

Not all resources need the same strategy. The matrix below is the full design:

| Resource | Strategy | Why |
|---|---|---|
| **Task position (drag)** | Last-write-wins, broadcast immediately | Position is fuzzy anyway; users can always redrag; no real conflict |
| **Task status transitions** | **Optimistic concurrency (`If-Match` / version)** | Two users dragging to *In Progress* simultaneously — one wins, the other gets 409 and the UI shows *"Alice already moved this card"* |
| **Task prompt edit** | Optimistic concurrency with `updated_at` | Intentional conflict prompt; the UI shows both versions and asks the user to merge — same as GitHub's file-edit conflict |
| **Planning chat message** | Append-only, always OK | Each message is a new record; no conflict possible |
| **Planning thread rename / archive** | Optimistic concurrency | Cheap to conflict; rare race |
| **Spec file (via file explorer)** | `If-Match` on content hash | Reuse existing explorer flow; conflict returns 409 with latest content |
| **Workspace AGENTS.md** | Optimistic concurrency with `updated_at` | Shared template; conflict rare but high-impact |
| **Env vars, server config** | Admin-only + optimistic concurrency | Admin mutations go through audit log regardless |
| **Automation toggles** | Admin-only + optimistic concurrency | Same as config |

Every versioned record grows an `Version int64` field that monotonically increments on write. The wire contract:

- Mutations accept `If-Match: <version>` or a JSON `version` field.
- On mismatch: `409 Conflict` with the current representation and version.
- The UI on receiving 409 refreshes the resource and either auto-applies (for status transitions — show a toast) or prompts the user to merge (for prompt/text edits).

Versioning is per-resource, not per-tenant-monotonic. The existing `Seq` on the delta stream stays the way it is — it is a transport-layer broadcast sequence, not a record version.

### Why not CRDT / per-field locking

**Non-goal.** Prompt text is short, edits are rare, and the board is not a multiplayer document. CRDTs (yjs / automerge) and per-field locks (Google Docs) are both plausible and both overkill for this product. We use optimistic concurrency with clear conflict UX and move on. If the product evolves toward inline collaborative prompt editing, that is a separate spec.

---

## Chat with Identity (Planning Threads Extension)

Extends `specs/local/spec-coordination/spec-planning-ux/planning-chat-threads.md` (drafted) without overturning it.

### Per-message author

Every `PlanningMessage` gains `Author Actor`. User messages carry the logged-in user's identity. Agent messages carry a synthetic actor derived from the agent sandbox (`agent:claude-code`, `agent:codex`) — same shape, different `Kind`.

### Thread visibility

Threads gain a `Visibility` field with two values:

- **`private`** (default) — only the thread creator can see, read, send, or dispatch from this thread. Peer members see nothing.
- **`shared`** — any org member with `editor` or `admin` can read and send in the thread. Viewers can read but not send.

The existing thread tab bar gains a lock icon for `private` threads. Thread creation UI offers the two options; default is `private` (least surprising). Switching a thread from `private` to `shared` requires admin, because the decision to expose prior chat turns to peers is non-trivial and the creator may not have anticipated it.

### Per-thread exec isolation

The existing single-planner-container-per-workspace-group invariant stays. Concurrent thread sends from different users are queued, same FIFO as today. The UI shows *"Alice is running a turn in thread **auth-refactor**"* so a second user doesn't wonder why their own message is pending.

### Undo scoping

Per `planning-chat-threads.md`, undo is thread-scoped via git revert with `Plan-Thread: <id>` trailer. This spec adds: **undo is authorized for the thread creator and admins**. Editors who happened to contribute a turn in the thread cannot undo the whole thread.

---

## RBAC — Endpoint-by-Endpoint Matrix

Role gate on every route. The matrix below is the **authoritative gate definition**; deviations from it are bugs.

Legend: `A` = admin, `E` = editor, `V` = viewer. `self` means the actor can only act on their own resource.

### Tasks

| Endpoint | V | E | A | Notes |
|---|:-:|:-:|:-:|---|
| `GET /api/tasks*` | ✓ | ✓ | ✓ | All read endpoints |
| `POST /api/tasks` | — | ✓ | ✓ | Create |
| `POST /api/tasks/batch` | — | ✓ | ✓ | Batch create |
| `PATCH /api/tasks/{id}` | — | ✓ | ✓ | Status/prompt/goal/timeout/deps |
| `POST /api/tasks/{id}/feedback` | — | ✓ | ✓ | Waiting task feedback |
| `POST /api/tasks/{id}/done` | — | ✓ | ✓ | Mark done |
| `POST /api/tasks/{id}/cancel` | — | ✓ | ✓ | Cancel |
| `POST /api/tasks/{id}/resume` | — | ✓ | ✓ | Resume failed |
| `POST /api/tasks/{id}/sync` | — | ✓ | ✓ | Rebase worktrees |
| `POST /api/tasks/{id}/test` | — | ✓ | ✓ | Test verification |
| `POST /api/tasks/{id}/archive` | — | ✓ | ✓ | Archive |
| `POST /api/tasks/{id}/unarchive` | — | ✓ | ✓ | Unarchive |
| `POST /api/tasks/archive-done` | — | — | ✓ | Bulk — admin only |
| `DELETE /api/tasks/{id}` | — | `self` | ✓ | Editors delete own tasks; admin deletes any |
| `POST /api/tasks/{id}/restore` | — | `self` | ✓ | Restore own; admin restores any |
| `POST /api/tasks/generate-titles` | — | — | ✓ | Bulk — admin only |
| `POST /api/tasks/generate-oversight` | — | — | ✓ | Bulk — admin only |

### Refinement / Ideation

| Endpoint | V | E | A | Notes |
|---|:-:|:-:|:-:|---|
| `POST /api/tasks/{id}/refine` | — | ✓ | ✓ | |
| `DELETE /api/tasks/{id}/refine` | — | `self` | ✓ | Cancel only your own refinement |
| `POST /api/tasks/{id}/refine/apply` | — | `self` | ✓ | Apply only your own refinement result |
| `POST /api/tasks/{id}/refine/dismiss` | — | `self` | ✓ | |
| `GET /api/ideate` | ✓ | ✓ | ✓ | |
| `POST /api/ideate` | — | — | ✓ | Ideation creates tasks on behalf of the org — admin only |
| `DELETE /api/ideate` | — | — | ✓ | |

### Planning / Specs

| Endpoint | V | E | A | Notes |
|---|:-:|:-:|:-:|---|
| `GET /api/specs/*`, `/api/planning/*` (reads) | ✓ | ✓ | ✓ | Shared threads only; private thread reads enforced by thread creator |
| `POST /api/specs/dispatch` | — | ✓ | ✓ | |
| `POST /api/specs/undispatch` | — | ✓ | ✓ | |
| `POST /api/specs/archive` | — | ✓ | ✓ | |
| `POST /api/planning/messages` | — | ✓ | ✓ | Editor on shared threads; creator+admin on private |
| `DELETE /api/planning/messages` | — | `self` | ✓ | Creator clears own thread; admin clears any |
| `POST /api/planning/undo` | — | `self` | ✓ | Thread creator only, plus admin |
| `POST /api/planning/threads` | — | ✓ | ✓ | |
| `PATCH /api/planning/threads/{id}` | — | `self` | ✓ | Rename own threads |
| `POST /api/planning/threads/{id}/archive` | — | `self` | ✓ | |
| `POST /api/planning/threads/{id}/activate` | ✓ | ✓ | ✓ | UI preference — per-user, not a mutation of shared state |
| `POST /api/planning` (start container) | — | ✓ | ✓ | |
| `DELETE /api/planning` (stop container) | — | — | ✓ | Shared resource; admin only |

### File Explorer / Terminal

| Endpoint | V | E | A | Notes |
|---|:-:|:-:|:-:|---|
| `GET /api/explorer/*` | ✓ | ✓ | ✓ | |
| `PUT /api/explorer/file` | — | ✓ | ✓ | |
| `GET /api/terminal/ws` | — | — | ✓ | **Admin only** — terminal gives shell access to the wallfacer server pod |

### Configuration & Settings (admin-only)

| Endpoint | V | E | A | Notes |
|---|:-:|:-:|:-:|---|
| `GET /api/config` | ✓ | ✓ | ✓ | Read surfaces UI defaults; safe |
| `PUT /api/config` | — | — | ✓ | |
| `GET /api/env` | — | — | ✓ | Masked, but the mere shape is sensitive |
| `PUT /api/env` | — | — | ✓ | |
| `POST /api/env/test` | — | — | ✓ | |
| `GET /api/system-prompts`, `PUT/DELETE /api/system-prompts/{name}` | ✓/—/— | ✓/—/— | ✓ | Read is safe; writes change behavior for everyone |
| `GET /api/instructions` | ✓ | ✓ | ✓ | |
| `PUT /api/instructions` | — | ✓ | ✓ | Shared `AGENTS.md` — editor allowed; event-sourced so reversible |
| `POST /api/instructions/reinit` | — | — | ✓ | Destructive regenerate — admin only |
| `GET /api/templates` | ✓ | ✓ | ✓ | |
| `POST /api/templates` | — | ✓ | ✓ | |
| `DELETE /api/templates/{id}` | — | `self` | ✓ | Delete own templates |

### Workspaces

| Endpoint | V | E | A | Notes |
|---|:-:|:-:|:-:|---|
| `GET /api/workspaces/browse` | — | — | ✓ | Reveals server filesystem layout |
| `PUT /api/workspaces` | — | — | ✓ | Switching workspace groups affects every member |

### Git Operations

| Endpoint | V | E | A | Notes |
|---|:-:|:-:|:-:|---|
| `GET /api/git/status`, `GET /api/git/stream`, `GET /api/git/branches` | ✓ | ✓ | ✓ | |
| `POST /api/git/push` | — | ✓ | ✓ | |
| `POST /api/git/sync` | — | ✓ | ✓ | |
| `POST /api/git/rebase-on-main` | — | ✓ | ✓ | |
| `POST /api/git/checkout` | — | — | ✓ | Switches workspace state for the whole team |
| `POST /api/git/create-branch` | — | ✓ | ✓ | |
| `POST /api/git/open-folder` | — | — | ✓ | Not meaningful in cloud mode — rejected outright |

### OAuth / Auth / Admin

| Endpoint | V | E | A | Notes |
|---|:-:|:-:|:-:|---|
| `POST /api/auth/{provider}/*` (sandbox-creds OAuth) | — | — | ✓ | These write tokens into the org-shared env |
| `POST /api/admin/rebuild-index` | — | — | ✓ | |
| `GET /api/debug/*` | — | — | ✓ | Server internals |
| `GET /api/containers`, `GET /api/stats`, `GET /api/usage` | ✓ | ✓ | ✓ | Usage transparency |
| `GET /api/images*`, `POST/DELETE /api/images*` | — | — | ✓ | Sandbox image cache management |

### New endpoints this spec adds

| Endpoint | V | E | A | Purpose |
|---|:-:|:-:|:-:|---|
| `GET /api/auth/me` | ✓ | ✓ | ✓ | Current user's id, name, roles (from JWT) |
| `GET /api/org/members` | ✓ | ✓ | ✓ | Members of the current org (fetched from auth service, cached) |
| `POST /api/presence/focus` | ✓ | ✓ | ✓ | Update focus hint |
| `GET /api/audit` | — | — | ✓ | Query audit log (filter by actor, action, date) |
| `GET /api/audit/stream` | — | — | ✓ | SSE live audit tail |

### Implementation

A central `authz.Require(roles ...Role)` HTTP middleware factory, applied per route when the router is built in `internal/handler/handler.go`. The role set is read from the JWT claims once per request and cached in the context. `self`-scoped routes run the role check first, then call `authz.RequireSelf(ctx, resourceOwnerID)` which lets through when the caller is the owner or has `admin`. Tests hit the matrix table-driven to catch regressions.

---

## Automation Loops as a Service Actor

Every automation mutation is currently faceless. This spec gives them names:

| Loop | Service actor | Admin gate for enable/disable |
|---|---|---|
| Autopilot (backlog → in_progress promotion) | `service:autopilot` | `PUT /api/config` (admin) |
| Auto-retry (failed retry on budget) | `service:auto-retry` | `PUT /api/config` (admin) |
| Auto-submit (commit + push on done) | `service:auto-submit` | `PUT /api/config` (admin) |
| Auto-sync (rebase on main) | `service:auto-sync` | `PUT /api/config` (admin) |
| Auto-test (test container post-completion) | `service:auto-test` | `PUT /api/config` (admin) |
| Title generation | `service:title-gen` | Always on; no toggle |
| Oversight generation | `service:oversight` | `PUT /api/config` (admin) |
| Commit-message generation | `service:commit-msg` | Always on |
| Soft-delete retention prune | `service:retention` | Never toggleable |

Service actors appear in `TaskEvent.Actor`, in `ConfigEdit` (as the actor for cascading changes), and on audit-log entries for their own mutations.

Every config change that toggles an automation loop writes two records: a `ConfigEdit` audit entry with the admin actor, and a `task.event` on each task that is immediately affected (e.g. autopilot being disabled mid-queue emits *"autopilot disabled by Alice — 3 waiting tasks remain in backlog"* on the stats dashboard).

### Per-member autopilot budget

A member with role `editor` can create tasks that the admin-enabled autopilot will promote. Per-org config (`admin`-only) adds:

- `WALLFACER_AUTOPILOT_ALLOWED_ACTORS` — optional allow-list of principal IDs whose tasks autopilot will promote. Empty (default) means "any member."
- `WALLFACER_AUTOPILOT_PER_ACTOR_LIMIT` — cap on concurrent in_progress tasks promoted by autopilot per actor. Prevents one member hogging the entire parallel-worker budget.

Both are org-level config, admin-editable via `PUT /api/config`.

---

## Audit Log

Append-only, admin-read. Covers every write the RBAC matrix labels as admin-only, plus every soft-delete and every automation-config change.

### What goes in

1. `env.edit` — admin edited `.env`. Payload: actor, timestamp, list of **field names** (never values). `ANTHROPIC_API_KEY` → `{"fields": ["ANTHROPIC_API_KEY"]}` — that is the entire diff.
2. `env.test` — `POST /api/env/test` called. Payload: actor, timestamp, provider.
3. `config.edit` — admin changed autopilot / automation / workspace. Payload: actor, timestamp, field, old value, new value (only non-secret fields).
4. `workspace.switch` — workspace group changed. Payload: actor, old fingerprint, new fingerprint.
5. `git.checkout` / `git.rebase-on-main` — branch-level mutation. Payload: actor, workspace, from/to.
6. `task.delete.admin` — admin force-deleted another user's task. Payload: actor, task, original-creator.
7. `thread.visibility-change` — private → shared or vice versa. Payload: actor, thread, old, new.
8. `instructions.reinit` — workspace instructions regenerated.
9. `system-prompt.override-write` / `system-prompt.override-delete`.
10. `apikey.rotate` / `apikey.reveal` — sensitive env surface access.
11. `automation.toggle` — autopilot / auto-retry / auto-submit / auto-sync flipped.

### Storage

Append-only `data/.audit/YYYY-MM-DD.ndjson`. No fsync-per-write (too expensive); batch fsync on a 1 s tick, like the event stream. Retention default 180 days (configurable via `WALLFACER_AUDIT_RETENTION_DAYS`).

### Exposure

`GET /api/audit?since=<rfc3339>&actor=<id>&action=<name>&limit=<n>` — admin-only, cursor-paginated. `GET /api/audit/stream` — SSE live tail for a control-plane view.

### Never in the audit log

Token values. Prompt contents. Feedback text. File contents. Diff bodies. The log is about **what did who touch**, not **what did they write** — that data lives in the task event stream and is governed by task-level RBAC.

---

## UI Changes

### Header

- Avatar chip for logged-in user on the right, with dropdown: name, email, role badges, sign-out.
- Avatar stack for other present members to the left of the user's avatar. Hover reveals name + what card / thread they have focused.

### Task card

- Small `created_by` chip in the lower-left of the card. Distinct color for service actors.
- Top-right corner: stacked avatars of current viewers (max 3, then `+N`).
- Conflict banner when a 409 comes in: *"Alice moved this card while you were editing — refresh?"*

### Timeline drawer

Every event line prefixed with the actor. Service actors rendered with a small gear icon.

### Planning chat

- Each message row shows the author's avatar + name on the left; agent messages keep their existing agent-icon styling.
- Thread tab bar: lock icon for private threads, globe icon for shared.
- *"Alice is typing…"* indicator at the bottom of the input when another member has focus on the compose input within TTL.

### Settings

- **Admin-only tabs are hidden** (not just disabled) from editors and viewers. This communicates the permission boundary without a nag.
- A new **Audit Log** tab under admin settings.
- A new **Members** tab showing the org member list (from `GET /api/org/members`) with their role badge. Role management is a link out to `auth.latere.ai`; wallfacer does not edit roles.

### Status bar

- Small online-count pill: *"3 online"*. Click opens the member drawer with per-member focus.

---

## Real-Time Wire Protocol

Additions to the existing SSE stream on `/api/tasks/stream`. The event types grow:

| Event | Direction | Payload |
|---|---|---|
| `presence` | Server → client | `{present: [{id, name, avatar, role, focus: {task_id?, thread_id?, editing?}}]}` |
| `task-updated` | Server → client | (existing; gains `actor` field in diff) |
| `conflict` | Server → client | `{resource: "task:<id>", actor, reason}` — surfaced when a 409 happened on any peer mutation |
| `config-changed` | Server → client | `{actor, field, value}` — so everyone's autopilot toggle updates live |
| `member-joined` / `member-left` | Server → client | Derived from presence; rendered as a toast |

Client-initiated via POST (not SSE):

- `POST /api/presence/focus` — per-client heartbeat, 500 ms rate limit
- `POST /api/presence/typing` — optional short-lived typing indicator

WebSocket is **not** introduced by this spec. The existing SSE + POST pattern is sufficient for presence and awareness at the scale of an org board (dozens, not thousands). If the product ever needs bidirectional low-latency (live cursor co-browsing, operational transforms), that is a separate spec.

---

## Security Considerations

- **JWT validation is the single choke point.** If `pkg/jwtauth.Middleware` fails open, RBAC collapses. Tests must include a "JWT validation failure → 401" case on every mutating route.
- **Service-actor injection.** The constant `service:<name>` must never be accepted from the wire. A defense-in-depth check in `internal/handler/middleware.go` rejects any request whose resolved principal ID matches `^service:`. Only the automation loops, invoking store methods directly, can attach a service actor.
- **Private planning threads.** The creator's `principal_id` is part of the thread file path's permission check. A bug here leaks another member's chat. Dedicated test harness validates cross-member access returns 404 (not 403 — we don't confirm existence).
- **Audit log tamper resistance.** The NDJSON files are append-only from the app's perspective, but the filesystem allows rewrites. For latere.ai cloud, the audit files are uploaded to the cold tier with object-lock / versioning enabled (out of scope for this spec — delegated to `cloud/tenant-filesystem.md`).
- **Cross-org data leak.** Not a concern of this spec. The instance-per-org invariant from `multi-tenant.md` is the firewall.

---

## Non-Goals

Explicitly out of scope, to keep the spec bounded:

1. **CRDT text editing.** Collaborative prompt editing with live cursors is not delivered here. Conflicts use optimistic concurrency.
2. **Voice / video / screen-share.** Not a comms product.
3. **Per-field locks.** No Google-Docs-style "Alice is editing this field, you can't."
4. **Cross-org collaboration.** An external contributor from org B cannot post into org A's board. The invite mechanism that bridges orgs is an auth-service concern.
5. **Org creation UI.** Orgs are created and managed at `auth.latere.ai`. Wallfacer only reads `org_id` from the JWT.
6. **Role definition UI.** Role names are assigned by the auth service. Wallfacer only interprets three known role names.
7. **Fine-grained resource permissions (ACLs per task / per spec).** Role-based only. Per-resource ACLs are a follow-up if demanded.
8. **Notification delivery (email / Slack).** The audit log is the data; push delivery is the domain of `cloud/multi-tenant.md`'s control-plane integrations.
9. **Mobile clients.** Out of scope — the presence / RBAC work here does not preclude them later but does not deliver them.
10. **Activity history beyond audit log.** Long-running activity graphs / "contributions last 30 days" views — separate spec if needed.

---

## Implementation Order

Sequenced to deliver value at each step and avoid a year-long landing. Each step is a dispatchable child spec once this one is validated.

1. **Identity plumbing through the server** — `Actor` type, `ctx.Actor()` helper, JWT → Actor resolution, fallback `principal:local` in anonymous mode. Lands without any UI change.
2. **Actor fields on all store records + migration** — additive only; tests ensure rehydration of existing per-task directories yields `principal:local` as creator. Lands on `main` with no user-visible change other than event timeline entries gaining a muted *"(unknown)"* chip for legacy events.
3. **Authentication wire-up** — Consume `specs/identity/authentication.md` in earnest: apply `jwtauth.Middleware` to every route; keep `WALLFACER_SERVER_API_KEY` as a coexisting fallback per the auth spec.
4. **RBAC middleware + matrix enforcement** — `authz.Require(…)` wired on every mutating route per the matrix. Table-driven test on every route.
5. **Service actors on automation loops** — Replace the automation-loop context plumbing so every write picks up `service:<name>`.
6. **Audit log infrastructure** — Backend append, `GET /api/audit`, admin-only UI tab.
7. **Optimistic concurrency** — `Version` field on mutable records, `If-Match` handling, 409 UX in the UI.
8. **Presence + focus** — New events on the SSE stream, `/api/presence/focus` endpoint, avatar stack in the header, viewers on cards.
9. **Attribution UI** — Timeline actor chips, created-by chips on cards, audit log tab.
10. **Planning thread visibility** — `Visibility` field, creator-gated read for private, admin-only visibility change, lock icon in the tab bar.
11. **Typing indicators + compose awareness** — Optional polish; last to land because it is the least load-bearing.
12. **Per-member autopilot controls** — `WALLFACER_AUTOPILOT_ALLOWED_ACTORS`, `WALLFACER_AUTOPILOT_PER_ACTOR_LIMIT`, admin UI.

Steps 1–5 form the **RBAC-on-rails** minimum viable milestone. Any cloud-hosted deployment can launch with 1–5 and still provide correct attribution + authorization; 6–12 layer collaborative polish.

### Dispatch split

This spec is large enough that it should be broken down into child specs under `specs/identity/multi-user-collaboration/` before dispatch. Proposed children:

- `identity-plumbing.md` (steps 1–2)
- `rbac-matrix.md` (steps 3–4)
- `service-actors.md` (step 5)
- `audit-log.md` (step 6)
- `optimistic-concurrency.md` (step 7)
- `presence.md` (steps 8, 11)
- `attribution-ui.md` (step 9)
- `private-threads.md` (step 10)
- `autopilot-fairness.md` (step 12)

Each child is `large` or smaller and dispatchable as a single agent task once its parent chunk's interfaces are validated.

---

## Acceptance Criteria

1. A user authenticated via `auth.latere.ai` sees their avatar in the header and every action they perform shows their actor chip on the corresponding event row.
2. Two members on the same board see each other in the presence list within 2 seconds of either joining; leaving drops the presence within 60 seconds.
3. A viewer attempting any mutating endpoint receives 403. An editor attempting an admin-only endpoint receives 403. The 403 body is consistent shape across all refused routes.
4. Dragging the same card to *In Progress* simultaneously from two browsers: exactly one wins, the other sees a 409 and a "refresh" toast, no card is duplicated.
5. Editing the shared `AGENTS.md` concurrently from two admins: one commits, the other gets a merge UI showing both versions.
6. Toggling autopilot is admin-only; the audit log has a `config.edit` entry with the admin's actor and the old→new value. Every task promoted by autopilot carries `service:autopilot` as its state-change actor in the timeline.
7. Creating a private planning thread and sending messages: a peer member's `GET /api/planning/messages?thread=<id>` returns 404 (not 403).
8. Upgrading a pre-multi-user wallfacer installation preserves all existing tasks, which appear with `principal:local` attribution on legacy events and `admin`-equivalent behavior until `AUTH_URL` is configured.
9. Audit log endpoint rejects non-admin reads and never leaks token values — fuzz test with every admin-touched endpoint confirms no secret-shaped string appears in any audit entry.
10. `GET /api/auth/me` returns the caller's `{id, email, name, roles}` derived from the JWT; no local user table is introduced.

---

## Open Questions

1. **Guest / external-reviewer role.** Common ask: invite a non-member to view one task. Deferred; would need per-task ACL in addition to the role system. Noted for follow-up.
2. **Workspace-group per-member override.** Today the workspace group is org-global. A member might want their own local workspace view without forcing the whole team. This is probably a UI-only preference (filter the set of displayed workspaces) rather than a store change. Noted.
3. **Agent actor identity in multi-user context.** When two users both trigger title generation on different tasks, the agents are indistinguishable from each other. Not a correctness issue but makes the timeline noisy. Considered noise-reduction in agent-side telemetry later.
4. **Instructions edit frequency.** `AGENTS.md` is mutated rarely today; if editors editing it becomes high-frequency, a simple edit-log tab on the settings page may be warranted. Deferred.
5. **Removing a member's in-flight work.** When an org removes a member at the auth service, wallfacer discovers this lazily on the next JWT refresh. Tasks the member has *in progress* continue running; this is the right default (don't lose agent work) but should surface in the audit log as *"Alice's access revoked — 2 of her tasks still running."* Exact behavior noted for the child spec.
