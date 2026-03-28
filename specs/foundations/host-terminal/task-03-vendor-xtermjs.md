# Task 3: Vendor xterm.js Assets

**Status:** Done
**Depends on:** None
**Phase:** Phase 1 — Single Terminal Session
**Effort:** Small

## Goal

Vendor xterm.js and its fit addon so the frontend can render a terminal emulator. Add the corresponding `<script>` and `<link>` tags to the HTML.

## What to do

1. Download xterm.js release files (latest v5.x):
   - `xterm.min.js` → `ui/js/vendor/xterm.min.js`
   - `xterm-addon-fit.min.js` → `ui/js/vendor/xterm-addon-fit.min.js`
   - `xterm.css` → `ui/css/vendor/xterm.css`

   Source: npm registry or unpkg CDN. Match the pattern of existing vendored files (`sortable.min.js`, `marked.min.js`, `highlight.min.js`).

2. **`ui/partials/initial-layout.html`** — Add a `<link>` for xterm.css in the `<head>` section, after the existing stylesheet links:
   ```html
   <link rel="stylesheet" href="/css/vendor/xterm.css">
   ```

3. **`ui/partials/initial-layout.html`** — Add `<script>` tags for the vendor JS in the `<head>` section, after the existing vendor scripts (`highlight.min.js`):
   ```html
   <script src="/js/vendor/xterm.min.js"></script>
   <script src="/js/vendor/xterm-addon-fit.min.js"></script>
   ```

4. Verify the embedded filesystem still works: `go build ./...` (the `//go:embed ui` directive picks up new files automatically).

## Tests

- `go build ./...` succeeds (embedded FS includes new files)
- Manual: load the page in a browser, check the Network tab shows xterm.js files loading with 200 status
- `window.Terminal` is defined in the browser console after page load

## Boundaries

- Do NOT create `ui/js/terminal.js` yet (Task 5)
- Do NOT modify any existing JS files
- Do NOT connect xterm.js to anything — just vendor the assets
