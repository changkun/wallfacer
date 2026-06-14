---
title: Inline File Panel with Multi-Modal Preview
status: drafted
depends_on:
  - specs/foundations/file-explorer.md
affects:
  - frontend/src/components/ExplorerPanel.vue
  - frontend/src/views/BoardPage.vue
  - internal/handler/explorer.go
  - internal/apicontract/routes.go
  - internal/constants/constants.go
effort: large
created: 2026-06-14
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Inline File Panel with Multi-Modal Preview

Supersedes the archived [[file-panel-viewer]] (specs/local/file-panel-viewer.md), which was written against the deleted vanilla-JS frontend (`ui/js/explorer.js`) and the container model. Every file and symbol it names is gone. This spec re-targets the same idea against the current Vue architecture.

## Problem

The explorer previews files in a centered modal. `ExplorerPanel.vue` (template lines around `.explorer-preview-backdrop`, line 537) opens a `role="dialog"` overlay over the board grid. This is disruptive: the modal blocks the board, and browsing several files means opening and closing a dialog for each one. Users coming from VS Code expect an inline panel with tabs.

The preview is also format-blind. `selectFile()` (ExplorerPanel.vue:295) fetches `GET /api/explorer/file`, which returns either `text/plain` bytes or, for binary content, a JSON body `{binary:true, size}` with no payload (explorer.go:284). The component stringifies that JSON into `fileContent` (line 306), so a PNG or PDF renders as the literal text `{"binary":true,"size":1234}`. Images, video, audio, and PDFs have no visual preview, and there is no hex fallback for opaque binaries.

## Goal

Replace the modal preview with an inline, tabbed file panel inside the explorer, and add multi-modal rendering: text with syntax highlighting, image, video, audio, PDF, and a hex view for binary. Media renders via a new raw-bytes endpoint with the correct `Content-Type`, streamed natively by the browser.

## Current State

- `frontend/src/components/ExplorerPanel.vue` is the Vue explorer, mounted inside `frontend/src/views/BoardPage.vue` to the left of the board grid. The header comment (lines 13-16) notes the preview is a modal precisely because the board grid occupies the inline space a pane would use.
  - `selectFile(entry)` (line 295) fetches `GET /api/explorer/file?workspace=&path=` and stores the body in `fileContent`. Only one file is open at a time; opening another replaces it.
  - Text: `highlightCode()` + `previewLines` (line 338) render line-numbered hljs HTML.
  - Markdown (`.md`, `.markdown`): `isMarkdownFile` + `renderMarkdown()` with a Raw/Preview toggle (line 376).
  - Edit mode: `startEdit` / `saveFile` / `cancelEdit` persist via `PUT /api/explorer/file` (line 210). Escape closes the modal first, then the panel (`onKeydown`, line 384).
  - Binary, video, audio, image, PDF: no handling. The JSON sentinel is shown as text.
- `internal/handler/explorer.go`: `ExplorerReadFile` (line 210) validates via `isAllowedWorkspace` + `isWithinWorkspace`, enforces `constants.ExplorerMaxFileSize` (2 MiB, constants.go:128), sniffs the first 8 KiB with `isBinaryContent` (line 204), and either streams `text/plain` or returns the binary JSON sentinel. There is no raw-bytes path and no `Range` support.
- `internal/apicontract/routes.go` (lines 656-666) declares only `GET /api/explorer/file` (`ExplorerReadFile`) and `PUT /api/explorer/file` (`ExplorerWriteFile`).
- `withAuthToken(url)` (frontend/src/api/client.ts:31) appends `?token=` so URL-driven loads (like the existing EventSource stream) carry auth.
- Tests today: Go `internal/handler/explorer_test.go`, frontend `frontend/src/lib/explorerTree.test.ts`. There is no `ExplorerPanel.vue` component test.

## Design

### Raw-content endpoint

Add a sibling route `GET /api/explorer/file/raw?workspace=&path=` (`Name: ExplorerReadFileRaw`, `JSName: readFileRaw`) in routes.go, mirroring the existing entry shape and `explorer` tag. The handler reuses `isAllowedWorkspace` + `isWithinWorkspace` for identical path validation (same escape and not-found handling), then serves the file with `http.ServeContent(w, r, name, modTime, file)`. `ServeContent` gives us three things the panel needs: a `Content-Type` from the extension (with content sniff fallback), `Range` request support (required for video and audio seeking), and conditional GET.

The 2 MiB limit stays on the existing JSON/text path (content is read into memory and highlighted). The raw endpoint is deliberately not subject to it: media uses native browser streaming, so a large video is fine and never buffered server-side.

### Inline tabbed panel

`ExplorerPanel.vue` replaces the `.explorer-preview-backdrop` modal with an inline panel rendered in the explorer body, below the tree. The explorer already lives left of the board grid in `BoardPage.vue`; the panel occupies the lower region of the explorer aside (the tree and panel split the aside's height), so the board grid is never covered. On narrow viewports the panel may expand to fill the aside with the tree collapsed to a header strip.

Open-files state: a small reactive store `openFiles: { path, workspace, pinned, dirty }[]` plus an active index, replacing the single `selectedPath`. Tab semantics follow VS Code:

- Single-click a tree file: open in a preview tab (italic title, reused/replaced by the next single-click).
- Double-click: pin the tab (normal title, persists until closed). Promotes the current preview tab.
- Duplicate filenames disambiguate by parent directory in the tab label.
- Close via `×`, middle-click, or `Ctrl/Cmd+W`. Closing the last tab hides the panel.

### Multi-modal rendering

Dispatch by extension via a shared map. Media tabs build their `src` with `withAuthToken('/api/explorer/file/raw?...')` so `<img>`, `<video>`, `<audio>`, and `<iframe>` carry the `?token=` (element src URLs cannot send auth headers).

| Type | Extensions | Rendering |
|------|-----------|-----------|
| Text/code | default | `highlightCode` + line numbers (current path) |
| Markdown | `.md`, `.markdown` | `renderMarkdown` with Raw/Preview toggle (current path) |
| Image | `.png .jpg .jpeg .gif .svg .webp .ico .avif` | `<img>` fit-to-panel, click toggles original size |
| Video | `.mp4 .webm .mov .ogv` | `<video controls preload="metadata">` |
| Audio | `.mp3 .wav .ogg .flac .m4a` | `<audio controls>` |
| PDF | `.pdf` | `<iframe>` at the raw URL, download-link fallback |
| Binary (other) | sniffed via the JSON sentinel | hex view of the first 256 bytes (offset, hex, ASCII) |

Text, markdown, and image continue to use the existing `GET /api/explorer/file` JSON/text response for classification and content. Media and PDF use the raw endpoint directly and skip the 2 MiB ceiling. The hex view reuses the binary sentinel (`{binary:true, size}`); to populate bytes it fetches a bounded slice from the raw endpoint via a `Range` request (`bytes=0-255`).

### Edit mode

Edit controls (Edit / Save / Discard, dirty dot) move from the modal header into the active tab's toolbar; `saveFile` / `cancelEdit` and the dirty-edit guard are unchanged. Edit applies only to text and markdown tabs.

## Phasing / Acceptance Criteria

Phase 1, panel shell and tabs:
- Inline panel replaces the modal; `openFiles` store with preview vs pinned tabs, duplicate-name disambiguation, close affordances, `Ctrl/Cmd+W`.
- Text, markdown, and edit mode work in the panel; Escape returns focus to the board.
- New component test under `frontend/src/components/__tests__/` covers: open preview tab, double-click pins, single-click another file replaces the preview, `Cmd+W` closes active, duplicate-name labels.

Phase 2, raw endpoint and media:
- `ExplorerReadFileRaw` added to `explorer.go` and `routes.go`; serves correct `Content-Type` and honors `Range`.
- Extension dispatch renders image, video, audio, PDF, and hex; media URLs pass through `withAuthToken`.
- Go tests in `explorer_test.go`: Content-Type per extension, `Range` partial-content (206), path-escape rejection (400), missing file (404), binary sniff for hex.

## Non-Goals

- No full code editor (Monaco, CodeMirror). The textarea editor stays.
- No split-view or diff comparison.
- No file create, delete, or rename from the panel.
- No raising the 2 MiB limit for the text path. Media streams natively via the raw URL.
- No `sessionStorage` persistence of open tabs (possible later polish).
