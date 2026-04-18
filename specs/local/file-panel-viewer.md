---
title: Inline File Panel Viewer
status: drafted
depends_on:
  - specs/foundations/file-explorer.md
affects:
  - ui/js/explorer.js
  - ui/js/events.js
  - ui/index.html
  - ui/css/explorer.css
  - internal/apicontract/routes.go
  - internal/handler/files.go
effort: large
created: 2026-03-28
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Inline File Panel Viewer

---

## Problem Statement

The file explorer previews files in a centered popup modal (`ui/js/explorer.js:389` `_openFilePreview`, backdrop `#explorer-preview-backdrop`). This is disruptive: the modal obscures the board, blocks interaction with tasks, and makes browsing multiple files tedious since each file opens a new modal that must be closed before opening the next. Users familiar with VS Code expect files to open in an inline panel within the main layout, with tabs for switching between open files.

The current preview is also raw and format-blind: non-text content falls through to a single `"Binary file (X KB)"` placeholder (`ui/js/explorer.js:477`). Images, video, audio, and PDFs have no visual preview, and there is no hex fallback for actual binary blobs.

---

## Current State (2026-04-19)

- Modal-only preview lives in `ui/js/explorer.js`:
  - `_openFilePreview(node)` builds `#explorer-preview-backdrop` per open; `closeExplorerPreview()` tears it down.
  - Text: `_renderHighlightedContent(content, filename)` (highlight.js + line numbers).
  - Markdown (`.md`, `.mdx`): rendered view with Raw toggle (`_toggleMarkdownView`).
  - Edit mode: `_enterEditMode` swaps content for a `<textarea>`; `_saveFile`/`_discardEdit` persist via `PUT /api/explorer/file`.
  - Large files: size-limit placeholder.
  - Binary: single placeholder string, no media handling, no hex dump.
- Only one file can be previewed at a time; opening another replaces it.
- Escape-to-close is wired via `ui/js/events.js` (`closeExplorerPreview`).
- Styles live in `ui/css/explorer.css` under the `.explorer-preview__*` namespace.

Nothing from this spec has been implemented yet — all phases are open.

---

## Goal

Replace the modal-based file preview with a VS Code-style inline file panel that opens in the main content area alongside (or replacing) the board grid. Add multi-modal file preview for images, video, audio, and PDFs.

---

## Design

### Layout

When a file is opened, the board grid slides right (or is replaced) to make room for a file panel. The explorer tree remains on the left. The layout transitions from two-column (explorer + board) to three-column (explorer + file panel + board) or two-column (explorer + file panel) depending on viewport width.

```
+--header----------------------------------------------+
| [Explorer] ... [other buttons]                       |
+------+--------------------+--------------------------+
|      |                    |                          |
| File | [tab1] [tab2]      |  board-grid              |
| Tree |                    |  (narrowed or hidden)     |
|      | file content       |                          |
|      | (code / image /    |                          |
|      |  markdown / video) |                          |
+------+--------------------+--------------------------+
+--status-bar------------------------------------------+
```

**Narrow viewports:** When the viewport is too narrow for three columns, the file panel replaces the board entirely. A back button or keyboard shortcut returns to the board view.

### Tab Behavior (VS Code-style)

- **Single-click** a file in the tree: opens in a *preview tab* (italic title, replaced when the next file is single-clicked). This is for quick glancing.
- **Double-click** a file: opens in a *pinned tab* (normal title, persists until explicitly closed). The current preview tab, if any, is promoted to pinned.
- Tabs show the filename. If two open files share the same name, show parent directory as disambiguation (e.g., `handler/config.go` vs `cli/config.go`).
- Close tab via X button or middle-click. Closing the last tab returns to the board-only layout.
- `Ctrl+W` / `Cmd+W` closes the active tab.
- Tab overflow: horizontal scroll with left/right arrows when tabs exceed panel width.

### File Panel Content

The file panel reuses the existing rendering from the file explorer but in a panel instead of a modal:

| File type | Rendering |
|-----------|-----------|
| Text (code) | Reuse `_renderHighlightedContent` from explorer.js |
| Markdown (`.md`, `.mdx`) | Rendered markdown with Raw/Preview toggle (reuse existing path) |
| Images (`.png`, `.jpg`, `.jpeg`, `.gif`, `.svg`, `.webp`, `.ico`, `.avif`) | Inline `<img>` with fit-to-panel scaling, click to zoom / toggle original size |
| Video (`.mp4`, `.webm`, `.mov`, `.ogv`) | HTML5 `<video controls preload="metadata">` |
| Audio (`.mp3`, `.wav`, `.ogg`, `.flac`, `.m4a`) | HTML5 `<audio controls>` |
| PDF (`.pdf`) | `<iframe>` pointing at the file endpoint with `Content-Type: application/pdf` |
| Binary (other) | File size + hex dump of first 256 bytes (16 bytes/row, offset + hex + ASCII) |

Media files are loaded via a direct GET against the file endpoint — the existing `GET /api/explorer/file` currently returns JSON with base64 for binary payloads (see `_classifyFileResponse`). For streaming media we need a sibling raw endpoint (or a `?raw=1` mode) that returns the bytes with the right `Content-Type` so `<img>`, `<video>`, `<audio>`, and `<iframe>` can consume it without base64 round-tripping. This backend change is in scope for Phase 2.

### Edit Mode

Edit mode (from file explorer Phase 2) works the same way in the panel — the Edit/Save/Discard buttons appear in the tab's toolbar area instead of a modal header. Dirty tabs show a dot indicator on the tab title.

### Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| `Ctrl/Cmd+W` | Close active tab |
| `Ctrl/Cmd+Tab` | Next tab |
| `Ctrl/Cmd+Shift+Tab` | Previous tab |
| `Escape` | Return focus to board (when in file panel) |

### Migration from Modal

The modal-based preview (`_openFilePreview`, `closeExplorerPreview`) is replaced entirely. The explorer tree click handler opens files in the panel instead. The Escape-key integration in `events.js` is updated accordingly.

---

## Phasing

### Phase 1: Panel Shell + Tab Management

- Add a `#file-panel` container to `ui/index.html` sitting between the explorer tree and the board grid; gate visibility on "at least one open file".
- Introduce a small open-files store (array of `{path, workspace, pinned, dirty}` + active index) in `ui/js/explorer.js` or a new `ui/js/file-panel.js` module.
- Render a tab bar with preview (italic) vs. pinned semantics; single-click replaces the preview tab, double-click pins. Disambiguate duplicate filenames with parent directory.
- Close via `×` button, middle-click, and `Ctrl/Cmd+W`. Closing the last tab hides the panel and restores the board to full width.
- Migrate text, markdown, and edit-mode rendering from the modal (`_openFilePreview`, `_renderHighlightedContent`, `_toggleMarkdownView`, `_enterEditMode`, `_saveFile`, `_discardEdit`) into the panel body. Edit controls move into the tab toolbar.
- Delete `_openFilePreview`, `closeExplorerPreview`, and their backdrop DOM; update the Escape handler in `ui/js/events.js` and the `window.closeExplorerPreview` export accordingly.
- Tests in `ui/js/tests/` covering: open preview tab, promote to pinned on double-click, single-click another file replaces preview, Cmd+W closes active, duplicate-name disambiguation.

### Phase 2: Multi-Modal Preview

- Extend the file endpoint to serve raw bytes with correct `Content-Type` (new `GET /api/explorer/file/raw` or `?raw=1`); update `apicontract/routes.go` and docs.
- Dispatch by extension to image / video / audio / PDF / hex renderers; add a shared extension→renderer map.
- Image renderer: fit-to-panel by default, click toggles original size; show dimensions and byte size in the tab footer.
- Video / audio renderers: native HTML5 controls, `preload="metadata"`.
- PDF renderer: `<iframe src=…>`; fall back to a download link if the browser cannot render PDFs inline.
- Hex dump renderer for other binaries: first 256 bytes, 16-per-row offset/hex/ASCII; reuse for the "too large" branch when the binary is small enough to sniff.

### Phase 3: Polish

- Tab overflow: horizontal scroll with left/right chevrons when tabs exceed panel width; keyboard `Ctrl/Cmd+Tab` / `Ctrl/Cmd+Shift+Tab` to cycle.
- Viewport-adaptive layout: below a threshold (e.g. 1100 px), the file panel replaces the board; provide a back-to-board affordance and `Escape` to return focus.
- Dirty indicator: a dot on the tab title while edit-mode has unsaved changes; closing a dirty tab prompts to discard.
- Persist the active tab set (paths + active index) in `sessionStorage` per workspace group so a reload restores the panel state.

---

## Boundaries

- Do NOT add a full code editor (Monaco, CodeMirror) — keep the plain textarea for editing
- Do NOT add split-view / side-by-side file comparison (that's a diff viewer concern)
- Do NOT add file creation, deletion, or renaming from the panel (separate spec)
- Do NOT stream large files — the existing 2 MB limit for text files stays; media files use the browser's native streaming via direct URL
