---
title: File Explorer Panel
status: complete
depends_on: []
affects:
  - internal/handler/explorer.go
  - ui/js/explorer.js
  - ui/css/explorer.css
effort: xlarge
created: 2026-03-22
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Plan: File Explorer Panel

---

## Problem Statement

Wallfacer has no way to browse or edit workspace files from the web UI. The only file-related features are a flat list for @mention autocomplete (`GET /api/files`), a directory-only browser for the workspace picker (`GET /api/workspaces/browse`), and git diff viewing for task worktrees (`GET /api/tasks/{id}/diff`). To inspect or modify workspace files, users must switch to a separate editor or terminal, breaking the task-board workflow.

---

## Goal

Add a VS Code-style file explorer panel to the board UI that lets users browse workspace directory trees, preview file contents with syntax highlighting, and make quick edits — all without leaving the Wallfacer interface.

---

## Current State

### Existing File Infrastructure

| Endpoint | Purpose | Limitation |
|----------|---------|------------|
| `GET /api/files` | Flat list of up to 8000 files for @mention | No hierarchy, no content |
| `GET /api/workspaces/browse` | List child directories (one level) | Directories only, no files |
| `GET /api/tasks/{id}/diff` | Git diff for task worktrees | Diffs only, not raw content |
| `GET /api/tasks/{id}/outputs/{filename}` | Claude output per turn | Task outputs only |

**No general-purpose file content endpoint exists.** There is no way to read or write arbitrary workspace file contents via the API.

### Existing Reusable Code

| Component | Location | Reuse |
|-----------|----------|-------|
| `extToLang()` | `ui/js/modal-diff.js` | Syntax highlighting language mapping (40+ extensions) |
| `splitHighlightedLines()` | `ui/js/modal-diff.js` | Line-numbered code display |
| `isAllowedWorkspace()` | `internal/handler/git.go:634` | Workspace boundary validation |
| `skipDirs` | `internal/handler/files.go` | Default collapsed/hidden directories in tree |
| `_initPanelResize()` pattern | `ui/js/status-bar.js:128` | Drag-to-resize panel handle |
| `BrowseWorkspaces` handler | `internal/handler/workspace.go` | Same request/response shape, extend to include files |
| `writeJSON()` / `decodeJSONBody()` | `internal/handler/` helpers | Standard response/request handling |
| CSS custom properties | `ui/css/base.css` | `--bg`, `--border`, `--text-primary`, etc. for theming |

---

## Design

### Integration: Collapsible Left Side Panel

The file explorer is a collapsible panel on the left side of the board, between the header and status bar. This mirrors VS Code's explorer placement.

```
+--header----------------------------------------------+
| [Explorer toggle] ... [other buttons]                |
+------+-----------------------------------------------+
|      |                                               |
| File |  board-grid (4 kanban columns)                |
| Tree |                                               |
|      |                                               |
+------+-----------------------------------------------+
+--status-bar------------------------------------------+
```

- **Resizable** via a vertical drag handle (same pattern as the terminal panel's horizontal resize in `status-bar.js`)
- **Panel width** persisted to `localStorage`
- **Toggle button** in the header (file tree icon), keyboard shortcut `Ctrl+E` / `Cmd+E`
- **File click** opens a preview/edit modal overlay (reusing the existing modal pattern), not inside the side panel itself — keeps the panel narrow and focused on navigation

### User Experience

#### Opening the Explorer

The explorer panel is hidden by default. Users open it via:

1. **Header button** — a file-tree icon in the header bar, placed before the existing settings/docs buttons. Clicking toggles the panel open/closed.
2. **Keyboard shortcut** — `Ctrl+E` (Linux/Windows) or `Cmd+E` (macOS). Same toggle behavior.

When the panel opens, it slides in from the left, pushing the board grid narrower. The board columns reflow to fit the remaining width. When closed, the board reclaims the full width. The transition should feel instant (no animation delay).

#### Browsing the Tree

Each active workspace appears as a root node in the tree, labeled by its directory basename (e.g., `wallfacer` for `/home/user/dev/wallfacer`). On first open, the panel fetches the top-level listing for each workspace root.

- **Expand a directory**: Click the directory name or press Enter when focused. A loading indicator appears briefly while children are fetched. Children render indented below the parent with a disclosure triangle (▶ → ▼).
- **Collapse a directory**: Click again or press Enter. Children are removed from the DOM (state discarded to keep memory lean).
- **Navigate with keyboard**: Arrow keys move focus up/down through visible nodes. Right arrow expands a collapsed directory; left arrow collapses an expanded one or moves focus to the parent.
- **Sorting**: Directories appear first, then files, each group sorted alphabetically (case-insensitive). Hidden entries (`.`-prefixed like `.github`, `.env`) are included but rendered with dimmed text so they don't dominate the tree.
- **Lazy loading**: Only one level is fetched at a time. Deep directory trees don't cause upfront load — users drill down as needed.

#### Previewing a File

Clicking a file name (or pressing Enter on a focused file node) opens a **modal overlay** centered on the screen:

- **Title bar**: Full file path relative to the workspace root (e.g., `src/internal/handler/config.go`)
- **Content area**: Syntax-highlighted code with line numbers. Highlighting uses the existing `extToLang()` mapping (40+ extensions) and highlight.js. For unknown extensions, content renders as plain text.
- **Binary files**: If the file is detected as binary (null bytes in first 8 KB), the modal shows a placeholder: "Binary file (12.4 KB)" instead of content.
- **Large files**: Files over 2 MB return a 413 error; the modal shows: "File too large to preview (3.2 MB, max 2 MB)."
- **Close**: Click the backdrop, press Escape, or click the X button.

The modal pattern matches existing modals in the UI (task detail, diff viewer, oversight) — same backdrop, same close behavior, same z-index layering.

#### Editing a File (Phase 2)

In the file preview modal, an "Edit" button in the title bar switches to edit mode:

- The highlighted `<pre>` block is replaced by a monospace `<textarea>` with the raw file content.
- Tab key inserts a tab character (not focus-shift).
- "Save" button writes via `PUT /api/explorer/file` and returns to preview mode with updated content.
- "Discard" button reverts to preview mode without saving.
- If the user closes the modal with unsaved changes, a confirmation prompt appears: "You have unsaved changes. Discard?"
- Save errors (permission denied, disk full) display inline in the modal, not as a separate alert.

#### Resize Behavior

The vertical drag handle on the panel's right edge follows the same interaction pattern as the terminal panel's horizontal resize:

- Drag to resize between a minimum (200px) and maximum (50% of viewport width).
- Width is persisted to `localStorage` and restored on next visit.
- The board grid adjusts in real time during drag (no snap-on-release).
- Double-clicking the handle resets to the default width (260px).

#### Relationship to Terminal Panel

The explorer (left side panel) and terminal (bottom panel) are independent and can be open simultaneously:

```
+--header----------------------------------------------+
| [Explorer] ... [other buttons]                       |
+------+-----------------------------------------------+
|      |                                               |
| File |  board-grid (4 kanban columns)                |
| Tree |                                               |
|      |                                               |
+------+-----------------------------------------------+
|      | terminal panel                                |
+------+-----------------------------------------------+
+--status-bar------------------------------------------+
```

The terminal panel spans the full width below the board (including under the explorer panel). When both are open, the board grid occupies the remaining rectangle. This avoids complex corner-case layout logic — each panel owns one axis (explorer = horizontal, terminal = vertical).

### Backend: Three New API Endpoints

#### 1. `GET /api/explorer/tree` — Directory Listing

Lists one level of a directory within a configured workspace, returning both files and directories.

**Query parameters:**
- `path` (required) — absolute path to list
- `workspace` (required) — the workspace root this path belongs to (for validation)

**Response:**
```json
{
  "path": "/home/user/project/src",
  "entries": [
    {"name": "handler", "type": "dir", "modified": "2026-03-22T10:00:00Z"},
    {"name": "main.go", "type": "file", "size": 2048, "modified": "2026-03-22T10:00:00Z"}
  ]
}
```

Entries sorted: directories first, then files, alphabetically case-insensitive within each group. Hidden entries (starting with `.`) are included but marked so the frontend can style or filter them.

**Handler:** New file `internal/handler/explorer.go`, following the same pattern as `BrowseWorkspaces` in `workspace.go`.

#### 2. `GET /api/explorer/file` — File Content Reading

Returns the contents of a single file within a configured workspace.

**Query parameters:**
- `path` (required) — absolute path to the file
- `workspace` (required) — the workspace root

**Response:**
- Text files: `Content-Type: text/plain; charset=utf-8`, raw file content in body
- Binary files: `Content-Type: application/json` with `{"binary": true, "size": <n>}` and header `X-File-Binary: true`
- Always sets `X-File-Size: <bytes>` header

**Limits:**
- Maximum file size: **2 MB**. Files exceeding this return 413 with `{"error": "file too large", "size": <n>, "max": 2097152}`
- Binary detection: read first 8192 bytes, check for null bytes (same heuristic as git)

#### 3. `PUT /api/explorer/file` — File Content Writing

Writes content to a file within a configured workspace.

**Request body:**
```json
{
  "path": "/home/user/project/src/main.go",
  "workspace": "/home/user/project",
  "content": "package main\n\nfunc main() {\n}\n"
}
```

**Response:** `{"status": "ok", "size": <bytes_written>}`

**Constraints:**
- Maximum write size: 2 MB
- Refuse writes to paths inside `.git/` directories
- Atomic write: temp file + rename (same pattern as `internal/store/`)

### Security

All three endpoints share a common path validation function:

```go
func isWithinWorkspace(requestedPath, workspace string) (string, error)
```

1. Resolve symlinks on both `requestedPath` and `workspace` via `filepath.EvalSymlinks()`
2. Clean both paths via `filepath.Clean()`
3. Verify `requestedPath == workspace` or `strings.HasPrefix(requestedPath, workspace + separator)`
4. `workspace` must pass `isAllowedWorkspace()` check against the active workspace set

The `PUT` endpoint additionally:
- Requires bearer auth (already enforced by middleware for non-GET)
- Has CSRF protection (already applied by `CSRFMiddleware`)
- Rejects paths containing `/.git/` or ending with `/.git`

### Frontend Components

#### New Files

- **`ui/js/explorer.js`** — Tree component, file preview, panel management (~300-400 lines)
- **`ui/css/explorer.css`** — Side panel, tree nodes, resize handle styles
- **`ui/partials/explorer-panel.html`** — HTML partial for the side panel structure

#### Tree Component State

```javascript
let _explorerRoots = [];    // one root per active workspace
let _explorerOpen = false;  // panel visibility
let _explorerWidth = 260;   // persisted to localStorage
```

Each node: `{ path, name, type, expanded, children, loading }`

**Lazy loading:** On first open, fetch top-level listing for each active workspace. Expanding a directory node fetches its children via `GET /api/explorer/tree`. Collapsed nodes discard their children to keep state lean.

#### File Preview Modal

1. Fetch content via `GET /api/explorer/file`
2. Open modal with file path as title
3. Render with highlight.js via `extToLang()` from `modal-diff.js`
4. Display line numbers via `splitHighlightedLines()` from `modal-diff.js`
5. Phase 2: "Edit" button switches `<pre>` to `<textarea>` with "Save" / "Discard" actions

#### Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| `Ctrl/Cmd+E` | Toggle explorer panel |
| Arrow keys | Navigate tree (when explorer focused) |
| `Enter` | Expand directory or open file |
| `Escape` | Close file preview modal |

---

## Phasing

### Phase 1: Read-Only Browsing + Preview

**Backend:**
- `internal/handler/explorer.go`: `ExplorerTree` and `ExplorerReadFile` handlers with `isWithinWorkspace()` validation
- Register two routes in `internal/apicontract/routes.go`

**Frontend:**
- `ui/js/explorer.js`: tree component with lazy loading + file preview modal
- `ui/css/explorer.css`: panel and tree styles
- `ui/partials/explorer-panel.html`: panel HTML
- Update `ui/partials/initial-layout.html`: add explorer toggle button to header
- Update `ui/partials/scripts.html`: add script tag
- Update `ui/css/styles.css`: import explorer.css

**Tests:**
- `internal/handler/explorer_test.go`: path validation, workspace boundary enforcement, binary detection, size limits, symlink traversal prevention
- `ui/js/tests/explorer.test.js`: tree state management, node expansion/collapse

**Complexity:** Medium. Backend is straightforward (two handlers with validation). Frontend tree component is the main work.

### Phase 2: File Editing + Saving

**Backend:**
- `ExplorerWriteFile` handler in `explorer.go`
- Atomic write, `.git/` write protection

**Frontend:**
- Edit mode toggle in file preview modal
- `<textarea>` with monospace font, tab-key support
- Save button with loading/error states
- Unsaved changes warning on modal close

**Tests:**
- Write handler tests (path validation, atomic write, `.git/` protection)
- Frontend save flow tests

**Complexity:** Low-Medium.

---

## Outcome

Phase 1 (read-only browsing + preview) and Phase 2 (file editing + saving) are fully implemented across 12 tasks. The explorer provides a VS Code-style side panel with lazy-loading directory trees, syntax-highlighted file preview, rendered markdown view, inline editing with atomic saves, and file-type-specific icons.

### What Shipped

- **3 API endpoints** in `internal/handler/explorer.go`: tree listing, file reading (with binary detection and size limits), and file writing (with `.git/` protection and atomic writes)
- **Explorer panel** (`ui/js/explorer.js`, ~800 lines): toggle, resize, tree component with keyboard navigation, file preview modal with syntax highlighting, markdown rendering with Raw/Preview toggle, edit mode with save/discard and dirty-state warnings, VS Code-style file icons for ~20 file types
- **19 frontend tests** covering pure logic (basename, child node building, visible node traversal, parent finding, file response classification, relative path, dirty detection, file icons, markdown toggle)
- **Backend tests** for path validation, workspace boundary enforcement, binary detection, size limits, `.git/` write rejection, atomic writes

### Design Evolution

1. **Keyboard shortcut changed.** The spec proposed `Ctrl+E` / `Cmd+E` for the panel toggle. This was changed to bare `E` key because `Ctrl+E` conflicts with the browser's address bar focus shortcut. The bare-key pattern matches existing shortcuts (`N` for new task, `?` for help, `` ` `` for terminal).

2. **Flex wrapper placement.** The spec proposed wrapping the board-grid inside `ui/partials/board.html`. Instead, the `<div class="board-with-explorer">` wrapper was placed in `ui/index.html` around both the explorer-panel and board template includes, keeping each partial self-contained.

3. **hljs scoping fix.** Syntax highlighting colors were scoped under `.diff-block-modal .hljs-*` and didn't apply to explorer previews. Fixed by adding `.explorer-preview .hljs-*` selectors to each rule block in `diffs.css`.

4. **Markdown rendered view.** Added rendered markdown as the default view for `.md`/`.mdx` files with a Raw/Preview toggle, reusing the existing `renderMarkdown()` helper and `marked` library.

5. **File icons added.** Not in the original spec — added as Task 11 to provide VS Code-style file/folder icons using inline SVGs with distinct colors per file type.

6. **Explorer tree reload.** The tree didn't populate on first page load because `_initExplorer()` ran before `fetchConfig()` set `activeWorkspaces`. Fixed by calling `reloadExplorerTree()` at the end of `fetchConfig()`.

7. **Dep graph layout regression.** The `board-with-explorer` flex wrapper caused the dependency graph panel to appear beside the board instead of below it. Fixed by inserting the panel after the wrapper element instead of after `#board`.

### Phase 3: Advanced Features (Future)

- **Git status indicators** — decorate tree nodes with modified/staged/untracked from `GET /api/git/status`
- **File search** — filter tree by filename (reuse `fileIndex` data)
- **Task worktree browsing** — extend tree to show task-specific worktree files
- **File create/delete** — context menu actions
- **Multi-tab viewer** — open multiple files in tabs instead of modal

### Phase 4: Cloud Backend File Access (Future)

Phases 1–3 use direct filesystem access (`os.ReadDir`, `os.ReadFile`, `os.WriteFile`) on the server host. This works for:
- **Local backend** — workspaces are on the host filesystem
- **Cloud with shared volume** — if the server pod mounts the same PVC/NFS as sandbox pods, file access works unchanged

It does **not** work when workspaces only exist inside sandbox pods (K8s backend without shared volume). For that scenario, the explorer handlers need a filesystem abstraction:

```go
// WorkspaceFS abstracts file access to workspace directories.
type WorkspaceFS interface {
    ReadDir(path string) ([]fs.DirEntry, error)
    ReadFile(path string) ([]byte, error)
    WriteFile(path string, data []byte) error
    Stat(path string) (fs.FileInfo, error)
}
```

- **Local implementation:** Delegates to `os` package (current behavior)
- **K8s implementation:** Proxies via `kubectl exec` into a sidecar or uses the K8s API to read from PVCs
- **Remote Docker:** Proxies via `docker exec` or `docker cp`

This aligns with the `SandboxBackend` abstraction in [sandbox-backends.md](sandbox-backends.md) — the backend knows where files live. A `SandboxBackend.FileAccess()` method could return a `WorkspaceFS` for the active backend.

**Recommendation:** Defer to Phase 4. Phases 1–3 deliver full value for the local and shared-volume deployments that exist today. The `WorkspaceFS` interface is a clean extension point that doesn't require rearchitecting the handlers — just swap the `os.*` calls for interface calls.

### Relationship to Epic Coordination

The file explorer is a prerequisite for the epic coordination UX ([epic-coordination.md](epic-coordination.md)). The planning workflow revolves around iterating on spec markdown files — browsing specs in the explorer, viewing them in a focused markdown panel, and updating them via a chat-driven agent. Phase 1 (read-only browsing + preview) unblocks the planner task creation UX; Phase 2 (editing) enables chat-driven spec iteration where the planner agent proposes spec changes and the user reviews them inline.

---

## File Inventory

### New Files
| File | Purpose |
|------|---------|
| `internal/handler/explorer.go` | Backend handlers (tree, read, write) |
| `internal/handler/explorer_test.go` | Backend tests |
| `ui/js/explorer.js` | Frontend tree component and file viewer |
| `ui/js/tests/explorer.test.js` | Frontend tests |
| `ui/css/explorer.css` | Explorer panel and tree styles |
| `ui/partials/explorer-panel.html` | HTML partial for side panel |

### Modified Files
| File | Change |
|------|--------|
| `internal/apicontract/routes.go` | Add 3 routes under `explorer` tag |
| `ui/css/styles.css` | `@import "explorer.css"` |
| `ui/partials/initial-layout.html` | Explorer toggle button in header |
| `ui/partials/scripts.html` | `<script>` tag for `explorer.js` |
| `ui/index.html` | Wrap board template in flex container with explorer panel |
| `ui/css/diffs.css` | Extend hljs token colors to `.explorer-preview` scope |
| `docs/guide/board-and-tasks.md` | Document file explorer feature |
| `CLAUDE.md` | Add explorer API routes |
