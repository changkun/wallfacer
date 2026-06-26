---
title: Inline Diff Feedback
status: drafted
depends_on: []
affects:
  - frontend/src/lib/diff.ts
  - frontend/src/lib/diffComments.ts
  - frontend/src/stores/diffComments.ts
  - frontend/src/components/TaskDetail.vue
  - frontend/src/styles/diffs.css
  - internal/cli/server.go
effort: medium
created: 2026-04-02
updated: 2026-06-26
author: changkun
dispatched_task_id: null
---

# Inline Diff Feedback

## Overview

Feedback on a waiting task is a single freeform textarea: the user types one
message and submits it. For review-shaped work the user wants to mark up specific
lines across multiple files in the Changes-tab diff — the way a GitHub PR review
does — collect those line comments, and send them all at once. Today there is no
way to attach a comment to a diff line, collect several, or batch them into one
feedback message.

This spec adds code-review-style inline commenting to the Vue Changes-tab diff: a
gutter affordance per diff line, an inline editor, a comments panel with batch
submit, and a formatter that collapses every comment into one structured feedback
message the runner already consumes. The whole inline surface is gated to
logged-in users, enforced server-side (mirroring how spec comments was gated).

This revision retargets and supersedes the original design (written against the
deleted vanilla-JS `ui/js/modal-diff.js` renderer) and folds in the Vue-targeted
[[diff-review-comments]] design, which is archived in its favor. Two changes
relative to those drafts: (1) the inline feature is **login-gated** like spec
comments; (2) the existing `submitFeedback` key bug (frontend sends `feedback`,
backend reads `message`) is **fixed here** as the first commit, because the whole
feature submits through that path.

## Current State

**Feedback flow.** When a task reaches `waiting`, the Overview tab shows a
textarea (`TaskDetail.vue`, gated on `isWaiting`) and a "Submit Feedback" button.
`submitFeedback` (`TaskDetail.vue:725`) POSTs to `/api/tasks/{id}/feedback`. The
handler `SubmitFeedback` (`internal/handler/execute.go:120`) decodes
`{ "message": string }` (`json:"message"`), records a feedback event, transitions
the task to `in_progress`, and resumes the runner with the message as the next
turn's prompt. Route at `internal/apicontract/routes.go:606`. Body cap is
`BodyLimitFeedback` (512 KiB, `internal/constants/constants.go`).

**Existing key bug.** `submitFeedback` sends `{ feedback: text }`, but the handler
reads `json:"message"`. The keys do not match, so the message is always empty and
the handler returns `400 message is required`: the Overview feedback path is
currently broken end-to-end. The Go handler tests post `{ "message": ... }`
directly, so they pass while the real UI fails. This must be fixed first.

**Diff view.** The Changes tab (`TaskDetail.vue:1027-1042`) iterates `diffFiles`,
rendering each file as `<details class="diff-file">` with a
`<pre class="diff-block diff-block-modal">`. Each line is a
`<span class="diff-line" :class="lineClass(ln.kind)">`. Two render branches:
highlighted (`diffHighlights[fi]`, a `HighlightedDiffLine[]` from
`frontend/src/lib/diffHighlight.ts`) and a plain fallback over `f.lines`. The
`<pre>` is `font-size: 0` (collapsing inter-line whitespace) with `.diff-line`
resetting to `display: block; font-size: 12px; padding: 0 10px`
(`frontend/src/styles/diffs.css:224-237`). The diff loads from
`GET /api/tasks/{id}/diff` into `diffFiles` (`parseDiffFiles`,
`frontend/src/lib/diff.ts`).

**Diff data.** `DiffLine` is only `{ kind, text }` where `kind` is
`add | del | hunk | header | ctx`. There are no line numbers and no per-line
identity beyond the array index.

**Auth signals.** `/api/config` returns `auth_enabled` (`internal/handler/config.go`,
`ServerConfig.auth_enabled` in `frontend/src/api/types.ts:110`), surfaced by the
task store as `store.config?.auth_enabled` (see `Sidebar.vue:114`). The session
store `useAuthStore` (`frontend/src/stores/auth.ts`) resolves the browser
principal from `GET /api/me` into `auth.me` (null when signed out / local mode).
The backend gate is `RequirePrincipalMiddleware`
(`internal/handler/handler.go`): when `h.HasAuth()` is true and no browser
principal is on the request, respond `401`; in local mode (no auth) it is a no-op.
Routes opt in via `requiresPrincipal(name)` in `internal/cli/server.go`.

**Gap.** No mechanism to attach comments to diff lines, collect them, gate the
surface on login, or batch-submit them as structured feedback.

## How Spec Comments Was Enabled (the pattern to mirror)

Spec comments are login-gated in two layers (commits `84c03ac`, `e1ed8e7`,
`1fe6f68`, `d92135a5`):

- **Backend owns the boundary.** The comment routes were added to
  `requiresPrincipal()` and wrapped by `RequirePrincipalMiddleware`, which 401s a
  request with no browser principal when auth is configured. Frontend gating
  alone was explicitly rejected as "the wrong layer."
- **Frontend mirrors with a server-driven signal, for UX only.** The surface is
  hidden when the server says the user cannot use it. Spec comments used
  `available` (a successful `GET` flips it true; a `401` keeps it false), so the
  chrome matches the signed-in state without the SPA reasoning about tokens.

This feature applies the same two layers. The difference: there is no `GET` to
probe for an `available` signal, so the frontend computes the equivalent
client-side mirror directly (below). The backend 401 remains the real boundary.

## Architecture

Primarily frontend. Comments live in a client-side Pinia store keyed by task until
the user submits; the backend then sees one combined `message`. The only backend
change is adding the feedback route to the principal gate.

```
┌──────────────────────────────────────────────────────────┐
│  TaskDetail · Changes tab (waiting task, signed in)       │
│  ┌────────────────────────────────────────────────────┐  │
│  │ diff-file: internal/handler/execute.go             │  │
│  │  [•] + req := DecodeBody[...]{ Message string }    │  │
│  │  [+]   if strings.TrimSpace(req.Message) == "" {   │  │
│  │        ┌────────────────────────────────────────┐  │  │
│  │        │ Use the message key, not feedback.   ⏎ │  │  │
│  │        │ [ Save ] [ Cancel ]                    │  │  │
│  │        └────────────────────────────────────────┘  │  │
│  └────────────────────────────────────────────────────┘  │
│  ┌── Review comments (2) ─────────────────────────────┐   │
│  │ execute.go:122  "Use the message key…"  [Edit][✕]  │   │
│  │ diff.ts:8       "Carry line numbers…"   [Edit][✕]  │   │
│  │ General: [textarea........................]         │   │
│  │ [ Submit 2 comments ]                              │   │
│  └────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────┘
   gutter [•]=has comment  [+]=add (hover) — commentable lines only
```

## Login Gating (the new requirement)

The inline review surface (gutter, inline editor, comments panel, batch submit) is
available only to logged-in users. Enforced server-side; mirrored client-side.

**Backend (the boundary).** Add `"SubmitFeedback"` to `requiresPrincipal()` in
`internal/cli/server.go` so `RequirePrincipalMiddleware` wraps
`POST /api/tasks/{id}/feedback`. Effect:

- Cloud mode (`HasAuth()` true), signed-out browser → `401 sign in required`.
- Cloud mode, signed-in browser → passes (cookie principal present).
- Local mode (`HasAuth()` false) → no-op; feedback works as today.

This is the smallest faithful mirror of the spec-comments gate. The backend cannot
distinguish inline-composed feedback from Overview feedback (both are just a
`message` string), so a dedicated endpoint would be a near-duplicate that exists
only to carry the gate; gating the one feedback route covers both paths. No human
flow regresses: in cloud mode `ForceLogin` already redirects signed-out HTML
navigation to `/login`, so a signed-out browser never reaches `TaskDetail`; the
gate hardens the API against a direct unauthenticated call (and against a static
`WALLFACER_SERVER_API_KEY` request, which carries no browser principal — no caller
submits feedback that way). Acceptance: a `BuildMux`-level test asserts the route
401s without a principal when auth is configured and passes in local mode (mirror
the existing `requiresPrincipal` test in `handler_core_test.go`).

**Frontend (UX mirror, not a security gate).** Compute a single signal:

```ts
const canReview = computed(() => !authEnabled.value || !!auth.me);
// authEnabled = store.config?.auth_enabled === true
```

`canReview` is true in local mode (auth disabled) and in cloud mode when signed
in, and false only for a signed-out cloud browser — the same truth set as spec
comments' `available`, without a probe `GET`. The gutter, inline editor, and the
review panel render only when `isWaiting && canReview`. The existing Overview
feedback textarea keeps its current `isWaiting` gate; if a signed-out cloud user
ever reaches it, the backend 401 is the real stop.

## Components

### 0. Fix the feedback key bug (prerequisite, commit 1)

`TaskDetail.vue:730`: change the POST body from `{ feedback: text }` to
`{ message: text }` so the backend's `json:"message"` actually receives the text.
Add a frontend unit test asserting `submitFeedback` posts a body whose `message`
field carries the textarea text (fails before, passes after). The batch-submit
path below sends `message` for the same reason; this fix makes the existing
Overview path correct too.

### 1. Line anchoring — `frontend/src/lib/diff.ts`

`DiffLine` carries no line number, so comments need an anchor. Extend
`parseDiffFiles` to derive line numbers while walking each file's lines:

- Add `oldLine: number | null` and `newLine: number | null` to `DiffLine`.
- On a `hunk` line, parse `@@ -a,b +c,d @@` to seed the old/new counters.
- `add` → set `newLine`, advance the new counter; `oldLine` null.
- `del` → set `oldLine`, advance the old counter; `newLine` null.
- `ctx` → set both, advance both.
- `hunk` / `header` → both null.

A comment anchors on `(filename, lineIndex)` where `lineIndex` is the position in
`DiffFile.lines` (stable for a loaded diff, already the `v-for` key). The derived
`oldLine` / `newLine` are carried for display and for the formatted agent message,
not as identity. Anchoring on the array index keeps the gutter, panel, and
formatter all referencing the same `(filename, lineIndex)` pair. The highlighted
branch indexes `f.lines[li]` in parallel with `diffHighlights[fi]`, so the same
index works in both render branches.

Tests in `frontend/src/lib/diff.test.ts`: adds, dels, context, multi-hunk files,
headers, and that `hunk`/`header` lines carry null numbers. Existing diff render
output is unchanged (the new fields are additive).

### 2. Comment store — `frontend/src/stores/diffComments.ts`

New Pinia store, composition style (matching `frontend/src/stores/toast.ts`). Per
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

State: `Map<string, DiffComment[]>` keyed by task id. Actions: `add`,
`update(id, body)`, `remove(id)`, `clear(taskId)`; getters `forTask(taskId)` and
`forLine(taskId, filename, lineIndex)`. Pure client state; nothing persists across
reloads (the workflow is open diff → comment → submit). Unit tested for CRUD and
the per-line getter.

### 3. Batch formatter — `frontend/src/lib/diffComments.ts`

`formatBatchFeedback(comments: DiffComment[], general: string): string` collapses
the comments and the optional general message into one markdown string:

```
## Inline Review Comments

### internal/handler/execute.go

**Line 122** (`+ Message string `json:"message"``):
Use the message key here, not feedback.

### frontend/src/lib/diff.ts

**Line 8** (`  context line text`):
Carry old/new line numbers through here.

## General Feedback

Overall the anchoring looks right.
```

Use `newLine` for adds/context, `oldLine` for deletions. Group by filename in
first-seen order. Omit the General Feedback section when `general` is empty; omit
the Inline Review Comments section when there are no line comments; return empty
string when both are empty. Unit tested for: line comments only, general only,
both, none, and multi-file grouping. The output fits well within
`BodyLimitFeedback`.

### 4. Gutter, inline editor, and panel — `TaskDetail.vue` + `diffs.css`

Render only when `isWaiting && canReview`.

- **Gutter.** Make `.diff-block-modal .diff-line` `position: relative` and widen
  its left padding to open a gutter lane. For commentable lines
  (`add | del | ctx`; skip `hunk` / `header`), render an absolutely-positioned
  gutter `<button>` in that lane: a `+` on hover; a filled dot when the line
  already has a comment (`store.forLine(...)`). This keeps the `<pre>` text layout
  intact (no flex inside `<pre>`).
- **Inline editor.** Clicking the gutter opens a compact editor block directly
  under that line: a textarea plus Save / Cancel. Render it as its own
  `display: block` element after the `.diff-line` span inside the `<pre>` (it sets
  its own font-size, so the `font-size: 0` parent does not shrink it, and the
  inter-line `\n` text nodes stay zero-height). Save calls `store.add` (or
  `store.update` when editing an existing comment for that line); Cancel discards.
  Both render branches (highlighted and plain) need the gutter + editor, so factor
  the per-line render so both share it (a child `DiffLineRow` component with a
  fragment root, or a shared sub-template).
- **Panel.** Below the diff, a "Review comments (N)" panel lists
  `store.forTask(taskId)` grouped by filename. Each entry shows
  `filename:newLine` (or `:oldLine` for deletions), the truncated body, and
  Edit / Delete. Clicking an entry scrolls its line into view and flashes it.
  Above the submit button sits an optional general-feedback textarea. A
  "Submit N comments" button (N from the store) is disabled when there are no line
  comments and the general textarea is empty.

### 5. Batch submit — `TaskDetail.vue`

On "Submit N comments":

1. `formatBatchFeedback(store.forTask(taskId), general)` assembles the message.
2. POST `/api/tasks/{id}/feedback` with `{ message }` (the key the handler reads,
   and the key the prerequisite fix establishes) via the existing `api('POST', …)`
   call — the same resume path the Overview feedback uses.
3. On success: `store.clear(taskId)`, reset the editors and general textarea.

The Overview quick-feedback textarea remains for single-message feedback; the
inline flow is the Changes-tab path. They are mutually exclusive per submission and
share the one backend endpoint.

## Data Flow

1. User opens a waiting task → Changes tab loads the diff
   (`GET /api/tasks/{id}/diff`).
2. `parseDiffFiles` derives `oldLine`/`newLine`; the render adds gutters on
   commentable lines (only when `isWaiting && canReview`).
3. User clicks a gutter → inline editor opens → Save stores a `DiffComment`.
4. Panel updates; the gutter shows a dot on commented lines.
5. Repeat across files; optionally type general feedback.
6. "Submit N comments" → `formatBatchFeedback` → POST `{ message }` →
   `RequirePrincipalMiddleware` admits the signed-in request → runner resumes →
   `store.clear(taskId)`.

## API Surface

No new routes. `POST /api/tasks/{id}/feedback` with `{ message: string }` is
reused. The only backend change is gating it via `requiresPrincipal`. The
formatted message fits within `BodyLimitFeedback` (512 KiB).

## Phasing and Acceptance Criteria

1. **Key-bug fix (prerequisite).** `submitFeedback` posts `{ message }`; frontend
   test asserts the posted body's `message` carries the textarea text.
2. **Backend gate.** `"SubmitFeedback"` added to `requiresPrincipal`; `BuildMux`
   test: route 401s without a principal when auth is configured, passes in local
   mode. Existing handler unit tests (which bypass the mux) stay green.
3. **Line numbers.** `DiffLine` + `parseDiffFiles` carry `oldLine`/`newLine`; unit
   tests for adds, dels, context, multi-hunk, headers. Render output unchanged.
4. **Store + formatter.** `diffComments.ts` store (CRUD + getters) and
   `formatBatchFeedback` unit tested, including the four-way line/general matrix
   and multi-file grouping.
5. **Gutter, editor, panel, submit.** Acceptance:
   - Comments can be added on add / del / context lines across multiple files;
     hunk and header lines are not commentable.
   - The panel lists comments grouped by file with edit/delete; clicking scrolls
     to and flashes the line.
   - "Submit N comments" posts a single `{ message }`, the runner resumes, the
     store clears.
   - The gutter, editor, and panel appear only when `isWaiting && canReview`;
     a signed-out cloud browser sees none of it, and a submit it forces anyway is
     401'd by the backend.
   - `canReview` is true in local mode (auth disabled) so the feature works in
     single-user `wallfacer run`.

## Non-Goals

- Server-side persistence of individual comments. Comments are client-only and do
  not survive a reload.
- A dedicated feedback endpoint for the inline flow. The existing route is reused
  and gated.
- Threaded replies, reactions, or multi-round review state on diff comments.
- Commenting on card-level diffs or any surface outside the Changes tab.
- Backend feedback schema changes beyond the principal gate.

## Outcome

(To be filled after dispatch/implementation.)
