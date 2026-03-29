---
title: "File Icons in Explorer Tree"
status: complete
track: foundations
depends_on:
  - specs/foundations/file-explorer/task-04-frontend-tree-component.md
affects:
  - ui/js/explorer.js
  - ui/css/explorer.css
effort: medium
created: 2026-03-22
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 11: File Icons in Explorer Tree

## Goal

Add VS Code-style file and folder icons to the explorer tree nodes so the tree is visually richer and file types are immediately recognizable at a glance.

## What to do

1. Add an `_getFileIcon(name, type, expanded)` function to `ui/js/explorer.js`:
   - Returns an inline SVG string (14×14, `viewBox="0 0 24 24"`, matching the project's Feather-style icon convention: `fill="none"`, `stroke-width="2"`, `stroke-linecap="round"`, `stroke-linejoin="round"`)
   - **Folder icons**: closed folder (▶ state) and open folder (▼ state), using `currentColor` or a muted gold/yellow tone
   - **File icons by extension** — map common extensions to distinct colored icons. At minimum:
     - Go (`.go`) — cyan
     - JavaScript (`.js`, `.mjs`, `.cjs`) — yellow
     - TypeScript (`.ts`, `.tsx`) — blue
     - CSS (`.css`) — purple
     - HTML (`.html`, `.htm`) — orange
     - JSON (`.json`) — yellow-green
     - Markdown (`.md`) — light blue
     - YAML (`.yaml`, `.yml`) — red-pink
     - Python (`.py`) — blue-yellow
     - Rust (`.rs`) — orange-brown
     - Shell (`.sh`, `.bash`, `.zsh`) — green
     - Docker (`Dockerfile`, `Dockerfile.*`, `docker-compose.*`) — blue
     - Git (`.gitignore`, `.gitmodules`, `.gitattributes`) — orange-red
     - Config/env (`.env`, `.toml`, `.ini`, `.cfg`) — gray with gear motif
     - Images (`.png`, `.jpg`, `.jpeg`, `.gif`, `.svg`, `.webp`, `.ico`) — green/magenta
     - SQL (`.sql`) — light orange
     - Text (`.txt`, `.log`) — gray
   - **Special filenames**: `Makefile`, `LICENSE`, `README.*`, `CLAUDE.md`, `AGENTS.md` can have distinct icons if desired
   - **Default**: generic document icon in `var(--text-muted)` for unknown extensions

2. Insert the icon element in `_renderNode()`:
   - Add a `<span class="explorer-node__icon">` between the disclosure toggle and the name span
   - Set its `innerHTML` to the result of `_getFileIcon(node.name, node.type, node.expanded)`
   - For directory nodes, update the icon when toggling expanded/collapsed (open folder vs closed folder)

3. Add CSS for `.explorer-node__icon` in `ui/css/explorer.css`:
   - `display: inline-flex; align-items: center; justify-content: center;`
   - `width: 16px; height: 16px; flex-shrink: 0;`
   - SVGs inside should be `width: 14px; height: 14px;` (or 100%)
   - Icon colors are set directly on the SVG `stroke` or `fill` attributes, not inherited from `currentColor` (so each file type keeps its distinctive color regardless of hover/focus state)
   - Exception: folder icons and default file icon can use `currentColor` or `var(--text-muted)` to blend with the theme

4. Expose `_getFileIcon` on `window` for testing.

## Tests

Add to `ui/js/tests/explorer.test.js`:

- `_getFileIcon` returns an SVG string containing `<svg` for known extensions (`.go`, `.js`, `.md`)
- `_getFileIcon` returns a folder SVG for `type === "dir"` and different SVGs for expanded vs collapsed
- `_getFileIcon` returns a default file SVG for unknown extensions (e.g., `.xyz`)
- `_getFileIcon` matches by special filename (`Makefile`, `Dockerfile`)

## Boundaries

- Do NOT add an external icon library or font (no `vscode-icons`, no icon fonts) — use inline SVGs only
- Do NOT change the tree data model — icons are derived purely from `node.name` and `node.type` at render time
- Do NOT add icon configuration or user-customizable icon themes
- Do NOT change the existing disclosure triangle (▶/▼) — the file icon is a separate element beside it
- Keep the icon set reasonable (~20 types) — do not try to cover every possible extension
- Icons should look acceptable in both light and dark themes; test with `prefers-color-scheme` or the app's theme toggle
