---
title: UI Per-Message Undo
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/undo-snapshots/undo-api.md
affects:
  - ui/js/planning-chat.js
  - ui/css/spec-mode.css
  - ui/js/tests/planning-chat.test.js
  - internal/planner/conversation.go
  - internal/handler/planning.go
  - internal/handler/planning_git.go
effort: small
created: 2026-04-04
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# UI Per-Message Undo

## Goal

Every assistant message in the planning chat that is associated with a
`plan: round N` commit gets a **tiny inline undo button**. The chat stream is
append-only: clicking undo does **not** remove any existing bubbles — instead,
a new system-style bubble is appended to the end of the stream announcing what
was undone, so the history of "what the agent did, and what I reverted" stays
visible and self-describing.

This replaces the previous single-button-in-header design. The new affordance
is a per-message action, matching how modern chat UIs (Slack, Discord) expose
message-level operations.

## UX Principles

1. **Per-message affordance.** Each assistant bubble that triggered a commit
   renders a small `⟲` icon button in its action row (top-right of the
   bubble, similar to copy/retry affordances in other chat UIs). Not a giant
   button — unobtrusive, discoverable on hover.
2. **Append-only stream.** Bubbles are never removed from the DOM. Undo
   appends; it does not delete. Users can scroll back and see everything the
   agent did, including things that were later reverted.
3. **Self-narrating undo.** Each successful undo appends a system bubble like
   *"↶ Undid round 3 — drafted foo spec. Reverted: specs/foo.md"*, formatted
   as a distinct role (`system` or similar) so it's visually clear these
   aren't agent outputs.
4. **Reverted bubbles are visually dimmed.** The original assistant bubble
   whose commit was reverted stays in place but gets a `--reverted` modifier
   class (strikethrough title, reduced opacity, small "reverted in round
   ↓system-N" caption). This makes the stream self-documenting without
   requiring the user to reconstruct the timeline.

## What to do

### 1. Remove the header Undo button (from the earlier design)

The previous spec placed a single `#spec-chat-undo` in
`.spec-chat-stream__header`. Do **not** add that button; the header stays at
its current state (Clear + Close only, per `ui/partials/spec-mode.html`).

### 2. Per-bubble undo button

In `ui/js/planning-chat.js`, extend the assistant bubble rendering (around
line 534, where `planning-chat-bubble--assistant` is built) to include an
action row with an undo button:

```html
<div class="planning-chat-bubble planning-chat-bubble--assistant"
     data-round="3">
  <div class="planning-chat-bubble__content">…</div>
  <div class="planning-chat-bubble__actions">
    <button class="planning-chat-bubble__undo"
            title="Undo this round"
            aria-label="Undo round 3">⟲</button>
  </div>
  <div class="planning-chat-bubble__time">…</div>
</div>
```

- Add the button to **every** assistant bubble that has an associated planning
  round (see §3 for how the UI learns that).
- Assistant bubbles from rounds with no writes (no planning commit) do **not**
  get the button.
- Only the most recent planning-round bubble has the button enabled; older
  bubbles render it disabled with `title="Only the most recent round can be
  undone"` (see §4 for the reasoning).
- CSS lives in `ui/css/spec-mode.css` — small icon-sized button, appears on
  bubble hover, fades when disabled.

### 3. Round attribution per message

The UI needs to know which assistant bubble corresponds to which
`plan: round N` commit. Two viable sources of truth — pick one:

- **Option A: server attaches round metadata to the message record.** Extend
  `planner.Message` with an optional `plan_round int` field. In
  `internal/handler/planning.go`, after `commitPlanningRound` succeeds,
  capture the round number (derivable with the same `git log --grep='^plan: round'`
  count the commit helper already does) and set it on the assistant
  `Message` before `cs.AppendMessage(...)`. `GetPlanningMessages` returns it
  verbatim. The UI renders the undo button only when `plan_round > 0`.
- **Option B: UI infers round from the message history.** The UI queries
  `GET /api/planning/commits` (a new read-only endpoint that returns the
  list of `{round, summary, commit_hash, timestamp}` tuples) and matches by
  timestamp / ordering. Heavier client-side logic and one extra endpoint.

**Prefer Option A.** Keeps attribution authoritative on the server; the UI
just reads a scalar. Adds ~5 lines in `planning.go` and one field to the
persisted message record. The schema addition is backward-compatible because
it's an optional int field that defaults to zero for pre-existing messages.

### 4. Which bubbles' buttons are enabled

The existing `POST /api/planning/undo` only reverts the planning commit at
HEAD (with a safety check refusing the reset when it isn't). That means, at
any moment, only **one** round — the latest — is actually revertible without
destroying intermediate history.

- UI reflects this by enabling the undo button only on the latest-round
  bubble (the one whose `data-round` value is the max among all visible
  assistant bubbles). All other round-bearing bubbles render the button
  disabled with the tooltip above.
- When a new planning commit lands (server appends a new assistant bubble),
  the UI re-computes which bubble is latest: move the enabled state to the
  new bubble, demote the previous one to disabled.
- When an undo succeeds, the reverted bubble gets the `--reverted` class
  *and* loses its undo button entirely (that round no longer exists). The
  previous round's bubble (if any) becomes the new enabled candidate and
  regains its button — since after `git reset --hard HEAD~1`, *its* commit
  is now at HEAD.

This model keeps the affordance per-message without requiring the
complexity of a targeted-undo server endpoint. The Open Questions section
below notes when we might want to extend the server.

### 5. Appended system bubble on undo

On a successful `POST /api/planning/undo` response, append a new bubble with a
new role class `planning-chat-bubble--system` (or equivalent) to the end of
the stream. The content is rendered inline — not a modal:

```
↶ Undid round 3 — drafted foo spec
  Reverted files:
  • specs/foo.md
  • specs/bar.md
```

- Use the `round`, `summary`, and `files_reverted` fields from the undo
  response.
- Do **not** persist the undo announcement to the server conversation store
  in this iteration — it's a client-side visual marker derived from the
  response. (If persistence is later deemed useful for cross-session
  visibility, it belongs in a follow-up spec.)
- The previous "original" bubble gains the `--reverted` class so the user
  can immediately see which agent turn's work is no longer on disk.

### 6. Error handling

- On 409 with `error: "no planning commits to undo"` — silently disable the
  button (the UI shouldn't have offered it; this is a defensive catch).
- On 409 with `error: "latest planning commit is not at HEAD ..."` — append
  a system bubble:
  *"⚠ Can't undo: you have unrelated commits since the last planning round.
  Resolve manually before using undo."*
- On 409 with `error: "stash pop conflict after undo; stash retained ..."` —
  append:
  *"⚠ Undo partially applied: git reset succeeded but your working-tree
  edits couldn't be reapplied cleanly. Your changes are preserved in the
  stash — run `git stash list` to recover."*
- On 5xx or network failure — append a transient error bubble, re-enable
  the originating button after 4 seconds.

### 7. Remove the keyboard shortcut

The earlier design suggested binding `u` to trigger undo in spec mode. With
the per-message affordance, this is ambiguous (undo which message?). Skip
the keyboard shortcut — users click the button on the target bubble. If a
shortcut is later desired, "latest round only" semantics can be revisited.

## Tests

Frontend (vitest under `ui/js/__tests__/`):

- `test_undo_button_not_rendered_on_user_bubble` — user messages never show
  the button.
- `test_undo_button_not_rendered_on_roundless_assistant_bubble` — assistant
  bubbles with no `plan_round` metadata (no writes) show no button.
- `test_undo_button_enabled_on_latest_round_only` — with three round-bearing
  assistant bubbles in the stream, exactly one (the latest) has the button
  enabled; the other two are disabled with the correct tooltip.
- `test_undo_button_promotes_after_append` — simulate a new assistant bubble
  arriving; the previously-latest bubble's button becomes disabled, the new
  bubble's becomes enabled.
- `test_undo_success_dims_original_appends_system_bubble` — stub `fetch` to
  return 200 with `{round: 3, summary: "drafted foo", files_reverted: […], workspace: "/ws"}`,
  click the button, verify (a) the originating bubble gains the `--reverted`
  class, (b) a new system bubble with the expected text is appended, (c) no
  existing bubbles are removed.
- `test_undo_conflict_not_at_head_appends_warning_bubble` — stub 409 with the
  not-at-HEAD error, verify warning bubble is appended and the originating
  button is re-enabled.
- `test_undo_stash_conflict_appends_stash_warning` — similar for the stash
  pop conflict branch.
- `test_undo_network_error_transient_notice` — stub `fetch` to reject,
  verify a transient system bubble appears and the button is re-enabled
  after the retry window.

Backend (only if Option A in §3 is taken):

- `TestSendPlanningMessage_AttachesPlanRound` — mock the planner to write a
  spec file and return a summary; verify the assistant message written to
  the conversation store has `plan_round = N` where N matches the newly-
  created planning commit's count.

## Boundaries

- Do **not** remove any existing bubbles from the DOM — the chat is an
  append-only log.
- Do **not** add a global header Undo button.
- Do **not** modify the existing Clear / Send / Interrupt / @ / / buttons.
- Do **not** purge server-side messages on undo (the undo is a git operation,
  not a conversation-log operation).
- `reloadSpecTree()` may be called best-effort after a successful undo to
  refresh the explorer; if the function isn't in scope, skip silently.
- Per-round targeted undo (revert a middle round while preserving later
  ones) is out of scope here; see the open question below.

## Open Questions

Flag for reviewer before this spec returns to `validated`:

1. **Targeted undo for non-HEAD rounds.** The UX shows a disabled button on
   older round-bearing bubbles with a tooltip. Is that the right behaviour,
   or should clicking an older bubble trigger a `git revert <commit>` that
   creates a new reversing commit (potentially conflicting with later
   rounds)? Tentative: stick with "latest only" until we see real demand —
   the append-only history already gives the user a clear recovery path
   (click undo repeatedly to walk backward round by round).
2. **Round attribution storage.** §3 recommends Option A (server attaches
   `plan_round` to the message). Confirm this is acceptable — adds one
   optional int to the persisted message schema and ~5 lines in the handler.
3. **System-bubble persistence.** Should the "↶ Undid round N" announcements
   be written to the server conversation store so they survive page
   reloads and cross-session visibility? Current spec says no (client-side
   only); persisting them would require a small server change and a new
   message role. Tentative: defer to a follow-up.
4. **Reverted-bubble styling.** Strikethrough + dim is the sketch; may want
   a subtler treatment (thin caption only, no opacity change) if reverted
   bubbles turn out to be too visually intrusive. Design-call, not
   architectural.
5. **Focus on the newly-enabled button after undo.** When undo succeeds
   and the previous bubble's button becomes the new "latest enabled",
   should we focus it automatically so a second undo is one keystroke
   away? Accessibility tradeoff — easy to add, easy to remove.

## Implementation notes

Implementation landed in three commits (`b2e8424` server, `82edcdc` UI, plus this
spec wrap-up). Deviations from the spec, in the order they came up:

- **Round-number plumbing via changed signature, not a new helper.** The spec
  suggested extracting a shared `currentRoundNumber(ctx, ws)` helper. Instead,
  `commitPlanningRound` itself now returns `(int, error)` — the round number
  was already computed internally; returning it is cheaper than a second
  `git log` pass. Callers use `n, err := commitPlanningRound(...)`; zero means
  no commit was made (clean tree or git-status failure).
- **Multi-workspace round attribution uses max, not a per-workspace record.**
  `SendPlanningMessage` iterates `h.currentWorkspaces()` and attaches
  `max(round)` across all successful commits to the assistant `Message`. The
  UI only needs a single "is this bubble associated with a planning commit,
  and what label to show" signal; recording per-workspace rounds would add
  schema churn with no UI benefit at this scale. Documented on the struct
  field.
- **History refetch after streaming.** The spec didn't prescribe how the
  streamed assistant bubble (created before the round is known) picks up
  its `plan_round`. Simplest approach: call `_loadHistory()` at the end of
  `_stopStreaming(interrupted=false)`. The entire chat re-renders from
  server-authoritative data; the visual flicker is imperceptible because
  the content matches. Interrupted streams skip the refetch (no committed
  round to attribute).
- **Undo button wiring lives in JS, not HTML.** The spec mentioned
  `ui/partials/spec-mode.html` in `affects`, but the partial doesn't
  render per-bubble markup — bubbles are built in `_createBubble` /
  `_appendMessageBubble`. The only HTML-side change would have been the
  now-rejected header Undo button. `affects` updated accordingly:
  `ui/js/planning-chat.js` + `ui/css/spec-mode.css` cover the whole UI
  layer.
- **Helper split.** Added three small private helpers alongside
  `_appendMessageBubble`:
  `_applyTimestamp` (extracted from the duplicated timestamp-rendering path
  in both `_appendMessageBubble` and `_appendMessageBubbleWithActivity`),
  `_attachUndoIfRound` (decorates an assistant bubble with its round
  attribute + undo button), and `_updateUndoButtonStates` (promotes the
  latest-round bubble's button to enabled, demotes the rest). Keeping the
  helpers explicit kept `_appendMessageBubble*` readable.
- **Error-branch text.** Two 409 paths get targeted copy — `⚠ Can't undo:
  you have unrelated commits since the last planning round` and `⚠ Undo
  partially applied: git reset succeeded but your working-tree edits
  couldn't be reapplied cleanly. Your changes are preserved in the stash —
  run `git stash list` to recover` — matched against the server error
  string (`indexOf("not at HEAD")`, `indexOf("stash pop conflict")`) rather
  than HTTP status alone, since both are 409s. Matches the server's
  response shape exactly.
- **Test-harness extension instead of new file.** The new tests live in
  `ui/js/tests/planning-chat.test.js` alongside the existing 8. The
  harness's `makeEl` stub was extended with `setAttribute`, `getAttribute`,
  `hasAttribute`, `removeAttribute`, a proper `remove()` that unlinks from
  the parent's child list, a `click()` helper that dispatches the click
  listener list, and a `querySelectorAll` that understands the
  `.class[attr]` compound selector needed for
  `.planning-chat-bubble--assistant[data-round]`. No existing test depended
  on the previous stub's limitations, so the extensions are
  backward-compatible.
- **Open Questions resolved (tentative answers adopted).** Q1 targeted
  undo: deferred (button disabled on non-HEAD rounds with explanatory
  tooltip). Q2 round attribution: Option A (server-side `PlanRound` on
  `Message`). Q3 system-bubble persistence: client-side only, not written
  to the conversation store. Q4 reverted-bubble styling: `opacity 0.5 +
  line-through` on the content element only (kept actions unaffected).
  Q5 auto-focus after undo: not implemented — leave the focus on the
  current cursor position.
