---
title: Cloud Infrastructure
status: drafted
depends_on:
  - specs/foundations/storage-backends.md
  - specs/cloud/latere-integration.md
affects:
  - deploy/
effort: medium
created: 2026-03-28
updated: 2026-05-30
author: changkun
dispatched_task_id: null
---

# Cloud Infrastructure

> **Rescoped 2026-05-30.** This spec previously had wallfacer provision its own
> K8s sandbox execution (RBAC for Jobs, a dedicated sandbox node pool,
> NetworkPolicy for sandbox pods, a `K8sBackend` creating Jobs directly). That
> half is gone: **Cella owns sandbox runtime** — wallfacer dispatches to it
> through the runtime seam ([latere-integration/cella-runtime.md](latere-integration/cella-runtime.md)),
> it does not schedule sandboxes itself. What remains is the part that is
> genuinely wallfacer's: **deploying the task-board server into the existing
> `latere` cluster**, reusing the proven `wallfacerd` web deployment pattern.

## Problem

Wallfacer runs locally today — a Go binary on the host, storing task data on the
local filesystem. To offer a hosted task board under latere.ai, the **task-board
server** (`wallfacer run`) must be deployed as a workload in the existing DOKS
`latere` cluster, alongside the other latere.ai services.

The deploy *mechanics* are already proven: the public site (`wallfacer web`)
ships today as the `wallfacerd` Deployment at `wf.latere.ai`
(`deploy/prod/{deployment,service,ingress}.yaml`, `Dockerfile.wallfacerd`,
`.github/workflows/wallfacerd.yml` → `deploy-wallfacerd.yml`). It runs in the
`latere` namespace with cert-manager TLS, nginx ingress, OTLP export, and
`AUTH_URL`/`AUTH_REDIRECT_URL` wired to Identity. The task-board server is a
second workload following the same pattern, plus the one thing the stateless
website doesn't need: **durable task-data storage**.

## What's already deployed vs. what this spec adds

| Component | State |
|-----------|-------|
| DOKS cluster, nginx ingress, cert-manager, Spaces, DNS, OTEL→ClickHouse→Grafana | exists (latere.ai/terraform) |
| `wallfacerd` = `wallfacer web` (public site) at `wf.latere.ai` | **deployed** |
| Deploy pattern (Deployment/Service/Ingress + TLS + OTLP into `latere` ns) | **proven by `wallfacerd`** |
| **task-board server** (`wallfacer run`) Deployment + PVC for task data | **this spec** |

## Ownership boundary

| Concern | Owner |
|---------|-------|
| K8s cluster, node pools, Spaces, DNS, TLS ClusterIssuer | latere.ai/terraform |
| **Sandbox runtime** (scheduling, pods, warm pools, egress, hardening, RBAC for Jobs) | **Cella** (`latere.ai/sandbox`) — consumed via [cella-runtime.md](latere-integration/cella-runtime.md) |
| Identity / sign-in | Identity (auth.latere.ai), already wired in `wallfacerd` |
| Workspace files | FS (fs.latere.ai), see [tenant-filesystem.md](tenant-filesystem.md) |
| Task-board server Deployment, Service, Ingress, PVC, Secret | **wallfacer** (`deploy/`) |

Wallfacer does **not** add RBAC for Job creation, a sandbox node pool, or a
sandbox NetworkPolicy — those moved to Cella with the runtime seam.

## What the task-board server needs in the cluster

### 1. Deployment

A second Deployment in `latere` running the task-board server, modeled on
`wallfacerd`. Differences from the website workload:

- Command `wallfacer run` (not `web`), serving the board API/UI on `:8080`.
- Larger resource requests than the website's `10m`/`32Mi` (it holds the store
  and runs automation loops) — size from load, start modest.
- A mounted data volume (see PVC below).
- Runtime backend selects **Cella** in cloud mode (`--backend cella`), not local
  podman — so the pod needs no container runtime or privileged access.
- Env: LLM creds (`CLAUDE_CODE_OAUTH_TOKEN`/`ANTHROPIC_API_KEY`, optional
  `OPENAI_API_KEY`), `WALLFACER_SERVER_API_KEY`, `WALLFACER_CLOUD=true`,
  `AUTH_*` (as `wallfacerd` already sets), `CELLA_URL`, and `OTEL_EXPORTER_OTLP_ENDPOINT`.

### 2. Service + Ingress + TLS

ClusterIP Service and an Ingress for the board host (e.g. `app.latere.ai` or a
subpath), reusing the `letsencrypt-prod` ClusterIssuer and nginx ingress class
exactly as `wallfacerd` does.

### 3. PVC for task data (the one genuinely new piece)

`FilesystemBackend` (already implemented) on a `do-block-storage` PVC mounted
into the pod:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: wallfacer-data
  namespace: latere
spec:
  accessModes: ["ReadWriteOnce"]
  storageClassName: do-block-storage
  resources:
    requests:
      storage: 20Gi
```

`ReadWriteOnce` ties the server to a single replica — fine for v1. Multi-replica
(a Postgres/object-storage `StorageBackend`) is deferred; see
[storage-backends.md](../foundations/storage-backends.md) (those backend tasks
are archived until multi-instance scaling is actually needed).

### 4. Secret

A K8s Secret (mirroring `wallfacerd-auth`) supplying the env above.

## Manifest structure

```
deploy/
├── prod/
│   ├── deployment.yaml      # wallfacerd (website) — exists
│   ├── service.yaml         # exists
│   ├── ingress.yaml         # exists
│   ├── board-deployment.yaml  # NEW: task-board server
│   ├── board-service.yaml     # NEW
│   ├── board-ingress.yaml     # NEW
│   └── board-pvc.yaml         # NEW: task-data volume
└── secret.yaml.example      # template for the board Secret
```

(No `rbac.yaml`, no `networkpolicy.yaml` — sandbox concerns are Cella's.)

## Implementation tasks

| # | Task | Depends on | Effort |
|---|------|-----------|--------|
| 1 | Add board `Deployment`/`Service`/`Ingress`/`PVC` manifests under `deploy/prod/`, modeled on `wallfacerd` | runtime seam defined ([cella-runtime.md](latere-integration/cella-runtime.md)) | Small |
| 2 | Extend the existing `wallfacerd` CI to build/push the board image (or reuse the same image, different command) | — | Small |
| 3 | Board Ingress host + TLS cert in terraform (DNS A record + Certificate, same pattern as `wf.latere.ai`) | 1 | Small |
| 4 | E2E: deploy to `latere`, sign in, create a task, run it via Cella, verify task data persists across pod restart | 1, 2, 3, Cella backend | Medium |
| 5 | Document the deploy workflow (manifests, secrets, terraform additions) | 4 | Small |

## Cost (DigitalOcean, incremental over the existing latere.ai baseline)

| Item | Cost |
|------|------|
| Task-board server pod | +$0 (fits existing node pool) |
| `wallfacer-data` PVC (20Gi `do-block-storage`) | ~$2/mo |
| Sandbox compute | **$0 here** — owned and billed by Cella, not this spec |

The sandbox node-pool line items from the previous version are gone: Cella runs
sandboxes on its own (tainted) pool and accounts for that compute.

## Dependencies

- [storage-backends.md](../foundations/storage-backends.md) — `FilesystemBackend` on the PVC (complete).
- [latere-integration.md](latere-integration.md) — the umbrella; the runtime
  ([cella-runtime.md](latere-integration/cella-runtime.md)) and FS
  ([tenant-filesystem.md](tenant-filesystem.md)) seams the deployed server consumes.
- Identity (auth.latere.ai) — already wired into `wallfacerd`; the board server reuses the same `AUTH_*` config.

## What depends on this

- The cloud integration track's runnable surface: once the board server is
  deployed, the Cella runtime and FS seams have somewhere to run.

## Future work (deferred)

- Postgres + S3 `StorageBackend` for multi-replica scaling (storage tasks are
  archived until demand exists).
- Autoscaling / multi-region — terraform concerns, not wallfacer's.
- Self-hosted deployment guide — the manifests work on any cluster; only
  StorageClass and Ingress differ.
