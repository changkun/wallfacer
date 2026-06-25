---
title: Deferred Chat Creation
status: drafted
depends_on: []
affects:
  - frontend/src/composables/useChatSession.ts
  - frontend/src/components/plan/SessionList.vue
  - frontend/src/components/plan/PlanningChatPanel.vue
  - frontend/src/components/plan/SpecChatPopup.vue
effort: small
created: 2026-06-25
updated: 2026-06-25
author: changkun
dispatched_task_id: null
---

# Deferred Chat Creation

## Problem

Clicking "New chat" immediately creates and persists a server thread named
`Chat <N>` (`internal/planner/threads.go` `nextDefaultName`). The AI auto-titler
(`maybeAutoTitleThread`, `internal/handler/planning.go`) only renames the thread
*after* the first message is sent. So if the user clicks "New chat" and never
sends anything, an empty, numbered `Chat 13` phantom sits in the session list
forever. The numbering is also user-visible churn the user dislikes.

The desired behavior: a session should only be tracked once a real conversation
has happened. Clicking "New chat" should open a blank conversation; the thread is
created (and AI-titled) only when the first message is sent.

## Current State

- Three surfaces call `chat.createThread()`: `SessionList.vue` (Chat-view
  sub-sidebar), `PlanningChatPanel.vue` (legacy/editor tabs), `SpecChatPopup.vue`
  (spec-mode popup). All route through `useChatSession.createThread`.
- `createThread()` (`useChatSession.ts:459`) POSTs `/api/planning/threads`
  immediately, reloads the thread list, and switches to the new thread.
- `sendMessage(text)` (`useChatSession.ts:290`) targets `activeThreadId`, guards
  on `!targetId` (no thread) and `streaming.value` (enqueue), optimistically
  pushes the user bubble when `targetId === activeThreadId`, POSTs the message,
  then `startStreaming()`.
- Backend already AI-titles: `maybeAutoTitleThread` runs on first message as long
  as the thread still has its default `Chat N` name. **No backend change needed
  for the core path** — keep creating the thread with the `Chat N` default so the
  titler still fires; the name is now transient (lives only between create and
  title-ready).

## Approach (frontend-only)

Centralize in `useChatSession`; all three surfaces inherit it.

1. **Draft state.** Add a `draft` ref. `createThread()` no longer POSTs — it
   enters draft mode: clears the active selection so the conversation shows blank
   and no server thread is created yet.

2. **Promote on first send.** `sendMessage(text)`, when `draft` is true, first
   POSTs `/api/planning/threads`, reloads threads, activates the new thread, then
   sends the message to it. The existing auto-titler names it from that message.

3. **UI affordance.** Highlight the "New chat" control while a draft is active so
   the user sees they are in a new (not yet saved) conversation.

## Interactions to get right (the real work)

1. **Draft entry while another thread is streaming.** `createThread` must
   replicate `switchToThread`'s detach: `clearRetryTimer()`, abort `streamHandle`,
   set `streaming.value = false`. Otherwise the streaming guard in `sendMessage`
   enqueues the first draft message into nowhere. (The background turn continues
   server-side via `busyThreadId` — that is fine.)

2. **Optimistic-bubble race.** `watch(activeThreadId)` fires `loadHistory()`
   fire-and-forget; for the fresh empty thread it resolves to `[]` and can wipe
   the just-pushed user bubble. Sequence promotion so the blank `loadHistory`
   settles *before* the optimistic push and the message POST.

3. **AI title surfaces live.** AI-titling is now the primary naming path, so the
   tab/row must flip from `Chat N` to the AI title without a reload. `finishStreaming`
   currently calls `loadHistory` (messages), not `loadThreads`, and `refreshBusy`
   does not disturb the thread list. Add a `loadThreads()` after the turn settles
   if the rename does not otherwise surface.

## Acceptance Criteria

- Clicking "New chat" shows a blank conversation and creates **no** server thread
  (verified: thread list count unchanged until a message is sent).
- Sending the first message in a draft creates exactly one thread, sends the
  message, streams the reply, and the user bubble survives into streaming.
- The new session's name flips from `Chat N` to the AI-generated title after the
  first turn, with no manual reload.
- Entering a draft while another thread is mid-stream then sending works (message
  is sent, not silently enqueued).
- Existing surfaces (sub-sidebar, tabs, spec popup) all defer creation.

## Out of Scope

- Backend `migrateOrInit` still seeds an empty `Chat 1` on a brand-new workspace.
  The handlers assume an active thread exists (wide blast radius), so this is left
  as accepted scope. Revisit only if the first-ever empty chat bothers the user.

## Verification

Value-only store tests pass while the UI is frozen (known happy-dom blind spot),
so verify in a real browser: (a) draft is blank and nothing persists until send,
(b) the user bubble survives the promote-and-send sequence, (c) `Chat N` flips to
the AI title after the first turn.
