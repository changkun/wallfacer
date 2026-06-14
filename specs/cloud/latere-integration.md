---
title: Latere Platform Integration
status: drafted
depends_on:
  - specs/identity/authentication.md
  - specs/foundations/sandbox-backends.md
  - specs/foundations/storage-backends.md
affects:
  - internal/executor/
  - internal/store/
  - internal/auth/
  - internal/workspace/
  - internal/runner/
effort: large
created: 2026-05-30
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Latere Platform Integration

## Problem

Wallfacer is a Latere product, but today it runs as a self-contained binary
that knows nothing about the rest of the Latere ecosystem. Identity sign-in
(Phase 2) is the only integration that has shipped. Meanwhile the platform has
grown standalone services that own concerns wallfacer's older cloud specs
proposed to build from scratch:

- **Identity** (`latere.ai/x/auth`, auth.latere.ai) - OIDC, JWTs, orgs, teams,
  service accounts, Stripe billing, RFC 8693 token exchange.
- **Cella** (`latere.ai/x/sandbox`, cella.latere.ai) - K8s sandbox execution,
  warm pools, durable workspaces, credential vault, per-sandbox identity JWTs.
- **FS** (`latere.ai/x/fs`, fs.latere.ai) - two-tier file data plane (Spaces
  cold + PVC hot); `/files/*` is live, `/workspaces/*` is planned.
- **Lux** - model key custody and routing.
- **MCP Registry** - approved tool catalog.
- **`latere.ai/x/pkg`** - shared Go libraries (`jwtauth`, `oidc`, `authkit`,
  `scopes`, `otel`, `audit`); already a wallfacer dependency.

This umbrella defines **how wallfacer integrates with those services** so that
the local-first product keeps working unchanged while cloud mode gains
ecosystem value incrementally.

## Principle: consume, don't absorb

From the product north star (`latere.ai/specs/products/wallfacer.md`):

> Wallfacer is the autonomous engineering control plane. It should consume
> Latere platform services in cloud mode rather than absorbing them. Cloud v1
> is metadata coordination, not cloud execution.

Wallfacer **owns**: the spec model, task lifecycle, agents/flows, oversight,
git/worktree workflow, autonomy controls, and local-first UX. Wallfacer **does
not own** runtime infrastructure (Cella), identity (Identity), the file data
plane (FS), model keys (Lux), or the MCP catalog (Registry). Each integration
is a thin client over a service boundary, never a reimplementation.

## Integration seams

Each seam is either an interface that already exists in wallfacer (cloud impl
slots in, local stays default) or a new thin adapter. Selection is config-gated
(`WALLFACER_CLOUD` + the relevant service URL); a nil/absent client means the
seam is inert and local behavior is byte-identical to today.

| Seam | Latere service | Wallfacer interface | Status | Spec |
|------|----------------|---------------------|--------|------|
| **Identity** | auth.latere.ai | `internal/auth` middleware + `pkg/jwtauth`/`oidc`; `authkit.Identity{Sub,OrgID}` principal | ✅ shipped (Phase 1+2) | [identity/authentication.md](../identity/authentication.md) |
| **Runtime** | Cella | `executor.Backend` (today: Host only; the cloud impl slots in as a third executor) | drafted | [latere-integration/cella-runtime.md](latere-integration/cella-runtime.md) |
| **Cella wire client** | Cella | shared Go client at `latere.ai/x/sandbox/client`, consumed by Wallfacer's `CellaBackend` and Topos's `cella.Provider` | drafted | [latere-integration/shared-cella-client.md](latere-integration/shared-cella-client.md) |
| **File data plane** | FS | `internal/workspace` + `internal/runner` (worktree staging) | drafted; **blocked on FS Workspace API** | [tenant-filesystem.md](tenant-filesystem.md) |
| **Per-task delegation** | auth (RFC 8693) | mint short-lived agent tokens so sandboxes call back | drafted | [identity/agent-token-exchange.md](../identity/agent-token-exchange.md) |
| **Deploy** | terraform (DOKS) | thin deploy module + `pkg/otel` OTLP emit | drafted | [cloud-infrastructure.md](cloud-infrastructure.md) |
| **Model keys** | Lux | credential injection into task env | future | - (specced when scheduled) |
| **MCP catalog** | MCP Registry | approved-tool resolution | future | - |
| **Cloud metadata / history / usage** | (TBD cloud metadata service) | tap the `store.TaskEvent` stream, redact to an allow-list, push | undefined - **pending scope decision** | - (candidate: a `metadata-sync.md` leaf) |

## Design rules

1. **Local-first is invariant.** No seam may change local-anonymous behavior.
   The default build with no Latere services configured runs exactly as today
   (host agent process + filesystem store + no auth).
2. **Config-gated, nil-safe selection.** Mirror the existing pattern: cloud
   wiring activates only when `WALLFACER_CLOUD=true` and the service's config is
   present; otherwise the client is nil and call sites short-circuit (the way
   `AuthProvider`/`jwtValidator` already do).
3. **Interface first.** Prefer implementing an existing wallfacer interface
   (`executor.Backend`, `store.StorageBackend`, `handler.AuthProvider`) over
   inventing a new one. Extract a new seam only where none fits.
4. **Data boundary holds.** Anything leaving the machine obeys
   [data-boundary-enforcement.md](data-boundary-enforcement.md):
   metadata may leave; source, diffs, secrets, and repo paths may not. Cloud
   *execution* (Cella) deliberately crosses this boundary and is therefore a
   later, explicitly opt-in phase - not part of metadata coordination.
5. **One service, one owner.** If a Latere service owns a concern, wallfacer
   consumes it and does not keep a parallel implementation.

## Phasing

- **Phase 1 - done.** Identity sign-in + JWT + principal/org model.
- **Phase 2 - integration interfaces (this track).** Define each seam's contract
  and the config selection. The runtime seam ([cella-runtime.md](latere-integration/cella-runtime.md))
  is the lead example because it adds a cloud runtime behind the existing
  `executor.Backend` interface.
- **Phase 3+ - gated by demand & FS Workspace API.** Cella execution, FS
  workspace staging, agent token exchange, model-key routing via Lux.

## Boundaries

- Do **not** build runtime scheduling, identity, file storage, model-key
  custody, or an MCP catalog inside wallfacer - those are owned by Cella,
  Identity, FS, Lux, and the Registry respectively.
- Do **not** gate or alter local execution behind any of these seams.
- This umbrella defines *contracts and selection*; each leaf spec carries the
  concrete adapter design, tests, and docs.

## Open questions

- Does cloud "metadata coordination" (history/usage/team visibility) live in a
  separate Latere service, or as a `wallfacer cloud` server mode? Decides
  whether a `metadata-sync.md` leaf is needed and where its store lives.
- How do worktrees reach a remote Cella sandbox (FS Workspace API vs git
  push/pull vs Cella durable workspace)? Owned by the runtime + FS leaves.
