---
title: Cloud Infrastructure
status: drafted
depends_on:
  - specs/foundations/sandbox-backends.md
  - specs/foundations/storage-backends.md
affects:
  - deploy/
effort: large
created: 2026-03-28
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Cloud Infrastructure

## Problem

Wallfacer runs locally today — a Go binary on the host, launching containers via podman/docker, storing task data on the local filesystem. To run wallfacer as a hosted service under latere.ai, it needs to be deployed as a workload in an existing Kubernetes cluster alongside latere.ai's other services.

Latere.ai already operates a DigitalOcean infrastructure (see `latere.ai/terraform/`) that provisions a DOKS cluster, Spaces (S3-compatible) storage, DNS, TLS, and an observability stack. Wallfacer doesn't need to provision its own infrastructure — it needs K8s manifests that deploy it into this existing cluster, and clear documentation of what cluster-level resources (RBAC, PVCs, Secrets) must be added to the latere.ai terraform.

## Current Infrastructure (latere.ai)

The latere.ai terraform provisions (all in DigitalOcean, `fra1` region):

| Resource | Name/Type | Cost |
|----------|-----------|------|
| K8s cluster | `latere-k8s`, single node pool (`s-2vcpu-4gb`) | ~$24/mo |
| Load balancer | nginx-ingress-controller | ~$12/mo |
| Object storage | `latere-storage` Spaces bucket + CDN | ~$2/mo |
| TLS | cert-manager + Let's Encrypt (ClusterIssuer) | Free |
| DNS | DigitalOcean DNS for `latere.ai` | Free |
| Observability | ClickHouse + OTEL Collector + Grafana (in-cluster) | $0 (runs on existing node) |
| Container registry | ghcr.io (GitHub Container Registry) | Free tier |
| File storage service | fs.latere.ai (cold: Spaces/S3, hot: local disk) | Included |

fs.latere.ai is the platform's **user data plane** — a two-tier file storage and workspace service. Cold tier persists files durably in S3; hot tier stages files onto compute nodes for fast I/O. Its Workspace API (`POST /workspaces`, `DELETE /workspaces/{id}`) provides sandboxes with locked, mounted working copies. Wallfacer integrates as a consumer of this API for per-tenant repo storage and sandbox file mounts.

**What latere.ai does NOT currently have:**
- **fs.latere.ai Phase 5 (Workspace API)** — wallfacer needs the workspace endpoints for staging repos to the hot tier before sandbox execution; this API is spec'd but implementation is in progress
- **RBAC for Job creation** — wallfacer's K8s sandbox backend needs a ServiceAccount with permission to create/watch/delete Jobs and Pods
- **Dedicated node pool for sandbox pods** — sandbox containers (Claude, Codex) are resource-heavy; running them on the same node as the control plane risks resource contention

## Architecture: Two Layers

The "two layers" model remains correct, but with a key clarification: latere.ai owns the infrastructure layer, and wallfacer owns the application layer.

```
┌──────────────────────────────────────────────────────┐
│  Application Layer (wallfacer's K8s manifests)       │
│                                                      │
│  wallfacer Deployment ──▶ K8s API (sandbox Jobs)     │
│                       ──▶ fs.latere.ai (file storage)│
│                       ──▶ PG or filesystem (tasks)   │
│                       ──▶ S3 API (blobs)             │
│                                                      │
│  Lives in: wallfacer repo (deploy/)                  │
└──────────────────────────────┬───────────────────────┘
                               │
                               │ Secrets, ConfigMaps, RBAC
                               │
┌──────────────────────────────▼───────────────────────┐
│  Infrastructure Layer (latere.ai terraform)          │
│                                                      │
│  K8s cluster, Spaces bucket, DNS records,            │
│  TLS certs, node pools, ServiceAccounts              │
│                                                      │
│  Lives in: latere.ai/terraform/                      │
└──────────────────────────────────────────────────────┘
```

### Ownership Boundary

| Concern | Owner | Location |
|---------|-------|----------|
| K8s cluster, node pools | latere.ai terraform | `latere.ai/terraform/main.tf` |
| Spaces bucket | latere.ai terraform | `latere.ai/terraform/main.tf` |
| DNS records (`wallfacer.latere.ai`) | latere.ai terraform | `latere.ai/terraform/main.tf` |
| TLS certificates | latere.ai terraform | cert-manager ClusterIssuer |
| ServiceAccount + RBAC for sandbox Jobs | latere.ai terraform | New resources in `main.tf` |
| Wallfacer Deployment, Service, Ingress | wallfacer repo | `deploy/` |
| Wallfacer ConfigMap/Secret | wallfacer repo | `deploy/` |
| PVC for task data | wallfacer repo | `deploy/` |
| NetworkPolicy for sandbox pods | wallfacer repo | `deploy/` |

---

## Decision Point: Storage Backend

Wallfacer has two storage concerns with different strategies:

### Repo / workspace files → fs.latere.ai

Per-tenant repositories, worktrees, and workspace config are managed by fs.latere.ai. Wallfacer's `RepoProvisioner` clones repos onto the hot tier via the Workspace API; sandbox Jobs mount the hot tier path. This replaces the local filesystem's absolute-path workspace model. See `specs/cloud/tenant-filesystem.md` for the full integration design.

### Task metadata and blobs → StorageBackend

Wallfacer's `StorageBackend` interface handles task state, events, and output blobs. Two options:

**Option A: Filesystem on PVC (start here)**

Use `FilesystemBackend` (already implemented) with a PersistentVolumeClaim. Task data lives on a DO Volume mounted into the wallfacer pod.

- **Pros:** No new infrastructure needed, zero migration from local, works immediately
- **Cons:** Single-pod constraint (ReadWriteOnce volume), no SQL queries across tasks, backup requires volume snapshots
- **Infrastructure needed:** One PVC (`do-block-storage` StorageClass, ~$0.10/GB/mo)

**Option B: PostgreSQL + S3**

Use `DatabaseBackend` (spec'd, not yet implemented) for task metadata and `ObjectStorageBackend` for blobs.

- **Pros:** Multi-pod ready, SQL queries, horizontal scaling, standard backup/restore
- **Cons:** Requires implementing database + object storage backends, needs managed PG or in-cluster PG
- **Infrastructure needed:**
  - Managed DO PostgreSQL (`db-s-1vcpu-1gb`, ~$15/mo) — or in-cluster via CloudNativePG operator
  - Spaces bucket access (already exists: `latere-storage`, or a dedicated bucket)

**Recommendation:** Start with Option A for task metadata. Repo/workspace files go through fs.latere.ai regardless. Transition task storage to Option B when multi-instance scaling is needed.

---

## What Wallfacer Needs in the Cluster

### 1. Wallfacer Server Pod

A Deployment running the wallfacer binary with environment variables for configuration.

```yaml
# deploy/deployment.yaml (simplified)
apiVersion: apps/v1
kind: Deployment
metadata:
  name: wallfacer
  namespace: latere
spec:
  replicas: 1
  template:
    spec:
      serviceAccountName: wallfacer
      containers:
        - name: wallfacer
          image: ghcr.io/changkun/wallfacer:latest
          ports:
            - containerPort: 8080
          envFrom:
            - secretRef:
                name: wallfacer-env
          volumeMounts:
            - name: data
              mountPath: /data
      volumes:
        - name: data
          persistentVolumeClaim:
            claimName: wallfacer-data
```

### 2. Ingress + TLS

An Ingress resource routing `wallfacer.latere.ai` to the wallfacer Service, using the existing cert-manager ClusterIssuer.

### 3. ServiceAccount + RBAC

Wallfacer's K8s sandbox backend creates Jobs to run sandbox containers. The server pod needs RBAC permissions:

```yaml
# deploy/rbac.yaml (simplified)
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: wallfacer-sandbox
  namespace: latere
rules:
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["create", "get", "list", "watch", "delete"]
  - apiGroups: [""]
    resources: ["pods", "pods/log", "pods/exec"]
    verbs: ["get", "list", "watch", "create"]
  - apiGroups: [""]
    resources: ["persistentvolumeclaims"]
    verbs: ["create", "get", "list", "delete"]
```

### 4. PVC for Task Data

A PersistentVolumeClaim using DO's `do-block-storage` StorageClass:

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

### 5. Secrets

Environment variables injected via K8s Secret:

- `CLAUDE_CODE_OAUTH_TOKEN` or `ANTHROPIC_API_KEY` — LLM credentials
- `OPENAI_API_KEY` — for Codex sandbox (optional)
- `WALLFACER_SERVER_API_KEY` — server auth
- `FS_LATERE_AI_URL` + `FS_LATERE_AI_TOKEN` — fs.latere.ai endpoint and JWT for Workspace API
- `DATABASE_URL` — if using PG backend (Option B)

### 6. Sandbox Image Availability

Sandbox images (Claude Code, Codex) must be pullable from within the cluster. Options:
- Pull from public registries (Docker Hub, ghcr.io) — simplest
- Mirror to a private registry and use imagePullSecrets — more control

### 7. Node Pool Sizing

Sandbox pods are resource-intensive. The current single `s-2vcpu-4gb` node is insufficient for running wallfacer + sandbox containers concurrently. Options:

- **Dedicated sandbox node pool:** Add a pool of larger nodes (e.g., `s-4vcpu-8gb` or `g-2vcpu-8gb`) with taints so only sandbox Jobs schedule there
- **Autoscaling node pool:** DOKS supports node pool autoscaling (min 1, max N); sandbox Jobs trigger scale-up, nodes drain after idle timeout
- **Start small:** Use the existing node for wallfacer server only; launch sandbox containers on a single additional node, scale up manually as needed

---

## Infrastructure Changes to latere.ai Terraform

The following resources need to be added to `latere.ai/terraform/main.tf`:

### Required

1. **DNS record** for `wallfacer.latere.ai` → load balancer IP (A record, same pattern as existing subdomains)
2. **TLS certificate** for `wallfacer.latere.ai` (cert-manager Certificate resource, same pattern as `latere-tls`)
3. **Sandbox node pool** (new `digitalocean_kubernetes_node_pool`) — at minimum one `s-4vcpu-8gb` node for sandbox Jobs

### Optional (for Option B storage)

4. **Managed PostgreSQL** (`digitalocean_database_cluster`, engine `pg`, size `db-s-1vcpu-1gb`)
5. **Dedicated Spaces bucket** for wallfacer blobs (or reuse `latere-storage` with a key prefix)

---

## Manifest Structure

```
deploy/
├── namespace.yaml            # Skip if using existing 'latere' namespace
├── deployment.yaml           # Wallfacer server Deployment
├── service.yaml              # ClusterIP Service
├── ingress.yaml              # Ingress for wallfacer.latere.ai
├── pvc.yaml                  # PersistentVolumeClaim for task data
├── rbac.yaml                 # ServiceAccount, Role, RoleBinding for sandbox Jobs
├── secret.yaml.example       # Template for wallfacer-env Secret
├── networkpolicy.yaml        # Restrict sandbox pod egress
└── kustomization.yaml        # Kustomize overlay for environment variants
```

---

## Implementation Tasks

| # | Task | Depends on | Effort |
|---|------|-----------|--------|
| 1 | Write `deploy/` K8s manifests (Deployment, Service, Ingress, PVC, RBAC, NetworkPolicy) | Sandbox backend interface (done) | Medium |
| 2 | Build wallfacer container image + CI pipeline (Dockerfile, GitHub Actions) | — | Medium |
| 3 | Add DNS record + TLS cert + sandbox node pool to latere.ai terraform | 1 | Small |
| 4 | End-to-end deployment test: deploy to latere.ai cluster, create task, run sandbox | 1, 2, 3 | Medium |
| 5 | Document deployment workflow (terraform changes, manifest apply, secrets setup) | 4 | Small |

Tasks 1 and 2 are independent and can proceed in parallel.

---

## Future Work (deferred)

- **Multi-cloud support** (AWS, GCP, Alibaba): Not needed now. The application layer is cloud-agnostic by design; adding another provider means writing new terraform + adjusting manifests, not changing wallfacer code.
- **PostgreSQL + S3 storage backend**: Implement when scaling beyond single-instance. Requires database backend implementation (see `specs/foundations/storage-backends/`).
- **Autoscaling sandbox node pool**: Configure DOKS autoscaler when sandbox utilization justifies it.
- **Self-hosted / on-prem deployment guide**: Write when there's demand. The K8s manifests work on any cluster; only StorageClass and Ingress differ.

---

## Dependencies

- **Sandbox Backend Interface** (`specs/foundations/sandbox-backends.md`) — defines the `Backend` interface that `K8sBackend` will implement (complete)
- **Storage Backend Interface** (`specs/foundations/storage-backends.md`) — defines `StorageBackend`; `FilesystemBackend` is implemented (complete)
- **K8s Sandbox Backend** (`specs/cloud/k8s-sandbox.md`) — implements `K8sBackend` for dispatching Jobs; must be implemented before sandbox tasks work in-cluster

## What depends on this

- **Multi-Tenant** (`specs/cloud/multi-tenant.md`) — control plane deployment requires working cloud infrastructure

---

## Note: Cloud Track Alignment

This spec has been rewritten to reflect latere.ai's real infrastructure. The other cloud track specs (`cloud-backends.md`, `tenant-filesystem.md`, `k8s-sandbox.md`, `multi-tenant.md`, `tenant-api.md`) still assume a greenfield, multi-cloud architecture and will need similar alignment. Key shifts:

- **cloud-backends.md**: The "VPS mode" is effectively what we have now (single K8s node). No separate VPS provisioner needed.
- **tenant-filesystem.md**: Integrates with fs.latere.ai for per-tenant storage (cold tier for config persistence, hot tier for runtime workspace). No standalone tenant PVC needed — fs.latere.ai owns storage allocation.
- **k8s-sandbox.md**: RBAC and namespace assumptions should match the latere.ai cluster setup. Volume mounts point at fs.latere.ai hot tier paths instead of standalone PVCs.
- **multi-tenant.md**: Control plane runs in the same cluster, not a separate provisioning system.
