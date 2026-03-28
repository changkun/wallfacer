# Plan: File & Image Drag-and-Drop Attachments for Task Prompts

**Status:** Draft
**Date:** 2026-02-21

---

## Problem Statement

The task prompt input (`#new-prompt` textarea) accepts only plain markdown text today. There is no way to attach screenshots, diagrams, reference documents, or data files that Claude Code should use as context when executing in the sandbox. Users currently have to describe the content in words or copy-paste text into the prompt, which is lossy for visual artefacts.

---

## Goal

Allow users to drag-and-drop (or click-to-select) files and images onto the task creation form and the feedback form. Those files get stored alongside the task data, mounted read-only into the sandbox container, and referenced in the prompt so Claude Code can inspect them via its built-in `Read` tool.

---

## Claude Code Headless Capability Research

### What the CLI supports

The `claude` CLI (as invoked by wallfacer via `os/exec`) has no `--file`, `--image`, or `--attachment` flags in headless (`-p`) mode. The full flag list is documented at `code.claude.com/docs/en/cli-reference`; none apply to binary/file injection.

The only documented file-context mechanisms in headless mode are:

| Mechanism | Headless? | Notes |
|---|---|---|
| `cat file \| claude -p "..."` | Yes | Text content via stdin only |
| File path in prompt text | Undocumented in `-p` mode | May trigger Read tool |
| `@filename` in prompt | Interactive only (confirmed) | May work in `-p`, not documented |
| `--image` / `--file` flags | No | Do not exist |
| Base64 via CLI flag | No | No such flag |
| Agent SDK (Python/TS) | Yes | Native image content blocks |
| Clipboard/drag-and-drop | No | Interactive terminal only |

### The viable path for wallfacer

Claude Code's built-in **`Read` tool** explicitly supports images (PNG, JPG, GIF, WebP) and PDFs — the tool definition says *"This tool allows Claude Code to read images."* When the model encounters a file path in the prompt or context and the file exists inside the container, it invokes `Read` and receives the image visually.

**Strategy: mount-and-reference**

1. Save uploaded files to `data/<uuid>/attachments/` on the host.
2. Mount that directory read-only into the container as `/workspace/.attachments/`.
3. On turn 1, append a stanza to the prompt listing the attached files and their paths, instructing Claude to read them as needed.

No changes to the `claude` CLI invocation beyond one extra `-v` mount. No base64 encoding. No stdin piping. This leverages existing container isolation and the Read tool's native image handling.

**Image constraints** (from Anthropic documentation):
- Supported formats: JPEG, PNG, GIF, WebP
- Max file size: 5 MB per image
- Max resolution: 8000 × 8000 px
- PDFs: supported via Read tool (up to 20 pages per call)

---

## Architecture

### Data storage

Each task already has a per-task directory:

```
~/.wallfacer/data/<uuid>/
├── task.json
├── traces/
└── outputs/
```

Add:

```
~/.wallfacer/data/<uuid>/
├── task.json           ← gains Attachments []Attachment field
├── traces/
├── outputs/
└── attachments/        ← NEW: uploaded files stored here
    ├── screenshot.png
    └── requirements.txt
```

Attachments persist with the task for the full lifecycle (including retry and archive). They are never modified after upload — immutable once written.

### Data model changes

**`internal/store/models.go`** — add to the existing models:

```go
type Attachment struct {
    Name        string    `json:"name"`          // sanitized original filename
    Size        int64     `json:"size"`           // bytes
    ContentType string    `json:"content_type"`   // MIME type
    UploadedAt  time.Time `json:"uploaded_at"`
}
```

**`Task` struct** — add one field:

```go
Attachments []Attachment `json:"attachments,omitempty"`
```

No schema migration needed: `omitempty` means existing `task.json` files without the field deserialise cleanly with a nil slice.

### Container mount

**`internal/runner/container.go`** — in `buildContainerArgs()`, after the existing workspace mounts:

```go
attachmentsDir := filepath.Join(r.store.DataDir(), taskID.String(), "attachments")
if entries, err := os.ReadDir(attachmentsDir); err == nil && len(entries) > 0 {
    args = append(args, "-v", attachmentsDir+":/workspace/.attachments/:z,ro")
}
```

The directory is only mounted if it exists and contains files; tasks without attachments are unaffected.

### Prompt augmentation

**`internal/runner/execute.go`** — in `Run()`, on turn 1 only, when `task.Attachments` is non-empty, wrap the user prompt:

```
<user prompt text>

---
## Attached Files

The following files are available at `/workspace/.attachments/`:

| File | Type | Size |
|------|------|------|
| screenshot.png | image/png | 45.2 KB |
| requirements.txt | text/plain | 1.2 KB |

Use the `Read` tool to examine any file that is relevant to the task above.
---
```

This is appended before passing to `runContainer`. On subsequent auto-continue turns (`max_tokens`/`pause_turn`), the prompt is already empty and unchanged.

---

## Implementation Phases

### Phase 1 — Backend: store layer

**Files:** `internal/store/models.go`, `internal/store/tasks.go` (or new `internal/store/attachments.go`)

New store methods:

```go
// SaveAttachment writes bytes to data/<uuid>/attachments/<name>, appends to task.Attachments.
func (s *Store) SaveAttachment(ctx context.Context, taskID uuid.UUID, name, contentType string, data []byte) (*Attachment, error)

// DeleteAttachment removes a file from the attachments dir and task.Attachments slice.
func (s *Store) DeleteAttachment(ctx context.Context, taskID uuid.UUID, name string) error

// AttachmentPath returns the absolute host path for an attachment (for serving).
func (s *Store) AttachmentPath(taskID uuid.UUID, name string) string
```

`SaveAttachment` must:
1. Sanitize the filename (strip directory components, reject traversal, normalize to safe chars, deduplicate if name already exists via suffix `-2`, `-3`, etc.).
2. Create `attachments/` dir if missing.
3. Write file bytes atomically (temp + rename).
4. Append an `Attachment` entry to `task.Attachments` and re-save `task.json`.
5. Enforce per-task limits (proposed: 20 files max, 10 MB per file, 100 MB total per task).
6. Acquire the store write lock for the task.json update.

`DataDir()` accessor also needed on `Store` so runner can read the path without importing store internals.

---

### Phase 2 — Backend: HTTP handlers

**Files:** `internal/handler/tasks.go` (or new `internal/handler/attachments.go`)

Three new handlers:

#### `POST /api/tasks/{id}/attachments`

- Accept `multipart/form-data` with field `file` (one file per request; frontend calls multiple times for multiple files).
- Validate task is in `backlog` or `waiting` status (attachments on feedback).
- Validate MIME type against allowlist (see below).
- Validate file size ≤ 10 MB.
- Call `store.SaveAttachment()`.
- Return `201 Created` with the `Attachment` JSON.

Allowlisted MIME types:
```
image/png, image/jpeg, image/gif, image/webp
application/pdf
text/plain, text/markdown, text/csv, text/html, text/xml
application/json, application/yaml, application/toml
application/octet-stream (code files by extension)
```
Additionally allowlist by extension for code files (`.go`, `.py`, `.js`, `.ts`, `.rs`, `.java`, `.c`, `.cpp`, `.h`, `.sh`, `.sql`, `.tf`, etc.) regardless of MIME type reported by the browser.

#### `GET /api/tasks/{id}/attachments/{filename}`

- Validate `filename` does not contain `/` or `..`.
- Serve the file from `data/<uuid>/attachments/<filename>` using `http.ServeFile`.
- Set `Content-Disposition: inline` so images preview in the browser.

#### `DELETE /api/tasks/{id}/attachments/{filename}`

- Only allowed when task is `backlog`.
- Call `store.DeleteAttachment()`.
- Return `204 No Content`.

#### Route registration

**`main.go`** — add three routes in the existing routing block:

```go
mux.HandleFunc("POST /api/tasks/{id}/attachments",            wrap(h.UploadAttachment))
mux.HandleFunc("GET /api/tasks/{id}/attachments/{filename}",  wrap(h.ServeAttachment))
mux.HandleFunc("DELETE /api/tasks/{id}/attachments/{filename}", wrap(h.DeleteAttachment))
```

---

### Phase 3 — Backend: runner integration

**Files:** `internal/runner/container.go`, `internal/runner/execute.go`

`container.go` — `buildContainerArgs()`:
- After existing workspace mounts, conditionally add the attachments volume (see Architecture section).

`execute.go` — `Run()`:
- After loading the task, if `task.Attachments` is non-empty and `turns == 0` (first invocation), call a helper `augmentPromptWithAttachments(prompt, task.Attachments) string`.
- The helper produces the markdown table stanza and appends it to the prompt.
- Pass the augmented prompt to `runContainer` for turn 1 only.
- For feedback turns (resumed from waiting): if new attachments were added since the last turn (tracked by comparing counts or a `feedback_attachments` field), augment the feedback message similarly.

---

### Phase 4 — Frontend: task creation form

**Files:** `ui/index.html`, `ui/js/tasks.js`

#### HTML changes (`index.html`)

Replace the current form block (around line 167):

```html
<div id="new-task-form" class="hidden mb-2">
  <textarea id="new-prompt" rows="6" …></textarea>

  <!-- NEW: attachment drop zone -->
  <div id="new-drop-zone" class="drop-zone mt-2">
    <span class="drop-zone-label">Drop files or images here, or <label for="new-file-input" class="drop-zone-link">browse</label></span>
    <input id="new-file-input" type="file" multiple class="hidden">
  </div>

  <!-- NEW: staged attachment chips -->
  <div id="new-attachments" class="attachment-list hidden"></div>

  <div class="flex items-center justify-between mt-2">
    <div class="flex items-center gap-1">
      <button onclick="createTask()" class="btn btn-accent">Save</button>
      <button onclick="hideNewTaskForm()" class="btn-ghost">Cancel</button>
    </div>
    <select id="new-timeout" …> … </select>
  </div>
</div>
```

Add CSS classes (`ui/css/styles.css`):
- `.drop-zone` — dashed border, rounded, padding, hover highlight, active-drag highlight.
- `.drop-zone-link` — inline clickable label styled as a link.
- `.attachment-chip` — pill with filename, size, optional image thumbnail, remove ×.
- `.attachment-list` — flex-wrap row of chips.

#### JavaScript changes (`tasks.js`)

New module-level state:
```javascript
let pendingAttachments = [];  // Array of File objects staged before task creation
```

New functions:

```javascript
// Wire up drag-and-drop and file input events for the new-task form.
function initAttachmentZone(dropZoneId, fileInputId, listId) { … }

// Add a File to pendingAttachments; render a chip in the list.
function stageFile(file) { … }

// Remove a staged file by index; re-render chip list.
function unstageFile(index) { … }

// Render the chip list from pendingAttachments.
function renderAttachmentChips(listId) { … }

// Upload all pendingAttachments to /api/tasks/{id}/attachments sequentially.
async function uploadPendingAttachments(taskId) { … }
```

Modified `createTask()`:

```javascript
async function createTask() {
  const prompt = document.getElementById('new-prompt').value.trim();
  if (!prompt) { /* existing validation */ return; }

  const timeout = parseInt(document.getElementById('new-timeout').value, 10) || DEFAULT_TASK_TIMEOUT;
  const task = await api('/api/tasks', { method: 'POST', body: JSON.stringify({ prompt, timeout }) });

  // Upload any staged attachments
  if (pendingAttachments.length > 0) {
    await uploadPendingAttachments(task.id);
    pendingAttachments = [];
  }

  hideNewTaskForm();
  fetchTasks();
}
```

Modified `hideNewTaskForm()`: clear `pendingAttachments` and reset chip list.

The upload helper sends one `multipart/form-data` POST per file (sequential to avoid race conditions on the store lock):

```javascript
async function uploadPendingAttachments(taskId) {
  for (const file of pendingAttachments) {
    const fd = new FormData();
    fd.append('file', file);
    await fetch(`/api/tasks/${taskId}/attachments`, { method: 'POST', body: fd });
    // api() cannot be used here because it forces Content-Type: application/json
  }
}
```

Note: `fetch` with `FormData` sets `Content-Type: multipart/form-data` with the correct boundary automatically. The existing `api()` helper forces `application/json` and cannot be used for uploads; call `fetch` directly.

#### Validation in the browser

Before staging a file, validate:
- Size ≤ 10 MB (soft client-side check; server enforces the hard limit).
- Extension is in the allowlist (show an alert and reject if not).
- Total staged count ≤ 20.

---

### Phase 5 — Frontend: task modal (display)

**Files:** `ui/js/modal.js` or `ui/js/render.js`

When the task modal is open, if `task.attachments` is non-empty, render a section in the modal:

```
## Attached Files
[thumbnail/icon] screenshot.png  45.2 KB  ↗ (link to GET /api/tasks/{id}/attachments/screenshot.png)
[icon]           requirements.txt  1.2 KB  ↗
```

Images should show a small thumbnail (served from the attachment endpoint). Other file types show a generic file icon.

This section is read-only in the modal for non-backlog tasks. For backlog tasks, show a delete `×` button per attachment that calls `DELETE /api/tasks/{id}/attachments/{filename}` and refreshes the task.

---

### Phase 6 — Frontend: feedback form (waiting tasks)

**Files:** `ui/index.html` (feedback modal section), `ui/js/tasks.js` (`submitFeedback()`)

The feedback modal (`#modal-feedback` textarea) needs the same drop-zone treatment:
- A drop-zone below the feedback textarea.
- Staged attachments shown as chips.
- `submitFeedback()` first uploads files, then posts the feedback message.
- Uploaded files go to `POST /api/tasks/{id}/attachments` with the same endpoint.
- The runner already re-augments the prompt on feedback turns if `task.Attachments` grew.

---

## Open Questions (resolve before implementing)

1. **Retry behaviour**: When a task is retried (reset to backlog), should existing `attachments/` carry over or be cleared? Carrying over is safe since it's the same task directory — unless the retry prompt has nothing to do with the original attachments. Recommend: **keep attachments on retry**, let the user delete individual ones via the modal.

2. **Feedback-turn attachment augmentation**: The runner needs to know which attachments are "new since the last turn" to avoid repeating the full list on every feedback turn. Options:
   - Track a `last_seen_attachment_count` in the task struct.
   - Maintain a separate `FeedbackAttachments` slice with a `since_turn` field.
   - Simply re-emit the full list every time (safest, mildly redundant).
   Recommend starting with **re-emit full list** and refine later.

3. **File size limits**: Proposed defaults — 10 MB per file, 100 MB total per task, 20 files max. Are these suitable for the target use case?

4. **`@filename` syntax in headless mode**: The Claude Code docs document `@filename` for interactive mode only. It is worth testing `claude -p "see @/workspace/.attachments/screen.png"` to see if it resolves in headless mode. If it does, we could use `@path` syntax instead of free-text prose in the prompt stanza, which may trigger more reliable image loading. This test should be done before implementing Phase 3.

5. **Multiple workspaces and attachment path**: The container mounts workspaces as `/workspace/<basename>`. The attachment path `/workspace/.attachments/` starts with `.` to avoid colliding with any workspace basename. Confirm this is safe given the `-w /workspace` working directory setting.

---

## Files Touched Summary

| File | Change type | Phase |
|------|-------------|-------|
| `internal/store/models.go` | Add `Attachment` struct, `Attachments` field on `Task` | 1 |
| `internal/store/tasks.go` or new `attachments.go` | `SaveAttachment`, `DeleteAttachment`, `AttachmentPath` | 1 |
| `internal/store/store.go` | `DataDir()` accessor | 1 |
| `internal/handler/tasks.go` or new `attachments.go` | `UploadAttachment`, `ServeAttachment`, `DeleteAttachment` | 2 |
| `main.go` | 3 new routes | 2 |
| `internal/runner/container.go` | Conditional attachments volume mount | 3 |
| `internal/runner/execute.go` | Prompt augmentation helper, call on turn 1 | 3 |
| `ui/index.html` | Drop-zone + chip list in new-task form and feedback modal | 4, 6 |
| `ui/css/styles.css` | `.drop-zone`, `.attachment-chip`, `.attachment-list` | 4 |
| `ui/js/tasks.js` | `stageFile`, `unstageFile`, `renderAttachmentChips`, `uploadPendingAttachments`, modify `createTask`, `hideNewTaskForm`, `submitFeedback` | 4, 6 |
| `ui/js/modal.js` or `render.js` | Attachment section in task detail modal | 5 |

No changes to the sandbox `Dockerfile`, `go.mod`, or any git/instructions logic.

---

## What This Does NOT Require

- Changes to the `claude` CLI binary or invocation flags.
- Base64 encoding of files into the prompt.
- MCP servers, external storage, or cloud services.
- Changes to the git worktree isolation system.
- New dependencies (Go stdlib `mime/multipart`, `net/http` already handle uploads).
