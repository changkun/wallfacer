---
title: Multi-User Collaboration (Org-Scoped Board)
status: stale
depends_on:
  - specs/foundations/storage-backends.md
affects:
  - internal/store/
  - internal/handler/
  - internal/runner/
  - internal/workspace/
  - internal/planner/
  - internal/apicontract/
  - internal/auth/
  - frontend/src/
  - docs/internals/
  - docs/guide/
effort: xlarge
created: 2026-04-18
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# Multi-User Collaboration (Org-Scoped Board)

## Problem

Wallfacer today is a **single-user, single-process server**. Every request is anonymous once the shared bearer token matches, every in-memory map is process-global, and every state-changing action (creating a task, dragging it to *In Progress*, submitting feedback, editing the workspace `AGENTS.md`, toggling autoimplement, rotating the Anthropic API key) is indistinguishable from any other. There is no concept of "who did this."

Note: the identity *plumbing* (the data fields and the actor context carrier) has since shipped. The store now records `Task.CreatedBy` / `Task.OrgID` and `TaskEvent.ActorSub` / `TaskEvent.ActorType`, and handlers stamp the actor at the request boundary. See the Data Model section for exactly what landed and what remains. The remaining gap is the collaboration surface on top of those fields: presence, RBAC role gates, the audit log, optimistic concurrency, and the attribution UI.

This blocks the cloud movement in ways that the (now archived) `cloud/multi-tenant.md` spec did **not** solve:

1. **Teams are the unit that buys software, not individuals.** A team on `wallfacer.latere.ai` wants a **shared board** (multiple engineers seeing the same task list, same planning threads, same oversight history) not N isolated per-user instances that only communicate through GitHub PRs.
2. **No attribution in the UI.** The store carries the actor now, but the board still renders timelines as *"moved to In Progress at 14:02"*. In a shared board it must say *"Alice moved to In Progress"* using the actor fields that already exist on `TaskEvent`.
3. **No presence.** Two engineers dragging the same card, editing the same spec, or chatting in the same planning thread have no idea the other is there. They step on each other's work silently.
4. **No permission boundaries.** Everything is admin-equivalent. A junior contributor can rotate the org API key, disable autoimplement, delete production tasks, or edit the shared `AGENTS.md` template, no different from the team lead who pays the bill.
5. **Automation loops have no distinct identity.** Autoimplement promotion, auto-retry, title generation, oversight runs all stamp a single generic `ActorType "system"` with an empty sub today. In an audit-conscious multi-user setup these need clearly-named per-loop service actors, and the toggle that enables them needs an admin gate.

Until presence, attribution UI, and RBAC exist on top of the data model, *"wallfacer runs in the cloud"* only means *"each user runs their own private wallfacer in the cloud."* That is a hosted single-tenant product, not a collaboration platform. This spec defines the work needed to close that gap.

---

## Relationship to the (archived) `cloud/multi-tenant.md`

`cloud/multi-tenant.md` is **archived** (superseded 2026-05-30 by the Latere platform boundary). It originally **rejected** the shared-multi-user-server design, citing the cost of per-user scoping every in-memory map, workspace manager, automation loop, SSE stream, and circuit breaker. Its answer was **one wallfacer instance per user**, hibernated when idle. The hard dependency on that spec has therefore been dropped from the frontmatter; what survives is the *instance-per-tenant* framing, retained here for context only.

This spec **reframes the tenant unit**: from *user* to *organization* (team). It does not overturn the instance-per-tenant decision; it redefines what a tenant is.

| Dimension | `multi-tenant.md` (archived) | This spec (org-scoped) |
|---|---|---|
| Tenant = | One user | One org (>=1 user) |
| Instances per user | 1 dedicated | 0 (user logs into an org instance) |
| Instances per org | n/a | 1 shared instance, hibernated when all members idle |
| Data isolation boundary | User | Org |
| In-memory scoping | Not needed (process is single-user) | Not needed (process is single-org) |
| Collaboration within a tenant | n/a | **Yes (primary goal)** |
| Cross-tenant access | Forbidden (infrastructure layer) | Forbidden (infrastructure layer) |

The benefit is that the archived spec's architectural simplicity survives: the wallfacer process still serves **one tenant**, so we do not rewrite every store to be keyed by `tenant_id`. But within that one tenant, there can be many users, each authenticated, each attributed, each with a role. The org-scoped read filter is already in place: `store.TasksForPrincipal` (internal/store/principal.go) filters tasks by `OrgID` for org callers and by `CreatedBy` for personal callers, with legacy (no-owner) tasks treated as deployment-shared.

The spec maintains three deployment modes from the archived analysis and adds collaboration as a new axis:

| Mode | Runs on | Tenant | Users | Needs this spec? |
|---|---|---|---|---|
| **Local anonymous** | User machine | - | 1 anonymous | No |
| **Local authenticated** | User machine | - | 1 authenticated (just attribution) | Partial (just actor fields, shipped) |
| **Cloud hosted (personal)** | latere.ai K8s | Personal org of 1 | 1 | Partial (same code path as team, one member) |
| **Cloud hosted (team)** | latere.ai K8s | Org of N | 1..N concurrent | Full (this spec) |

Local authenticated benefits from the identity plumbing (event timeline shows *"Alice moved to In Progress"* even though there is only ever one Alice on the machine). Cloud personal is architecturally identical to cloud team with a membership of one. There is no fork in the code path.

**Alternatives considered and rejected:**

- **(a) Reverse the archived instance-per-tenant decision** (make a single process multi-tenant, keyed by `tenant_id` everywhere). Rejected because the original analysis is still correct: the refactor touches the hot path of the runner, the store, the workspace manager, every SSE subscriber, and every automation loop, with no product benefit that the org-scoped instance model does not already deliver.
- **(b) Read-only observers only** (other users can watch the board but only the owner can mutate). Rejected because this is not "collaboration" in any useful sense; it is *pair-programming-by-screensharing-over-HTTPS*. The user's explicit ask includes "who operated what," "multiple users in the same chat," and "role-based automation permissions," none of which a read-only observer mode addresses.

---

## Identity Model

Wallfacer consumes identity produced by the platform auth service. The principal surfaced to handlers is `auth.Identity` (a type alias for `authkit.Identity`, internal/auth/auth.go), carrying `Sub`, `OrgID`, `Email`, `PrincipalType` (`"user" | "service" | "agent" | "dev"`), `IsSuperadmin`, and `Scopes []string`. This spec is **purely a consumer** of those claims; it adds no user model, no org model, no password storage, no invite flow.

### Identity types wallfacer persists

The actor that lands on a stored mutation is two flat fields, not a nested struct. On `TaskEvent` these are `ActorSub` (the principal `Sub`) and `ActorType` (a `store.ActorType`, internal/store/actor.go). The handler stamps them via `store.WithActorPrincipal(ctx, sub, type)`; background writers stamp `store.WithSystemActor(ctx)`.

| Concept | Source | Field(s) | Example |
|---|---|---|---|
| Principal sub | `Identity.Sub` | `Task.CreatedBy`, `TaskEvent.ActorSub` | `u_01J8B…` |
| Org | `Identity.OrgID` | `Task.OrgID` | `org_01J…` |
| Actor type | `Identity.PrincipalType` / context | `TaskEvent.ActorType` | `"user"`, `"service"`, `"apikey"`, `"system"`, `""` |
| Service actor (future) | Constant sentinel | per-loop `ActorSub` | `service:autoimplement`, `service:auto-retry`, `service:title-gen` |

The shipped `store.ActorType` enum is `{ActorUser "user", ActorService "service", ActorAPIKey "apikey", ActorSystem "system", ActorAnonymous ""}`. Note that the *future* "service actor" namespace (`service:<name>` sentinel **subs** for distinguishing automation loops) is a different thing from the shipped `ActorService` **type** (which means "service-account JWT"). Reconciling the two is called out in Open Questions; it is not yet built.

A `service:<name>` actor would be a reserved `ActorSub` namespace used when wallfacer's own automation mutates state on nobody's behalf. It is not minted by the auth service; it is a local constant. The UI would render service actors with a distinct chip color so the timeline is unambiguous: *"autoimplement moved task X to In Progress"*.

### Roles

The platform JWT exposes coarse privilege via `Identity.IsSuperadmin` and `Identity.Scopes []string`; there is **no** `roles[]` claim in `authkit.Identity` today. Wallfacer's admin/editor/viewer model in the RBAC section therefore maps onto **scopes** (the shipped `auth.RequireScope(scope)` and `auth.RequireSuperadmin` wrappers in internal/auth/authorize.go are the enforcement primitives), not onto a role claim that does not exist. Defining the canonical scope-to-permission mapping is part of the `rbac-matrix` child spec.

The minimum viable permission set wallfacer recognizes:

| Role (scope group) | Within wallfacer |
|---|---|
| `admin` | All editor permissions + settings, env, API keys, executor routing, autoimplement toggles, instruction templates, member removal (via auth service), task force-delete, audit log access |
| `editor` | Create/update/cancel/feedback/delete-own tasks; edit planning specs; edit instructions; send planning chat; dispatch specs |
| `viewer` | Read-only: board, task events, diffs, oversight, usage, planning chat history, spec tree |

Callers with no recognized scope default to `viewer`. `IsSuperadmin=true` overrides all per-scope checks (used for latere.ai operations, not for regular members).

### Fallback: local / anonymous modes

In deployments where the auth service is not configured, wallfacer continues to run in single-user anonymous mode. In that mode `PrincipalFromContext` returns no principal, the store records empty `CreatedBy` / `ActorSub` (`ActorAnonymous`), and the RBAC wrappers are simply not installed on the router (equivalent to everyone being `admin`). This preserves the current behavior exactly: no regression for self-hosted anonymous use.

---

## Data Model Changes

The identity-plumbing portion of this migration has **shipped**. Every record that represents a user action needs an actor field; the high-traffic records already have one. Records that represent org-scoped data (cloud mode only) need an org field; in local/anonymous mode that field stays empty.

The canonical shape is two flat fields per record (`...Sub string` for the principal and `...Type store.ActorType`, or a single `...By string` for the principal sub where the type is implied). There is **no** nested `Actor{ID, Kind}` struct; the original design's inline struct was flattened so the fields travel with every JSON snapshot without a sub-object.

### Fields added

| Store record | File | Fields | Status | Meaning |
|---|---|---|---|---|
| `Task` | `internal/store/models.go` | `CreatedBy string`, `OrgID string` | **DONE** | Task creator (`Sub`) and owning org |
| `TaskEvent` | `internal/store/models.go` | `ActorSub string`, `ActorType string` | **DONE** | Who caused this event, critical for the timeline |
| `Principal` + visibility filter | `internal/store/principal.go` | `Principal{Sub, OrgID}`, `TasksForPrincipal`, `principalSeesTask` | **DONE** | Org / personal / legacy read scoping |
| Actor context carrier | `internal/store/actor.go` | `ActorType` enum, `WithActorPrincipal`, `WithSystemActor` | **DONE** | Stamps actor onto writes from request or background ctx |
| `Task` | `internal/store/models.go` | `UpdatedBy string` | pending | Last mutator (distinct from creator) |
| `Tombstone` | `internal/store/models.go` | `DeletedBy string` | pending | Who soft-deleted (Tombstone today carries only `DeletedAt`/`Reason`) |
| `TaskFeedback` (event payload) | `internal/store/models.go` | `Author string` | pending | Waiting-task feedback author |
| `Message` (planning chat) | `internal/planner/conversation.go` | `Author string` | pending | Who wrote this chat turn (no author field yet) |
| `PlanningThread` | `internal/planner/threads.go` | `CreatedBy string`, `Visibility {private, shared}` | pending | Private = only creator; shared = any org member (no visibility yet) |
| `WorkspaceInstructions` (AGENTS.md edit log) | `internal/prompts/` | `UpdatedBy string`, `UpdatedAt time.Time` | pending | Who last edited the shared instruction template |
| `EnvEdit` (new, audit-log entry) | `internal/store/audit.go` (new) | `ActorSub`, `Diff []string` (field names only, never values) | pending | Admin edit of `.env` |
| `ConfigEdit` (new, audit-log entry) | `internal/store/audit.go` (new) | `ActorSub`, `Field`, `Old`, `New` | pending | Admin toggle of autoimplement/automation |
| `APIKeyAction` (new, audit-log entry) | `internal/store/audit.go` (new) | `ActorSub`, `Action {rotate, reveal, test}` | pending | Sensitive env access |
| `SpecDispatch` event | `internal/spec/`, `internal/handler/specs_dispatch.go` | `ActorSub` | pending | Which team member dispatched this spec |
| `SystemPromptOverride` | existing | `UpdatedBy string` | pending | Who edited the built-in prompt template |

(The removed task-board refine subsystem is gone, so the original `RefinementSession` / `RefinementJob` actor row has been dropped. The planning `/refine` slash command, internal/planner/commands_templates/refine.tmpl, is a separate live feature and is unaffected.)

### On-disk layout changes (pending)

Per-task directories (`data/<task-uuid>/`) could gain `actors.json`, an append-only log redundant with the events stream but cheaper to read for the explorer/diff views. Its primary value is recovery: if the events file is corrupted on a workspace switch, `actors.json` still answers "who created this task." Not yet built; the actor lives on the events stream today.

Audit log (`data/.audit/`) is a new directory with one append-only file per day (`data/.audit/2026-04-18.ndjson`). Separate from task events because it lives at the workspace/org level, not the task level, and retention is different (audit retention is configurable per org; task retention is not).

### Migration (shipped intent)

Existing per-task files lack the new actor fields. The shipped behavior:

1. `CreatedBy` defaults to empty on legacy records, and `principalSeesTask` treats an empty-`CreatedBy`, empty-`OrgID` task as deployment-shared (visible to any signed-in user and to local mode), preserving single-user-upgrades-to-cloud history. **DONE.**
2. `OrgID` stays empty (local mode) until the workspace is migrated into an org on the cloud. **DONE.**
3. `TaskEvent` entries without `ActorSub`/`ActorType` should render as `"unknown"` in the UI with a muted chip (UI pending).
4. Audit log files only exist from the upgrade date forward (pending).

The migration is non-destructive: files are rewritten atomically (temp + rename) only when the task is next mutated, matching the existing `internal/store/migrate.go` pattern.

### Workspace key: still fingerprint-based

The existing workspace key is a SHA-256 of sorted workspace paths. Cloud mode introduces a second dimension (the org) but the path fingerprint is still what identifies a *workspace group*. Cloud path is `orgs/<org_id>/workspaces/<fingerprint>/`. Local path is unchanged (`~/.wallfacer/<fingerprint>/`). The in-process store sees only one workspace at a time, so it does not need org-aware keying; the per-instance wallfacer serves one org exclusively.

---

## Real-Time Collaboration

Four concerns, in order of complexity. All four are pending.

**Topology note.** The presence and collaboration mechanisms below were
originally designed process-local within a single hosted instance
(instance-per-org). They are **re-homed** onto the [cloud coordination
plane](../cloud/latere-integration/coordination-plane.md): signed-in local
instances each hold one outbound connection to a coordinator on wallfacerd, so
presence aggregates across instances and teammates on their own machines
collaborate. Both deployment models are supported (local-first sync is primary;
hosted-shared instance-per-org stays an alternative, just another client of the
same coordinator). This spec's RBAC matrix, audit log, attribution, and
optimistic-concurrency are valid under both and are **re-homed, not rewritten**;
only the *source* of the presence/collaboration feed moves to the coordinator.
The connection, presence, and spec-comment designs now live in the
coordination-plane children; this spec keeps the RBAC/audit/attribution surface.

### 1. Presence (who is here)

**Goal.** Every member opening the board sees a list of other members who are currently connected.

**Mechanism.** The existing `/api/tasks/stream` SSE connection is the liveness signal. A user is *present* iff they have an active SSE subscription. On connect: `store.Presence.Join(Sub, connectionID)`. On disconnect (or 60 s silence): `Leave`. The presence map is process-local (no persistence needed).

A new delta event `event: presence` rides the existing tasks-stream with payload `{present: [{id, name, avatar, focus}]}`. Clients render a stack of avatar chips in the header (StatusBar.vue / the header avatar component). On any presence change the server broadcasts the full list to all current subscribers (fan-out is cheap; membership is small, max ~50 per org).

No new SSE endpoint. No WebSocket. The single bidirectional surface we already have (`/api/terminal/ws`) stays scoped to terminal.

### 2. Activity focus (what are they looking at)

**Goal.** Show which card other members currently have open. A small avatar appears on the top-right of the card when someone is viewing it.

**Mechanism.** The client sends focus changes via a cheap `POST /api/presence/focus` (not SSE, it is a write). Payload: `{task_id: "…" | null, thread_id: "…" | null}`. The server updates the presence entry and broadcasts. Focus changes are rate-limited per client to one every 500 ms (server-side, to guard against a rogue client flapping).

### 3. Typing / live-edit awareness (are they composing)

**Goal.** In planning chat, a "X is typing…" indicator. In task prompt editing, a lock icon while someone is mid-edit.

**Mechanism.** Same focus endpoint carries an optional `editing: "task:<id>.prompt" | "thread:<id>.compose" | null`. TTL is 3 seconds from last heartbeat; client refreshes while the field is focused. On lapse the server clears. This is *awareness*, not *locking*: two users can type into the same field simultaneously; the conflict resolution lives in section 4.

### 4. Concurrent mutation conflicts

**Goal.** Prevent silent clobber when two users mutate the same resource.

Not all resources need the same strategy. The matrix below is the full design:

| Resource | Strategy | Why |
|---|---|---|
| **Task position (drag)** | Last-write-wins, broadcast immediately | Position is fuzzy anyway; users can always redrag; no real conflict |
| **Task status transitions** | **Optimistic concurrency (`If-Match` / version)** | Two users dragging to *In Progress* simultaneously: one wins, the other gets 409 and the UI shows *"Alice already moved this card"* |
| **Task prompt edit** | Optimistic concurrency with `updated_at` | Intentional conflict prompt; the UI shows both versions and asks the user to merge, same as GitHub's file-edit conflict |
| **Planning chat message** | Append-only, always OK | Each message is a new record; no conflict possible |
| **Planning thread rename / archive** | Optimistic concurrency | Cheap to conflict; rare race |
| **Spec file (via file explorer)** | `If-Match` on content hash | Reuse existing explorer flow; conflict returns 409 with latest content |
| **Workspace AGENTS.md** | Optimistic concurrency with `updated_at` | Shared template; conflict rare but high-impact |
| **Env vars, server config** | Admin-only + optimistic concurrency | Admin mutations go through audit log regardless |
| **Automation toggles** | Admin-only + optimistic concurrency | Same as config |

Every versioned record grows a `Version int64` field that monotonically increments on write. The wire contract:

- Mutations accept `If-Match: <version>` or a JSON `version` field.
- On mismatch: `409 Conflict` with the current representation and version.
- The UI on receiving 409 refreshes the resource and either auto-applies (for status transitions, show a toast) or prompts the user to merge (for prompt/text edits).

Versioning is per-resource, not per-tenant-monotonic. The existing `Seq` on the delta stream stays the way it is: it is a transport-layer broadcast sequence, not a record version.

### Why not CRDT / per-field locking

**Non-goal.** Prompt text is short, edits are rare, and the board is not a multiplayer document. CRDTs (yjs / automerge) and per-field locks (Google Docs) are both plausible and both overkill for this product. We use optimistic concurrency with clear conflict UX and move on. If the product evolves toward inline collaborative prompt editing, that is a separate spec.

---

## Chat with Identity (Planning Threads Extension)

Extends the planning chat threads work (drafted) without overturning it. The planning conversation log today is `planner.Message` (internal/planner/conversation.go): `Role`, `Content`, `Timestamp`, `FocusedSpec`, `FocusedTask`, `RawOutput`, `PlanRound`. It has **no** author or visibility field yet; both are added by this spec.

### Per-message author

Every `Message` gains `Author string`. User messages carry the logged-in user's `Sub`. Agent messages carry a synthetic actor derived from the executor (`agent:claude-code`, `agent:codex`), distinguished by a future `ActorType`/`PrincipalType` of `"agent"`.

### Thread visibility

Planning threads (internal/planner/threads.go) gain a `Visibility` field with two values:

- **`private`** (default): only the thread creator can see, read, send, or dispatch from this thread. Peer members see nothing.
- **`shared`**: any org member with `editor` or `admin` can read and send in the thread. Viewers can read but not send.

The existing thread tab bar (PlanningChatPanel.vue) gains a lock icon for `private` threads. Thread creation UI offers the two options; default is `private` (least surprising). Switching a thread from `private` to `shared` requires admin, because the decision to expose prior chat turns to peers is non-trivial and the creator may not have anticipated it.

### Per-thread exec isolation

The existing single-planner-execution-per-workspace-group invariant stays. Concurrent thread sends from different users are queued, same FIFO as today. The UI shows *"Alice is running a turn in thread **auth-refactor**"* so a second user doesn't wonder why their own message is pending.

### Undo scoping

Undo is thread-scoped via git revert with a `Plan-Round`/thread trailer. This spec adds: **undo is authorized for the thread creator and admins**. Editors who happened to contribute a turn in the thread cannot undo the whole thread.

---

## RBAC: Endpoint-by-Endpoint Matrix

Role gate on every route. The matrix below is the **authoritative gate definition**; deviations from it are bugs. Enforcement builds on the shipped `auth.RequireScope` / `auth.RequireSuperadmin` wrappers (internal/auth/authorize.go), which currently scaffold the mechanism but are not yet applied per-route.

Legend: `A` = admin, `E` = editor, `V` = viewer. `self` means the actor can only act on their own resource.

### Tasks

| Endpoint | V | E | A | Notes |
|---|:-:|:-:|:-:|---|
| `GET /api/tasks*` | ✓ | ✓ | ✓ | All read endpoints |
| `POST /api/tasks` | - | ✓ | ✓ | Create |
| `POST /api/tasks/batch` | - | ✓ | ✓ | Batch create |
| `PATCH /api/tasks/{id}` | - | ✓ | ✓ | Status/prompt/timeout/deps |
| `POST /api/tasks/{id}/feedback` | - | ✓ | ✓ | Waiting task feedback |
| `POST /api/tasks/{id}/done` | - | ✓ | ✓ | Mark done |
| `POST /api/tasks/{id}/cancel` | - | ✓ | ✓ | Cancel |
| `POST /api/tasks/{id}/resume` | - | ✓ | ✓ | Resume failed |
| `POST /api/tasks/{id}/sync` | - | ✓ | ✓ | Rebase worktrees |
| `POST /api/tasks/{id}/test` | - | ✓ | ✓ | Test verification |
| `POST /api/tasks/{id}/archive` | - | ✓ | ✓ | Archive |
| `POST /api/tasks/{id}/unarchive` | - | ✓ | ✓ | Unarchive |
| `POST /api/tasks/archive-done` | - | - | ✓ | Bulk, admin only |
| `DELETE /api/tasks/{id}` | - | `self` | ✓ | Editors delete own tasks; admin deletes any |
| `POST /api/tasks/{id}/restore` | - | `self` | ✓ | Restore own; admin restores any |
| `POST /api/tasks/generate-titles` | - | - | ✓ | Bulk, admin only |
| `POST /api/tasks/generate-oversight` | - | - | ✓ | Bulk, admin only |

### Ideation

| Endpoint | V | E | A | Notes |
|---|:-:|:-:|:-:|---|
| `GET /api/ideate` | ✓ | ✓ | ✓ | Ideation status |
| `POST /api/ideate` | - | - | ✓ | Ideation creates tasks on behalf of the org, admin only |
| `DELETE /api/ideate` | - | - | ✓ | Cancel ideation |

### Planning / Specs

| Endpoint | V | E | A | Notes |
|---|:-:|:-:|:-:|---|
| `GET /api/specs/*`, `/api/planning/*` (reads) | ✓ | ✓ | ✓ | Shared threads only; private thread reads enforced by thread creator |
| `POST /api/specs/dispatch` | - | ✓ | ✓ | |
| `POST /api/specs/undispatch` | - | ✓ | ✓ | |
| `POST /api/specs/archive` | - | ✓ | ✓ | |
| `POST /api/planning/messages` | - | ✓ | ✓ | Editor on shared threads; creator+admin on private |
| `DELETE /api/planning/messages` | - | `self` | ✓ | Creator clears own thread; admin clears any |
| `POST /api/planning/undo` | - | `self` | ✓ | Thread creator only, plus admin |
| `POST /api/planning/threads` | - | ✓ | ✓ | |
| `PATCH /api/planning/threads/{id}` | - | `self` | ✓ | Rename own threads |
| `POST /api/planning/threads/{id}/archive` | - | `self` | ✓ | |
| `POST /api/planning/threads/{id}/activate` | ✓ | ✓ | ✓ | UI preference, per-user, not a mutation of shared state |
| `POST /api/planning` (start exec) | - | ✓ | ✓ | |
| `DELETE /api/planning` (stop exec) | - | - | ✓ | Shared resource; admin only |

### File Explorer / Terminal

| Endpoint | V | E | A | Notes |
|---|:-:|:-:|:-:|---|
| `GET /api/explorer/*` | ✓ | ✓ | ✓ | |
| `PUT /api/explorer/file` | - | ✓ | ✓ | |
| `GET /api/terminal/ws` | - | - | ✓ | **Admin only**, terminal gives shell access to the wallfacer server pod |

### Configuration & Settings (admin-only)

| Endpoint | V | E | A | Notes |
|---|:-:|:-:|:-:|---|
| `GET /api/config` | ✓ | ✓ | ✓ | Read surfaces UI defaults; safe |
| `PUT /api/config` | - | - | ✓ | |
| `GET /api/env` | - | - | ✓ | Masked, but the mere shape is sensitive |
| `PUT /api/env` | - | - | ✓ | |
| `POST /api/env/test` | - | - | ✓ | |
| `GET /api/system-prompts`, `PUT/DELETE /api/system-prompts/{name}` | ✓/-/- | ✓/-/- | ✓ | Read is safe; writes change behavior for everyone |
| `GET /api/instructions` | ✓ | ✓ | ✓ | |
| `PUT /api/instructions` | - | ✓ | ✓ | Shared `AGENTS.md`, editor allowed; event-sourced so reversible |
| `POST /api/instructions/reinit` | - | - | ✓ | Destructive regenerate, admin only |

### Workspaces

| Endpoint | V | E | A | Notes |
|---|:-:|:-:|:-:|---|
| `GET /api/workspaces/browse` | - | - | ✓ | Reveals server filesystem layout |
| `PUT /api/workspaces` | - | - | ✓ | Switching workspace groups affects every member |

### Git Operations

| Endpoint | V | E | A | Notes |
|---|:-:|:-:|:-:|---|
| `GET /api/git/status`, `GET /api/git/stream`, `GET /api/git/branches` | ✓ | ✓ | ✓ | |
| `POST /api/git/push` | - | ✓ | ✓ | |
| `POST /api/git/sync` | - | ✓ | ✓ | |
| `POST /api/git/rebase-on-main` | - | ✓ | ✓ | |
| `POST /api/git/checkout` | - | - | ✓ | Switches workspace state for the whole team |
| `POST /api/git/create-branch` | - | ✓ | ✓ | |
| `POST /api/git/open-folder` | - | - | ✓ | Not meaningful in cloud mode, rejected outright |

### Auth / Admin

| Endpoint | V | E | A | Notes |
|---|:-:|:-:|:-:|---|
| `POST /api/auth/{provider}/*` (executor-creds OAuth) | - | - | ✓ | These write tokens into the org-shared env |
| `POST /api/admin/rebuild-index` | - | - | ✓ | |
| `GET /api/debug/*` | - | - | ✓ | Server internals |
| `GET /api/stats`, `GET /api/usage` | ✓ | ✓ | ✓ | Usage transparency |

### New endpoints this spec adds

The current-user lookup (`GET /api/me`, handler `AuthMe`) and org switching (`GET /api/auth/orgs`, `PATCH /api/auth/me` / `POST /api/me/switch-org` exposed as `switchOrg`) have **shipped** and are no longer part of this spec. The remaining additions:

| Endpoint | V | E | A | Purpose |
|---|:-:|:-:|:-:|---|
| `GET /api/org/members` | ✓ | ✓ | ✓ | Members of the current org (fetched from auth service, cached). Distinct from the shipped `/api/auth/orgs` (which lists the *orgs* a caller belongs to); this lists the *members* of one org |
| `POST /api/presence/focus` | ✓ | ✓ | ✓ | Update focus hint |
| `GET /api/audit` | - | - | ✓ | Query audit log (filter by actor, action, date) |
| `GET /api/audit/stream` | - | - | ✓ | SSE live audit tail |

### Implementation

A scope-aware HTTP middleware (building on `auth.RequireScope` / `auth.RequireSuperadmin`, internal/auth/authorize.go), applied per route when the router is built in `internal/handler/`. The identity is read from the validated principal in context once per request (`auth.PrincipalFromContext`). `self`-scoped routes run the role check first, then a `RequireSelf(ctx, resourceOwnerID)` that lets through when the caller is the owner (`Sub`) or `IsSuperadmin`/admin-scoped. Tests hit the matrix table-driven to catch regressions.

---

## Automation Loops as a Service Actor

Every automation mutation is currently faceless: the runner and scheduler goroutines stamp `store.WithSystemActor(ctx)`, which records a single `ActorType "system"` with an empty sub. This spec gives each loop a distinct name:

| Loop | Service actor (sub) | Admin gate for enable/disable |
|---|---|---|
| Autoimplement (backlog → in_progress promotion) | `service:autoimplement` | `PUT /api/config` (admin) |
| Auto-retry (failed retry on budget) | `service:auto-retry` | `PUT /api/config` (admin) |
| Auto-submit (commit + push on done) | `service:auto-submit` | `PUT /api/config` (admin) |
| Auto-sync (rebase on main) | `service:auto-sync` | `PUT /api/config` (admin) |
| Auto-test (test run post-completion) | `service:auto-test` | `PUT /api/config` (admin) |
| Title generation | `service:title-gen` | Always on; no toggle |
| Oversight generation | `service:oversight` | `PUT /api/config` (admin) |
| Commit-message generation | `service:commit-msg` | Always on |
| Soft-delete retention prune | `service:retention` | Never toggleable |

Service actors appear in `TaskEvent.ActorSub`, in `ConfigEdit` (as the actor for cascading changes), and on audit-log entries for their own mutations. Implementing this means replacing the single `WithSystemActor` stamp with a per-loop sub, and reconciling it with the existing `ActorType` enum (see Open Questions): the natural shape is `ActorType "system"` (or a new `"service-loop"`) plus a `service:<name>` sub.

Every config change that toggles an automation loop writes two records: a `ConfigEdit` audit entry with the admin actor, and a `task.event` on each task that is immediately affected (e.g. autoimplement being disabled mid-queue emits *"autoimplement disabled by Alice, 3 waiting tasks remain in backlog"* on the stats dashboard).

### Per-member autoimplement budget

A member with role `editor` can create tasks that the admin-enabled autoimplement will promote. Per-org config (`admin`-only) adds:

- `WALLFACER_AUTOIMPLEMENT_ALLOWED_ACTORS` - optional allow-list of principal subs whose tasks autoimplement will promote. Empty (default) means "any member."
- `WALLFACER_AUTOIMPLEMENT_PER_ACTOR_LIMIT` - cap on concurrent in_progress tasks promoted by autoimplement per actor. Prevents one member hogging the entire parallel-worker budget.

Both are org-level config, admin-editable via `PUT /api/config`.

---

## Audit Log

Append-only, admin-read. Covers every write the RBAC matrix labels as admin-only, plus every soft-delete and every automation-config change. Pending; none of it is built yet.

### What goes in

1. `env.edit` - admin edited `.env`. Payload: actor, timestamp, list of **field names** (never values). `ANTHROPIC_API_KEY` → `{"fields": ["ANTHROPIC_API_KEY"]}` is the entire diff.
2. `env.test` - `POST /api/env/test` called. Payload: actor, timestamp, provider.
3. `config.edit` - admin changed autoimplement / automation / workspace. Payload: actor, timestamp, field, old value, new value (only non-secret fields).
4. `workspace.switch` - workspace group changed. Payload: actor, old fingerprint, new fingerprint.
5. `git.checkout` / `git.rebase-on-main` - branch-level mutation. Payload: actor, workspace, from/to.
6. `task.delete.admin` - admin force-deleted another user's task. Payload: actor, task, original-creator.
7. `thread.visibility-change` - private → shared or vice versa. Payload: actor, thread, old, new.
8. `instructions.reinit` - workspace instructions regenerated.
9. `system-prompt.override-write` / `system-prompt.override-delete`.
10. `apikey.rotate` / `apikey.reveal` - sensitive env surface access.
11. `automation.toggle` - autoimplement / auto-retry / auto-submit / auto-sync flipped.

### Storage

Append-only `data/.audit/YYYY-MM-DD.ndjson`. No fsync-per-write (too expensive); batch fsync on a 1 s tick, like the event stream. Retention default 180 days (configurable via `WALLFACER_AUDIT_RETENTION_DAYS`).

### Exposure

`GET /api/audit?since=<rfc3339>&actor=<id>&action=<name>&limit=<n>` is admin-only, cursor-paginated. `GET /api/audit/stream` is an SSE live tail for a control-plane view.

### Never in the audit log

Token values. Prompt contents. Feedback text. File contents. Diff bodies. The log is about **what did who touch**, not **what did they write**: that data lives in the task event stream and is governed by task-level RBAC.

---

## UI Changes

All UI work lives under `frontend/src/` (the legacy vanilla `ui/` tree was deleted). Pending.

### Header

- Avatar chip for logged-in user on the right, with dropdown: name, email, role badges, sign-out. The account-menu data source (`GET /api/me`, plus org list/switch via `stores/auth.ts`) already ships; this adds the role badges and presence stack.
- Avatar stack for other present members to the left of the user's avatar. Hover reveals name + what card / thread they have focused.

### Task card (`frontend/src/components/TaskCard.vue`)

- Small `created_by` chip in the lower-left of the card. Distinct color for service actors.
- Top-right corner: stacked avatars of current viewers (max 3, then `+N`).
- Conflict banner when a 409 comes in: *"Alice moved this card while you were editing, refresh?"*

### Timeline drawer (`frontend/src/components/TaskDetail.vue`)

Every event line prefixed with the actor (from `TaskEvent.ActorSub` / `ActorType`). Service actors rendered with a small gear icon; legacy events with empty actor render as a muted *"(unknown)"* chip.

### Planning chat (`frontend/src/components/plan/PlanningChatPanel.vue`)

- Each message row shows the author's avatar + name on the left; agent messages keep their existing agent-icon styling.
- Thread tab bar: lock icon for private threads, globe icon for shared.
- *"Alice is typing…"* indicator at the bottom of the input when another member has focus on the compose input within TTL.

### Settings (`frontend/src/components/SettingsModal.vue` + `frontend/src/components/settings/`)

- **Admin-only tabs are hidden** (not just disabled) from editors and viewers. This communicates the permission boundary without a nag.
- A new **Audit Log** tab under admin settings.
- A new **Members** tab showing the org member list (from `GET /api/org/members`) with their role badge. Role management is a link out to the auth service; wallfacer does not edit roles.

### Status bar (`frontend/src/components/StatusBar.vue`)

- Small online-count pill: *"3 online"*. Click opens the member drawer with per-member focus.

---

## Real-Time Wire Protocol

Additions to the existing SSE stream on `/api/tasks/stream`. The event types grow:

| Event | Direction | Payload |
|---|---|---|
| `presence` | Server → client | `{present: [{id, name, avatar, role, focus: {task_id?, thread_id?, editing?}}]}` |
| `task-updated` | Server → client | (existing; gains `actor` field in diff) |
| `conflict` | Server → client | `{resource: "task:<id>", actor, reason}` surfaced when a 409 happened on any peer mutation |
| `config-changed` | Server → client | `{actor, field, value}` so everyone's autoimplement toggle updates live |
| `member-joined` / `member-left` | Server → client | Derived from presence; rendered as a toast |

Client-initiated via POST (not SSE):

- `POST /api/presence/focus` - per-client heartbeat, 500 ms rate limit
- `POST /api/presence/typing` - optional short-lived typing indicator

WebSocket is **not** introduced by this spec. The existing SSE + POST pattern is sufficient for presence and awareness at the scale of an org board (dozens, not thousands). If the product ever needs bidirectional low-latency (live cursor co-browsing, operational transforms), that is a separate spec.

---

## Security Considerations

- **JWT validation is the single choke point.** If the auth middleware (internal/auth/middleware.go) fails open, RBAC collapses. Tests must include a "JWT validation failure → 401" case on every mutating route.
- **Service-actor injection.** The `service:<name>` sub must never be accepted from the wire. A defense-in-depth check at the request boundary rejects any request whose resolved principal sub matches `^service:`. Only the automation loops, invoking store methods directly via `WithActorPrincipal`, can attach a service actor.
- **Private planning threads.** The creator's `Sub` is part of the thread file path's permission check. A bug here leaks another member's chat. A dedicated test harness validates cross-member access returns 404 (not 403, we don't confirm existence).
- **Audit log tamper resistance.** The NDJSON files are append-only from the app's perspective, but the filesystem allows rewrites. For latere.ai cloud, the audit files are uploaded to the cold tier with object-lock / versioning enabled (out of scope for this spec, delegated to the cloud filesystem layer).
- **Cross-org data leak.** Not a concern of this spec. The instance-per-org invariant is the firewall; the in-process `TasksForPrincipal` filter is the second line of defense for shared-instance reads.

---

## Non-Goals

Explicitly out of scope, to keep the spec bounded:

1. **CRDT text editing.** Collaborative prompt editing with live cursors is not delivered here. Conflicts use optimistic concurrency.
2. **Voice / video / screen-share.** Not a comms product.
3. **Per-field locks.** No Google-Docs-style "Alice is editing this field, you can't."
4. **Cross-org collaboration.** An external contributor from org B cannot post into org A's board. The invite mechanism that bridges orgs is an auth-service concern.
5. **Org creation UI.** Orgs are created and managed at the auth service. Wallfacer only reads `OrgID` from the principal.
6. **Role definition UI.** Role / scope names are assigned by the auth service. Wallfacer only interprets three known role groups.
7. **Fine-grained resource permissions (ACLs per task / per spec).** Role-based only. Per-resource ACLs are a follow-up if demanded.
8. **Notification delivery (email / Slack).** The audit log is the data; push delivery is a control-plane concern.
9. **Mobile clients.** Out of scope: the presence / RBAC work here does not preclude them later but does not deliver them.
10. **Activity history beyond audit log.** Long-running activity graphs / "contributions last 30 days" views are a separate spec if needed.

---

## Implementation Order

Sequenced to deliver value at each step and avoid a year-long landing. Each step is a dispatchable child spec once this one is validated.

1. **Identity plumbing through the server** - actor context carrier (`store.WithActorPrincipal` / `WithSystemActor`), `auth.PrincipalFromContext`, JWT/cookie → Identity resolution, anonymous fallback. **DONE** (internal/auth/, internal/store/actor.go).
2. **Actor fields on store records + migration** - `Task.CreatedBy`/`OrgID`, `TaskEvent.ActorSub`/`ActorType`, `Principal` + `TasksForPrincipal` read filter, legacy-record handling. **DONE** for the high-traffic records (internal/store/models.go, principal.go). Remaining records (`UpdatedBy`, `Tombstone.DeletedBy`, `TaskFeedback.Author`, etc.) are pending.
3. **Authentication wire-up** - apply auth middleware to routes; keep `WALLFACER_SERVER_API_KEY` as a coexisting fallback. **PARTIAL**: middleware, principal resolution, `RequireSuperadmin` / `RequireScope` scaffolding ship; the per-route application across the full surface is not complete.
4. **RBAC middleware + matrix enforcement** - scope-aware gate wired on every mutating route per the matrix. Table-driven test on every route. **Pending** (the wrappers exist but no route applies the admin/editor/viewer matrix yet).
5. **Service actors on automation loops** - replace the single `WithSystemActor` stamp so each loop picks up `service:<name>`, reconciling with the `ActorType` enum. **Pending.**
6. **Audit log infrastructure** - backend append, `GET /api/audit`, admin-only UI tab. **Pending.**
7. **Optimistic concurrency** - `Version` field on mutable records, `If-Match` handling, 409 UX in the UI. **Pending.**
8. **Presence + focus** - new events on the SSE stream, `/api/presence/focus` endpoint, avatar stack in the header, viewers on cards. **Pending.**
9. **Attribution UI** - timeline actor chips, created-by chips on cards, audit log tab. **Pending.**
10. **Planning thread visibility** - `Visibility` field, creator-gated read for private, admin-only visibility change, lock icon in the tab bar. **Pending.**
11. **Typing indicators + compose awareness** - optional polish; last to land because it is the least load-bearing. **Pending.**
12. **Per-member autoimplement controls** - `WALLFACER_AUTOIMPLEMENT_ALLOWED_ACTORS`, `WALLFACER_AUTOIMPLEMENT_PER_ACTOR_LIMIT`, admin UI. **Pending.**

Steps 1-5 form the **RBAC-on-rails** minimum viable milestone. Steps 1-2 have landed; with steps 3-5 any cloud-hosted deployment can launch with correct attribution + authorization; 6-12 layer collaborative polish.

### Dispatch split

This spec is large enough that it should be broken down into child specs under `specs/identity/multi-user-collaboration/` before dispatch. With steps 1-2 already shipped, the proposed children that remain:

- `rbac-matrix.md` (steps 3-4) - break down first; everything else gates on the authorization surface being correct
- `service-actors.md` (step 5)
- `audit-log.md` (step 6)
- `optimistic-concurrency.md` (step 7)
- `presence.md` (steps 8, 11)
- `attribution-ui.md` (step 9)
- `private-threads.md` (step 10)
- `autoimplement-fairness.md` (step 12)

Each child is `large` or smaller and dispatchable as a single agent task once its parent chunk's interfaces are validated.

---

## Acceptance Criteria

1. A user authenticated via the auth service sees their avatar in the header and every action they perform shows their actor chip on the corresponding event row.
2. Two members on the same board see each other in the presence list within 2 seconds of either joining; leaving drops the presence within 60 seconds.
3. A viewer attempting any mutating endpoint receives 403. An editor attempting an admin-only endpoint receives 403. The 403 body is consistent shape across all refused routes.
4. Dragging the same card to *In Progress* simultaneously from two browsers: exactly one wins, the other sees a 409 and a "refresh" toast, no card is duplicated.
5. Editing the shared `AGENTS.md` concurrently from two admins: one commits, the other gets a merge UI showing both versions.
6. Toggling autoimplement is admin-only; the audit log has a `config.edit` entry with the admin's actor and the old→new value. Every task promoted by autoimplement carries `service:autoimplement` as its state-change actor in the timeline.
7. Creating a private planning thread and sending messages: a peer member's `GET /api/planning/messages?thread=<id>` returns 404 (not 403).
8. Upgrading a pre-multi-user wallfacer installation preserves all existing tasks, which appear with empty (legacy) attribution rendered as *"(unknown)"* on legacy events and `admin`-equivalent behavior until the auth service is configured. (The store-side legacy handling already ships via `principalSeesTask`.)
9. Audit log endpoint rejects non-admin reads and never leaks token values: fuzz test with every admin-touched endpoint confirms no secret-shaped string appears in any audit entry.
10. `GET /api/me` returns the caller's principal derived from the JWT/cookie (already shipped as handler `AuthMe`); no local user table is introduced.

---

## Open Questions

1. **Spec should be broken down into children before dispatch.** Steps 1-2 (identity plumbing + actor fields/migration) have shipped, so the breakdown should start from `rbac-matrix.md` (steps 3-4), which everything downstream gates on. See the Dispatch split for the full child list.
2. **Archived dependencies.** Both `cloud/multi-tenant.md` and `specs/identity/authentication.md` are now `archived`. The hard `depends_on` edges have been dropped from the frontmatter; the prose retains them as historical context. Confirm there is no live successor spec for the authentication surface that this spec should depend on instead.
3. **Service-actor naming vs the shipped `ActorType` enum.** The spec wants `service:<name>` sentinel **subs** to distinguish automation loops, but the shipped enum already uses `ActorService "service"` to mean "service-account JWT" and stamps all loops with `ActorSystem "system"`. Reconcile: probably `ActorType "system"` (or a new value) plus a `service:<name>` sub, decided in `service-actors.md`.
4. **Roles vs scopes.** `authkit.Identity` exposes `Scopes []string` and `IsSuperadmin`, not a `roles[]` claim. The admin/editor/viewer model must be defined as a scope-to-permission mapping in `rbac-matrix.md`; confirm the platform issues scopes granular enough to express the three groups.
5. **`/api/org/members` vs the shipped `/api/auth/orgs`.** Org *listing* and switching already ship (`/api/auth/orgs`, `switchOrg`). This spec's `/api/org/members` (members of one org) is related but distinct; confirm the auth service exposes a members endpoint to proxy, and whether the cached member list can reuse the existing orgs-list plumbing in `stores/auth.ts`.
6. **Guest / external-reviewer role.** Common ask: invite a non-member to view one task. Deferred; would need per-task ACL in addition to the role system. Noted for follow-up.
7. **Workspace-group per-member override.** Today the workspace group is org-global. A member might want their own local workspace view without forcing the whole team. This is probably a UI-only preference (filter the set of displayed workspaces) rather than a store change. Noted.
8. **Agent actor identity in multi-user context.** When two users both trigger title generation on different tasks, the agents are indistinguishable from each other. Not a correctness issue but makes the timeline noisy. Considered for agent-side telemetry later.
9. **Removing a member's in-flight work.** When an org removes a member at the auth service, wallfacer discovers this lazily on the next JWT refresh. Tasks the member has *in progress* continue running; this is the right default (don't lose agent work) but should surface in the audit log as *"Alice's access revoked, 2 of her tasks still running."* Exact behavior noted for the child spec.
