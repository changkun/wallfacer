---
title: Tenant Filesystem
status: drafted
depends_on:
  - specs/foundations/sandbox-backends.md
  - specs/foundations/storage-backends.md
affects: [internal/workspace/, internal/gitutil/]
effort: large
created: 2026-03-28
updated: 2026-06-14
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
| **Worktrees** | `~/.wallfacer/worktrees/<task-uuid>/` | Ephemeral per task | Must be reachable by the runner and (in cloud) by the Cella sandbox |
| **Config/state** | `~/.wallfacer/{workspace-groups.json, prompts/}` | Long-lived per tenant | Must survive hibernate/wake cycles |
| **Ephemera** | `~/.wallfacer/tmp/` (board snapshots) | Throwaway | Instance-local, no persistence needed |

Cloud storage handles task data (row 2). This spec handles the remaining four rows.

## External Dependency: fs.latere.ai

**latere.ai/fs** is the platform's canonical user data plane, a per-user persistent storage service with two tiers:

- **Cold tier** (S3/DO Spaces): durable blob storage. Source of truth for all user files. Accessed via Files API (`PUT/GET/DELETE /files/{path...}`).
- **Hot tier** (local disk on compute node): fast ephemeral I/O for sandbox runtimes. Accessed via Workspace API (`POST/GET/DELETE /workspaces`). Stages files from cold to hot, syncs dirty files hot to cold on destroy.

fs.latere.ai is the right home for wallfacer's per-tenant persistent state. Building a standalone PVC-per-tenant architecture in wallfacer would duplicate what fs.latere.ai already provides (or is building in Phase 5: Workspaces). Instead, wallfacer integrates as a consumer.

**What fs.latere.ai provides:**
- Durable per-user file storage (config, instructions, prompts)
- Hot tier workspace allocation for sandbox I/O
- TTL-based workspace reaping for crashed sandboxes
- Conflict-safe rw locks on workspace mounts

**What fs.latere.ai does NOT provide:**
- Git operations (clone, fetch, push, worktree add/remove). It's a blob store, not a VCS
- Git credential management (SSH keys, HTTPS tokens)
- Workspace group identity mapping (local paths to repo URIs)

This spec covers the wallfacer-specific git and workspace concerns, delegating persistence to fs.latere.ai.

**Prerequisite:** fs.latere.ai Phase 5 (Workspace API) must be implemented. The two can be developed in parallel, but wallfacer cloud deployment requires the workspace endpoints.

---

## Current Local Architecture

```
~/.wallfacer/                          # Config root
├── workspace-groups.json              # Persisted workspace group definitions
├── instructions/                      # Per-group AGENTS.md files (keyed by SHA-256 of sorted paths)
├── prompts/                           # User-overridden system prompt templates
├── data/                              # Task data (owned by StorageBackend, cloud storage)
│   └── <workspace-key>/
├── worktrees/                         # Ephemeral per-task git worktrees
│   └── <task-uuid>/
│       └── <repo-basename>/
└── tmp/                               # Board snapshots, ephemeral files

~/repos/                               # User's source repos (outside wallfacer)
├── project-a/
└── project-b/
```

Key coupling points in today's code:
- **Workspace key** = `strings.Join(sorted absolute paths, "\n")`. Depends on local path identity (`internal/workspace/manager.go`, `internal/workspace/groups.go`).
- **Worktree creation** calls `git worktree add` against the repo's `.git` dir on the host (`internal/gitutil/worktree.go` primitives, orchestrated by `internal/runner/worktree.go`).
- **Worktree GC** scans the `worktrees/` directory (`PruneUnknownWorktrees` in `internal/runner/worktree.go`, `StartWorktreeGC` / `ScanOrphanedWorktrees` in `internal/runner/worktree_gc.go`).
- **Instructions file** references per-repo `AGENTS.md` / `CLAUDE.md` by host path.

All of these assume the runner and the agent process share a local filesystem. In host execution (`internal/executor/host.go`, the only executor backend today) that holds: the agent runs as a host process with no container, so worktree paths are directly usable. The cloud question is what happens when execution moves off the host to a Cella sandbox.

---

## Design

### Integration with fs.latere.ai

Wallfacer in cloud mode uses fs.latere.ai at two levels:

**1. Config persistence (cold tier / Files API):**

Long-lived tenant config is stored as files under the user's namespace, keyed by the `authkit.Identity` principal (the platform-canonical principal surfaced via `internal/auth`):

```
/{principal_id}/wallfacer/config/workspace-groups.json
/{principal_id}/wallfacer/config/instructions/<sha256>.md
/{principal_id}/wallfacer/config/prompts/<template>.tmpl
```

On startup, wallfacer reads these via `GET /files/{path}`. On change (workspace switch, instructions edit, prompt override), wallfacer writes via `PUT /files/{path}`. These are small files with infrequent writes, so the Files API is a natural fit.

**2. Runtime workspace (hot tier / Workspace API):**

On instance start (or wake from hibernate), wallfacer creates a workspace via `POST /workspaces`:

```json
{
  "owner_id": "<principal-id>",
  "mounts": [
    {
      "source": "/<principal_id>/wallfacer/config",
      "target": "/wallfacer/config",
      "mode": "rw"
    }
  ]
}
```

The workspace hot path becomes the working directory for the wallfacer instance. Git repos and worktrees are created directly in the hot path. They are ephemeral runtime state, not persisted blobs.

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
- `.env` (API keys, model config). Provided to the server as environment from the deployment Secret (see Config Persistence below), never staged to a tenant workspace.
- `data/` (handled by `StorageBackend`, cloud storage's PG + S3 backends).

### Lifecycle: Hibernate and Wake

The fs.latere.ai workspace lifecycle (create / sync / destroy / renew) is wallfacer's to drive. The pod scheduling around it (when a server instance starts, sleeps, or is reclaimed) is owned by the platform (Cella runtime + terraform deploy), not by a wallfacer-built control plane. This section covers only the FS side of the cycle.

**On hibernate (server going idle):**
1. Wallfacer syncs config changes to cold tier: `POST /workspaces/{id}/sync`
2. Workspace is destroyed: `DELETE /workspaces/{id}` (final sync + hot tier cleanup)
3. Task data is already in cloud storage, so nothing in the hot path needs preserving.

**On wake (server resuming):**
1. Wallfacer creates a new fs.latere.ai workspace (config is staged from cold tier)
2. Wallfacer loads `workspace-groups.json` from the staged config
3. Repo provisioner re-clones repos into the hot path (partial clones are fast)
4. Wallfacer connects to cloud storage for task data and resumes

Git repos are treated as **ephemeral runtime state**, re-cloned on every wake. With `--filter=blob:none` partial clones, only tree objects are fetched initially; blobs download on demand. This avoids git-aware sync logic and keeps the hibernate/wake cycle simple.

For tenants with very large repos, the initial clone cost is amortized: repos only need re-cloning on wake from hibernate, not on every task. During active use, repos persist on the hot tier for the workspace's TTL (extended via `POST /workspaces/{id}/renew`).

### Repo Provisioner

A new component manages the lifecycle of git repos on the runtime workspace. Its placement splits along the existing package seam:

- **Mapping / orchestration** (canonical identifier to hot-path repo dir, list, prune) lives in `internal/workspace/`, alongside the manager and `RepoResolver`.
- **Git primitives** (clone, fetch) live in `internal/gitutil/`, the package that already owns git operations. `FetchOrigin` exists there today; a partial-clone `Clone` is new. Push from a worktree branch and worktree add/remove already exist (`internal/gitutil/ops.go`, `internal/gitutil/worktree.go`).

**Operations:**

| Operation | When | What |
|-----------|------|------|
| **Clone** | Instance start / workspace add | `git clone --filter=blob:none <url>` into hot path (new `gitutil.Clone`) |
| **Fetch** | Periodic during active use | `git fetch origin` to pick up upstream changes (existing `gitutil.FetchOrigin`) |
| **Push** | Task commit pipeline (existing) | `git push` from worktree branch, same as today |
| **Prune** | Workspace removal | Remove repo directory, clean up worktrees that referenced it |

**Credential management:**

Repos may be private. Git credentials (SSH keys or HTTPS tokens) are tenant-scoped via the `authkit.Identity` principal. In the single shared-instance cloud deployment (one task-board server Deployment in the `latere` cluster, see [cloud-infrastructure.md](cloud-infrastructure.md)), credentials reach the server through the deployment Secret and are applied per principal at clone/fetch time:

1. The deployment Secret carries the credential material the server needs (or a reference the server resolves at runtime against the platform's secret store, keyed by principal).
2. The server configures a git credential helper / `GIT_SSH_COMMAND` scoped to the operation, using the resolved credential for the requesting principal.
3. Credentials are never written to the hot tier or cold store.

For SSH: materialize the key at a known runtime path, set `GIT_SSH_COMMAND` to use it.
For HTTPS: feed a credential helper that reads the resolved token, or write an ephemeral `.git-credentials` outside the persisted tiers.

> Open question for the platform boundary: whether per-tenant git credentials are encrypted-at-rest in wallfacer's store or resolved on demand from a platform secret service. This spec assumes Identity-scoped resolution; the storage detail is a Cloud Infrastructure / Multi-Tenant credential concern.

### Workspace Group Mapping

**Problem:** Workspace keys are currently derived from absolute local paths. In cloud, repos aren't at user-chosen local paths. They're at provisioned paths on the hot tier.

**Solution:** Introduce a **canonical repo identifier** that is stable across environments.

```
Local:  workspace key = "/Users/alice/repos/project-a\n/Users/alice/repos/project-b"
Cloud:  workspace key = "git@github.com:alice/project-a.git\ngit@github.com:alice/project-b.git"
```

The workspace manager already normalizes and sorts paths before joining (`internal/workspace/manager.go`, `internal/workspace/groups.go`). The change is what constitutes a "path":

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
- `git@github.com:alice/project-a.git` resolves to `/hot/<workspace-id>/repos/project-a/`

This means `Snapshot.Workspaces` still contains absolute paths (for the runner and handler to use), but the persisted group definitions are portable across instances.

### Config Persistence

| Item | Storage | Rationale |
|------|---------|-----------|
| `.env` (API keys, model config) | Deployment Secret, injected as env vars | Credentials must not touch persistent storage; the platform manages the Secret |
| `workspace-groups.json` | fs.latere.ai cold tier (`/wallfacer/config/`) | Defines the tenant's workspace setup; survives hibernate via cold tier |
| `prompts/` overrides | fs.latere.ai cold tier (`/wallfacer/config/prompts/`) | User customizations; survives hibernate via cold tier |
| `instructions/` (AGENTS.md) | fs.latere.ai cold tier (`/wallfacer/config/instructions/`) | Generated from templates + repo files; preserved to avoid re-init churn |

### Worktree Management

Worktree creation logic is unchanged: `internal/runner/worktree.go` orchestrates `internal/gitutil/worktree.go` primitives (`CreateWorktree`, `CreateWorktreeAt`, `RemoveWorktree`) against the repo's `.git` dir. The difference in cloud is that both the repo and the worktree live on the fs.latere.ai hot tier instead of the host filesystem.

**Worktree reachability for the sandbox:**

In host execution (`internal/executor/host.go`, the only executor backend today) the agent runs as a host process and reads the worktree directly. There is no wallfacer-managed container and no volume assembly: `internal/runner/buildContainerSpecForSandbox` describes a host process launch, not a pod, and wallfacer no longer schedules sandbox pods or owns mount manifests (that moved to Cella, see [cloud-infrastructure.md](cloud-infrastructure.md) and [latere-integration/cella-runtime.md](latere-integration/cella-runtime.md)).

When execution moves to a Cella sandbox, the sandbox has no access to the host or hot-tier filesystem, so the task's worktree must reach it by transport, not by mount. That transport is the cella-runtime seam's concern, with two candidate mechanisms:

1. **FS Workspace API:** stage the worktree to the fs.latere.ai hot tier and have Cella mount it. The hot-path layout in this spec (worktrees under `/hot/<workspace-id>/worktrees/<task-uuid>/`) is exactly what makes this staging cheap, since worktrees already live on the FS tier.
2. **Git push/pull:** push the worktree branch to a remote the sandbox can reach, run there, pull results back.

This spec's contribution is the filesystem layout that makes option 1 cheap. The transport mechanism itself is resolved in [latere-integration/cella-runtime.md](latere-integration/cella-runtime.md), not here.

**Worktree GC** works the same: `PruneUnknownWorktrees` (`internal/runner/worktree.go`) and `StartWorktreeGC` / `ScanOrphanedWorktrees` (`internal/runner/worktree_gc.go`) scan the `worktrees/` directory, on the hot tier in cloud mode.

---

## Interface Changes

### Workspace Manager (`internal/workspace/`)

```go
// WorkspaceIdentifier can be a local path or a repo URI.
// The manager resolves it to a filesystem path at runtime.
type WorkspaceIdentifier string

// RepoResolver maps canonical workspace identifiers to local paths.
// Local mode: identity function (path -> path).
// Cloud mode: looks up the provisioned repo path on the hot tier.
type RepoResolver interface {
    Resolve(id WorkspaceIdentifier) (localPath string, err error)
    // ReverseResolve returns the canonical identifier for a local path.
    // Used when reading existing workspace-groups.json from local deployments.
    ReverseResolve(localPath string) (WorkspaceIdentifier, error)
}
```

The workspace manager takes a `RepoResolver` at construction. `Switch()` (`internal/workspace/manager.go`) calls `resolver.Resolve()` on each workspace identifier before creating the snapshot.

### Runner

No changes to worktree orchestration itself. The runner already receives workspace paths from the snapshot. There is no wallfacer-side volume-mount assembly to change; off-host worktree transport is owned by the cella-runtime seam.

### New: Repo Provisioner (`internal/workspace/` + `internal/gitutil/`)

```go
// RepoProvisioner manages git repo lifecycle on the runtime workspace.
// Mapping and orchestration sit in internal/workspace/; the underlying
// clone/fetch primitives are in internal/gitutil/.
type RepoProvisioner struct {
    reposDir string          // e.g., /hot/<workspace-id>/repos/
    creds    CredentialStore // Identity-scoped git credentials
}

func (p *RepoProvisioner) Clone(ctx context.Context, repoURL string) (localPath string, err error)
func (p *RepoProvisioner) Fetch(ctx context.Context, repoURL string) error
func (p *RepoProvisioner) Remove(ctx context.Context, repoURL string) error
func (p *RepoProvisioner) LocalPath(repoURL string) string
func (p *RepoProvisioner) ListRepos() ([]string, error)
```

### New: fs.latere.ai Client (new package)

```go
// FSClient wraps the fs.latere.ai Files and Workspace APIs.
type FSClient struct {
    baseURL string
    token   string  // JWT from Identity (auth.latere.ai)
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
| 1 | Implement `FSClient`, wrapper for fs.latere.ai Files + Workspace APIs | fs.latere.ai Phase 5 | Medium |
| 2 | Define `RepoResolver` interface; implement `LocalResolver` (identity) | (none) | Small |
| 3 | Thread `RepoResolver` through workspace manager | 2 | Medium |
| 4 | Add `gitutil.Clone` (partial clone); implement `RepoProvisioner` (clone, fetch, remove on hot path) | (none) | Medium |
| 5 | Implement `CloudResolver` using `RepoProvisioner` + `FSClient` | 1, 2, 4 | Small |
| 6 | Extend `workspace-groups.json` to store repo URIs; migrate existing format | 3 | Medium |
| 7 | Credential injection: Identity-scoped SSH key / HTTPS token applied at clone/fetch | 4 | Medium |
| 8 | Integration test: workspace create -> clone -> worktree -> config sync round-trip | 1-7 | Medium |

---

## Dependencies

- **Multi-Workspace Groups** (`specs/foundations/multi-workspace-groups.md`), complete. Workspace key mechanics (`internal/workspace/manager.go`, `groups.go`) and multi-group lifecycle are the foundation this builds on.
- **Storage Backend Interface** (`specs/foundations/storage-backends.md`, archived), task data goes through `StorageBackend`, not the tenant workspace.
- **Cella runtime seam** (`specs/cloud/latere-integration/cella-runtime.md`), resolves off-host worktree transport. This spec provides the hot-path layout that makes FS-staged transport cheap.
- **fs.latere.ai Phase 5 (Workspace API)**, external prerequisite. Provides hot tier workspace allocation and lifecycle.

## What depends on this

- **Cella runtime** (`latere-integration/cella-runtime.md`), consumes the hot tier workspace layout and `RepoResolver` so the worktree can be staged to or reached by the sandbox.
- **Cloud Infrastructure** (`cloud-infrastructure.md`), the task-board server Deployment carries the Secret that resolves Identity-scoped git credentials and FS/Identity tokens.
