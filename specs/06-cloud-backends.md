# M6: Cloud Deployment

**Status:** Not started | **Date:** 2026-03-28

## Overview

Cloud deployment decomposes into three sub-milestones, plus the already-scoped cloud storage tasks in M2:

| Sub-milestone | Spec | Delivers |
|---------------|------|----------|
| **M6a: Tenant Filesystem** | [06a-tenant-filesystem.md](06a-tenant-filesystem.md) | Per-tenant persistent volume, repo provisioner, workspace group cloud mapping, config persistence across hibernate/wake |
| **M6b: K8s Sandbox Backend** | [06b-k8s-sandbox.md](06b-k8s-sandbox.md) | `K8sBackend` implementing `sandbox.Backend` — dispatches containers as K8s Jobs with PVC mounts |
| **M2 cloud tasks** | [02-storage-backends.md](02-storage-backends.md) (tasks 4–8) | PostgreSQL + S3 backends for task data, composite backend, migration tool |

```
                     M6a: Tenant Filesystem
                    (repos, volumes, config)
                            │
                            ▼
M1 (sandbox) ──────▶ M6b: K8s Sandbox ──────▶ M8: Multi-Tenant
                    (Jobs, PVC mounts)              ▲
                                                    │
M2 (storage) ──────▶ M2 cloud tasks ───────────────┘
                    (PG, S3, migration)
```

M6a is the foundation — it defines where tenant repos, worktrees, and config live. M6b consumes that layout to mount volumes into K8s pods. M2's cloud tasks handle task data independently. M8 ties everything together with auth, provisioning, and lifecycle.

---

## Single-User VPS Deployment (works today)

Deploy the Go binary to any Linux VM with Docker/Podman installed. No cloud sub-milestones required.

**Architecture:**
```
Internet → Caddy (HTTPS) → wallfacer :8080 (WALLFACER_SERVER_API_KEY)
                                ↓
                           Podman (task containers)
                                ↓
                  /home/user/repos/<workspace>
```

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

## Architecture: Per-User Instances ("Codespaces Model")

The wallfacer server is deeply stateful: in-memory task maps, filesystem-backed store, local git worktrees, local container runtime via `os/exec`, per-process automation loops. Making a single server serve multiple users would require replacing nearly every core subsystem.

Instead, the cloud deployment strategy follows the **Codespaces model**: a control plane provisions a dedicated wallfacer instance per user. Each instance is a full stateful server with its own workspace, storage, and sandbox access. This preserves the existing single-user architecture while enabling multi-user deployment.

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
   │  User A Instance │   │  User B Instance │   │  User C Instance │
   │  wallfacer :8080 │   │  wallfacer :8080 │   │  wallfacer :8080 │
   │  + tenant PVC    │   │  + tenant PVC    │   │  + tenant PVC    │
   │  + cloud store   │   │  + cloud store   │   │  + cloud store   │
   └────────┬────────┘   └────────┬────────┘   └────────┬────────┘
            │                      │                      │
            └──────────────────────┼──────────────────────┘
                                   │
                        ┌──────────▼──────────┐
                        │  K8s Cluster         │
                        │  (sandbox Jobs,      │
                        │   tenant PVCs,       │
                        │   shared services)   │
                        └─────────────────────┘
```

Per instance, the layers connect as:

| Layer | Component | Storage |
|-------|-----------|---------|
| **Task data** (metadata, events, blobs) | `StorageBackend` (M2) | PostgreSQL + S3 |
| **Filesystem** (repos, worktrees, config) | Tenant volume (M6a) | PVC per tenant |
| **Sandbox execution** | `K8sBackend` (M6b) | K8s Jobs mounting tenant PVC |
| **Identity & lifecycle** | Control plane (M8) | Control plane DB |

---

## Decision Matrix

| Approach | Effort | Auth | Multi-user | When to use |
|----------|--------|------|------------|-------------|
| **VPS + Caddy** | Done | `WALLFACER_SERVER_API_KEY` | No | Personal/single-team use today |
| **Per-user instances** | M6a + M6b + M2 cloud + M8 | OAuth2/OIDC via control plane | Yes | Multi-user cloud deployment |
| **Shared stateless server** | Very High | Per-user sessions | Yes | Not recommended — too much refactoring |

---

## Docker-in-Docker

Only relevant if a platform requires the server itself to be containerized. Requires mounting the Docker socket (`-v /var/run/docker.sock:/var/run/docker.sock`) — a deliberate security trade-off. In the per-user instance model, each user's wallfacer runs inside a K8s pod, with sandbox containers dispatched as separate Jobs via `K8sBackend` (no socket mount needed).
