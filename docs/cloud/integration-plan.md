# Latere Platform Integration — Gap Analysis & Adapter Design

> **Status: working proposal (2026-05-30).** Not a tracked spec yet. Purpose:
> reconcile wallfacer's drafted cloud specs against the latere.ai components
> that already exist, and design the thinnest adapter layer that keeps local
> mode unchanged while enabling the cloud transition. Decision points are at
> the bottom — this is meant to be reacted to, not executed as-is.

## Governing principle (from `latere.ai/specs/products/wallfacer.md`)

> Wallfacer is the autonomous engineering control plane. It should **consume
> Latere platform services in cloud mode rather than absorbing them.**
> Cloud v1 is **metadata coordination, not cloud execution.**

This single sentence resolves most of the "what's still necessary" question:
several drafted cloud specs describe building infrastructure that Cella / FS /
Identity / Lux now own. The work is mostly *deletion and rescoping*, plus one
genuinely new, small adapter.

**Wallfacer owns:** spec model, task lifecycle, agents/flows, oversight,
git/worktree workflow, autonomy controls, local-first UX.
**Wallfacer consumes (does not build):** identity (Identity/auth), runtime
(Cella), file data plane (FS), model keys (Lux), MCP catalog (Registry).

---

## Part 1 — What already exists (the good news)

### Already shipped in wallfacer
- **Identity Phase 1+2** (`internal/auth/`): OIDC sign-in, RS256 JWT validation
  via `latere.ai/x/pkg/jwtauth`, `OptionalAuth`/`Auth` middleware (nil-safe in
  local mode), `store.Principal{Sub, OrgID}`, `Task.CreatedBy/OrgID`,
  `Group.CreatedBy/OrgID`, and `TasksForPrincipal` org-scoped filtering. The
  authentication spec is **archived/complete**.
- **The integration seams are already interfaces**, selected by config:
  - `sandbox.Backend` (`internal/sandbox/backend.go:74`) — `LocalBackend`
    (podman/docker) and `HostBackend` coexist; `--backend` selects. Optional
    `WorkerManager` for per-task worker lifecycle.
  - `store.StorageBackend` (`internal/store/backend.go:9`) — tasks/events/blobs;
    `FilesystemBackend` is the only impl, but the seam is clean.
  - `handler.AuthProvider` (`internal/handler/login.go:13`) — swappable; nil ⇒
    local mode short-circuits to 503/204.
  - `WALLFACER_CLOUD` gate is centrally wired (`internal/cli/server.go`), not
    scattered.

**Implication for "easy integration":** for most seams the design is *not* new
architecture. It is "cloud impl implements the existing interface X, selected
by config; local stays the default." The only seam needing real design work is
workspace/tenancy keying — and that is a Phase 3 problem (see Part 4).

### Already exists in `latere.ai/` (verified via READMEs / module layout)
| Component | Module / host | Provides | Maturity |
|---|---|---|---|
| **auth** | `latere.ai/x/auth`, auth.latere.ai | OIDC server (Ory Fosite), JWKS, orgs/teams, service accounts, **Stripe billing**, RFC 8693 token exchange | Deployed |
| **pkg** | `latere.ai/x/pkg` (wallfacer already imports v0.15.0) | `jwtauth`, `oidc`, `authkit`, `scopes` (already has wallfacer scopes), `otel`, `audit`, `md` | Live |
| **Cella (sandbox)** | `latere.ai/x/sandbox`, cella.latere.ai | K8s sandbox exec, warm pools, durable workspaces, credential vault, per-sandbox identity JWT, `/v1/sandboxes` API | Deployed |
| **FS** | `latere.ai/x/fs`, fs.latere.ai | Two-tier (Spaces cold + PVC hot) file plane; `/files/*` live; **`/workspaces/*` planned, NOT built** | Partial |
| **terraform** | DOKS Frankfurt | `latere` namespace, OTEL→ClickHouse→Grafana chain (Grafana already has wallfacer filters); **no wallfacer module yet** | Deployed |
| **latere-ui** | npm `latere-ui` | Shared footer/session/account/CSS tokens (wallfacer already uses the footer) | Published |

---

## Part 2 — Spec reconciliation (the "strategic cleanup")

Every drafted cloud/storage spec mapped to: **CONSUME** (external component owns
it — rescope to a thin client), **ARCHIVE** (obsoleted by a boundary decision),
**KEEP** (genuinely wallfacer's), or **FOLD** (merge into the v1 sync adapter).

| Spec | Today | Verdict | Rationale |
|---|---|---|---|
| `cloud/multi-tenant.md` | drafted; per-org instance provisioning, control plane, routing, hibernation, sandbox model | **ARCHIVE most / KEEP slice** | Instance-per-org provisioning, routing, hibernation = Cella + infra concerns. Product doc says v1 is **one metadata service**, not per-tenant instances. Keep only the org-scoping-within-one-service slice — already largely done via `OrgID` filtering. |
| `cloud/tenant-filesystem.md` | drafted; FS cold/hot integration | **CONSUME (Phase 3+)** | Rescope to "thin `FSClient` over fs.latere.ai Workspace API." **Blocked on FS Phase 5 (not built, not ours to unblock).** This is the #1 external dependency. |
| `cloud/tenant-api.md` | drafted; external `v1/` tenant API | **DEFER (Phase 3+)** | Public tenant API belongs after the metadata service + Cella execution exist. |
| `cloud/billing-idempotency.md` | drafted; Stripe idempotency keys | **CONSUME** | auth already owns Stripe + charge mechanics. Wallfacer keeps only subscription/usage *UX*; charge idempotency is auth's. Defer until payment is introduced. |
| `cloud/cloud-infrastructure.md` | drafted; full K8s manifests for wallfacer | **KEEP (shrink)** | Reduce to: one deploy module in `terraform` for the metadata service + OTEL emit (pkg/otel ready). Not a from-scratch infra design. |
| `foundations/storage-backends/task-04-database-backend` | specced, not built | **DEFER / RECONSIDER** | The v1 model is "local filesystem stays source of truth; cloud *replicates* metadata into its own DB." That DB belongs to the **cloud metadata service**, not to a wallfacer `StorageBackend` swap. Only needed if we later host wallfacer itself fully in-cloud (Phase 3+). |
| `foundations/storage-backends/task-05-object-storage-backend` | specced, not built | **DEFER** | Same as above. Cella/FS own object storage; a wallfacer S3 blob backend is only for a fully-cloud-hosted instance. |
| `foundations/sandbox-backends.md` (+ a future K8s backend) | interface complete | **CONSUME (Phase 3+)** | A `CellaBackend implements sandbox.Backend` calling `/v1/sandboxes/exec` — gated by demand. Do **not** build wallfacer-owned K8s logic. |
| `observability/telemetry-queue-backpressure.md` | drafted; bounded offline queue | **FOLD into v1 sync adapter** | This *is* the offline-durability requirement of metadata sync. Not a separate effort. |
| `identity/data-boundary-enforcement.md` | drafted; CI allowlist of exportable fields | **FOLD into v1 sync adapter (+ CI gate)** | The sync adapter's redaction allowlist; CI test enforces it. Directly v1-relevant. |
| `observability/audit-log.md` | drafted | **DEFER (multi-user)** | Needed for team deployments, not single-user cloud v1. |
| `observability/telemetry-observability.md` | drafted | **PARTIAL (v1) / DEFER (rest)** | v1 piece = emit OTLP via pkg/otel (infra ready). Full Mimir/Loki/Tempo design is later. |
| `identity/agent-token-exchange.md` | drafted | **DEFER (Phase 3)** | Only needed when agents call FS/telemetry from sandboxes — i.e., Cella execution phase. RFC 8693 endpoint already exists in auth. |
| `identity/multi-user-collaboration.md` | drafted | **DEFER** | Team feature, post-v1. |
| `identity/{remote-control,third-party-oidc}.md` | vague | **KEEP vague** | Genuinely later/exploratory. |

**Net cleanup:** archive/shrink `multi-tenant`, defer `tenant-api` + storage
tasks 04/05 + agent-token + multi-user + audit, rescope `tenant-filesystem` and
`billing` to consume-clients, fold `queue-backpressure` + `data-boundary` into
the new sync adapter. That removes ~6 specs' worth of "build infra we don't own"
from the active roadmap.

---

## Part 3 — Cloud v1 design: the metadata-sync adapter (the only genuinely new work)

Goal: local mode byte-identical to today; cloud mode additionally **replicates
allowlisted metadata** to a cloud store, surviving offline, never leaking code.

### New seam in wallfacer (small)
```go
// internal/cloudsync (new) — nil in local mode.
type MetadataSink interface {
    // Record enqueues an allowlisted, redacted metadata event. Never blocks
    // task execution; returns immediately after enqueue.
    Record(ev MetadataEvent) // best-effort, fire-and-forget
    Flush(ctx context.Context) error
}
```
- **Tap point:** the existing event-sourcing stream (`store.TaskEvent`). One
  subscriber maps each event → `MetadataEvent` through the **data-boundary
  allowlist** (Part 2), dropping everything forbidden.
- **Offline durability:** bounded local queue (the `queue-backpressure` spec —
  cap ~10k, drop-oldest, exponential-backoff retry). Cloud outage ⇒ local
  execution unaffected.
- **Transport:** authenticated HTTPS to the cloud metadata service, bearer token
  minted from the signed-in principal (service-account/token-exchange via auth).
- **Selection:** `WALLFACER_CLOUD=true` + `AUTH_*` ⇒ real sink; else nil sink
  (no-op). Mirrors how `AuthProvider`/`jwtValidator` are already wired.

### New component outside wallfacer (separate small service)
The **cloud metadata service** (its own repo + Postgres, behind auth JWT,
deployed via a thin terraform module). It owns the replicated metadata DB — this
is why wallfacer needs **no** Postgres `StorageBackend` for v1. Wallfacer only
ships the sink client.

### What v1 delivers (matches product doc "Cloud v1 owns")
Account linkage (done) · persistent task/spec history · usage/cost aggregation ·
optional team visibility · subscription UX — all from replicated metadata, code
never leaving the machine.

---

## Part 4 — The one hard design problem (Phase 3, not v1)

**Workspace/tenancy keying.** Today `workspace.Manager` keys data by a SHA-256
hash of local absolute paths (`Snapshot.Key`, `ScopedDataDir`). For v1 this is a
non-issue — local paths are *forbidden export data* anyway, and cloud keys
records by `OrgID + Task UUID`. The problem only appears in **Phase 3 (cloud
execution / FS workspaces)**, where a workspace must be identified as
`org + workspace-slug` rather than a host path. That needs a `WorkspaceResolver`
seam (path-hash ↔ cloud identity) — design it then, not now.

---

## Part 5 — Phasing

- **Phase 1 — done.** Identity sign-in + JWT + principal/org model.
- **Phase 2 — Cloud v1 (do-now candidate).** Metadata-sync adapter (sink +
  allowlist + offline queue) · cloud metadata service · OTLP emit (pkg/otel) ·
  thin terraform deploy module · enforce data-boundary in CI. *No execution, no
  FS, no per-tenant instances.*
- **Phase 3+ — gated by demand & FS Phase 5.** `CellaBackend` (sandbox.Backend
  over `/v1/sandboxes`) · `FSClient` (consume FS Workspace API) · agent token
  exchange · `WorkspaceResolver`. **Blocked externally on fs.latere.ai.**
- **Phase 4 — team.** Multi-user collaboration · audit log · billing UX.

---

## Decision points (need your steer before formalizing)

1. **Confirm v1 = metadata-sync only** (defer all execution/storage/FS), per the
   product doc. If yes, I'll write `cloud/metadata-sync.md` as the one new
   tracked spec and rewire `specs/README.md`.
2. **Cleanup execution:** should I actually transition the five `specs/cloud/*`
   to their verdicts now (archive `multi-tenant`, rescope `tenant-filesystem`/
   `billing`, defer the rest via status + notes), or only after you review this?
3. **Cloud metadata service home:** new repo under `latere.ai/`, or a subpackage
   the wallfacer binary can also serve? (Affects deploy + auth boundary.)
4. **Where the sink interface lives:** new `internal/cloudsync/` (my proposal) vs
   extending an existing package.
