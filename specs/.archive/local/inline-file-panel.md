---
title: Editor Tabs (VS Code-Style File Tabs in the Board)
status: archived
depends_on:
  - specs/foundations/file-explorer.md
affects:
  - frontend/src/views/BoardPage.vue
  - frontend/src/components/ExplorerPanel.vue
  - frontend/src/components/editor/EditorTabStrip.vue
  - frontend/src/components/editor/FileEditor.vue
  - frontend/src/stores/editorTabs.ts
  - frontend/package.json
effort: large
created: 2026-06-14
updated: 2026-06-25
author: changkun
dispatched_task_id: null
---

# Editor Tabs (VS Code-Style File Tabs in the Board)

## Outcome (2026-06-25)

Shipped. Both phases landed directly on `main`:

- `stores/editorTabs.ts` is the source of truth (`d782b381`), with store tests
  (`editorTabs.test.ts`) covering open/focus/close, board-uncloseable,
  dirty-guard, and buffer survival.
- `EditorTabStrip.vue` renders the VS Code-style tab band in the header spacer,
  with board task-status indicators (`e9a4a736`), compacted to a ~36px band
  (`5ba26a41`).
- `FileEditor.vue` uses CodeMirror 6 (`1d368f47`); the CM6 deps are pinned in
  `package.json` with the lockfile regenerated (`a71a2531`).
- Preview (temporary) tabs: single-click previews, save/double-click pins
  (`fd442bf7`); `Cmd/Ctrl+S` pins the tab instead of the browser save dialog
  (`b6377dc4`); `editorTabHotkeys.ts` handles `Cmd/Ctrl+W` close.
- `ExplorerPanel.vue` opens files into tabs via `openFile`; the
  `.explorer-preview-backdrop` modal and its dead CSS are removed
  (`19e51b27`, `c86c1d43`). Board docs updated (`ace40275`).

Non-goals held: no split panes, drag-reorder, diff view, media rendering, or
raw-bytes endpoint. The [Future](#future) items remain unstarted.

## Design Revision (2026-06-25)

This spec originally proposed an inline preview panel *inside the explorer aside*,
keeping the board always visible, retaining the `<textarea>` editor, and adding
multi-modal media rendering (image/video/audio/PDF/hex). Per direct user
direction it is re-scoped to a **VS Code-style tab model in the board's top
bar**:

1. The Kanban board becomes a pinned, non-closeable **tab** (tab 0).
2. Clicking a file in the tree opens (or focuses) a **file tab**; the active tab
   swaps the center pane. The board stays open as a tab you switch back to.
3. Editing uses a **real code editor (CodeMirror 6)**, not a textarea, for a
   VS Code-like experience (syntax highlighting while editing, line numbers,
   bracket matching, find).

The earlier media-rendering and aside-panel design moves to [Future](#future);
the raw-bytes endpoint is not part of this scope.

## Problem

The explorer previews files in a centered modal. `ExplorerPanel.vue` opens an
`.explorer-preview-backdrop` `role="dialog"` overlay over the board grid. This is
disruptive: the modal blocks the board, only one file is open at a time, and
browsing several files means opening and closing a dialog for each one. The
board's top bar (`app-header`) has a large unused `.app-header__spacer` (literally
where workspace-group tabs used to live). Users coming from VS Code expect file
tabs there, multiple files open at once, and a proper text editor.

## Current State

- `frontend/src/views/BoardPage.vue` owns the board: `<header class="app-header">`
  (the spacer + `SearchBar` + action buttons) and `.board-with-explorer`
  (the `ExplorerPanel` aside + `.board-grid`).
- `frontend/src/components/ExplorerPanel.vue` is the file tree + preview modal.
  `selectFile(entry)` fetches `GET /api/explorer/file?workspace=&path=` into
  `fileContent`/`selectedPath` and renders the `.explorer-preview-backdrop`
  modal. Editing is a `<textarea>`; `saveFile` persists via
  `PUT /api/explorer/file`; a `dialog.confirm` dirty guard protects discards.
  View highlighting uses `highlight.js` via `highlightCode`/`splitHighlightedLines`.
- `frontend/src/stores/ui.ts` toggles `showExplorer` (tree visibility); the tree
  is independently shown/hidden from any tab state.
- No code-editor library is installed (no Monaco/CodeMirror). Build is
  `vite-ssg` (pages are prerendered), so any editor must mount client-side only.
  `useMermaid` is the existing precedent for a browser-only render path.

## Design

### Tab model and source of truth

A new Pinia store `frontend/src/stores/editorTabs.ts` is the source of truth, so
tabs survive `BoardPage` unmount (navigating to /chat and back) and editor DOM
teardown:

```ts
interface FileTab {
  path: string;          // workspace-relative path; identity key
  workspace: string;
  name: string;          // basename; disambiguated by parent dir on collision
  content: string;       // live buffer (CM rehydrates from this)
  baseline: string;      // last-saved content; dirty = content !== baseline
  loading: boolean;
  loadError: string | null;
  saving: boolean;
  saveError: string | null;
}
// state: tabs: FileTab[]; activeId: string ('board' | path)
// 'board' is a synthetic, pinned, non-closeable tab that is always present.
```

Actions: `openFile(ws, path)` (focus if already open, else fetch + append + focus),
`focus(id)`, `close(id)` (board is uncloseable; dirty tabs run the confirm guard),
`setContent(path, text)`, `save(path)`, `markSaved(path)`, `isDirty(path)`.
The file-read (`GET /api/explorer/file`) moves into `openFile`; the
write (`PUT /api/explorer/file`) into `save`.

### Tab strip in the top bar

`frontend/src/components/editor/EditorTabStrip.vue` renders into
`.app-header__spacer` in `BoardPage.vue` (left-aligned, `flex: 1`, horizontally
scrollable on overflow). `SearchBar` + action buttons stay right-aligned. Tabs:
the pinned **Board** tab first, then file tabs in open order. Each file tab shows
a file-type icon, the basename, a dirty dot when unsaved, and a `×` close (also
middle-click and `Ctrl/Cmd+W` on the active tab). Styling mirrors VS Code:
square flush tabs filling the header band, separators between them, and an
active tab with a top accent rule over the editor background.

**Preview (temporary) tabs.** Single-clicking a file in the tree opens it in a
*preview* tab (italic label); the next single-click reuses that slot rather than
piling up tabs. A preview tab is promoted to a permanent (kept) tab on **save**
(`Cmd/Ctrl+S`), on **double-click** (tree row or tab), or implicitly when it is
dirty and another file is previewed (so edits are never discarded). Modeled by a
`preview` flag on `FileTab` and a `promote(path)` store action.

**Board tab status.** The Board tab surfaces task state without leaving a file:
a small spinner when `tasks.inProgress` is non-empty (work running) and an amber
dot when `tasks.waiting` is non-empty (tasks need feedback). Both can show at
once; the spinner respects `prefers-reduced-motion`.

### Center-pane swap

`.board-with-explorer` keeps the `ExplorerPanel` aside (tree, independently
toggled by `showExplorer`). The center area holds the board grid plus one editor
per open file, switched by the active tab. State preservation is mandatory, so
this uses `v-show`, never `v-if`: switching to a file tab must not reset the
board (filter, scroll, drag) and must not wipe another editor's undo/cursor.

```
.board-grid                       v-show="activeId === 'board'"
FileEditor (one per tab, v-for)   v-show="activeId === tab.path"
```

### Editor: CodeMirror 6

`frontend/src/components/editor/FileEditor.vue` wraps a CodeMirror 6 instance
(chosen over Monaco: Monaco fights `vite-ssg` prerender with web workers and a
multi-MB bundle; CM6 mounts cleanly client-side and is modular). The CM view is
constructed in `onMounted` only. Setup: line numbers, history (undo/redo),
bracket matching, default search, active-line highlight, and a theme bound to
the app light/dark pref. Per-file language is resolved lazily via
`@codemirror/language-data`'s `matchFilename` (do not reuse the highlight.js
`extToLang` map; different system). The editor is editable; an `update` listener
writes the buffer back to the store (`setContent`) which recomputes dirty.

New deps (`frontend/package.json`): `codemirror`, `@codemirror/state`,
`@codemirror/view`, `@codemirror/commands`, `@codemirror/language-data`, and a
theme package. Pin versions verified against current CM6 docs at implement time.

The store buffer is the source of truth; the CM instance rehydrates from
`tab.content` on (re)mount. highlight.js stays for any other consumers (diff,
spec view) but the explorer file-preview path stops using it.

### Save and dirty handling

Per-tab dirty = `content !== baseline`. A toolbar above the editor shows the
path, a Save button (`Ctrl/Cmd+S`), and save/error state. `save` calls
`PUT /api/explorer/file` and sets `baseline = content` on success. Closing a
dirty tab, and an explicit guard, reuse the existing `dialog.confirm`
("Discard changes?"). Navigating routes does not destroy buffers (store-held),
so unsaved work persists across board ↔ chat navigation.

### ExplorerPanel changes

- `selectFile(entry)` calls `editorTabs.openFile(ws, entry.path)` instead of
  setting `selectedPath` / fetching inline.
- Delete the `.explorer-preview-backdrop` modal block and its now-unused
  preview/edit/highlight state. Keep the tree, lazy children fetch, and keyboard
  navigation untouched.

## Phasing / Acceptance Criteria

Phase 1, store + tab shell:
- `editorTabs` store with board-pinned-uncloseable, open/focus-existing, close,
  close-dirty-guard, duplicate-name disambiguation, dirty computed.
- `EditorTabStrip` in the top-bar spacer; center-pane `v-show` swap; board state
  (filter/scroll) survives switching to a file tab and back.
- Vitest store test: open appends+focuses, opening an open file focuses (no dup),
  close removes, board cannot be closed, dirty guard invoked for dirty close,
  buffers survive a simulated route change. (Do not assert CM layout in
  happy-dom; verify the editor in a real browser.)

Phase 2, CodeMirror editor:
- `FileEditor` mounts CM6 client-side only; line numbers, syntax highlighting,
  undo, find; theme follows light/dark pref; language lazy-loaded by filename.
- Edit → buffer → store dirty → Save (`PUT`) → baseline updates; `Ctrl/Cmd+S`
  saves; dirty guard on close.
- ExplorerPanel modal deleted; tree still opens files into tabs.
- Real-browser verification (vite dev + Playwright): open two files, edit one,
  switch tabs preserving state, save, close with dirty guard.

## Non-Goals

- No split panes, no tab drag-reorder, no diff view, no minimap.
- No custom find/replace UI (CM's built-in search is enough).
- No URL-syncing of open files (active-tab-in-query is a possible later polish;
  file tabs do not belong in the URL).
- No media rendering (image/video/audio/PDF/hex) or raw-bytes endpoint in scope.
- Tabs are board-only (the user said "in the Kanban UI"); not added to Chat/Plan
  now. The store is kept extractable so that is cheap later.

## Future

- Multi-modal rendering + a `GET /api/explorer/file/raw` endpoint with `Range`
  support (the prior revision of this spec), for image/video/audio/PDF/hex.
- Tab drag-reorder and split view.
- Reusing the tab shell on Chat/Plan.
