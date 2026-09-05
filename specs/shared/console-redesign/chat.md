---
title: Chat
status: drafted
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

- Existing `useChatSession.*.test.ts` cover behaviour; add
  `components/ChatMessageList.test.ts`: turn kinds render their classes, tool
  call rows collapse, streaming pill present while `streaming`.
- `tests/designSystem.test.ts`: `ChatPage.vue`, `SessionList.vue`,
  `SpecChatPopup.vue`, `multi-turn.css` have no hex literal, no
  `backdrop-filter`, no `border-radius` literal.
- `checks.mjs` scene `chat`: `/chat` renders the session list and composer,
  composer card is at the bottom of the main card, message column max width
  ≤ 760px, popup opens on `/routines` and stays inside the viewport.
  Screenshots `chat` light and dark, `chat-popup`.
