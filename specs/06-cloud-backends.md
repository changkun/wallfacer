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

This decomposes into three cross-cutting epics (see [README.md](README.md) for the full milestone roadmap):

| Epic | Spec | What it covers |
|------|------|----------------|
| **Sandbox Executor** | [01-sandbox-backends.md](01-sandbox-backends.md) | Pluggable `sandbox.Backend` interface — **complete** (`LocalBackend` + all runner callers migrated). Remote backends (K8s, remote Docker) below. |
| **Data Storage** | [02-storage-backends.md](02-storage-backends.md) | Pluggable `StorageBackend` interface; filesystem, PostgreSQL, and S3 backends |
| **Multi-Tenant** | [08-cloud-multi-tenant.md](08-cloud-multi-tenant.md) | Control plane, user auth, instance provisioning and lifecycle |

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

---

## Remote Sandbox Backends

The `sandbox.Backend` interface ([01-sandbox-backends.md](01-sandbox-backends.md)) and `LocalBackend` are complete. Remote backends implement the same interface for cloud execution. The runner and handlers are unaware of the backend — they call `backend.Launch(ctx, spec)` and interact with the returned `sandbox.Handle`.

### Kubernetes backend

Implement `K8sBackend` in `internal/sandbox/k8s.go` dispatching containers as K8s Jobs.

**Work:**
1. Implement `sandbox.Backend` via `client-go`
2. Map `sandbox.ContainerSpec` → K8s Job spec (image, env, volumes, resource limits → `resources.limits`)
3. `k8sHandle` with state tracking via pod watch
4. Log streaming via pod log follow API
5. Worktree mounting via shared PVC (see Worktree Management below)
6. Add `k8s` as a value for `WALLFACER_SANDBOX_BACKEND`
7. Integration tests with kind or minikube

**New dependency:** `k8s.io/client-go`

### Remote Docker backend (optional)

Implement `RemoteDockerBackend` in `internal/sandbox/remote.go` for SSH/HTTPS dispatch to a remote Docker host.

**Work:**
1. Implement `sandbox.Backend` via Docker client SDK
2. SSH tunnel or TLS client cert for authentication
3. State tracking via Docker events API
4. Volume mounting via NFS or pre-provisioned volumes on the remote host

Lower priority than K8s. Useful for simple single-host remote setups.

---

## Worktree Management in Cloud

The biggest architectural challenge for remote backends. The `sandbox.Backend` interface takes a `ContainerSpec` with `Volumes` already populated — it does not manage worktrees. Worktree provisioning is an **orchestration concern** handled by the runner (or a cloud variant of it) before calling `backend.Launch()`.

Currently (local deployment):

1. `Runner.ensureTaskWorktrees()` creates worktrees at `~/.wallfacer/worktrees/<task-uuid>/`
2. `buildContainerSpecForSandbox()` adds worktree paths as bind-mount `VolumeMount` entries
3. Agent writes to `/workspace/<repo>` inside the container (= the worktree on the host)
4. After task completion, runner commits from the worktree and cleans it up

In a K8s/remote backend, the worktree filesystem must be accessible to both the wallfacer server (for git operations) and the sandbox pod (for agent writes). Options:

| Approach | How | Tradeoffs |
|----------|-----|-----------|
| **Shared volume (PVC/NFS)** | Both server and pods mount the same volume | Simple; requires ReadWriteMany PVC; potential contention |
| **Server-side worktree + rsync** | Server creates worktree, syncs to pod volume pre-launch, syncs back post-completion | No shared storage needed; adds latency; complex |
| **In-pod worktree creation** | Init container creates worktree; server reads results via K8s exec or shared volume | Decouples server from filesystem; git operations move to pod |
| **Git server sidecar** | Each pod has a git sidecar that handles worktree ops via API | Clean separation; most complex |

**Recommended:** Shared volume (PVC/NFS) for initial implementation.

---

## Cross-Cutting Concerns for Remote Backends

### Resource Limits

`ContainerSpec.CPUs` and `ContainerSpec.Memory` map directly to K8s `resources.limits`. No interface change needed — the backend interprets these fields.

### Sandbox Image Management

Currently checked via `podman images` / `docker images` in the handler. For K8s, images are pulled by the kubelet. The `GET /api/images` endpoint needs a backend-aware implementation:
- Local: check local image cache (as today)
- K8s: assume images are available (or check a registry)

### Network Control

`ContainerSpec.Network` is the abstraction point. Currently a single string (`"host"`, `"none"`, `"slirp4netns"`). Sufficient for local deployment. For egress filtering and DNS control, add optional fields to `ContainerSpec` when needed — primarily a multi-tenant concern ([08-cloud-multi-tenant.md](08-cloud-multi-tenant.md)).
