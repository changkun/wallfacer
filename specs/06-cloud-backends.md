# M6: Cloud Deployment

**Status:** Not started | **Date:** 2026-03-28

## Deployment Strategy

Wallfacer has two deployment modes. There is no intermediate step — the VPS model serves personal/development use, and when multi-tenant is needed, go straight to K8s.

### Mode 1: Single-User VPS (works today)

One VM, one user, everything local. No cloud milestones required.

```
Internet → Caddy (HTTPS) → wallfacer :8080 (WALLFACER_SERVER_API_KEY)
                                ↓
                           Podman (task containers)
                                ↓
                  /home/user/repos/<workspace>
```

- Filesystem storage on local disk (`~/.wallfacer/`)
- Task containers run locally via `LocalBackend`
- Cost: **~$48–96/mo** on DO (single Droplet)
- Setup: `git clone` repos, install Podman, systemd unit, Caddy for TLS

This is the development and personal environment. The cloud stack (Mode 2) is validated here first — you become tenant #1 on your own K8s cluster.

### Mode 2: Multi-Tenant K8s (scaling target)

When the business grows beyond a single user, deploy directly to K8s. Each tenant gets a dedicated wallfacer pod with a persistent volume, and task containers dispatch as K8s Jobs on shared worker nodes.

```
                        ┌─────────────────────────┐
                        │     Control Plane        │
                        │  (auth, provisioning,    │
                        │   instance lifecycle)    │
                        └──────────┬──────────────┘
                                   │
            ┌──────────────────────┼──────────────────────┐
            │                      │                      │
   ┌────────▼────────┐   ┌────────▼────────┐   ┌────────▼────────┐
   │  Tenant A Pod    │   │  Tenant B Pod    │   │  Tenant C Pod    │
   │  wallfacer :8080 │   │  wallfacer :8080 │   │  wallfacer :8080 │
   │  + tenant PVC    │   │  + tenant PVC    │   │  + tenant PVC    │
   └────────┬────────┘   └────────┬────────┘   └────────┬────────┘
            │                      │                      │
            └──────────────────────┼──────────────────────┘
                                   │
                        ┌──────────▼──────────┐
                        │  Shared K8s cluster  │
                        │  (sandbox Jobs,      │
                        │   PG, S3, LB)        │
                        └─────────────────────┘
```

Per instance, the layers connect as:

| Layer | Component | Storage |
|-------|-----------|---------|
| **Task data** (metadata, events, blobs) | `StorageBackend` (M2) | PostgreSQL + S3 |
| **Filesystem** (repos, worktrees, config) | Tenant volume (M6a) | PVC per tenant |
| **Sandbox execution** | `K8sBackend` (M6b) | K8s Jobs mounting tenant PVC |
| **Identity & lifecycle** | Control plane (M8) | Control plane DB |

**Why skip VM-per-tenant?** The wallfacer binary doesn't change between modes — the same code runs on a VM or in a K8s pod. A VM-per-tenant intermediate step would require building a VM provisioner in the control plane, then throwing it away when migrating to K8s. Going straight to K8s avoids that wasted work. On DigitalOcean, DOKS control plane is free, so the cost premium over VPS is ~$32/mo (managed PG + Spaces + LB) — worth it to avoid a migration.

### Cost Estimates (DigitalOcean)

| Scale | Mode 1 (VPS) | Mode 2 (DOKS) | Notes |
|-------|-------------|---------------|-------|
| **1 tenant** (personal) | $48–96/mo | $128/mo | You as tenant #1 on the cluster |
| **5 tenants** | N/A | ~$320/mo | 3 worker nodes, shared PG+Spaces |
| **10 tenants** | N/A | ~$430/mo | 4 worker nodes; idle tenants cost ~$0 |
| **20 tenants** | N/A | ~$530/mo | Cost per tenant drops as density grows |

---

## Sub-Milestones

| Sub-milestone | Spec | Delivers |
|---------------|------|----------|
| **M6a: Tenant Filesystem** | [06a-tenant-filesystem.md](06a-tenant-filesystem.md) | Per-tenant PVC, repo provisioner, workspace group cloud mapping, config persistence across hibernate/wake |
| **M6b: K8s Sandbox Backend** | [06b-k8s-sandbox.md](06b-k8s-sandbox.md) | `K8sBackend` implementing `sandbox.Backend` — dispatches containers as K8s Jobs with PVC mounts |
| **M6c: Cloud Infrastructure** | [06c-cloud-infrastructure.md](06c-cloud-infrastructure.md) | Per-provider IaC modules (DO first, then AWS/GCP/Alibaba/self-hosted) |
| **M2 cloud tasks** | [02-storage-backends.md](02-storage-backends.md) (tasks 4–8) | PostgreSQL + S3 backends for task data, composite backend, migration tool |

```
                     M6a: Tenant Filesystem
                    (repos, PVC, config)
                            │
                            ▼
M1 (sandbox) ──────▶ M6b: K8s Sandbox ──────▶ M8: Multi-Tenant
                    (Jobs, PVC mounts)              ▲
                                                    │
M2 (storage) ──────▶ M2 cloud tasks ──────────────┤
                    (PG, S3, migration)             │
                                                    │
M8a (auth) ────────────────────────────────────────┤
                                                    │
M6c (IaC: DO, AWS, GCP, Alibaba) ─────────────────┘
```

---

## VPS Deployment Reference

**Setup checklist:**

| Step | How |
|------|-----|
| TLS | Caddy with automatic TLS |
| Workspace repos | `git clone` repos to the VM |
| Container runtime | Install Docker or Podman |
| Persistent storage | Mount a volume at `~/.wallfacer/` |
| Survive reboots | Systemd unit file |

**Systemd unit:**
```ini
[Unit]
Description=Wallfacer
After=network.target

[Service]
User=wallfacer
ExecStart=/usr/local/bin/wallfacer run -no-browser /home/wallfacer/repos/myproject
Restart=on-failure
Environment=CONTAINER_CMD=docker

[Install]
WantedBy=multi-user.target
```

**Caddy:**
```
wallfacer.example.com {
    reverse_proxy localhost:8080
}
```

---

## Decision Matrix

| Approach | Effort | Auth | Multi-user | When to use |
|----------|--------|------|------------|-------------|
| **VPS + Caddy** | Done | `WALLFACER_SERVER_API_KEY` | No | Personal use, development, early validation |
| **K8s (DOKS/EKS/GKE)** | M6a + M6b + M6c + M2 cloud + M8a + M8 | OAuth2/OIDC | Yes | Growing business, multi-tenant |
