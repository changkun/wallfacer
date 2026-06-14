---
title: Inline Diff Review Comments
status: drafted
depends_on: []
affects:
  - frontend/src/components/TaskDetail.vue
  - frontend/src/lib/diff.ts
  - frontend/src/lib/diffHighlight.ts
  - internal/handler/execute.go
  - internal/apicontract/routes.go
effort: medium
created: 2026-06-14
updated: 2026-06-14
author: changkun
dispatched_task_id: null
---

# Inline Diff Review Comments

Supersedes the archived [[inline-diff-feedback]]
(`specs/local/inline-diff-feedback.md`), which was written against the deleted
vanilla-JS frontend and its `ui/js/modal-diff.js` diff renderer. The frontend is
now Vue, and the diff viewer lives in `frontend/src/components/TaskDetail.vue`
driven by `frontend/src/lib/diff.ts`. This spec retargets the same feature
(code-review-style inline comments) at the current surfaces.

## Problem

Feedback on a waiting task is a single freeform textarea. For review-shaped work
the user wants to mark up specific lines across multiple files in the diff, the
way a GitHub PR review does, then send everything at once. Today there is no way
to attach a comment to a diff line, collect several, or batch them into one
feedback message.

## Goal

Let the user click a gutter on any line in the Changes-tab diff (waiting tasks
only), write a comment anchored to that line, collect comments across files in a
client-side store, and submit them all as one structured feedback message
through the existing feedback endpoint. No new backend persistence: individual
comments live only in the client until submit, then collapse into the single
feedback message the runner already consumes.

## Current State

Diff parsing (`frontend/src/lib/diff.ts`): `parseDiffFiles(diff)` splits the raw
unified diff into `DiffFile { filename, workspace, adds, dels, lines }`. Each
`DiffLine` is only `{ kind, text }` where `kind` is `add | del | hunk | header |
ctx`. There is no line number on a `DiffLine` and no per-line identity other
than its array index. `classifyDiffLine` does the prefix classification.

Diff rendering (`frontend/src/components/TaskDetail.vue:999-1014`): the Changes
tab iterates `diffFiles`, renders each as a `<details class="diff-file">` with a
`<pre class="diff-block diff-block-modal">`; each line is a
`<span class="diff-line" :class="lineClass(ln.kind)">`. Highlighting comes from
`diffHighlights` (`frontend/src/lib/diffHighlight.ts`, `highlightDiffFile`
returning `HighlightedDiffLine[]`); the non-highlighted branch renders raw
`f.lines`. `lineClass` (`TaskDetail.vue:333`) maps kind to a CSS class. The diff
loads from `GET /api/tasks/{id}/diff` into `diffFiles`.

Feedback submission: the textarea lives in the Overview tab; `submitFeedback`
(`TaskDetail.vue:702-713`) POSTs to `/api/tasks/{id}/feedback`. The handler
`SubmitFeedback` (`internal/handler/execute.go:116`) decodes a body of
`{ "message": string }` (`json:"message"`), records a feedback event, and
resumes the runner with that string as the next turn's prompt. Route declared at
`internal/apicontract/routes.go:578` (`SubmitFeedback`). Body cap is
`BodyLimitFeedback` (512 KiB, `internal/constants/constants.go:124`).

Note for implementers: the current Vue call sends `{ feedback: text }`, but the
handler reads `json:"message"`. The two keys do not match. This spec does not fix
that (a separate change with its own test owns it), but the batch submit path
designed below MUST send the comment payload under the `message` key the backend
actually reads, so the new path is correct regardless of the existing key bug.

## Design

Frontend-only feature. Comments are in-memory client state keyed by task; the
backend sees one combined `message`.

### Line anchoring (the crux)

`DiffLine` carries no line number, so comments need an anchor. Extend
`parseDiffFiles` in `frontend/src/lib/diff.ts` to derive line numbers while
walking each file's lines:

- Add `oldLine: number | null` and `newLine: number | null` to `DiffLine`.
- On a `hunk` line, parse `@@ -a,b +c,d @@` to seed the old/new counters.
- For `add`, set `newLine` and advance the new counter; `oldLine` stays null.
- For `del`, set `oldLine` and advance the old counter; `newLine` stays null.
- For `ctx`, set both and advance both.
- For `hunk` / `header`, both null.

A comment anchors on `(filename, lineIndex)` where `lineIndex` is the position in
`DiffFile.lines` (stable for a loaded diff, already the `v-for` key). The derived
`oldLine` / `newLine` are carried for display and for the formatted output the
agent reads, not as the identity. Anchoring on array index keeps the gutter,
sidebar, and formatter all referencing the same `(filename, lineIndex)` pair.

### Comment store (new Pinia store)

New file `frontend/src/stores/diffComments.ts`, `defineStore` in the same style
as `frontend/src/stores/tasks.ts` and `frontend/src/stores/ui.ts`. It holds, per
task id, a list of:

```ts
interface DiffComment {
  id: string;          // crypto.randomUUID()
  taskId: string;
  filename: string;
  lineIndex: number;   // index into DiffFile.lines
  oldLine: number | null;
  newLine: number | null;
  kind: DiffLineKind;  // add | del | ctx (hunk/header not commentable)
  lineText: string;    // snapshot of the line, for the formatted output
  body: string;
}
```

State shape: `Map<string, DiffComment[]>` keyed by task id. Actions: `add`,
`update(id, body)`, `remove(id)`, `clear(taskId)`, getters `forTask(taskId)` and
`forLine(taskId, filename, lineIndex)`. Pure client state; nothing is persisted
across reloads (acceptable, the workflow is open diff, comment, submit).

### Gutter and inline editor (TaskDetail.vue)

Extend the Changes-tab render block (`TaskDetail.vue:999-1014`). Only when the
task is in `waiting` status (gate on the same condition the feedback section
uses):

- Prepend a small gutter affordance to each commentable line (`add | del | ctx`;
  skip `hunk` / `header`). Clicking it opens an inline editor row directly below
  that line: a compact textarea plus Save / Cancel.
- Save calls the store `add` (or `update` when editing an existing comment for
  that line). Cancel discards.
- Lines that already have a comment show a marker class on the gutter (a dot) so
  commented lines are visible at a glance.
- The editor row is a sibling element in the `<pre>` flow; it must not break the
  monospace line layout (render it outside the `diff-line` span as its own block
  under the line). Keep `lineClass` untouched for diff lines; the editor and
  gutter get their own classes in the Changes-tab CSS.

The non-highlighted and highlighted render branches both need the gutter, so
factor the per-line render into one path that both branches use, or add the
gutter wrapper around the existing `diff-line` span in both branches.

### Comment list and batch submit

Add a comments panel to the Changes tab (beside or below the diff) that lists
`store.forTask(taskId)` grouped by filename. Each entry shows
`filename:newLine` (or `:oldLine` for deletions), the truncated body, and
Edit / Delete. Clicking an entry scrolls its line into view and flashes it.

A "Submit N comments" button (N from the store) sits in this panel, with an
optional general-feedback textarea above it. On submit:

1. `formatBatchFeedback(comments, general)` (new helper, colocated in
   `TaskDetail.vue` or a small `frontend/src/lib/diffComments.ts`) assembles one
   markdown string. Format:

   ```
   ## Inline Review Comments

   ### internal/handler/execute.go

   **Line 142** (`+ added line text`):
   Use the message key here, not feedback.

   ### frontend/src/lib/diff.ts

   **Line 8** (`  context line text`):
   Carry old/new line numbers through here.

   ## General Feedback

   Overall the anchoring looks right.
   ```

   Use `newLine` for adds/context, `oldLine` for deletions. Omit the General
   Feedback section when the textarea is empty; omit the Inline Review Comments
   section when there are no line comments.

2. POST to `/api/tasks/{id}/feedback` reusing the existing `api('POST', ...)`
   call, sending the formatted string under the `message` key (the key
   `SubmitFeedback` decodes). On success, `store.clear(taskId)` and reset the
   editors.

No backend changes: the existing `SubmitFeedback` handler and route are
sufficient, and the formatted message fits well within `BodyLimitFeedback`.

## Phasing and Acceptance Criteria

Phase 1: line numbers. Extend `DiffLine` and `parseDiffFiles` with `oldLine` /
`newLine`. Unit tests in `frontend/src/lib/diff.test.ts` cover adds, dels,
context, multi-hunk files, and headers. Existing diff render keeps working
(highlight and plain branches unchanged in output).

Phase 2: store. `frontend/src/stores/diffComments.ts` with CRUD and getters, unit
tested.

Phase 3: gutter and editor in the Changes tab, waiting-only. Manual check: click
a line, write a comment, see the gutter marker and sidebar entry.

Phase 4: comment panel, `formatBatchFeedback`, and submit wiring. Acceptance:

- Comments can be added on add / del / context lines across multiple files; hunk
  and header lines are not commentable.
- The sidebar lists comments grouped by file with edit and delete; clicking
  scrolls to and flashes the line.
- "Submit N comments" posts a single message under the `message` key, the runner
  resumes, and the store clears.
- Gutter and panel appear only when the task is `waiting`.
- `formatBatchFeedback` output matches the format above for: line comments only,
  general only, both, and none (button disabled when both empty).

## Non-Goals

- Server-side persistence of individual comments. Comments are client-only and do
  not survive a reload.
- Fixing the existing `{ feedback }` vs `{ message }` key mismatch in
  `submitFeedback` (separate change, separate test).
- Threaded replies, reactions, or multi-round review state.
- Commenting on card-level diffs or any surface outside the Changes tab.
- Backend feedback API or schema changes; the route and handler are reused as-is.
