---
title: Per-Task Host Path References
status: stale
depends_on:
  - specs/foundations/file-explorer.md
affects:
  - internal/store/models.go
  - internal/runner/execute.go
  - internal/handler/tasks.go
  - internal/handler/workspace.go
  - internal/apicontract/routes.go
  - frontend/src/components/TaskComposer.vue
  - frontend/src/components/TaskDetail.vue
effort: medium
created: 2026-03-25
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# Per-Task Host Path References

---

## Problem Statement

Tasks sometimes need access to large datasets, model checkpoints, shared asset directories, or reference codebases that live on the host filesystem but are outside the configured workspace directories. Today the only way to provide this context is to either move the data into a workspace, copy it via task prompt attachments (see `specs/local/task-prompt-attachments.md`, limited to small files stored in task data), or describe it in the prompt and hope the agent locates the path on its own.

Users need a lightweight way to say "this task should also look at `/data/training-set/` and `/opt/design-tokens/palette.json`" without copying anything or reconfiguring workspaces.

---

## Architecture note: host execution, no container boundary

This spec was originally drafted against a container backend that bind-mounted host paths read-only into the agent's container at `/workspace/.mounts/<basename>`. That model is gone. `internal/sandbox/` was renamed to `internal/executor/`, `LocalBackend` was removed, and wallfacer now runs the agent **directly on the host** in a git worktree (`executor.HostBackend`). `executor.ContainerSpec` no longer carries any `Volumes`/`VolumeMount` field, container path translation, or mount options. `internal/runner/container.go` still exists and still builds the launch spec via `buildHostSpec`, but it composes a host process (WorkDir, Env, Cmd), not a container.

Two consequences shape this feature:

1. **There is nothing to mount.** The agent already has ambient access to the entire host filesystem, including every configured workspace, its worktree, and any path the user names. A host path reference is therefore a *registration plus context-surfacing convenience*, not a plumbing step. The agent reaches the path by its literal absolute host path, not a remapped container path.

2. **Read-only is no longer enforced by the runtime.** With a container boundary gone, there is no `ro` mount option and no sandbox to confine the agent. "Read-only" can only be advisory (a hint in the prompt stanza). The sensitive-path blocklist (`/etc`, `~/.ssh`, ...) no longer *blocks* the agent from reading those paths; it only governs what wallfacer is willing to advertise as a registered reference. This feature is prompt-context ergonomics, not an isolation boundary. See Open Questions.

`internal/runner/container.go:container_host_mode_test.go` (the existing host-mode tests) is the reference for how WorkDir, board context, and sibling worktrees are surfaced today: via `WALLFACER_*` env vars and turn-1 prompt text, not volumes.

---

## Goal

Let users register one or more host filesystem paths (files or directories) on a task. wallfacer validates them, stores them on the task, and surfaces them to the agent in the first-turn prompt so the agent knows they exist and where to look. No data is copied. No new storage is consumed.

---

## Architecture

### Data model

**`internal/store/models.go`** (new struct and field):

```go
// HostMount registers a host filesystem path that a task should be made
// aware of. In host-execution mode the agent already has filesystem access,
// so this records the path and surfaces it to the agent in the first-turn
// prompt; it is not a bind mount.
type HostMount struct {
    HostPath string `json:"host_path"` // absolute path on the host
    Label    string `json:"label"`     // user-facing display name (defaults to basename)
}
```

**`Task` struct** (add one field):

```go
HostMounts []HostMount `json:"host_mounts,omitempty"`
```

`omitempty` means existing `task.json` files without the field deserialise cleanly with a nil slice. No schema migration needed.

**What is NOT stored:** No derived container-side path. The agent uses the literal `HostPath`, so there is nothing else to persist.

### Prompt augmentation

**`internal/runner/execute.go`** (on turn 1): when `task.HostMounts` is non-empty and at least one referenced path still exists, append a stanza to the user prompt (the same place board context and sibling-worktree context are surfaced today):

```
---
## Host Paths

This task references the following host filesystem paths. Treat them as
read-only inputs unless the task explicitly asks you to modify them:

| Label | Type | Path |
|-------|------|------|
| Training Data | directory | /data/training-set |
| palette.json  | file      | /opt/tokens/palette.json |

Use the Read tool (for files) or Glob/Grep (for directories) to explore them.
---
```

The "Type" column is derived from `os.Stat` at augmentation time (file vs directory). Paths that no longer exist at launch are omitted from the stanza and a system event is logged. This follows the same augmentation pattern as the board-context and sibling-worktree stanzas already wired in `execute.go`. (Prompt augmentation, not an env-var manifest: nothing agent-side currently reads an arbitrary path manifest, so the prompt is the only wired surface.)

---

## API Changes

### `POST /api/tasks` (task creation)

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

### `POST /api/tasks/batch` (batch creation)

Each task object in the batch array gains the same `host_mounts` field. Validation is applied per-task. If any task in the batch fails validation, the entire batch is rejected (atomic).

### `PATCH /api/tasks/{id}` (task update)

`host_mounts` can be set or replaced when the task is in `backlog` or `waiting` status. The field is a full replacement (not a merge), so send the complete desired list. Sending `[]` or `null` clears all references.

Attempting to modify `host_mounts` on a task in any other status returns `409 Conflict`.

### `GET /api/workspaces/browse` (extend with file listing)

Add an optional query parameter `include_files=true`. When set, the response includes file entries alongside directory entries so the picker can select a file as well as a directory.

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

The `is_file` field is added to `workspaceBrowseEntry` in `internal/handler/workspace.go`. It is `false` for directories and `true` for regular files. Existing callers that do not pass `include_files=true` see no change (files are still filtered out by default). Document the new query param on the browse route in `internal/apicontract/routes.go`.

---

## Path Validation

All host path references are validated on creation and update. This is **input hygiene** (catch typos, refuse obviously wrong paths, keep the prompt stanza honest), not a security boundary: in host mode the agent can read any path regardless, so validation governs what wallfacer is willing to register and advertise, not what the agent can technically reach.

A reference is rejected if any check fails:

| Check | Rule | Error |
|-------|------|-------|
| Absolute path | Must start with `/` | "host_path must be absolute" |
| Cleaned path | `filepath.Clean(path) == path` (no `..`, `//`, trailing `/`) | "host_path must be a clean absolute path" |
| Exists | `os.Stat` succeeds | "host_path does not exist" |
| Regular file or dir | `ModeType` is a regular file or directory (reject sockets, devices, named pipes) | "host_path must be a regular file or directory" |
| Not a workspace | Path is not equal to, or a parent/child of, any active workspace directory | "host_path overlaps with a workspace directory" |
| Not sensitive | Not under `/etc`, `/var`, `/proc`, `/sys`, `/dev`, `/private/etc` (macOS), user home dot-directories (`~/.ssh`, `~/.gnupg`, etc.) | "host_path points to a restricted system location" |
| Max count | At most 10 references per task | "too many host mounts (max 10)" |
| No duplicates | No two references in the same task share the same `HostPath` | "duplicate host_path" |

The workspace-overlap check uses the currently active workspace set from `envconfig`. This prevents the confusing case of registering a directory both as a read-write workspace and a host reference.

Sensitive-path checks are intentionally conservative: wallfacer simply declines to *advertise* these locations. They do not constrain the agent, which can read them anyway. Users who want a sensitive path surfaced can symlink it from a non-restricted location.

---

## UI Flow

### Task creation form (`frontend/src/components/TaskComposer.vue`)

Below the prompt textarea, add a "Host Paths" section:

```
[Host Paths]
┌──────────────────────────────────────────────────┐
│  Path: [________________________] [Browse] [Add] │
└──────────────────────────────────────────────────┘
  ┌──────────────────────────┐
  │ /data/training-set    ×  │  ← chip (directory icon)
  │ /opt/tokens/palette   ×  │  ← chip (file icon)
  └──────────────────────────┘
```

- **Text input**: Accepts an absolute path typed directly.
- **Browse button**: Opens a picker reusing the workspace browser (`GET /api/workspaces/browse` with `include_files=true`), allowing navigation and selection of a file or directory. The selected path is added to the reference list.
- **Add button**: Validates the typed path client-side (non-empty, starts with `/`) and adds it to the staged list.
- **Chips**: Each staged reference displays as a removable chip showing the path (or label if set) and a `×` button.
- Chips are included as `host_mounts` in the `POST /api/tasks` request body.

### Task detail view (`frontend/src/components/TaskDetail.vue`)

When viewing a task with host path references:

- Display a "Host Paths" section listing each reference with its path, label, and type icon (file/directory).
- For `backlog` and `waiting` tasks: show an "Edit" affordance that allows adding/removing references (sends `PATCH /api/tasks/{id}`).
- For other statuses: read-only display.

---

## Edge Cases

| Scenario | Behaviour |
|----------|-----------|
| **Path disappears between creation and launch** | Reference omitted from the turn-1 prompt stanza; system event logged. Task runs normally with the remaining references. |
| **Path is a symlink** | Validate the resolved target (absolute, not sensitive, regular file/dir), not the symlink itself. The agent reads through the symlink at the original path. |
| **Path is a block device or socket** | `os.Stat` succeeds but `ModeType` is not regular file or directory. Reject with "host_path must be a regular file or directory". |
| **Workspace set changes after task creation** | Validation only runs at creation/update time. A reference that was valid when created but now overlaps with a newly-added workspace is still surfaced at launch. Re-saving via PATCH triggers re-validation. |
| **Task retry** | References are preserved on retry (same task data). Existence is re-checked at launch, and a path that no longer exists is omitted from the stanza. |
| **Batch creation with shared references** | Each task in the batch independently stores its own `host_mounts` slice. No sharing or deduplication across tasks. |

---

## Relationship to task prompt attachments

Host path references and task prompt attachments (`specs/local/task-prompt-attachments.md`, drafted) are complementary ways to get external files in front of the agent:

| | Task Prompt Attachments | Host Path References |
|---|---|---|
| **Mechanism** | Small files stored with task data | Absolute host paths recorded on the task |
| **Size** | Small files (stored) | Any size (no copy) |
| **Persistence** | Stored with task data | Reference only; host path must exist at launch |
| **How the agent reaches it** | Surfaced in the prompt / task data | Literal host path, surfaced in the turn-1 prompt stanza |
| **Use case** | Screenshots, small reference docs, data snippets | Large datasets, model checkpoints, shared asset dirs, reference repos |

Both can be used on the same task. Their turn-1 prompt stanzas are appended as separate sections.

---

## Scope Boundaries

This spec does **NOT** include:

- **Runtime-enforced read-only access.** In host mode there is no sandbox to enforce it. "Read-only" is an advisory hint in the prompt stanza. A true read-only guarantee would require reintroducing a confinement boundary and is out of scope (see `internal/runner/container.go`, which still exists but whose container model is being phased out).
- **Container path remapping.** There is no container, so no `/workspace/.mounts/<basename>` remapping. The agent uses the literal host path.
- **Global/workspace-level references.** References are per-task only. A "default references" feature (applied to all tasks in a workspace) could be added later as a configuration option.
- **Remote/network mounts.** Only local filesystem paths are supported. NFS/SMB mounts that happen to appear as local paths work transparently, but there is no explicit network mount support.
- **Reference quotas.** Beyond the 10-per-task limit, there is no global limit across tasks.

---

## Files Touched Summary

| File | Change type |
|------|-------------|
| `internal/store/models.go` | Add `HostMount` struct, `HostMounts` field on `Task` |
| `internal/runner/execute.go` | Turn-1 prompt augmentation for host path references; stat-and-omit missing paths |
| `internal/handler/tasks.go` | Validate `host_mounts` in create/batch/update handlers |
| `internal/handler/workspace.go` | `include_files` query param and `is_file` field in `BrowseWorkspaces` |
| `internal/apicontract/routes.go` | Document `include_files` param on browse route |
| `frontend/src/components/TaskComposer.vue` | Host Paths section in the task creation form: input, browse, chips, include in create/update payload |
| `frontend/src/components/TaskDetail.vue` | Display host path references in task detail; edit for backlog/waiting |

No changes to `go.mod`, git/worktree logic, or the instructions system. Because the change is prompt-only, `internal/runner/container.go`'s host spec builder (`buildHostSpec`) is unaffected; the references flow through `execute.go`'s prompt assembly.

---

## Open Questions

- **Does the "read-only" guarantee still belong in scope?** In host mode there is no runtime that can enforce it (no `ro` mount, no sandbox). The spec currently treats "read-only" as an advisory hint in the prompt stanza and validation as input hygiene. If a real read-only guarantee is required, this feature would need to wait on (or reintroduce) a confinement boundary, which is a much larger change. The original title and framing ("Read-Only Host Mounts") assumed enforcement; this refinement reframes it as path registration plus prompt-context surfacing. Confirm that the reduced (advisory) guarantee is acceptable, or defer the feature until a confinement boundary exists.
- **Title and "mounts" vocabulary.** The doc was renamed from "Per-Task Read-Only Host Mounts" to "Per-Task Host Path References" and the body now says "references" rather than "mounts", while the wire field, request key, and `HostMount` struct stay named `host_mounts` / `HostMount` for backward compatibility with any existing drafts. Confirm whether to keep the user-facing "references" wording or revert to "mounts" for naming consistency with the API field.
