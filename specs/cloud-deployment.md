# Cloud Deployment

**Date:** 2026-02-21 | **Revised:** 2026-03-23

## Current State

Single-user VPS deployment (Option 1 from the original spec) is fully supported today. All necessary features are implemented:

- `-no-browser` flag, `CONTAINER_CMD` env var, `WALLFACER_SERVER_API_KEY` authentication
- Filesystem storage on persistent disk, systemd unit, Caddy reverse proxy for TLS
- See the VPS deployment section below for the complete recipe

The remaining cloud deployment work is about **multi-user scalability**: allowing multiple users to each run their own wallfacer instance in the cloud, with shared infrastructure for sandbox execution and data persistence.

---

## Architecture: Per-User Instances ("Codespaces Model")

The wallfacer server is deeply stateful: in-memory task maps (`sync.RWMutex`), filesystem-backed store (`data/<workspace-key>/`), local git worktrees, local container runtime via `os/exec`, per-process automation loops (auto-promote, auto-retry, auto-test), and goroutine-tracked background work. Making a single server instance serve multiple users would require replacing nearly every core subsystem.

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
     │  + local store   │   │  + local store   │   │  + local store   │
     └────────┬────────┘   └────────┬────────┘   └────────┬────────┘
              │                      │                      │
              └──────────────────────┼──────────────────────┘
                                     │
                          ┌──────────▼──────────┐
                          │  Sandbox Cluster     │
                          │  (K8s Jobs / VMs /   │
                          │   container pool)    │
                          └─────────────────────┘
```

This decomposes into three cross-cutting epics:

| Epic | Spec | What it covers |
|------|------|----------------|
| **Multi-Tenant** | [`cloud-multi-tenant.md`](cloud-multi-tenant.md) | Control plane, user auth, instance provisioning and lifecycle |
| **Sandbox Executor** | [`cloud-sandbox-executor.md`](cloud-sandbox-executor.md) | Abstract `ContainerExecutor` to support remote backends (K8s Jobs, cloud VMs) |
| **Cloud Data Storage** | [`cloud-data-storage.md`](cloud-data-storage.md) | Replace filesystem `Store` with pluggable backends (DB, object storage) |

### Dependency Graph

```
Multi-Tenant ──depends-on──▶ Cloud Data Storage
     │                              ▲
     │                              │
     └──depends-on──▶ Sandbox Executor ─depends-on─┘
```

- **Cloud Data Storage** is the foundation: the store interface must exist before instances can be provisioned with cloud-backed persistence.
- **Sandbox Executor** can proceed in parallel once the store interface is defined, since it primarily affects `internal/runner/` rather than `internal/store/`.
- **Multi-Tenant** is the top-level epic that wires everything together: it needs both cloud storage (for per-user data isolation) and the sandbox executor (for remote container execution).

---

## Single-User VPS Deployment (works today)

Deploy the Go binary to any Linux VM with Docker/Podman installed.

**Setup checklist:**

| Step | How |
|------|-----|
| TLS | Caddy with automatic TLS |
| Workspace repos | `git clone` repos to the VM |
| Container runtime | Install Docker or Podman |
| Persistent storage | Mount a volume at `~/.wallfacer/` |
| Survive reboots | Systemd unit file |

**Architecture:**
```
Internet → Caddy (HTTPS) → wallfacer :8080 (WALLFACER_SERVER_API_KEY)
                                  ↓
                             Podman (task containers)
                                  ↓
                    /home/user/repos/<workspace>
```

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

## Docker-in-Docker (containerize the server)

Only relevant if a platform requires the server itself to be containerized. Requires mounting the Docker socket (`-v /var/run/docker.sock:/var/run/docker.sock`) — a deliberate security trade-off. This approach becomes the default for per-user instances in the multi-tenant model, where each user's wallfacer runs inside a container or pod.

---

## Decision Matrix

| Approach | Effort | Auth | Multi-user | When to use |
|----------|--------|------|------------|-------------|
| **VPS + Caddy** | Done | `WALLFACER_SERVER_API_KEY` | No | Personal/single-team use today |
| **Per-user instances** | High | OAuth2/OIDC via control plane | Yes | Multi-user cloud deployment |
| **Shared stateless server** | Very High | Per-user sessions | Yes | Not recommended — too much refactoring for the benefit |
