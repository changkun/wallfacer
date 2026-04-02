---
title: Inline Diff Feedback
status: drafted
depends_on: []
affects:
  - ui/js/modal-diff.js
  - ui/js/modal-core.js
  - ui/js/tasks.js
  - ui/css/diffs.css
  - ui/partials/task-detail-modal.html
  - internal/handler/execute.go
  - internal/store/models.go
effort: medium
created: 2026-04-02
updated: 2026-04-02
author: changkun
dispatched_task_id: null
---

# Inline Diff Feedback

## Overview

Currently, feedback for waiting tasks is limited to a single freeform textarea — the user types one message and submits it. This is insufficient for code-review-style workflows where the user wants to comment on specific lines across multiple files in the diff. This spec adds GitHub-PR-style inline commenting on the diff view, a comment collection sidebar, and batch submission that combines all saved comments into a single structured feedback prompt.

## Current State

**Feedback flow:** When a task reaches `waiting`, the modal shows a `<textarea id="modal-feedback">` and a "Submit Feedback" button (`ui/partials/task-detail-modal.html:744-792`). The `submitFeedback()` function in `ui/js/tasks.js:437-453` sends `POST /api/tasks/{id}/feedback` with `{message: string}`. The handler in `internal/handler/execute.go:114-167` validates the message, records a `feedback` event, transitions the task to `in_progress`, and resumes the runner with the message as the next turn's prompt.

**Diff view:** The Changes tab (`ui/partials/task-detail-modal.html:1033-1037`) renders file-split diffs via `renderDiffFiles()` in `ui/js/modal-diff.js:144-241`. Each file is a `<details class="diff-file">` with a `<pre class="diff-block diff-block-modal">` containing `<span class="diff-line diff-{add|del|hunk}">` elements. Lines have syntax highlighting via highlight.js.

**Gap:** No mechanism exists to attach comments to specific diff lines, collect multiple comments, or submit them as a batch.

## Architecture

The feature is primarily frontend with minimal backend changes. Comments are collected in-memory on the client until the user explicitly submits the batch. The backend receives a single combined feedback message — no new data model for individual comments is needed.

```
┌─────────────────────────────────────────────────────┐
│  Modal: Changes Tab                                  │
│  ┌───────────────────────┬─────────────────────────┐ │
│  │  Diff View            │  Comment Sidebar         │ │
│  │  (file-split)         │  ┌─────────────────────┐ │ │
│  │  ┌──────────────────┐ │  │ file.go:42          │ │ │
│  │  │ + line 42  [+]   │ │  │ "Use mutex here"    │ │ │
│  │  │   line 43        │ │  │ [Edit] [Delete]     │ │ │
│  │  │ - line 44  [+]   │ │  ├─────────────────────┤ │ │
│  │  └──────────────────┘ │  │ main.go:10          │ │ │
│  │                       │  │ "Missing err check"  │ │ │
│  │                       │  │ [Edit] [Delete]     │ │ │
│  │                       │  └─────────────────────┘ │ │
│  │                       │                         │ │
│  │                       │  General feedback:      │ │
│  │                       │  [textarea]             │ │
│  │                       │                         │ │
│  │                       │  [Submit 2 comments]    │ │
│  └───────────────────────┴─────────────────────────┘ │
└─────────────────────────────────────────────────────┘
```

## Components

### 1. Diff Line Interaction Layer

**Where:** `ui/js/modal-diff.js` — extend `renderDiffFiles()`

Add a clickable comment gutter to each diff line in the modal (not on cards). When the user clicks the gutter icon on a diff line:

- A small inline textarea expands below that line (like GitHub's review comment box).
- The textarea captures the file path, line number, line content, and diff type (add/del/context).
- "Save comment" stores the comment in a client-side `Map<string, Comment[]>` keyed by `taskId`.
- "Cancel" collapses the textarea without saving.
- Lines with saved comments show a visual indicator (colored gutter dot or highlight).

Each `Comment` object:
```javascript
{
  file: string,       // e.g., "internal/runner/execute.go"
  line: number,       // line number within the hunk
  lineContent: string, // the actual line text
  diffType: string,   // "add" | "del" | "context" | "hunk"
  body: string,       // user's comment text
  id: string          // crypto.randomUUID() for stable identity
}
```

Line numbers are derived from the hunk headers (`@@ -a,b +c,d @@`). The rendering function must parse these headers to assign line numbers to each diff line. For added lines, use the new-file line number; for deleted lines, use the old-file line number; for context lines, use the new-file line number.

**Key decisions:**
- Comment gutter only appears in the modal Changes tab, not on card diffs (cards are too compact).
- Only enabled when task is in `waiting` state (matches existing feedback visibility logic).
- Comments are ephemeral client-side state — not persisted to the server individually. They survive modal close/reopen within the same page session but are lost on page reload. This is acceptable because the review workflow is: open diff → comment → submit — not a multi-day process.

### 2. Comment Sidebar Panel

**Where:** `ui/partials/task-detail-modal.html` — new element in Changes tab; `ui/js/modal-diff.js` — rendering logic

A sidebar panel to the right of the diff view that lists all saved comments grouped by file. Each entry shows:
- File path and line number (clickable — scrolls to and highlights the line in the diff)
- Truncated comment body
- Edit and Delete buttons

Below the comment list, the existing general feedback textarea is relocated here, allowing the user to add an overall comment alongside line-specific ones.

At the bottom: a "Submit N comments" button (disabled when N = 0 and general feedback is empty). The button text dynamically reflects the comment count.

**Layout:** The Changes tab currently uses full width for the diff. When comments exist or the task is in `waiting` state, the layout splits: diff takes ~70% width, sidebar takes ~30%. When no comments exist and the task is not waiting, the sidebar is hidden and the diff takes full width.

### 3. Batch Feedback Formatter

**Where:** `ui/js/tasks.js` — new function `formatBatchFeedback(comments, generalMessage)`

Combines all inline comments and the optional general message into a single structured text message suitable for the agent. Format:

```
## Inline Review Comments

### internal/runner/execute.go

**Line 42** (`+ added line content here`):
Use a mutex here to prevent the race condition.

**Line 108** (`- removed line content here`):
This removal breaks the error handling path. Please restore it.

### ui/js/render.js

**Line 15** (`  context line content`):
Consider extracting this into a helper function.

## General Feedback

Overall the approach looks good, but please address the thread-safety issues noted above.
```

This format gives the agent enough context to locate the exact lines and understand what change the user wants. The agent receives this as a single feedback message through the existing `POST /api/tasks/{id}/feedback` endpoint — no backend changes to the feedback API are needed.

### 4. Hunk Header Parser

**Where:** `ui/js/modal-diff.js` — new function `parseHunkLineNumbers(diffContent)`

Parses `@@ -old,count +new,count @@` headers to assign real line numbers to each diff line. Returns an array parallel to the diff lines:

```javascript
{ oldLine: number | null, newLine: number | null, type: "add" | "del" | "context" | "hunk" | "header" }
```

This is needed both for the comment gutter (to display meaningful line numbers) and for the batch formatter (to reference lines the agent can find). The existing `renderDiffFiles()` function processes lines but does not track line numbers — this parser runs as a preprocessing step before rendering.

### 5. Integration with Existing Feedback UI

**Where:** `ui/partials/task-detail-modal.html`, `ui/js/modal-core.js`

The existing `modal-feedback-section` (textarea + Submit Feedback button) in the Overview tab remains as-is for quick single-message feedback. The inline comment flow is only available in the Changes tab.

When the user clicks "Submit N comments" in the Changes tab sidebar:
1. `formatBatchFeedback()` assembles the combined message.
2. The existing `submitFeedback()` codepath is reused (calls `POST /api/tasks/{id}/feedback`).
3. All client-side comments for that task are cleared.
4. The modal closes (same behavior as current feedback submission).

This means both feedback paths (quick textarea in Overview, inline comments in Changes) use the same backend endpoint. They are mutually exclusive per submission — the user uses one or the other.

## Data Flow

1. User opens modal for a waiting task → Changes tab loads diff via `GET /api/tasks/{id}/diff`.
2. `renderDiffFiles()` renders diff with line numbers and comment gutters (waiting tasks only).
3. User clicks gutter → inline comment box opens → user types and saves → comment stored in client `commentStore` Map.
4. Sidebar updates to show the new comment. Gutter dot appears on the commented line.
5. Repeat for additional comments across files.
6. User clicks "Submit N comments" → `formatBatchFeedback()` → `POST /api/tasks/{id}/feedback` → task resumes → comments cleared.

## API Surface

No new API routes. The existing `POST /api/tasks/{id}/feedback` with `{message: string}` is sufficient. The batch formatter produces a structured markdown string that fits within the existing 512 KiB body limit (`BodyLimitFeedback` in `internal/constants/constants.go:118`).

## Testing Strategy

**Frontend tests** (`ui/js/tests/`):

- `modal-diff.test.js` — Test `parseHunkLineNumbers()`: verify line number assignment for adds, dels, context, multi-hunk files, renamed files, binary files.
- `modal-diff.test.js` — Test `renderDiffFiles()` with comment gutters: verify gutter elements are present when task is waiting, absent otherwise.
- New `inline-feedback.test.js` — Test comment store CRUD: add, edit, delete comments; verify sidebar rendering; test `formatBatchFeedback()` output format with various comment combinations (single file, multi-file, with/without general message, empty comments).
- `tasks.test.js` — Test batch submission flow: verify `submitFeedback()` is called with the formatted message.

**Manual testing:**
- Open a waiting task → Changes tab → click line gutter → add comment → verify sidebar shows it.
- Add comments across multiple files → submit → verify the combined message in the task event log.
- Verify comments persist across modal close/reopen within the same session.
- Verify comments are cleared after submission.
- Verify the general feedback textarea in Overview tab still works independently.
