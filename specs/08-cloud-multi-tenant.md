# Cloud Multi-Tenant

**Date:** 2026-03-23

## Problem

Wallfacer is a single-user, single-process server. Multiple users cannot share a wallfacer instance — the server has no concept of user identity, and all state (tasks, worktrees, automation loops) is process-global. Cloud deployment for teams requires each user to get their own isolated wallfacer instance.

## Architecture: Control Plane + Per-User Instances

A lightweight **control plane** service manages user authentication and instance lifecycle. Each user gets a **dedicated wallfacer server** that is provisioned on login and destroyed (or hibernated) after idle timeout. The control plane is the only internet-facing service; per-user instances are on an internal network.

```
Browser ──HTTPS──▶ Control Plane (auth + routing)
                        │
                ┌───────┼───────┐
                │       │       │
           Instance A  Instance B  Instance C
           (user-a)    (user-b)    (user-c)
```

Each instance is a full wallfacer process with its own:
- In-memory store (`internal/store.Store`)
- Workspace directories (cloned from user's repos)
- Automation loops (auto-promote, auto-retry, etc.)
- Container execution (via sandbox executor — see `01-sandbox-backends.md`)
- Data directory (backed by cloud storage — see `02-storage-backends.md`)

### Why not a shared multi-user server?

The alternative — making the single wallfacer server handle multiple users — would require:
- Per-user scoping of every in-memory map (`tasks`, `events`, `searchIndex`)
- Per-user workspace managers with independent workspace switching
- Per-user automation loops (autopilot, auto-retry, auto-test × N users)
- Per-user container registries and circuit breakers
- Per-user SSE streams scoped to their tasks only
- User identity propagation through every handler, runner, and store method

This is a near-complete rewrite of the server. The per-instance model avoids all of this by keeping the existing single-user architecture intact and pushing multi-tenancy to the infrastructure layer.

---

## Components

### 1. Authentication Gateway

The control plane handles user authentication before any request reaches a wallfacer instance.

**Requirements:**
- OAuth2/OIDC provider integration (GitHub, Google, corporate SAML)
- Session management (JWT or server-side sessions)
- User identity → instance mapping
- Rate limiting and abuse prevention

**Approach:**
- Standalone Go service or extend the wallfacer binary with a `wallfacer control-plane` subcommand
- Use a proven auth library (e.g., `golang.org/x/oauth2`, `github.com/coreos/go-oidc`)
- Store user → instance mapping in a small database (SQLite or PostgreSQL)

### 2. Instance Provisioner

Creates and destroys per-user wallfacer instances.

**Instance lifecycle:**
```
User login → Provision instance → Route traffic → Idle timeout → Hibernate → Destroy
                                                       ↑
                                                  User returns → Wake
```

**Provisioning backends** (pick one based on infrastructure):

| Backend | How | Tradeoffs |
|---------|-----|-----------|
| **Docker/Podman** | `docker run` with socket mount | Simplest; single host; limited scale |
| **Kubernetes Pod** | Create a Pod/Deployment per user | Scalable; needs K8s cluster |
| **Cloud VM** | EC2/GCE instance per user | Full isolation; expensive; slow boot |
| **Firecracker/microVM** | Lightweight VM per user | Fast boot; complex setup |

**Instance configuration per user:**
- Workspace repos (cloned or mounted from shared storage)
- API keys (per-user `ANTHROPIC_API_KEY` or shared org key)
- Sandbox executor config (see `01-sandbox-backends.md`)
- Data directory (per-user, backed by cloud storage — see `02-storage-backends.md`)

### 3. Traffic Router

Routes authenticated requests to the correct wallfacer instance.

**Requirements:**
- Reverse proxy with dynamic upstream resolution
- WebSocket/SSE pass-through for live updates
- Health checking for instance liveness

**Approach:**
- Caddy or Envoy with dynamic configuration
- Or a thin Go reverse proxy in the control plane itself
- Route by `Host` header (e.g., `user-a.wallfacer.example.com`) or path prefix

### 4. Workspace Provisioner

Each instance needs workspace repos on its local filesystem (or mounted volume). This requires:

- **Initial clone:** When an instance is first created for a user, clone their configured repos
- **Incremental sync:** On wake-from-hibernate, `git fetch` to pick up new commits
- **Credential forwarding:** SSH keys or HTTPS tokens for private repos

**Options:**
- Pre-clone repos to a shared NFS/EFS volume; mount per-instance (fast start, shared storage)
- Clone on provision (slower start, per-instance storage, simpler isolation)
- Use git bundles or shallow clones for faster initial provisioning

---

## Data Model Changes

### Control Plane Database

```sql
CREATE TABLE users (
    id          UUID PRIMARY KEY,
    provider    TEXT NOT NULL,           -- "github", "google", etc.
    provider_id TEXT NOT NULL,           -- external user ID
    email       TEXT,
    created_at  TIMESTAMP DEFAULT NOW(),
    UNIQUE(provider, provider_id)
);

CREATE TABLE instances (
    id          UUID PRIMARY KEY,
    user_id     UUID REFERENCES users(id),
    backend     TEXT NOT NULL,           -- "docker", "k8s", "vm"
    state       TEXT NOT NULL,           -- "provisioning", "running", "hibernating", "destroyed"
    endpoint    TEXT,                    -- internal URL (e.g., "http://10.0.1.42:8080")
    created_at  TIMESTAMP DEFAULT NOW(),
    last_active TIMESTAMP,
    config      JSONB                   -- instance-specific configuration
);

CREATE TABLE workspaces (
    id          UUID PRIMARY KEY,
    user_id     UUID REFERENCES users(id),
    repo_url    TEXT NOT NULL,           -- git clone URL
    branch      TEXT DEFAULT 'main',
    credentials TEXT                    -- encrypted SSH key or token reference
);
```

### Wallfacer Server Changes

Minimal changes to the wallfacer server itself:

| Change | Where | What |
|--------|-------|------|
| Accept external auth | `internal/handler/middleware.go` | Trust `X-Forwarded-User` header from control plane (when behind trusted proxy) |
| Report health | `GET /api/debug/health` | Already exists; control plane polls this |
| Graceful hibernate | `internal/cli/server.go` | New signal handler that flushes state and exits cleanly |
| Cloud storage backend | `internal/store/` | See `02-storage-backends.md` |
| Remote sandbox executor | `internal/runner/executor.go` | See `01-sandbox-backends.md` |

---

## Cross-Cutting Concerns

### Instance Hibernation

To save resources, idle instances should hibernate (stop the process, persist state to disk/cloud storage). On wake:

1. Control plane detects incoming request for hibernated instance
2. Provision new instance with same data directory
3. Wallfacer loads state from cloud storage (see `02-storage-backends.md`)
4. Route traffic to new instance

**Idle detection:** No HTTP requests for N minutes (configurable). The control plane tracks `last_active` per instance.

### Cost Control

- Per-user spending limits (aggregate task costs)
- Instance auto-destroy after extended idle (e.g., 24h)
- Shared org API key with per-user usage tracking
- Control plane dashboard for admin visibility

### Security

- Instances must not be able to access each other's data
- Container execution must be scoped (no cross-user container access)
- Workspace repos must be isolated per user
- API keys must be stored encrypted, injected at instance start

---

## Implementation Order

1. **Control plane scaffold** — Auth, user DB, instance table, health check loop
2. **Docker provisioner** — Simplest backend; proves the pattern on a single host
3. **Traffic router** — Dynamic reverse proxy with SSE pass-through
4. **Workspace provisioner** — Clone repos on instance creation
5. **Hibernate/wake cycle** — Idle detection, graceful shutdown, state restore
6. **K8s provisioner** — Scale beyond single host (optional, depends on demand)

### Dependencies on Other Epics

- **Cloud Data Storage** (`02-storage-backends.md`): Required for hibernate/wake — instance state must survive process restarts. Without cloud storage, instances lose all task data on restart.
- **Sandbox Executor** (`01-sandbox-backends.md`): Required if instances should not run containers locally. Without it, each instance needs a local container runtime (Docker socket mount), which works but limits density.
