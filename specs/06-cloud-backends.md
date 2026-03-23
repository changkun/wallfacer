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

This decomposes into three cross-cutting epics, plus related specs that have cloud-dependent phases:

| Epic | Spec | What it covers |
|------|------|----------------|
| **Sandbox Executor** | [`01-sandbox-backends.md`](01-sandbox-backends.md) | Pluggable `SandboxBackend` interface; local, K8s, and remote Docker backends |
| **Data Storage** | [`02-storage-backends.md`](02-storage-backends.md) | Pluggable `StorageBackend` interface; filesystem, PostgreSQL, and S3 backends |
| **Container Reuse** | [`03-container-reuse.md`](03-container-reuse.md) | Long-lived worker containers inside `LocalBackend` (depends on sandbox executor) |
| **File Explorer** | [`04-file-explorer.md`](04-file-explorer.md) | Workspace file browser; Phase 4 adds cloud `WorkspaceFS` abstraction |
| **Host Terminal** | [`05-host-terminal.md`](05-host-terminal.md) | Web terminal; Phase 3 adds container exec for cloud |
| **Desktop App** | [`07-native-desktop-app.md`](07-native-desktop-app.md) | Wails native wrapper (independent) |
| **Multi-Tenant** | [`08-cloud-multi-tenant.md`](08-cloud-multi-tenant.md) | Control plane, user auth, instance provisioning and lifecycle |

### Implementation Milestones

The specs are sequenced into 8 milestones to minimize cross-impacts. Interface extractions come first (pure refactors, no behavior change), local UX features deliver value early, and cloud integration is deferred to the end.

```
M1: Sandbox Backend Interface ──┬──▶ M3: Container Reuse
                                │
                                ├──▶ M6: Cloud Backends ──▶ M8: Multi-Tenant
                                │           ▲                     (capstone)
M2: Storage Backend Interface ──┘───────────┘
                                                    ▲
M4: File Explorer (local) ─────────────────────────▶│ (Phase 4)
M5: Host Terminal (local) ─────────────────────────▶│ (Phase 3)
M7: Desktop App ───────────────────────────────────▶│ (ships after UX)
```

| # | Milestone | Spec Phases | Delivers |
|---|-----------|-------------|----------|
| **M1** | Sandbox backend interface | sandbox-executor Ph1 | `SandboxBackend`/`SandboxHandle` + `LocalBackend` (pure refactor) |
| **M2** | Storage backend interface | data-storage Ph1–3 | `StorageBackend` + `FilesystemBackend` + `TurnOutputWriter` (pure refactor) |
| **M3** | Container reuse | container-reuse Ph1–2 | Aux worker containers for title/oversight/commit (~10x startup savings) |
| **M4** | File explorer | file-explorer Ph1–3 | Browse + edit workspace files in the web UI |
| **M5** | Host terminal | host-terminal Ph1–2 | Interactive shell in the web UI (WebSocket + PTY) |
| **M6** | Cloud backends | sandbox-executor Ph2–3, data-storage Ph4–7 | K8s backend, PostgreSQL, S3, migration tool |
| **M7** | Desktop app | native-desktop-app | Wails native wrapper (macOS .app, Windows .exe) |
| **M8** | Multi-tenant (capstone) | multi-tenant all, deferred phases | Control plane, auth, instance lifecycle, cloud file/terminal access |

**Ordering rationale:**
- **M1–M2 first:** Pure refactors that create the abstraction seams all downstream milestones plug into. Low risk, high leverage.
- **M3 after M1:** Container reuse modifies the same `internal/runner/` files. Doing it right after M1 avoids revisiting them later.
- **M4–M5 before M6:** Deliver user-visible value with no cloud dependency. Exercise different code paths (`internal/handler/` + `ui/`) than M1–M3.
- **M7 after M4–M5:** Desktop app ships with file explorer + terminal already built in. (Fully independent — can move earlier.)
- **M8 last:** Capstone that wires everything together. Picks up deferred cloud phases from M3/M4/M5.

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
