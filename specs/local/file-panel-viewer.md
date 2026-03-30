---
title: Inline File Panel Viewer
status: drafted
depends_on:
  - specs/foundations/file-explorer.md
affects:
  - ui/js/explorer.js
  - ui/index.html
  - ui/css/styles.css
effort: large
created: 2026-03-28
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Inline File Panel Viewer

---

## Problem Statement

The file explorer previews files in a centered popup modal. This is disruptive: the modal obscures the board, blocks interaction with tasks, and makes browsing multiple files tedious since each file opens a new modal that must be closed before opening the next. Users familiar with VS Code expect files to open in an inline panel within the main layout, with tabs for switching between open files.

Additionally, the current preview only handles text and binary placeholders. Images, videos, PDFs, and other media formats show a generic "Binary file (X KB)" message with no visual preview.

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
| Text (code) | Syntax-highlighted with line numbers (existing `_renderHighlightedContent`) |
| Markdown (`.md`, `.mdx`) | Rendered markdown by default, Raw/Preview toggle (existing) |
| Images (`.png`, `.jpg`, `.gif`, `.svg`, `.webp`, `.ico`) | Inline `<img>` with fit-to-panel scaling, click to zoom/original size |
| Video (`.mp4`, `.webm`, `.mov`) | HTML5 `<video>` player with controls |
| Audio (`.mp3`, `.wav`, `.ogg`) | HTML5 `<audio>` player |
| PDF (`.pdf`) | `<iframe>` or `<embed>` with browser's native PDF viewer |
| Binary (other) | Placeholder with file size and hex dump of first 256 bytes |

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

- Create the file panel container in the layout
- Implement tab bar with preview/pinned tab semantics
- Migrate text file rendering from modal to panel
- Remove modal-based preview code

### Phase 2: Multi-Modal Preview

- Image preview with zoom
- Video/audio player
- PDF viewer
- Enhanced binary preview (hex dump)

### Phase 3: Polish

- Tab overflow scrolling
- Keyboard shortcuts for tab management
- Viewport-adaptive layout (narrow mode)
- Tab dirty indicators for unsaved edits

---

## Boundaries

- Do NOT add a full code editor (Monaco, CodeMirror) — keep the plain textarea for editing
- Do NOT add split-view / side-by-side file comparison (that's a diff viewer concern)
- Do NOT add file creation, deletion, or renaming from the panel (separate spec)
- Do NOT stream large files — the existing 2 MB limit for text files stays; media files use the browser's native streaming via direct URL
