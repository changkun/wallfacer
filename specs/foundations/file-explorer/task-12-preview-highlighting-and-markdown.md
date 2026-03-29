---
title: "Preview Syntax Highlighting and Markdown Rendering"
status: complete
track: foundations
depends_on:
  - specs/foundations/file-explorer/task-05-frontend-file-preview-modal.md
affects:
  - ui/css/diffs.css
  - ui/js/explorer.js
effort: small
created: 2026-03-22
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 12: Preview Syntax Highlighting and Markdown Rendering

## Goal

Fix syntax highlighting in the file preview modal (colors don't render because the hljs CSS is scoped to `.diff-block-modal`) and add rendered markdown view as the default for `.md` files, with a toggle to switch to raw source.

## Problem

1. **Syntax highlighting produces no colors.** The `_renderHighlightedContent()` function in `explorer.js` correctly calls `hljs.highlight()` which generates `<span class="hljs-keyword">` etc., but the color rules in `ui/css/diffs.css` are all scoped under `.diff-block-modal .hljs-*`. The explorer preview uses `.explorer-preview` — no hljs token colors apply.

2. **Markdown files show raw source.** There is no rendered markdown view. The `marked` library and `renderMarkdown()` helper (`ui/js/markdown.js`) are already loaded and used elsewhere (task modals, docs viewer), but the explorer preview doesn't use them.

## What to do

### 1. Fix hljs token colors for explorer preview

In `ui/css/diffs.css` (or `ui/css/explorer.css`), duplicate the `.diff-block-modal .hljs-*` rules to also match `.explorer-preview .hljs-*`. The cleanest approach is to use a comma-separated selector so both scopes share the same rules without duplication:

```css
/* Before */
.diff-block-modal .hljs-keyword,
.diff-block-modal .hljs-template-tag { color: #d73a49; }

/* After */
.diff-block-modal .hljs-keyword,
.diff-block-modal .hljs-template-tag,
.explorer-preview .hljs-keyword,
.explorer-preview .hljs-template-tag { color: #d73a49; }
```

Do this for all light-theme and dark-theme hljs rule blocks. Alternatively, extract the hljs rules into a shared scope (e.g., `.hljs-themed .hljs-keyword`) and add that class to both `.diff-block-modal` and `.explorer-preview` containers — whichever approach is simpler to maintain.

### 2. Rendered markdown view for .md files

In `explorer.js`, when the file extension is `.md` or `.mdx`:

a. **Default to rendered view**: Instead of calling `_renderHighlightedContent()`, render the content via `renderMarkdown(content)` into a `<div class="explorer-preview__markdown prose">` container. Reuse the existing prose/markdown CSS from the docs viewer or task modals.

b. **Add a Raw/Preview toggle button** in the modal header (next to the file path, before the close button):
   - Button text: "Raw" when showing rendered view, "Preview" when showing raw source
   - Clicking "Raw" replaces the rendered view with the syntax-highlighted source (using `_renderHighlightedContent()` with language `markdown`)
   - Clicking "Preview" switches back to the rendered markdown
   - The toggle state does not need to persist across modal opens

c. **Structure**: Store both the raw content string and rendered HTML so toggling is instant (no re-fetch). The modal content area should have two child containers (one for rendered, one for raw source), toggling visibility via a `hidden` class — same pattern as `toggleModalSection()` in `markdown.js`.

### 3. Prose styling for rendered markdown

Ensure the rendered markdown has appropriate styling:
- Use existing prose CSS classes if available (check `ui/css/` for `.prose` or markdown-specific styles)
- At minimum: headings, paragraphs, code blocks (with syntax highlighting), lists, links, tables, blockquotes should render readably
- Code blocks inside rendered markdown should also get hljs highlighting (check if `marked` is configured with a highlight option, or apply `hljs.highlightElement()` after rendering)

## Tests

Add to `ui/js/tests/explorer.test.js`:

- Verify `_renderHighlightedContent()` output contains `hljs-` class spans for known languages (`.go`, `.js`)
- Verify markdown files (`.md`) trigger rendered view by default (check that the rendered container is not hidden)
- Verify the raw/preview toggle switches visibility of the two containers

## Boundaries

- Do NOT change how non-markdown files are displayed (they keep the syntax-highlighted source view)
- Do NOT add new CSS libraries or markdown renderers — use existing `marked` and `hljs`
- Do NOT change the diff modal's highlighting behavior
- Do NOT persist the raw/preview toggle state
