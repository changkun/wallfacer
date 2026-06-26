---
title: Task Prompt File and Image Attachments
status: stale
depends_on: []
affects:
  - internal/store/models.go
  - internal/store/io.go
  - internal/store/store.go
  - internal/handler/tasks_events.go
  - internal/apicontract/routes.go
  - internal/cli/server.go
  - internal/runner/execute.go
  - internal/runner/worktree.go
  - internal/handler/execute.go
  - frontend/src/components/TaskComposer.vue
  - frontend/src/components/TaskDetail.vue
effort: medium
created: 2026-06-14
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# Task Prompt File and Image Attachments

Supersedes the archived [[file-attachments]] (specs/local/file-attachments.md), which was written against the deleted vanilla-JS frontend (`ui/index.html`, `ui/js/tasks.js`) and the per-task container model (`buildContainerArgs()` with `-v` mounts). Both are gone. This spec retargets the same feature at the Vue frontend (`frontend/src/`) and host-mode worktree staging (`internal/executor/`, `internal/runner/`).

## Problem

The task composer (`frontend/src/components/TaskComposer.vue`) accepts plain Markdown text only. There is no way to attach screenshots, diagrams, reference PDFs, or small data files for the agent to use as context. Users describe visual artefacts in prose (lossy) or paste text inline. The feedback composer on waiting tasks (`internal/handler/execute.go` `SubmitFeedback`) has the same gap.

## Goal

Let users drag-and-drop or click-select files and images onto the task composer and the feedback form. Files are staged with the task, copied into the agent's worktree under `.attachments/`, and listed in the first-turn prompt so Claude Code's `Read` tool (native PNG/JPG/GIF/WebP and PDF support) can inspect them. No container, no `-v` mount, no base64, no CLI flag changes: the agent runs with its worktree as CWD and reads the files in place.

## Current State

- **Composer**: `frontend/src/components/TaskComposer.vue` holds the prompt textarea, flow/tags/timeout options, mentions, batch and schedule toggles, and `submit()` which calls `store.createTask` / `store.batchCreateTasks`. No file input.
- **Task detail**: `frontend/src/components/TaskDetail.vue` renders a task; no attachment surface.
- **Task model**: `internal/store/models.go`, `Task` struct (line 262). Every optional field uses `omitempty`, so adding a slice field needs no migration.
- **Per-task blob storage**: the store backend already persists named per-task byte payloads. `Store.ReadBlob(taskID, key)` and `Store.ListBlobs(taskID, prefix)` (`internal/store/store.go`) read them; `backend.SaveBlob(taskID, key, data)` writes them (used by `internal/store/io.go` `SaveTurnOutput` for `outputs/turn-NNNN.json`, and by `oversight.json`, `summary.json`, `tombstone.json`). Keys are slash-namespaced paths under the task data dir.
- **Blob serving precedent**: `internal/handler/tasks_events.go` `ServeOutput` serves `outputs/<filename>` via `ReadBlob`, with a `strings.Contains(filename, "/"|"..")` traversal guard. Wired in `internal/cli/server.go` (the `ServeOutput` closure pulls `{id}` and `{filename}` path values). Route declared in `internal/apicontract/routes.go` as `GET /api/tasks/{id}/outputs/{filename}`.
- **Worktree staging**: `internal/runner/worktree.go` `ensureTaskWorktrees` / `setupWorktrees` build `worktreePaths` (host repoPath -> worktree path) under `r.worktreesDir/<taskID>/<basename>`, called from `internal/runner/execute.go` (around line 381). `internal/runner/container.go` `buildHostSpec` picks the agent CWD: the first workspace's worktree path. The prompt reaches the agent via `buildAgentCmd` (`-p <prompt>`) from `Runner.Run` (`execute.go` line 141).

## Design

### Data model

`internal/store/models.go`, new struct and one `Task` field:

```go
// Attachment is a small file staged with a task and copied into the agent
// worktree under .attachments/ for the Read tool to inspect.
type Attachment struct {
    Name        string    `json:"name"`         // sanitized basename, unique within the task
    Size        int64     `json:"size"`         // bytes
    ContentType string    `json:"content_type"` // sniffed MIME type
    UploadedAt  time.Time `json:"uploaded_at"`
}
```

```go
Attachments []Attachment `json:"attachments,omitempty"`
```

`omitempty` keeps existing `task.json` records loading cleanly (nil slice).

### Storage (store layer)

Attachments are blobs keyed `attachments/<name>`, reusing the existing backend. Add Store methods (in `internal/store/io.go` or a new `attachments.go`):

- `SaveAttachment(taskID, name, contentType string, data []byte) (*Attachment, error)`: sanitize `name` (basename only, reject `/` and `..`, normalize to a safe charset, dedupe with `-2`, `-3` suffixes against existing entries), enforce limits (10 MB per file, 100 MB total, 20 files per task), `SaveBlob(taskID, "attachments/"+name, data)`, append the `Attachment` entry to `task.Attachments`, and persist under the task write lock.
- `DeleteAttachment(taskID, name string) error`: remove the blob and the slice entry (backlog/waiting only, enforced by the handler).
- Listing reuses `ListBlobs(taskID, "attachments/")` / metadata reads `task.Attachments`.

### HTTP API

Three routes in `internal/apicontract/routes.go`, mirroring the `ServeOutput` shape, wired in `internal/cli/server.go`:

- `POST /api/tasks/{id}/attachments` (`UploadAttachment`): `multipart/form-data`, field `file`, one file per request (the frontend loops). Allowed only when the task is `backlog` or `waiting`. Validate content type / extension against an allowlist (images PNG/JPEG/GIF/WebP, PDF, common text and code types), sniff MIME with `http.DetectContentType`, enforce the size cap, call `SaveAttachment`, return `201` with the `Attachment` JSON.
- `GET /api/tasks/{id}/attachments/{filename}` (`ServeAttachment`): same traversal guard as `ServeOutput`, `ReadBlob(id, "attachments/"+filename)`, `Content-Disposition: inline` so images preview in the detail view.
- `DELETE /api/tasks/{id}/attachments/{filename}` (`DeleteAttachment`): `backlog` only, `204`.

Handlers live alongside `ServeOutput` in `internal/handler/tasks_events.go` (or a new `attachments.go`).

### Worktree staging and prompt augmentation

When a task has attachments, stage them into the agent's worktree so the `Read` tool can reach them at a stable relative path.

- In `internal/runner/execute.go` `Run`, after `ensureTaskWorktrees`/`setupWorktrees` returns `worktreePaths`, if `task.Attachments` is non-empty, write each blob into `<primaryWorktree>/.attachments/<name>` (the primary worktree is the CWD chosen by `buildHostSpec`). Add the dir to `.git/info/exclude` (or rely on it being ignored) so staged attachments never pollute the task's diff/commit. A small helper in `internal/runner/worktree.go`, e.g. `stageAttachments(worktreeDir string, atts []store.Attachment, read func(name string) ([]byte, error)) error`, owns the copy.
- On turn 1 only, append a stanza to the prompt before `buildAgentCmd`:

```
---
## Attached Files

These files are available in the working directory under .attachments/:

| File | Type | Size |
|------|------|------|
| screenshot.png | image/png | 45.2 KB |
| spec.pdf | application/pdf | 220 KB |

Use the Read tool on .attachments/<name> to inspect any file relevant to the task.
---
```

Subsequent auto-continue turns send an empty prompt and are unchanged. Feedback turns re-emit the full list (simplest correct behavior; refine later only if redundancy matters).

### Frontend (Vue)

- **TaskComposer.vue**: add a drop zone plus a hidden `<input type="file" multiple>` below the prompt textarea, and a `pendingFiles` ref rendered as removable chips (filename, size, image thumbnail via object URL). Client-side validate extension and size before staging. `submit()` creates the task, then loops `pendingFiles` issuing one `multipart/form-data` `fetch` per file to `POST /api/tasks/{id}/attachments` (the JSON `api` client cannot send multipart), then clears state. `collapse()` clears `pendingFiles`.
- **TaskDetail.vue**: when `task.attachments` is non-empty, render a section listing each attachment with a thumbnail/icon, size, and a link to `GET /api/tasks/{id}/attachments/{filename}`. Backlog tasks get a per-file delete control calling `DELETE` and refreshing.
- **Feedback composer**: the waiting-task feedback form gets the same drop zone; on submit, upload files first, then post the feedback message.

## Phasing and Acceptance Criteria

1. **Store**: `Attachment` struct and `Task.Attachments` field; `SaveAttachment` / `DeleteAttachment`; unit tests cover sanitize, dedupe, and the size/count caps.
2. **API**: upload / serve / delete handlers and routes; tests assert `backlog`/`waiting` gating, allowlist rejection, traversal-guard 400, and round-trip serve.
3. **Runner**: worktree staging plus first-turn prompt stanza; tests assert files land at `<worktree>/.attachments/<name>`, the stanza appears only on turn 1, and attachments are excluded from the task diff.
4. **Frontend**: composer drop zone, chips, sequential upload on create; detail-view listing and backlog delete; feedback-form drop zone.

Each phase is a self-contained commit with tests. A bug fix (per CLAUDE.md) ships with a regression test that fails without the fix.

## Non-Goals

- No `claude` / `codex` CLI flag, stdin-pipe, or base64 changes: staging plus the `Read` tool is the entire mechanism.
- No large-file or dataset support: that is the in-place host-path approach in [[host-mounts]] (`specs/local/host-mounts.md`), a separate feature. This spec is small files copied into the worktree.
- No read-write attachment surface and no agent-authored attachments: attachments are immutable once uploaded.
- No attachment edits on `in_progress` / `done` / archived tasks beyond read-only display.
- No cross-task or workspace-level shared attachment library.
