---
title: Tenant Filesystem
status: drafted
depends_on:
  - specs/foundations/sandbox-backends.md
  - specs/foundations/storage-backends.md
affects: [internal/workspace/, internal/runner/]
effort: large
created: 2026-03-28
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Tenant Filesystem

## Problem

Wallfacer's local deployment stores everything under `~/.wallfacer/` and the user's repo directories. Five filesystem concerns need cloud answers:

| Concern | Local path | Lifecycle | Cloud question |
|---------|-----------|-----------|----------------|
| **Git repos** (source code) | `~/repos/myproject/` | User-managed | Who clones? Where? How are creds managed? |
| **Task data** | `~/.wallfacer/data/<ws-key>/` | Store-managed | Solved by cloud storage (PG + S3) |
| **Worktrees** | `~/.wallfacer/worktrees/<task-uuid>/` | Ephemeral per task | Must be accessible to both server and sandbox pod |
| **Config/state** | `~/.wallfacer/{workspace-groups.json, prompts/}` | Long-lived per tenant | Must survive hibernate/wake cycles |
| **Ephemera** | `~/.wallfacer/tmp/` (board snapshots) | Throwaway | Instance-local, no persistence needed |

Cloud storage handles task data (row 2). This spec handles the remaining four rows.

## External Dependency: fs.latere.ai

**latere.ai/fs** is the platform's canonical user data plane — a per-user persistent storage service with two tiers:

- **Cold tier** (S3/DO Spaces): durable blob storage. Source of truth for all user files. Accessed via Files API (`PUT/GET/DELETE /files/{path...}`).
- **Hot tier** (local disk on compute node): fast ephemeral I/O for sandbox runtimes. Accessed via Workspace API (`POST/GET/DELETE /workspaces`). Stages files from cold → hot, syncs dirty files hot → cold on destroy.

fs.latere.ai is the right home for wallfacer's per-tenant persistent state. Building a standalone PVC-per-tenant architecture in wallfacer would duplicate what fs.latere.ai already provides (or is building in Phase 5: Workspaces). Instead, wallfacer integrates as a consumer.

**What fs.latere.ai provides:**
- Durable per-user file storage (config, instructions, prompts)
- Hot tier workspace allocation for sandbox I/O
- TTL-based workspace reaping for crashed sandboxes
- Conflict-safe rw locks on workspace mounts

**What fs.latere.ai does NOT provide:**
- Git operations (clone, fetch, push, worktree add/remove) — it's a blob store, not a VCS
- Git credential management (SSH keys, HTTPS tokens)
- Workspace group identity mapping (local paths ↔ repo URIs)

This spec covers the wallfacer-specific git and workspace concerns, delegating persistence to fs.latere.ai.

**Prerequisite:** fs.latere.ai Phase 5 (Workspace API) must be implemented. The two can be developed in parallel, but wallfacer cloud deployment requires the workspace endpoints.

---

## Current Local Architecture

```
~/.wallfacer/                          # Config root
├── workspace-groups.json              # Persisted workspace group definitions
├── instructions/                      # Per-group AGENTS.md files (keyed by SHA-256 of sorted paths)
├── prompts/                           # User-overridden system prompt templates
├── data/                              # Task data (owned by StorageBackend — cloud storage)
│   └── <workspace-key>/
├── worktrees/                         # Ephemeral per-task git worktrees
│   └── <task-uuid>/
│       └── <repo-basename>/
└── tmp/                               # Board snapshots, ephemeral files

~/repos/                               # User's source repos (outside wallfacer)
├── project-a/
└── project-b/
```

Key coupling points:
- **Workspace key** = `strings.Join(sorted absolute paths, "\n")` — depends on local path identity
- **Worktree creation** calls `git worktree add` against the repo's `.git` dir on the host
- **Container mounts** bind-mount host worktree paths into `/workspace/<basename>`
- **Instructions file** references per-repo `AGENTS.md` / `CLAUDE.md` by host path

All of these assume the server and containers share a local filesystem.

---

## Design

### Integration with fs.latere.ai

Wallfacer in cloud mode uses fs.latere.ai at two levels:

**1. Config persistence (cold tier / Files API):**

Long-lived tenant config is stored as files under the user's namespace:

```
/{principal_id}/wallfacer/config/workspace-groups.json
/{principal_id}/wallfacer/config/instructions/<sha256>.md
/{principal_id}/wallfacer/config/prompts/<template>.tmpl
```

On startup, wallfacer reads these via `GET /files/{path}`. On change (workspace switch, instructions edit, prompt override), wallfacer writes via `PUT /files/{path}`. These are small files with infrequent writes — the Files API is a natural fit.

**2. Runtime workspace (hot tier / Workspace API):**

On instance start (login or wake from hibernate), wallfacer creates a workspace via `POST /workspaces`:

```json
{
  "owner_id": "<principal-id>",
  "mounts": [
    {
      "source": "/<principal_id>/wallfacer/config",
      "target": "/wallfacer/config",
      "mode": "rw"
    }
  ],
  "sandbox_id": "wallfacer-<instance-id>"
}
```

The workspace hot path becomes the working directory for the wallfacer instance. Git repos and worktrees are created directly in the hot path — they are ephemeral runtime state, not persisted blobs.

**Filesystem layout on the hot path:**

```
/hot/<workspace-id>/                   # Workspace hot root (from fs.latere.ai)
├── config/                            # Staged from cold tier (rw mount)
│   ├── workspace-groups.json
│   ├── instructions/
│   └── prompts/
├── repos/                             # Cloned into hot path (not staged from cold)
│   ├── project-a/
│   └── project-b/
├── worktrees/                         # Per-task worktrees (same layout as local)
│   └── <task-uuid>/
│       └── <repo-basename>/
└── tmp/                               # Ephemeral board snapshots
```

**What's NOT on the hot path:**
- `.env` — injected by the control plane as environment variables or a mounted secret
- `data/` — handled by `StorageBackend` (cloud storage's PG + S3 backends)

### Lifecycle: Hibernate and Wake

**On hibernate:**
1. Wallfacer syncs config changes to cold tier: `POST /workspaces/{id}/sync`
2. Workspace is destroyed: `DELETE /workspaces/{id}` (final sync + hot tier cleanup)
3. The instance pod is deleted. Task data is already in cloud storage.

**On wake:**
1. Control plane provisions a new pod
2. Wallfacer creates a new fs.latere.ai workspace (config is staged from cold tier)
3. Wallfacer loads `workspace-groups.json` from the staged config
4. Repo provisioner re-clones repos into the hot path (partial clones are fast)
5. Wallfacer connects to cloud storage for task data and resumes

Git repos are treated as **ephemeral runtime state** — re-cloned on every wake. With `--filter=blob:none` partial clones, only tree objects are fetched initially; blobs download on demand. This avoids git-aware sync logic and keeps the hibernate/wake cycle simple.

For tenants with very large repos, the initial clone cost is amortized: repos only need re-cloning on wake from hibernate, not on every task. During active use, repos persist on the hot tier for the workspace's TTL (extended via `POST /workspaces/{id}/renew`).

### Repo Provisioner

A new component (`internal/repo/`) manages the lifecycle of git repos on the runtime workspace.

**Operations:**

| Operation | When | What |
|-----------|------|------|
| **Clone** | Instance start / workspace add | `git clone --filter=blob:none <url>` into hot path |
| **Fetch** | Periodic during active use | `git fetch origin` to pick up upstream changes |
| **Push** | Task commit pipeline (existing) | `git push` from worktree branch — same as today |
| **Prune** | Workspace removal | Remove repo directory, clean up worktrees that referenced it |

**Credential management:**

Repos may be private. The control plane stores per-tenant git credentials (SSH keys or HTTPS tokens) encrypted in its database. On instance provision:

1. Control plane injects credentials as a K8s Secret mounted into the server pod
2. Server configures git credential helper to use the mounted secret
3. Credentials are never written to the hot tier or cold store

For SSH: mount the key at a known path, set `GIT_SSH_COMMAND` to use it.
For HTTPS: mount a `.git-credentials` file or use a credential helper that reads from the secret.

### Workspace Group Mapping

**Problem:** Workspace keys are currently derived from absolute local paths. In cloud, repos aren't at user-chosen local paths — they're at provisioned paths on the hot tier.

**Solution:** Introduce a **canonical repo identifier** that is stable across environments.

```
Local:  workspace key = "/Users/alice/repos/project-a\n/Users/alice/repos/project-b"
Cloud:  workspace key = "git@github.com:alice/project-a.git\ngit@github.com:alice/project-b.git"
```

The workspace manager already normalizes and sorts paths before joining. The change is what constitutes a "path":

- **Local mode** (unchanged): absolute filesystem paths
- **Cloud mode**: normalized remote URLs (stripped of trailing `.git`, lowercased host, sorted)

`workspace-groups.json` stores the canonical identifiers. The repo provisioner maps them to actual filesystem paths on the hot tier:

```json
{
  "groups": [
    {
      "name": "my-group",
      "workspaces": [
        "git@github.com:alice/project-a.git",
        "git@github.com:alice/project-b.git"
      ]
    }
  ]
}
```

The workspace manager resolves identifiers to local paths at runtime:
- `git@github.com:alice/project-a.git` → `/hot/<workspace-id>/repos/project-a/`

This means `Snapshot.Workspaces` still contains absolute paths (for the runner and handler to use), but the persisted group definitions are portable across instances.

### Config Persistence

| Item | Storage | Rationale |
|------|---------|-----------|
| `.env` (API keys, model config) | K8s Secret, injected as env vars | Credentials must not touch persistent storage; control plane manages them |
| `workspace-groups.json` | fs.latere.ai cold tier (`/wallfacer/config/`) | Defines the tenant's workspace setup; survives hibernate via cold tier |
| `prompts/` overrides | fs.latere.ai cold tier (`/wallfacer/config/prompts/`) | User customizations; survives hibernate via cold tier |
| `instructions/` (AGENTS.md) | fs.latere.ai cold tier (`/wallfacer/config/instructions/`) | Generated from templates + repo files; preserved to avoid re-init churn |

### Worktree Management

Worktree creation logic (`internal/runner/worktree.go`) is unchanged — it calls `git worktree add` against the repo's `.git` dir. The difference is that both the repo and the worktree live on the fs.latere.ai hot tier instead of the host filesystem.

**Server ↔ sandbox pod access:**

The fs.latere.ai hot tier is a local directory on the compute node. Three pods must co-locate on the same node: the fs.latere.ai workspace pod (manages the hot tier), the wallfacer server pod, and sandbox pods. If fs.latere.ai runs as a DaemonSet (one per node), this simplifies to a 2-way constraint (wallfacer + sandbox pods). The K8s sandbox backend mounts worktree subdirectories into sandbox pods:

```
# Local (current)
Volume: hostPath /home/user/.wallfacer/worktrees/<task-uuid>/project-a → /workspace/project-a

# Cloud (K8s, via fs.latere.ai hot tier)
Volume: hostPath /hot/<workspace-id>/worktrees/<task-uuid>/project-a → /workspace/project-a
```

The runner's `buildContainerSpecForSandbox()` needs to emit hot-path-based volume mounts instead of host path mounts. This is a concern for K8s sandbox backend — this spec ensures the filesystem layout supports it.

**Worktree GC** works the same: `PruneUnknownWorktrees()` and `StartWorktreeGC()` scan the `worktrees/` directory on the hot tier.

---

## Interface Changes

### Workspace Manager

```go
// WorkspaceIdentifier can be a local path or a repo URI.
// The manager resolves it to a filesystem path at runtime.
type WorkspaceIdentifier string

// RepoResolver maps canonical workspace identifiers to local paths.
// Local mode: identity function (path → path).
// Cloud mode: looks up the provisioned repo path on the hot tier.
type RepoResolver interface {
    Resolve(id WorkspaceIdentifier) (localPath string, err error)
    // ReverseResolve returns the canonical identifier for a local path.
    // Used when reading existing workspace-groups.json from local deployments.
    ReverseResolve(localPath string) (WorkspaceIdentifier, error)
}
```

The workspace manager takes a `RepoResolver` at construction. `Switch()` calls `resolver.Resolve()` on each workspace identifier before creating the snapshot.

### Runner

No changes to worktree logic itself. The runner already receives workspace paths from the snapshot. Volume mount assembly changes are in K8s sandbox backend spec.

### New: Repo Provisioner

```go
// RepoProvisioner manages git repo lifecycle on the runtime workspace.
type RepoProvisioner struct {
    reposDir string          // e.g., /hot/<workspace-id>/repos/
    creds    CredentialStore // mounted secrets
}

func (p *RepoProvisioner) Clone(ctx context.Context, repoURL string) (localPath string, err error)
func (p *RepoProvisioner) Fetch(ctx context.Context, repoURL string) error
func (p *RepoProvisioner) Remove(ctx context.Context, repoURL string) error
func (p *RepoProvisioner) LocalPath(repoURL string) string
func (p *RepoProvisioner) ListRepos() ([]string, error)
```

### New: fs.latere.ai Client

```go
// FSClient wraps the fs.latere.ai Files and Workspace APIs.
type FSClient struct {
    baseURL string
    token   string  // JWT from auth service
}

// Config persistence (Files API)
func (c *FSClient) ReadConfig(ctx context.Context, path string) ([]byte, error)
func (c *FSClient) WriteConfig(ctx context.Context, path string, data []byte) error
func (c *FSClient) ListConfigs(ctx context.Context, prefix string) ([]string, error)

// Workspace lifecycle (Workspace API)
func (c *FSClient) CreateWorkspace(ctx context.Context, req WorkspaceRequest) (*Workspace, error)
func (c *FSClient) WorkspaceStatus(ctx context.Context, id string) (*Workspace, error)
func (c *FSClient) SyncWorkspace(ctx context.Context, id string) error
func (c *FSClient) RenewWorkspace(ctx context.Context, id string) error
func (c *FSClient) DestroyWorkspace(ctx context.Context, id string) error
```

---

## Implementation Tasks

| # | Task | Depends on | Effort |
|---|------|-----------|--------|
| 1 | Implement `FSClient` — wrapper for fs.latere.ai Files + Workspace APIs | fs.latere.ai Phase 5 | Medium |
| 2 | Define `RepoResolver` interface; implement `LocalResolver` (identity) | — | Small |
| 3 | Thread `RepoResolver` through workspace manager | 2 | Medium |
| 4 | Implement `RepoProvisioner` (clone, fetch, remove on hot path) | — | Medium |
| 5 | Implement `CloudResolver` using `RepoProvisioner` + `FSClient` | 1, 2, 4 | Small |
| 6 | Extend `workspace-groups.json` to store repo URIs; migrate existing format | 3 | Medium |
| 7 | Credential injection: SSH key / HTTPS token mounting from K8s secrets | 4 | Medium |
| 8 | Integration test: workspace create → clone → worktree → config sync round-trip | 1–7 | Medium |

---

## Dependencies

- **Sandbox Backend Interface** — complete. `ContainerSpec.Volumes` is the mount point.
- **Storage Backend Interface** — complete (enablers). Task data goes through `StorageBackend`, not the tenant workspace.
- **Multi-Workspace Groups** — complete. Workspace key mechanics and multi-group lifecycle are the foundation this builds on.
- **fs.latere.ai Phase 5 (Workspace API)** — external prerequisite. Provides hot tier workspace allocation and lifecycle.

## What depends on this

- **K8s Sandbox Backend** — needs the hot tier workspace layout and `RepoResolver` to assemble pod volume mounts.
- **Multi-Tenant** — the control plane calls the repo provisioner to set up tenant workspaces, and manages credential secrets.
