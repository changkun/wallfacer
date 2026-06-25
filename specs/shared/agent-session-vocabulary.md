---
title: Agent Session Vocabulary (Generalize "Planning")
status: drafted
depends_on: [shared/agent-abstraction.md, shared/harness-abstraction.md]
affects: [internal/planner/, internal/handler/, internal/apicontract/, internal/envconfig/, frontend/src/stores/, frontend/src/components/plan/, frontend/src/composables/, frontend/src/lib/, docs/, AGENTS.md]
effort: xlarge
created: 2026-06-25
updated: 2026-06-25
author: changkun
dispatched_task_id: null
---


# Agent Session Vocabulary (Generalize "Planning")

## Problem Statement

"Planning" was the first interactive chat surface we shipped, so the
machinery behind it was named after that one use: `internal/planner/`,
`PlanningThread`, `usePlanningStore`, `/api/planning/*`,
`PlanningChatPanel`. Since then the same machinery has spread well past
spec planning, it is now a generic chat window that shows up across tabs
and contexts:

- **spec mode** - edit a spec file, commit to `specs/` (the original use)
- **task mode** - refine a task's prompt (`focused_task`, no spec)
- the **dedicated `/chat` page** (`ChatPage.vue`) reuses the same store,
  composables, and `/api/planning/*` endpoints with no planning involved

The conversation/thread/message/command layer is already generic; only
its name still says "planning". The mislabel actively misleads: a reader
sees `usePlanningStore` on the standalone chat page and assumes a
spec-planning dependency that is not there. The frontend even half-fixed
this already by extracting `useChatSession` / `ChatComposer` /
`ChatMessageList` / `ChatPage`, but the data model and store underneath
them are still `Planning*`.

This spec generalizes the chat machinery onto a single **agent session**
vocabulary, while leaving genuinely spec-planning code (and anything
frozen into git history) named "plan".

## The Seam

Three buckets. The whole refactor hinges on cutting along this seam and
not over-reaching.

### 1. Generalize -> `AgentSession` / agent

Generic chat machinery that got mislabeled "planning". This is the work.

- Backend: `internal/planner/` package - `ThreadManager`, `ThreadMeta`,
  `ConversationStore`, `Message`, `CommandRegistry`, `RoundUsage`, the
  `Planner` process manager.
- Handlers: `planning.go`, `planning_threads.go`, `planning_tool.go`,
  and their methods (`SendPlanningMessage`, `ListPlanningThreads`, ...).
- Routes: `/api/planning/*`.
- Frontend: `stores/planning.ts` (`usePlanningStore`, `PlanningThread`,
  `PlanningMessage`), `PlanningChatPanel.vue`, `usePlanningAutocomplete`,
  `lib/planningBubble.ts`, `lib/planningUsage.ts`, `PLANNING_CHAT_ENABLED`.
- Stats: `PlanningGroupStat`, `aggregatePlanningStats`, the `Planning`
  map key.
- Config/storage contracts (with migration): `WALLFACER_PLANNING_WINDOW_DAYS`,
  `<configDir>/planning/<fingerprint>/`, `~/.wallfacer/planner/threads/`,
  localStorage keys.

### 2. Stays "plan" - genuinely about specs

Not a mislabel. The spec-design surface really is planning.

- `/plan` route, `PlanPage.vue`, the "Plan" sidebar item, the "Plan" /
  "Send to Plan" card action (`cardActions.ts`).
- Spec-mode commit logic: `planning_git.go` (`commitPlanningRound`),
  `planning_directive.go` (parses `/spec-new`), `selectPlanningSystemPrompt`.
- `mode: 'spec'` and the `FocusedSpec` field.

These keep "plan" naming even though they now hang off an `AgentSession`
in spec mode. "Plan" is the user-facing name of the spec-design tab and
stays.

### 3. Frozen - baked into git history, cannot rename

`UndoPlanningRound` reverts a past round by parsing trailers out of
**existing commits** (`planRoundTrailer`, `planThreadTrailer`,
`planCommitSubject` in `planning_undo.go`):

```
Subject:  <path>(plan): <imperative>
Trailers: Plan-Round: N
          Plan-Thread: <id>
```

`commitPlanningRound` writes these and `git log --grep="^Plan-Round: "`
reads them back. Renaming the prefix breaks undo on every repo that
already has plan commits, because we cannot rewrite users' history. The
constants `planRoundTrailerPrefix`, `planThreadTrailerPrefix`,
`planCommitScope` and the regexes that match them **do not change**. They
are spec-plan commits anyway, so "plan" is also the correct name.

## Naming Decision

The conversation container becomes an **`AgentSession`**; the store
becomes **`useAgentStore`**. This is collision-aware:

| Word | Already means | Our use |
|------|---------------|---------|
| `agents.Role` (`internal/agents/`, `/api/agents`) | the role catalog (implementation, testing, ...) - an actor *definition* | unchanged |
| harness `SessionID` (`harness.Request`, `--resume`) | the CLI resume token | unchanged; renamed `SessionInfo` -> `ResumeInfo` to free the word |
| **`AgentSession`** (this spec) | a running interactive conversation a user drives across turns | the thing we are renaming `PlanningThread`/`planner` into |

The mental model the user gave: an **agent** (an `agents.Role`) **runs
sessions** (`AgentSession`) **on tabs** (spec mode, task mode, the chat
page). We are renaming the *session container*, not the agent. "Agent" is
the user-facing chip/label; `AgentSession` is the internal type.

To keep "session" from overloading, the harness resume holder
`planner.SessionInfo` is renamed `ResumeInfo` (it holds the `--resume`
token, not an `AgentSession`).

## Identifier Mapping

### Backend - `internal/planner/` -> `internal/agentsession/`

| Now | Proposed |
|-----|----------|
| package `planner` | package `agentsession` |
| `Planner` (live process manager) | `Runner` |
| `ThreadManager` (owns the session set) | `Manager` |
| `ThreadMeta` | `SessionMeta` |
| `ConversationStore` | `ConversationStore` (keep - already generic) |
| `Message`, `RoundUsage`, `Command`, `CommandRegistry` | keep (already generic) |
| `SessionInfo` (harness `--resume` holder) | `ResumeInfo` |
| const `planningTaskID = "planning-sandbox"` | `agentSessionTaskID = "agent-session"` |

### Backend - handlers (`internal/handler/`)

| Now | Proposed |
|-----|----------|
| `planning.go`, `planning_threads.go`, `planning_tool.go` | `agentsession.go`, `agentsession_sessions.go`, `agentsession_tool.go` |
| `Get/Start/StopPlanning` | `Get/Start/StopAgentSession` |
| `Get/Send/Stream/Clear/InterruptPlanningMessage(s)` | `...AgentMessage(s)` |
| `GetPlanningCommands` | `GetAgentCommands` |
| `List/Create/Patch/DeletePlanningThread` | `List/Create/Patch/DeleteAgentSession` |
| `PlanningGroupStat`, `aggregatePlanningStats`, stats key `Planning` | `AgentSessionGroupStat`, `aggregateAgentSessionStats`, key `AgentSessions` |
| `planning_git.go`, `planning_undo.go`, `planning_directive.go`, `selectPlanningSystemPrompt`, `commitPlanningRound`, `UndoPlanningRound` | **unchanged** (bucket 2/3 - spec-plan) |

### Frontend (`frontend/src/`)

| Now | Proposed |
|-----|----------|
| `stores/planning.ts` | `stores/agentSession.ts` |
| `usePlanningStore` | `useAgentStore` |
| `PlanningThread` | `AgentSession` |
| `PlanningMessage` | `AgentMessage` |
| `components/plan/PlanningChatPanel.vue` | `AgentChatPanel.vue` |
| `composables/useChatSession.ts` controller iface `ChatSession` | `useAgentChat.ts` returning `AgentChat` (controller view-model; distinct from the `AgentSession` data model) |
| `usePlanningAutocomplete` / `PlanningAutocomplete` | `useAgentAutocomplete` / `AgentAutocomplete` |
| `lib/planningBubble.ts`, `lib/planningUsage.ts` | `lib/agentBubble.ts`, `lib/agentUsage.ts` |
| `PLANNING_CHAT_ENABLED` | `AGENT_CHAT_ENABLED` |
| `ChatComposer.vue`, `ChatMessageList.vue`, `SessionList.vue` | keep (already generic presentational) |
| `PlanPage.vue`, `/plan`, "Plan" labels, `cardActions.plan` | keep (bucket 2 - spec-design surface) |

## Wire / Disk Contract Migration

Decision: **migrate everything**. Rename the contracts and ship a
one-time shim so no existing state is orphaned.

- **Routes** `/api/planning/*` -> `/api/sessions/*`. (Chosen over
  `/api/agent/*` to avoid sitting next to the existing `/api/agents`
  role-catalog routes.) Routes ship with the embedded SPA, so renaming is
  low risk; regenerate `internal/apicontract` as its own commit.
- **Env** `WALLFACER_PLANNING_WINDOW_DAYS` ->
  `WALLFACER_AGENT_SESSION_WINDOW_DAYS`; config key `planning_window_days`
  -> `agent_session_window_days`. Read the old name as a fallback for one
  release so existing configs keep working.
- **On-disk** `<configDir>/planning/<fingerprint>/` ->
  `<configDir>/agent-sessions/<fingerprint>/`, and
  `~/.wallfacer/planner/threads/` -> the new layout. Migration shim on
  first boot: if the old dir exists and the new one does not, rename it.
  Idempotent, logged.
- **localStorage** thread/tab keys: read old key, write new key, delete
  old, once per browser.

## Open Sub-Decisions (resolve during breakdown)

1. **Route shape** - `/api/sessions/*` (proposed) vs `/api/agent/*`. The
   former avoids confusion with `/api/agents`; the latter matches
   `useAgentStore` more literally.
2. **Controller name** - `useAgentChat` returning `AgentChat` (proposed,
   keeps the controller distinct from the `AgentSession` data model) vs
   reusing `AgentSession` for both (clashes).
3. **Greeting strings** - "What should we plan?" / "Message the planning
   agent..." -> generic ("Message the agent...") vs context-aware per
   surface. i18n keys in `wf.tour.*` / `wf.cap.*` likewise.

## Implementation Plan

Small, layered commits; `make build` (golangci-lint) is the gate after
each (it is what catches symbols a rename leaves unused). Prefer
`gopls`/`gorename` over `sed` for Go identifiers. Update `AGENTS.md` and
`docs/` in the same commit as the layer they describe.

1. **Backend types** - rename `internal/planner/` -> `internal/agentsession/`,
   the types above, `SessionInfo` -> `ResumeInfo`. Keep routes/storage
   names untouched this commit. Gate: `make build` + package tests
   (rename `planner` test files alongside).
2. **Backend handlers** - rename handler methods + files (bucket 1 only;
   leave `planning_git.go` / `planning_undo.go` / `*directive*` alone).
   Gate: handler tests.
3. **Routes + api-contract** - rename `/api/planning/*` -> `/api/sessions/*`
   in `routes.go`, regenerate `internal/apicontract`. Separate commit.
4. **Storage + env migration** - new dir layout + first-boot shim, env var
   rename with old-name fallback. Test: shim moves a seeded old dir;
   old env var still honored.
5. **Frontend store + types** - `stores/agentSession.ts`, `useAgentStore`,
   `AgentSession`, `AgentMessage`; point all call sites at new API routes.
6. **Frontend components/composables/lib** - `AgentChatPanel`,
   `useAgentChat`/`AgentChat`, `useAgentAutocomplete`, `agentBubble.ts`,
   `agentUsage.ts`, `AGENT_CHAT_ENABLED`, localStorage migration.
7. **User-facing strings + i18n** - generic agent wording; resolve
   sub-decision 3.
8. **Docs sweep** - `AGENTS.md`, `docs/`, and `specs/README.md` status
   table; mark this spec complete.

## Acceptance Criteria

- No `Planning*` / `usePlanningStore` / `/api/planning` identifiers remain
  except the bucket-2 spec-plan code (`planning_git.go`,
  `planning_undo.go`, `planning_directive.go`, `commitPlanningRound`,
  `selectPlanningSystemPrompt`) and the bucket-3 frozen git trailers.
- `make build` passes (lint catches no orphaned symbols).
- `UndoPlanningRound` still reverts rounds on a repo whose commits predate
  the rename (regression test: seed a commit with the old `Plan-Round:`
  trailer, assert undo finds and reverts it).
- Storage migration shim moves a seeded `<configDir>/planning/<fp>/` to the
  new path on boot; old `WALLFACER_PLANNING_WINDOW_DAYS` still honored.
- The dedicated `/chat` page works with zero references to "planning".
- `/plan`, `PlanPage`, and the "Plan" tab label are unchanged.

## Risks

- **Over-reach into the frozen seam.** A blanket find/replace on "plan"
  would rewrite the git-trailer constants and break undo. The plan is
  explicitly bucketed to prevent this; the regression test in acceptance
  criteria is the backstop.
- **Concurrent edits.** The user edits this tree concurrently; stage
  explicit paths per commit, never `git add -A`.
- **api-contract drift.** Regenerate as its own step; a stale contract
  silently breaks the SPA's typed client.
