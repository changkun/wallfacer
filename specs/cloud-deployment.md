# Cloud Deployment

**Date:** 2026-02-21

## Core Constraints

The app has four hard runtime dependencies that shape cloud deployment:

1. **Container runtime** (`podman`/`docker` via `os/exec`) — required for every task execution
2. **Git** on the host — worktrees, rebase, merge all run on the host
3. **Workspace directories** must exist on the machine running the Go server
4. **No built-in authentication** — the HTTP server is open to anyone who can reach port 8080 (addressed by `WALLFACER_SERVER_API_KEY` or a reverse proxy)

---

## Option 1: VPS + Reverse Proxy (lowest effort)

Deploy the Go binary to any Linux VM (EC2, Hetzner, DigitalOcean, etc.) with Docker/Podman installed.

**What already works:**
- `-no-browser` flag suppresses the browser launch
- `CONTAINER_CMD` env var selects the container runtime
- `WALLFACER_SERVER_API_KEY` provides bearer-token authentication
- Filesystem storage works on a persistent disk

**Remaining gaps:**

| Gap | Fix |
|-----|-----|
| HTTPS | Caddy with automatic TLS, or Nginx + certbot |
| Workspace repos must be on the VM | `git clone` or rsync repos to the VM at setup |
| Container runtime | Install Docker or Podman on the VM |
| Persistent storage | Mount a volume at `~/.wallfacer/` |
| Survives reboots | Write a systemd unit file |

Deployable with about a day of infrastructure work. The biggest practical friction is that workspace repos need to exist on the remote machine.

**Architecture:**
```
Internet → Caddy (HTTPS) → wallfacer :8080 (WALLFACER_SERVER_API_KEY)
                                  ↓
                             Podman (task containers)
                                  ↓
                    /home/user/repos/<workspace>
```

**Systemd unit example:**
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

**Caddy example:**
```
wallfacer.example.com {
    reverse_proxy localhost:8080
}
```

---

## Option 2: Docker-in-Docker (containerize the server itself)

Run the wallfacer Go server inside a container, which then needs to spawn task containers.

**Problem:** The server uses `os/exec` to call `podman run`. Inside a container this requires one of:
- Mounting the Docker socket (`-v /var/run/docker.sock:/var/run/docker.sock`) — gives the container root-equivalent access to the host; a deliberate security trade-off
- Docker-in-Docker (DinD) with `--privileged` — fragile, not recommended in production
- Podman rootless inside a container — complex, kernel version dependent

**When to choose this:** Only if a platform (Railway, Render, Fly.io) requires the server to be containerized. The socket-mount approach works but must be a conscious security decision.

---

## Option 3: Kubernetes with Job API (cloud-native, major refactoring)

Replace `os/exec` container spawning with the Kubernetes `batch/v1 Job` API. Tasks become K8s Jobs that mount PersistentVolumeClaims for worktrees.

**Required changes:**

| Component | Current | Cloud-native replacement |
|-----------|---------|--------------------------|
| Task execution | `podman run` via `os/exec` | `client-go` creating K8s Jobs |
| Persistence | `~/.wallfacer/data/` filesystem | PostgreSQL or similar |
| Worktrees | Local git worktrees | Per-task PVCs or init containers |
| Log streaming | Container stdout via `os/exec` pipe | `k8s.io/client-go` pod log stream |
| State | In-memory `sync.RWMutex` map | DB-backed, enables replicas |

**Verdict:** Multi-week refactor. Worth it for multi-user, horizontal scaling, or enterprise deployment. For personal/team use, Option 1 is far more practical.

---

## Decision Matrix

| Approach | Effort | Auth | Multi-user | Notes |
|----------|--------|------|------------|-------|
| **VPS + Caddy** | Low | `WALLFACER_SERVER_API_KEY` + Caddy TLS | No | Works today with minimal infra setup |
| **Docker-in-Docker** | Medium | Same as VPS | No | Only if platform mandates containerized server |
| **K8s + Job API** | High | K8s RBAC | Yes | Multi-week refactor; enables horizontal scaling |

---

## Recommendation

Start with **Option 1 (VPS + Caddy)**. Authentication is already available via `WALLFACER_SERVER_API_KEY`; the only infrastructure work is TLS (Caddy handles automatically), a systemd unit, and cloning workspace repos to the VM. Migrate to Option 3 only if multi-user or horizontal scaling becomes a real need.
