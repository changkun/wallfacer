---
title: Chat
status: complete
depends_on:
  - specs/shared/console-redesign/shell.md
affects:
  - frontend/src/views/ChatPage.vue
  - frontend/src/components/plan/AgentChatPanel.vue
  - frontend/src/components/plan/ChatMessageList.vue
  - frontend/src/components/plan/ChatComposer.vue
  - frontend/src/components/plan/ChatModelBadge.vue
  - frontend/src/components/plan/SpecChatPopup.vue
  - frontend/src/components/plan/SessionList.vue
  - frontend/src/styles/multi-turn.css
effort: medium
created: 2026-09-05
updated: 2026-09-05
author: changkun
dispatched_task_id: null
---

# Chat

## Overview

The agent conversation surface, used standalone on `/chat`, inside the Plan
page, and as the floating popup on every other route. One message list, one
composer, one session list, restyled once and shared by all three hosts.

## Current State

- `ChatPage.vue` (453 lines, 237 scoped): session list left, chat right.
- `plan/AgentChatPanel.vue`, `ChatMessageList.vue`, `ChatComposer.vue` (styles
  in `multi-turn.css`, 322 lines): message bubbles, tool call blocks, model
  badge, streaming state, composer with mentions and model select.
- `SessionList.vue` (477 lines, 282 scoped): session rows with title, age,
  turn count, running state.
- `SpecChatPopup.vue` (191 scoped): the floating trigger button and popup card
  bottom right.

## Components

### Message list

Messages are not bubbles. User turns are a `.card-pad` block on `--bg-sunk`
with the author `.eyebrow`; assistant turns are prose on `--bg` with the model
as `.pill-neutral` in the eyebrow row; tool calls are collapsible `.rows`
inside a `.card` (glyph, mono name, duration, state pill), matching the
`AgentTrace` shape from the task-detail child so a trace reads the same in
both places. Streaming shows `.pill-run.pulse`. Code blocks use the
`syntax.css` ramp.

### Composer

A `.card` pinned to the bottom with the textarea borderless inside, and a
footer row: model select as `.field` (or `ChatModelBadge` as `.pill-neutral`
when fixed), mention hint `.muted`, send as `.btn.sm`, stop as
`.btn.sm.ghost.danger` while streaming. Mentions popover is a `.card` with
`--sh-pop` and `.rows`.

### Session list

`.rows` inside the left column on `--bg-deep`-adjacent `--bg-sunk`: title,
meta line `turns · age`, running dot. Active row is `--bg-card` with
`--sh-card`, the same treatment as the rail's active nav row. New session is
a `.btn.ghost` full width at the top.

### Popup

The trigger is a 44px `.btn` circle with the chat glyph (ink, inverts in
dark); the popup is a `.card` 420×560 with `--sh-pop`, the message list and
composer inside, close as `.icon-btn`.

## Testing Strategy

- Existing `useChatSession.*.test.ts` cover behavior; add
  `components/ChatMessageList.test.ts`: turn kinds render their classes, tool
  call rows collapse, streaming pill present while `streaming`.
- `tests/designSystem.test.ts`: `ChatPage.vue`, `SessionList.vue`,
  `SpecChatPopup.vue`, `multi-turn.css` have no hex literal, no
  `backdrop-filter`, no `border-radius` literal.
- `checks.mjs` scene `chat`: `/chat` renders the session list and composer,
  composer card is at the bottom of the main card, message column max width
  ≤ 760px, popup opens on `/routines` and stays inside the viewport.
  Screenshots `chat` light and dark, `chat-popup`.

## Outcome

**Status:** complete, 2026-09-05. Commit `65e802a3`.

**What shipped.** `ChatMessageList.css` renders a user turn as a block on the
sunk surface with a "You" eyebrow, the finished trajectory as a card of rows
(the agent-trace shape), a `pill-run pulse` "working" pill beside the live
title, errors on the err tint, and code on the radii ladder.
`ChatComposer.css` makes the composer a card with a borderless prompt, the
send as the ink button, the stop as the quiet danger ghost, the `/` and `@`
actions as small icon buttons, and the dropdown a popover card.
`AgentChatPanel.css` sets the panel on `--bg` with eyebrow title, pill thread
tabs and ghost controls. `SessionList.vue` is the sunk column with nav-row
geometry, the active session on the card surface, a dashed ghost "New chat",
eyebrow group heads and counts. `ChatPage.vue` drops the ember glow, sizes
the greeting on the type scale, pills the quick actions, and narrows the
stream to 760px. `SpecChatPopup.vue` is a 44px ink launcher and a card
window. `multi-turn.css` is on tokens and its id-addressed mobile modal rules
are replaced by sheet rules. `checks.mjs` `chat` asserts the list, the
composer, the entry width and the popup inside the viewport; `make ui-test`
passes thirteen scenes. `ChatMessageList.test.ts` covers the working pill,
the user block and the error tint.

**Decisions made during implementation.**
- The streaming title keeps its "Working…" text (an existing test pins it);
  the pill sits beside it rather than replacing it.
- The send stays a split button (send + shortcut toggle) on ink rather than a
  bare `.btn.sm`: the toggle is part of the same affordance.
- The chat popup keeps its drag and resize chrome; only material changed.

**Deviations from the spec.** The user turn's author eyebrow is a CSS
pseudo-element ("You") rather than template markup, so the message list's
render path is untouched.

**Follow-ups.** None beyond the sibling specs.
