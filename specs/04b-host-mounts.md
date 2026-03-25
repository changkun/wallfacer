# Per-Task Read-Only Host Mounts

**Status:** Draft
**Date:** 2026-03-25

---

## Problem Statement

Tasks sometimes need access to large datasets, model checkpoints, shared asset directories, or reference codebases that live on the host filesystem but are outside the configured workspace directories. Today the only way to provide this context is to either move the data into a workspace, copy it via 04a file attachments (limited to small files stored in task data), or describe it in the prompt and hope the agent can work without it.

Users need a lightweight way to say "this task should also see `/data/training-set/` and `/opt/design-tokens/palette.json`" without copying anything or reconfiguring workspaces.

---

## Goal

Allow users to attach one or more host filesystem paths (files or directories) to a task. These paths are bind-mounted **read-only** into the sandbox container at `/workspace/.mounts/<basename>` and referenced in the first-turn prompt so the agent knows they exist.

No data is copied. No new storage is consumed. The host path is mounted in-place, just like workspace directories, but scoped to individual tasks.

---

## Architecture

### Data model

**`internal/store/models.go`** — new struct and field:

```go
// HostMount describes a host filesystem path to be bind-mounted
// read-only into the sandbox container for a single task.
type HostMount struct {
    HostPath string `json:"host_path"` // absolute path on the host
    Label    string `json:"label"`     // user-facing display name (defaults to basename)
}
```

**`Task` struct** — add one field:

```go
HostMounts []HostMount `json:"host_mounts,omitempty"`
```

`omitempty` means existing `task.json` files without the field deserialise cleanly with a nil slice. No schema migration needed.

**What is NOT stored:** The container-side path (`/workspace/.mounts/<name>`) is derived at mount-build time from the basename of `HostPath` (plus collision suffixes). It is never persisted, avoiding stale data if mounts are reordered or renamed.

### Container mount mapping

**`internal/runner/container.go`** — in `buildContainerArgs()`, after existing workspace and attachment mounts:

```go
for _, hm := range task.HostMounts {
    info, err := os.Stat(hm.HostPath)
    if err != nil {
        // Path missing at launch time — skip silently, log a system event.
        r.store.AppendEvent(ctx, task.ID, store.EventSystem,
            fmt.Sprintf("host mount skipped: %s (%v)", hm.HostPath, err))
        continue
    }
    basename := sanitizeMountBasename(hm.HostPath, usedBasenames)
    usedBasenames[basename] = true
    containerPath := "/workspace/.mounts/" + basename
    spec.Volumes = append(spec.Volumes, VolumeMount{
        Host:      hm.HostPath,
        Container: containerPath,
        Options:   mountOpts("z", "ro"),
    })
}
```

**Basename derivation and collision handling:**

`sanitizeMountBasename(hostPath string, used map[string]bool) string`:

1. Take `filepath.Base(hostPath)`.
2. Sanitize: lowercase, replace non-alphanumeric characters (except `.` and `-`) with `_`, trim leading dots/underscores.
3. If the result collides with an already-used name, append `-2`, `-3`, etc.
4. Return the unique basename.

The leading-dot parent `.mounts/` avoids collisions with workspace directory basenames (same convention as `.attachments/` from 04a and `.tasks/` for board context).

### Prompt augmentation

**`internal/runner/execute.go`** — on turn 1, when `task.HostMounts` is non-empty and at least one mount was successfully added, append a stanza to the user prompt:

```
---
## Host Mounts

The following host paths are mounted read-only inside the container:

| Mount | Type | Container Path |
|-------|------|----------------|
| /data/training-set | directory | /workspace/.mounts/training-set |
| /opt/tokens/palette.json | file | /workspace/.mounts/palette.json |

Use the `Read` tool (for files) or `Glob`/`Grep` (for directories) to explore these paths as needed.
---
```

The "Type" column is derived from `os.Stat` at mount time (file vs directory). This follows the same augmentation pattern as 04a (file attachments).

---

## API Changes

### `POST /api/tasks` — task creation

The request body gains an optional `host_mounts` field:

```json
{
  "prompt": "Analyze the training data and suggest improvements",
  "timeout": 900,
  "host_mounts": [
    { "host_path": "/data/training-set", "label": "Training Data" },
    { "host_path": "/opt/tokens/palette.json" }
  ]
}
```

When `label` is omitted, it defaults to the basename of `host_path`.

Validation is applied before saving (see Path Validation below). Invalid entries are rejected with `400 Bad Request` and an error message identifying the offending path.

### `POST /api/tasks/batch` — batch creation

Each task object in the batch array gains the same `host_mounts` field. Validation is applied per-task. If any task in the batch fails validation, the entire batch is rejected (atomic).

### `PATCH /api/tasks/{id}` — task update

`host_mounts` can be set or replaced when the task is in `backlog` or `waiting` status. The field is a full replacement (not a merge) — send the complete desired list. Sending `[]` or `null` clears all mounts.

Attempting to modify `host_mounts` on a task in any other status returns `409 Conflict`.

### `GET /api/workspaces/browse` — extend with file listing

Add an optional query parameter `include_files=true`. When set, the response includes file entries alongside directory entries.

Current response shape (unchanged when `include_files` is absent or `false`):

```json
{
  "path": "/data",
  "entries": [
    { "name": "training-set", "path": "/data/training-set", "is_git_repo": false }
  ]
}
```

Extended response when `include_files=true`:

```json
{
  "path": "/data",
  "entries": [
    { "name": "training-set", "path": "/data/training-set", "is_git_repo": false, "is_file": false },
    { "name": "config.json",  "path": "/data/config.json",  "is_git_repo": false, "is_file": true }
  ]
}
```

The `is_file` field is added to `workspaceBrowseEntry`. It is `false` for directories and `true` for regular files. Existing callers that do not pass `include_files=true` see no change — files are still filtered out by default.

---

## Path Validation

All host mount paths are validated on creation and update. A mount is rejected if any check fails:

| Check | Rule | Error |
|-------|------|-------|
| Absolute path | Must start with `/` | "host_path must be absolute" |
| Cleaned path | `filepath.Clean(path) == path` — no `..`, `//`, trailing `/` | "host_path must be a clean absolute path" |
| Exists | `os.Stat` succeeds | "host_path does not exist" |
| Not a workspace | Path is not equal to, or a parent/child of, any active workspace directory | "host_path overlaps with a workspace directory" |
| Not sensitive | Not under `/etc`, `/var`, `/proc`, `/sys`, `/dev`, `/private/etc` (macOS), user home dot-directories (`~/.ssh`, `~/.gnupg`, etc.) | "host_path points to a restricted system location" |
| Max count | At most 10 mounts per task | "too many host mounts (max 10)" |
| No duplicates | No two mounts in the same task share the same `HostPath` | "duplicate host_path" |

The workspace-overlap check uses the currently active workspace set from `envconfig`. This prevents confusing scenarios where the same directory is mounted both as a read-write workspace and a read-only host mount.

Sensitive-path checks are intentionally conservative. The blocklist covers common locations where credentials, keys, and system configuration reside. Users who need to mount paths under these locations can use symlinks from a non-restricted location.

---

## UI Flow

### Task creation form

Below the prompt textarea and any attachment drop zone (04a), add a "Host Mounts" section:

```
[Host Mounts]
┌──────────────────────────────────────────────────┐
│  Path: [________________________] [Browse] [Add] │
└──────────────────────────────────────────────────┘
  ┌──────────────────────────┐
  │ /data/training-set    ×  │  ← chip (directory icon)
  │ /opt/tokens/palette   ×  │  ← chip (file icon)
  └──────────────────────────┘
```

- **Text input**: Accepts an absolute path typed directly.
- **Browse button**: Opens a modal reusing the workspace browser (`GET /api/workspaces/browse` with `include_files=true`), allowing navigation and selection of a file or directory. The selected path is added to the mount list.
- **Add button**: Validates the typed path client-side (non-empty, starts with `/`) and adds it to the staged list.
- **Chips**: Each staged mount displays as a removable chip showing the path (or label if set) and a `×` button.
- Chips are included as `host_mounts` in the `POST /api/tasks` request body.

### Task detail modal

When viewing a task with host mounts:

- Display a "Host Mounts" section listing each mount with its path, label, and type icon (file/directory).
- For `backlog` and `waiting` tasks: show an "Edit" affordance that allows adding/removing mounts (sends `PATCH /api/tasks/{id}`).
- For other statuses: read-only display.

---

## Edge Cases

| Scenario | Behaviour |
|----------|-----------|
| **Path disappears between creation and launch** | Mount silently skipped; system event logged. Container starts normally with remaining mounts. |
| **Path is a symlink** | Resolved by the container runtime. The symlink target must also pass validation (absolute, not sensitive, etc.). Validate the resolved path, not the symlink itself. |
| **Path is a block device or socket** | `os.Stat` succeeds but `ModeType` is not regular file or directory. Reject with "host_path must be a regular file or directory". |
| **Basename collision across mounts** | Handled by `-2`, `-3` suffix. E.g., two mounts `/a/data` and `/b/data` become `.mounts/data` and `.mounts/data-2`. |
| **Very long path** | Basename is truncated to 64 characters before suffix. |
| **Workspace set changes after task creation** | Validation only runs at creation/update time. A mount that was valid when created but now overlaps with a newly-added workspace is still honoured at launch. Re-saving the mounts (via PATCH) would trigger re-validation. |
| **Mount path contains spaces or special characters** | Handled by the container runtime's `-v` flag quoting. The `VolumeMount` struct is passed through `buildContainerArgs` which already handles quoting for workspace mounts. |
| **Task retry** | Host mounts are preserved on retry (same task data). The paths are re-validated at launch time — if a path no longer exists, it is silently skipped. |
| **Batch creation with shared mounts** | Each task in the batch independently stores its own `host_mounts` slice. No sharing or deduplication across tasks. |

---

## Relationship to 04a (File Attachments)

04a and 04b are complementary features for getting external files into the sandbox:

| | 04a: File Attachments | 04b: Host Mounts |
|---|---|---|
| **Mechanism** | Files copied into `data/<uuid>/attachments/` | Host paths bind-mounted in-place |
| **Size** | Small files (10 MB per file, 100 MB per task) | Any size (no copy) |
| **Persistence** | Stored with task data forever | Reference only; host path must exist at launch |
| **Container path** | `/workspace/.attachments/` | `/workspace/.mounts/<basename>` |
| **Access** | Read-only | Read-only |
| **Prompt augmentation** | Yes (turn 1) | Yes (turn 1) |
| **Use case** | Screenshots, small reference docs, data snippets | Large datasets, model checkpoints, shared asset dirs, reference repos |

Both can be used on the same task. The prompt augmentation stanzas are separate sections appended in order (attachments first, then host mounts).

---

## Scope Boundaries

This spec does **NOT** include:

- **Read-write mounts.** All host mounts are read-only. If a task needs to write output to a host path, that's a separate feature with significant safety implications.
- **Mount path remapping.** The container path is always `/workspace/.mounts/<basename>`. Users cannot choose a custom container-side path.
- **Global/workspace-level mounts.** Mounts are per-task only. A "default mounts" feature (applied to all tasks in a workspace) could be added later as a configuration option.
- **Remote/network mounts.** Only local filesystem paths are supported. NFS/SMB mounts that happen to appear as local paths will work transparently, but there is no explicit network mount support.
- **Mount quotas or rate limiting.** Beyond the 10-mount-per-task limit, there is no global limit on total mounts across tasks.
- **Changes to the sandbox Dockerfile.** The `/workspace/.mounts/` directory is created implicitly by the bind mount; no image changes needed.

---

## Files Touched Summary

| File | Change type |
|------|-------------|
| `internal/store/models.go` | Add `HostMount` struct, `HostMounts` field on `Task` |
| `internal/runner/container.go` | Add host mount volumes in `buildContainerArgs()`; `sanitizeMountBasename()` helper |
| `internal/runner/execute.go` | Prompt augmentation for host mounts on turn 1 |
| `internal/handler/tasks.go` | Validate `host_mounts` in create/batch/update handlers |
| `internal/handler/workspace.go` | `include_files` query param in `BrowseWorkspaces` |
| `internal/apicontract/routes.go` | Document `include_files` param on browse route |
| `ui/index.html` | Host mounts section in task creation form |
| `ui/js/tasks.js` | Mount input, browse integration, chip rendering, include in create/update payloads |
| `ui/js/modal.js` | Display host mounts in task detail modal; edit for backlog/waiting |

No changes to `go.mod`, sandbox Dockerfiles, git/worktree logic, or the instructions system.
