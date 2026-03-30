---
title: Cloud Deployment
status: drafted
depends_on: []
affects: [deploy/]
effort: large
created: 2026-03-28
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Cloud Deployment

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
| **Task data** (metadata, events, blobs) | `StorageBackend` | PostgreSQL + S3 |
| **Filesystem** (repos, worktrees, config) | Tenant volume | PVC per tenant |
| **Sandbox execution** | `K8sBackend` | K8s Jobs mounting tenant PVC |
| **Identity & lifecycle** | Control plane | Control plane DB |

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
| **Tenant Filesystem** | [tenant-filesystem.md](tenant-filesystem.md) | Per-tenant PVC, repo provisioner, workspace group cloud mapping, config persistence across hibernate/wake |
| **K8s Sandbox Backend** | [k8s-sandbox.md](k8s-sandbox.md) | `K8sBackend` implementing `sandbox.Backend` — dispatches containers as K8s Jobs with PVC mounts |
| **Cloud Infrastructure** | [cloud-infrastructure.md](cloud-infrastructure.md) | Per-provider IaC modules (DO first, then AWS/GCP/Alibaba/self-hosted) |
| **Cloud Storage** | [storage-backends.md](../foundations/storage-backends.md) (tasks 4–8) | PostgreSQL + S3 backends for task data, composite backend, migration tool |

```
                     Tenant Filesystem
                    (repos, PVC, config)
                            │
                            ▼
Sandbox Interface ──────▶ K8s Sandbox ──────▶ Multi-Tenant
                    (Jobs, PVC mounts)              ▲
                                                    │
Storage Interface ──────▶ Cloud Storage (PG, S3) ──────────────┤
                    (PG, S3, migration)             │
                                                    │
Authentication ────────────────────────────────────────┤
                                                    │
Cloud Infra (IaC) ─────────────────┘
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
| **K8s (DOKS/EKS/GKE)** | All cloud + auth specs | OAuth2/OIDC | Yes | Growing business, multi-tenant |

---

## Integration Boundary: What Cloud Mode Does NOT Manage

**Decision (2026-03-28):** Cloud wallfacer is a task runner, not a cloud workstation. It manages LLM API keys and git credentials — nothing else. External service integrations (GitHub API, Slack, Google, etc.) are not per-tenant concerns in cloud mode.

### Why not per-tenant external credentials?

Locally, wallfacer benefits from the user's existing tool credentials (`gh`, `gcloud`, Slack MCP, etc.) for free — they're already on the machine. Moving to cloud creates pressure to replicate this, but every approach has fundamental problems:

| Approach | Problem |
|----------|---------|
| **VM per tenant** (full user environment) | You're building a cloud workstation (Gitpod/Codespaces). One VM compromise leaks all tenant credentials. Control plane breach = access to every tenant's secrets. You inherit patching, hardening, and monitoring N internet-facing VMs. |
| **K8s + credential injectors** | Combinatorial explosion of tool-specific formats (`~/.config/gh/`, `~/.aws/`, `.npmrc`, etc.). OAuth tools need browser flows that don't work in pods. Token refresh/rotation becomes your problem. |
| **K8s + MCP as auth boundary** | Every external service needs a custom MCP wrapper. Agents must use MCP tools instead of native CLIs they already know. You're building an integration platform. |

All three turn wallfacer into a credential management platform, which is a different product.

### Why it's not needed

Tracing the idea → implementation → serve pipeline, the only credentials the sandbox agent genuinely needs are:

1. **LLM API key** — already handled (`.env`)
2. **Git read/write** — already handled (per-tenant SSH keys / HTTPS tokens)

Everything else separates cleanly into concerns *outside* the sandbox:

| Need | Who handles it | How |
|------|---------------|-----|
| **Create PR from completed task** | CI/CD or control plane webhook | Wallfacer pushes branch → GitHub Action or control plane API call creates PR. One GitHub App token in the control plane, not per-tenant. |
| **Notify user (Slack, email)** | Control plane | One bot/webhook token, shared. Not a sandbox concern. |
| **Run CI checks** | GitHub Actions / external CI | Triggered by push, not by wallfacer. |
| **Application secrets (DB, APIs)** | `serve.env` (live-serve spec) | Secrets for the *built app*, not for the agent. Already designed. |
| **Deploy** | CD pipeline or live-serve (live-serve spec) | Separate from task execution. |

The integrations feel essential locally because they're interleaved with the dev workflow. In an automated pipeline, they separate into "what the agent needs" (LLM + git) and "what happens around the agent" (CI/CD, notifications, deployment), which are better handled by dedicated systems.

### Implications

- **Local mode** remains the power-user environment with full host credential access. This is a feature, not a limitation — local mode is strictly more capable for integration-heavy workflows.
- **Cloud mode** is scoped to: receive task → execute in sandbox (LLM + git) → push result. The surrounding pipeline (PR creation, CI, notifications, deployment) connects via webhooks and the control plane, not per-tenant credentials in sandboxes.
- **No VM-per-tenant mode.** The flexibility gain doesn't justify the security liability and operational burden. K8s remains the only multi-tenant target.
- **Control plane integrations** (one GitHub App, one Slack bot, one notification webhook) are in scope for multi-tenant, but these are control-plane-level credentials, not per-tenant secrets.
