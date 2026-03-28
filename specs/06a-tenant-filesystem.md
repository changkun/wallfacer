# M6a: Tenant Filesystem

**Status:** Not started | **Date:** 2026-03-28

## Problem

Wallfacer's local deployment stores everything under `~/.wallfacer/` and the user's repo directories. Five filesystem concerns need cloud answers:

| Concern | Local path | Lifecycle | Cloud question |
|---------|-----------|-----------|----------------|
| **Git repos** (source code) | `~/repos/myproject/` | User-managed | Who clones? Where? How are creds managed? |
| **Task data** | `~/.wallfacer/data/<ws-key>/` | Store-managed | Solved by M2 (PG + S3) |
| **Worktrees** | `~/.wallfacer/worktrees/<task-uuid>/` | Ephemeral per task | Must be accessible to both server and sandbox pod |
| **Config/state** | `~/.wallfacer/{.env, workspace-groups.json, prompts/}` | Long-lived per tenant | Must survive hibernate/wake cycles |
| **Ephemera** | `~/.wallfacer/tmp/` (board snapshots) | Throwaway | Instance-local, no persistence needed |

M2 handles task data (row 2). This spec handles the remaining four rows and the connections between them. Without this, the K8s sandbox backend (M6b) has no filesystem to mount into pods, and the multi-tenant control plane (M8) has no way to provision or persist a tenant's working state.

## Current Local Architecture

```
~/.wallfacer/                          # Config root
├── .env                               # API keys, model config
├── workspace-groups.json              # Persisted workspace group definitions
├── instructions/                      # Per-group AGENTS.md files (keyed by SHA-256 of sorted paths)
├── prompts/                           # User-overridden system prompt templates
├── data/                              # Task data (owned by StorageBackend — M2)
│   └── <workspace-key>/
│       └── <task-uuid>/
│           ├── task.json
│           ├── traces/
│           └── outputs/
├── worktrees/                         # Ephemeral per-task git worktrees
│   └── <task-uuid>/
│       └── <repo-basename>/           # One worktree per workspace repo
└── tmp/                               # Board snapshots, ephemeral files

~/repos/                               # User's source repos (outside wallfacer)
├── project-a/                         # Workspace directory 1
└── project-b/                         # Workspace directory 2
```

Key coupling points:
- **Workspace key** = `strings.Join(sorted absolute paths, "\n")` — depends on local path identity
- **Worktree creation** calls `git worktree add` against the repo's `.git` dir on the host
- **Container mounts** bind-mount host worktree paths into `/workspace/<basename>`
- **Instructions file** references per-repo `AGENTS.md` / `CLAUDE.md` by host path

All of these assume the server and containers share a local filesystem.

---

## Design

### Per-Tenant Volume Architecture

Each tenant gets a single persistent volume that holds everything except task data (which goes through `StorageBackend`):

```
/wallfacer/<tenant-id>/                # Tenant volume root
├── config/
│   ├── workspace-groups.json          # Workspace group definitions
│   ├── instructions/                  # Per-group AGENTS.md files
│   └── prompts/                       # User-overridden system prompt templates
├── repos/                             # Cloned git repos
│   ├── project-a/                     # Full clone of tenant's repo
│   └── project-b/
├── worktrees/                         # Per-task worktrees (same layout as local)
│   └── <task-uuid>/
│       └── <repo-basename>/
└── tmp/                               # Ephemeral board snapshots
```

**What's NOT on the tenant volume:**
- `.env` — injected by the control plane as environment variables or a mounted secret (see Config Persistence below)
- `data/` — handled by `StorageBackend` (M2's PG + S3 backends); the store doesn't touch this volume
- Container images — pulled by the kubelet (K8s) or container runtime

**Volume type options:**

| Option | Access mode | Tradeoffs |
|--------|------------|-----------|
| **Block PVC (EBS/PD)** | ReadWriteOnce | Fast, simple; server and sandbox pods must co-locate on same node |
| **NFS/EFS PVC** | ReadWriteMany | Pods can schedule anywhere; higher latency; operational complexity |
| **Local SSD + pod affinity** | ReadWriteOnce | Fastest I/O; tied to a specific node; no cross-node failover |

**Recommended:** Start with ReadWriteOnce block PVC + pod affinity. The wallfacer server pod and sandbox pods for the same tenant are forced onto the same node. This is simple, fast, and avoids the complexity of distributed filesystems. NFS/EFS can be added later if scheduling flexibility is needed.

### Repo Provisioner

A new component (`internal/repo/` or `internal/tenant/`) manages the lifecycle of git repos on the tenant volume.

**Operations:**

| Operation | When | What |
|-----------|------|------|
| **Clone** | Tenant first login / workspace add | `git clone --filter=blob:none <url>` (partial clone for speed) |
| **Fetch** | Instance wake from hibernate, periodic | `git fetch origin` to pick up upstream changes |
| **Push** | Task commit pipeline (existing) | `git push` from worktree branch — same as today |
| **Prune** | Workspace removal | Remove repo directory, clean up worktrees that referenced it |

**Credential management:**

Repos may be private. The control plane stores per-tenant git credentials (SSH keys or HTTPS tokens) encrypted in its database. On instance provision:

1. Control plane injects credentials as a K8s Secret mounted into the server pod
2. Server configures git credential helper to use the mounted secret
3. Credentials are never written to the tenant volume

For SSH: mount the key at a known path, set `GIT_SSH_COMMAND` to use it.
For HTTPS: mount a `.git-credentials` file or use a credential helper that reads from the secret.

**Partial clone strategy:**

Full clones of large repos are slow and waste space. Use git's partial clone (`--filter=blob:none`) to fetch only tree objects initially. Blobs are fetched on demand during worktree creation and checkout. This trades network round-trips for disk space savings.

For tenants with very large repos, offer a `depth` option for shallow clones. Worktree creation from shallow repos requires `git fetch --unshallow` of the relevant branch, which can be done lazily.

### Workspace Group Mapping

**Problem:** Workspace keys are currently derived from absolute local paths. In cloud, repos aren't at user-chosen local paths — they're at provisioned paths on the tenant volume.

**Solution:** Introduce a **canonical repo identifier** that is stable across environments.

```
Local:  workspace key = "/Users/alice/repos/project-a\n/Users/alice/repos/project-b"
Cloud:  workspace key = "git@github.com:alice/project-a.git\ngit@github.com:alice/project-b.git"
```

The workspace manager already normalizes and sorts paths before joining. The change is what constitutes a "path":

- **Local mode** (unchanged): absolute filesystem paths
- **Cloud mode**: normalized remote URLs (stripped of trailing `.git`, lowercased host, sorted)

`workspace-groups.json` stores the canonical identifiers. The repo provisioner maps them to actual filesystem paths on the tenant volume:

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
- `git@github.com:alice/project-a.git` → `/wallfacer/<tenant-id>/repos/project-a/`

This means `Snapshot.Workspaces` still contains absolute paths (for the runner and handler to use), but the persisted group definitions are portable across instances.

### Config Persistence

| Item | Storage | Rationale |
|------|---------|-----------|
| `.env` (API keys, model config) | K8s Secret, injected as env vars | Credentials must not touch persistent volume; control plane manages them |
| `workspace-groups.json` | Tenant volume (`config/`) | Defines the tenant's workspace setup; must survive hibernate |
| `prompts/` overrides | Tenant volume (`config/prompts/`) | User customizations; must survive hibernate |
| `instructions/` (AGENTS.md) | Tenant volume (`config/instructions/`) | Generated from templates + repo files; *could* be regenerated but preserving avoids re-init churn |

On **hibernate**: the tenant volume persists (PVC is retained). Task data is already in cloud storage (M2). The instance pod is deleted.

On **wake**: a new pod is scheduled with the same PVC mounted. The wallfacer process starts, loads `workspace-groups.json` from the volume, connects to cloud storage for task data, and resumes. No migration or re-clone needed (repos are on the volume).

### Worktree Management on Shared Storage

Worktree creation logic (`internal/runner/worktree.go`) is unchanged — it calls `git worktree add` against the repo's `.git` dir. The difference is that both the repo and the worktree are on the tenant PVC instead of the host filesystem.

**Server ↔ sandbox pod access:**

With ReadWriteOnce PVC + pod affinity, the server pod and sandbox pods are on the same node and mount the same PVC. Worktree paths are valid in both pods. The `ContainerSpec.Volumes` entries use PVC subpaths instead of host bind-mounts:

```
# Local (current)
Volume: hostPath /home/user/.wallfacer/worktrees/<task-uuid>/project-a → /workspace/project-a

# Cloud (K8s)
Volume: PVC subPath worktrees/<task-uuid>/project-a → /workspace/project-a
```

The runner's `buildContainerSpecForSandbox()` needs to emit PVC-based volume mounts instead of host path mounts. This is a concern for M6b (K8s Sandbox Backend) — this spec just ensures the filesystem layout supports it.

**Worktree GC** works the same: `PruneUnknownWorktrees()` and `StartWorktreeGC()` scan the `worktrees/` directory on the tenant volume.

---

## Interface Changes

### Workspace Manager

```go
// WorkspaceIdentifier can be a local path or a repo URI.
// The manager resolves it to a filesystem path at runtime.
type WorkspaceIdentifier string

// RepoResolver maps canonical workspace identifiers to local paths.
// Local mode: identity function (path → path).
// Cloud mode: looks up the provisioned repo path on the tenant volume.
type RepoResolver interface {
    Resolve(id WorkspaceIdentifier) (localPath string, err error)
    // ReverseResolve returns the canonical identifier for a local path.
    // Used when reading existing workspace-groups.json from local deployments.
    ReverseResolve(localPath string) (WorkspaceIdentifier, error)
}
```

The workspace manager takes a `RepoResolver` at construction. `Switch()` calls `resolver.Resolve()` on each workspace identifier before creating the snapshot.

### Runner

No changes to worktree logic itself. The runner already receives workspace paths from the snapshot. Volume mount assembly changes are in M6b (K8s sandbox backend).

### New: Repo Provisioner

```go
// RepoProvisioner manages git repo lifecycle on the tenant volume.
type RepoProvisioner struct {
    reposDir string          // e.g., /wallfacer/<tenant-id>/repos/
    creds    CredentialStore // mounted secrets
}

func (p *RepoProvisioner) Clone(ctx context.Context, repoURL string) (localPath string, err error)
func (p *RepoProvisioner) Fetch(ctx context.Context, repoURL string) error
func (p *RepoProvisioner) Remove(ctx context.Context, repoURL string) error
func (p *RepoProvisioner) LocalPath(repoURL string) string
func (p *RepoProvisioner) ListRepos() ([]string, error)
```

---

## Implementation Tasks

| # | Task | Depends on | Effort |
|---|------|-----------|--------|
| 1 | Define `RepoResolver` interface; implement `LocalResolver` (identity) | — | Small |
| 2 | Thread `RepoResolver` through workspace manager | 1 | Medium |
| 3 | Implement `RepoProvisioner` (clone, fetch, remove) | — | Medium |
| 4 | Implement `CloudResolver` using `RepoProvisioner` | 1, 3 | Small |
| 5 | Extend `workspace-groups.json` to store repo URIs; migrate existing format | 2 | Medium |
| 6 | Config persistence: relocate `.env` to injected env vars in cloud mode | — | Small |
| 7 | Credential injection: SSH key / HTTPS token mounting from K8s secrets | 3 | Medium |
| 8 | Integration test: full clone → worktree → container mount round-trip | 2, 3, 4 | Medium |

---

## Dependencies

- **M1 (Sandbox Backend Interface)** — complete. `ContainerSpec.Volumes` is the mount point.
- **M2 (Storage Backend Interface)** — complete (enablers). Task data goes through `StorageBackend`, not the tenant volume.
- **M2a (Multi-Workspace Groups)** — complete. Workspace key mechanics and multi-group lifecycle are the foundation this builds on.

## What depends on this

- **M6b (K8s Sandbox Backend)** — needs the tenant volume layout and `RepoResolver` to assemble pod volume mounts.
- **M8 (Multi-Tenant)** — the control plane calls the repo provisioner to set up tenant workspaces, and manages credential secrets.
