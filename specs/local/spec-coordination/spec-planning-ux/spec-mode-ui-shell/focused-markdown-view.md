---
title: Focused markdown view
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/spec-mode-ui-shell/spec-mode-layout.md
affects:
  - ui/js/spec-mode.js
  - ui/js/markdown.js
effort: medium
created: 2026-03-30
updated: 2026-03-31
author: changkun
dispatched_task_id: null
---

# Focused markdown view

## Goal

Implement the center pane of spec mode: when a spec is selected in the explorer, load its content via the file explorer API and render it as formatted markdown using `marked.js`. The view updates live when the file changes (via polling).

## What to do

1. In `ui/js/spec-mode.js`, add a `focusSpec(specPath, workspace)` function:
   ```javascript
   let _focusedSpecPath = null;
   let _focusedSpecWorkspace = null;
   let _specRefreshTimer = null;

   function focusSpec(specPath, workspace) {
     _focusedSpecPath = specPath;
     _focusedSpecWorkspace = workspace;
     _loadAndRenderSpec();
     _startSpecRefreshPoll();
   }
   ```

2. Implement `_loadAndRenderSpec()`:
   - Fetch the spec file content via `GET /api/explorer/file?path=<specPath>&workspace=<workspace>` (existing endpoint in `ExplorerReadFile`).
   - Parse the YAML frontmatter to extract title, status, effort, depends_on.
   - Render the markdown body (everything after the closing `---`) into `spec-focused-body` using `renderMarkdown()` from `markdown.js`.
   - Update the header: set `spec-focused-title` text, show status badge in `spec-focused-status`, show/hide dispatch button based on whether it's a leaf spec with `validated` status.

3. Implement `_startSpecRefreshPoll()` / `_stopSpecRefreshPoll()`:
   - Poll every 2 seconds (faster than the explorer's 3s since the agent writes frequently during planning).
   - On each poll, re-fetch the file content. If the content has changed (compare against a stored hash or the raw text), re-render.
   - Stop polling when switching to board mode or when no spec is focused.

4. Add a simple YAML frontmatter parser in `spec-mode.js` (or a utility):
   ```javascript
   function parseSpecFrontmatter(text) {
     const match = text.match(/^---\n([\s\S]*?)\n---\n([\s\S]*)$/);
     if (!match) return { frontmatter: {}, body: text };
     // Parse YAML key-value pairs (simple line-by-line, no nested objects needed)
     // Return { frontmatter: { title, status, effort, depends_on, affects }, body }
   }
   ```
   This only needs to handle the flat frontmatter structure used by specs — no nested YAML.

5. Wire `focusSpec()` to the explorer: when a `.md` file in the spec tree is clicked, call `focusSpec(path, workspace)` instead of (or in addition to) the existing file preview behavior. This requires a hook point in the explorer's click handler — add a `window.onSpecFileClick` callback that `spec-mode.js` sets when in spec mode.

6. Update `switchMode()` to stop spec refresh polling when leaving spec mode.

## Tests

- `TestParseSpecFrontmatter`: Correctly extracts title, status, effort, depends_on from valid frontmatter. Returns empty frontmatter and full text as body when no frontmatter delimiters exist.
- `TestFocusSpecRendersMarkdown`: Calling `focusSpec()` fetches file content and renders markdown into `spec-focused-body`. Verify the rendered HTML contains expected elements (headings, lists, etc.).
- `TestFocusSpecUpdatesHeader`: Title and status badge update when a spec is focused.
- `TestSpecRefreshDetectsChanges`: When file content changes between polls, the rendered view updates.
- `TestSpecRefreshStopsOnModeSwitch`: Switching to board mode clears the refresh timer.

## Boundaries

- Do NOT implement the spec tree API or spec explorer — that's the spec-explorer spec. This task uses the existing `ExplorerReadFile` API to load individual spec files.
- Do NOT implement chat stream rendering or message sending.
- Do NOT implement the dispatch button behavior — just show/hide it based on frontmatter.
- Do NOT implement Mermaid diagram rendering within specs — plain markdown only for now.
