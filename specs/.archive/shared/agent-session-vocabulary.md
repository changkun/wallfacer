---
title: Agent Session Vocabulary (Generalize "Planning")
status: archived
depends_on: [shared/agent-abstraction.md, shared/harness-abstraction.md]
affects: [internal/agentsession/, internal/handler/, internal/apicontract/, internal/envconfig/, internal/store/, frontend/src/stores/, frontend/src/components/plan/, frontend/src/components/analytics/, frontend/src/composables/, frontend/src/lib/, docs/, AGENTS.md]
effort: xlarge
created: 2026-06-25
updated: 2026-06-26
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
| `Planner` (live process manager) | `Runtime` (not `Runner` — collides with `runner.Runner`) |
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

## Resolved Sub-Decisions

1. **Noun** - **Agent** / **AgentSession** (user's call, over the earlier
   Chat\* proposal). The conversation container is `AgentSession`; the
   store is `useAgentStore`. `agents.Role` and harness `SessionID` keep
   their meanings; `SessionInfo` -> `ResumeInfo` frees "session".
2. **Route shape** - **`/api/agent/*`** (matches `useAgentStore`; singular
   distinguishes it from the `/api/agents` role catalog), and the
   `/threads` subpath -> `/sessions` to match `AgentSession`.
3. **Controller name** - **keep `useChatSession` / `ChatSession`** as
   Chat\*. The surface-vs-actor split: presentation stays Chat\*
   (`ChatComposer`, `ChatMessageList`, `ChatPage`, `SessionList`,
   `useChatSession`); domain/state/persistence becomes Agent\*. This
   avoids a worse Agent\*/Chat\* mix and saves churn.
4. **Greeting strings** - generic ("Message the agent...", "What should we
   work on?"). Spec-design product copy (tour / capabilities / docs i18n,
   the analytics "Planning" cost label) left as legitimate planning copy.

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

## Progress (2026-06-25)

Shipped on `main` (each commit build + lint + tests green; pinned at
`wip/agent-session`). Migration reordered to last per review.

- ✅ **Backend package** - `internal/planner/` -> `internal/agentsession/`
  (package, imports, qualifiers; field accesses preserved).
- ✅ **Backend types** - `Planner`->`Runtime`, `ThreadManager`->`Manager`,
  `ThreadMeta`->`SessionMeta`, `SessionInfo`->`ResumeInfo`,
  `Threads()`->`Sessions()`, `planningTaskID`->`agentSessionTaskID`
  (value unchanged); files `planner.go`/`threads.go` ->
  `runtime.go`/`sessions.go`.
- ✅ **Backend handlers** - 13 exported chat handlers + `Route.Name` /
  handler-map keys renamed to Agent\*; `planning*.go` -> `agentsession*.go`
  (bucket-1 only). `UndoPlanningRound` + spec-plan helpers kept.
- ✅ **Frontend store/types** - `stores/planning.ts` ->
  `stores/agentSession.ts`, `useAgentStore`, `AgentSession`,
  `AgentMessage`, Pinia id `agentSession`.
- ✅ **Frontend components/composables/lib** - `AgentChatPanel`,
  `useAgentAutocomplete`/`AgentAutocomplete`, `lib/agentBubble`,
  `lib/agentUsage`, `AGENT_CHAT_ENABLED`. Chat\* presentation kept.
- ✅ **UI strings** - "Planning Chat"->"Chat", "Message the planning
  agent…"->"Message the agent…", greeting, aria-labels.
- ✅ **Route flip** - `/api/planning/*` -> `/api/agent/*`, `/threads` ->
  `/sessions`; routes.go + frontend URLs + test mocks + docs path refs;
  api-contract.json regenerated.

- ✅ **CSS companion** - `PlanningChatPanel.css` -> `AgentChatPanel.css`
  (the `<style src>` import had been updated without the file; caught by a
  full vite build).
- ✅ **Storage + env migration (4a/4b)** - `WALLFACER_PLANNING_WINDOW_DAYS`
  -> `WALLFACER_AGENT_SESSION_WINDOW_DAYS` (deprecated alias kept), field
  `AgentSessionWindowDays`, config key `agent_session_window_days`; on-disk
  `<configDir>/planning/<fp>/` -> `agent-sessions/` via
  `store.MigrateAgentSessionsDir` (first-boot, idempotent, no-clobber) +
  tests.
- ✅ **Stats / activity (4c)** - store usage API (`AgentSessionGroupKey`,
  `AgentSessionUsageDir/Path`, `Append/ReadAgentSessionUsage`);
  `SandboxActivityPlanning` -> `SandboxActivityAgentSession` (value
  `planning` -> `agent-session`) + container label; `PlanningGroupStat`
  -> `AgentSessionGroupStat`, `StatsResponse.AgentSessions`
  json `agent_sessions`; frontend `data.agent_sessions`, "Agent Sessions"
  cost label, usage-tab `AGENT_LABELS`. `by_sub_agent` re-buckets by the
  live constant, so no historical-data loss.

Complete-everywhere sweep (goal: "migrate completely everywhere"):

- ✅ **Prompts** - `planning.tmpl`/`Planning()` -> `spec.tmpl`/`Spec()`,
  `PlanningSystem{Empty,Nonempty}` -> `SpecSystem*`,
  `selectPlanningSystemPrompt` -> `selectSpecSystemPrompt`.
- ✅ **Handler helpers + prose** - `assembleAgentPrompt`,
  `mutateAgentSession`, `isTaskLockedByAgent`, `persistAgentRoundUsage`;
  comments, log prefixes, error strings.
- ✅ **agentsession package prose** - comments/errors to runtime/agent
  session; `agentSessionTaskID` value -> `agent-session`; process prefix
  `wallfacer-agent-`.
- ✅ **Field/method rename** - `planner` field -> `agentSession`,
  `SetAgentSession`, `GenerateAgentSessionTitle`,
  `runIdeationViaAgentSession` across handler/runner/cli.
- ✅ **Frontend prose/vars** - `planning` local var -> `agentStore`
  everywhere (CommandPalette, PlanPage, plan components, tests); composable
  / lib / layout / store comments; aria-label "Agent session cost window";
  dead `.planning-chat-*` CSS -> `.agent-chat-*`.
- ✅ **Test prose + names** - all `Test*Planning*` / `newPlanner*` helpers
  renamed across the suites.
- ✅ **Docs** - `docs/guide/`, `docs/internals/` (incl. Mermaid diagrams),
  `origin.md` updated to new code names; release notes left as historical.
- ✅ **Route descriptions + doc tags** - `/api/agent/*` descriptions and
  `Tags: "agent"`.

Verified: a repo-wide scan leaves only documented keepers (this spec,
release notes, frozen plan-commit code, legacy `TaskKindPlanning`,
plan-commit pipeline prose, and the user-facing Plan Mode surface).

## Final Residual Sweep (2026-06-26)

A comprehensive case-insensitive `grep -ni "planning\|planner"` (the gate
the earlier sweep should have run, rather than targeted-identifier greps)
surfaced a tail the "complete-everywhere" pass had missed. Closed it:

- ✅ **Production residuals** - `stats.go` local var `planningDir` /
  helper `planningGroupLabel` -> `agentSession*`; analytics cost tab
  `PlanningTimelineEntry` / `sortedPlanningKeys` / `seedPlanningPeriod`
  -> `AgentSession*` (the data layer was already `agent_sessions`).
- ✅ **Stale comments** - `prompts_test.go` (`planning_system_*` ->
  `spec_system_*`), `useChatSession.ts` (`StreamPlanningMessages` ->
  `StreamAgentMessages`), `stats.go` doc path (`<configDir>/planning/`
  -> `agent-sessions/`), `pinThreadToTask` ("actively planning" ->
  "actively working", it is task mode).
- ✅ **Test identifiers** - `Test*Planning*` / `withPlanning` across
  `usage_test.go`, `stats_test.go`, `store/agent_session_usage_test.go`,
  `cli/server_test.go`, `runner/ideate_test.go`,
  `config_test.go`, `envconfig_test.go` -> `*AgentSession*`. The
  window-days alias case became
  `TestParse_AgentSessionWindowDays_DeprecatedAlias` to avoid colliding
  with the canonical-var test; the `WALLFACER_PLANNING_WINDOW_DAYS` env
  literals it exercises stay.
- ✅ **Shared helper** - generic `initPlanningTestRepo` (just inits a temp
  git repo, used by plan, spec, and agent-session tests) -> neutral
  `initGitTestRepo`, plus its planning-flavoured git-identity fixtures.
- ✅ **gofmt** - committed the struct/map realignments orphaned by the
  earlier rename commits (HEAD was gofmt-dirty; a clean checkout would
  have failed the build gate).

Gate: `make build` green (golangci-lint 0 issues, vue-tsc clean, vite
build + go build OK). Final scan leaves only the keepers below.

## Kept by Design (not "planning" machinery)

- Frozen git history: `Plan-Round:` / `Plan-Thread:` / `(plan)` trailers,
  `commitPlanningRound`, `UndoPlanningRound`, and the
  `planning_git.go` / `planning_undo.go` / `planning_directive.go` files
  that read/write them.
- The spec-design product surface: **Plan Mode**, the **Plan** tab, the
  `/plan` route, "Send to Plan", and i18n tour/capability copy describing
  the planning conversation.
- `store.TaskKindPlanning` (unused legacy enum value `"planning"`) and the
  `commit_message_planning` / `title_planning` plan-round attribution
  labels.
- The spec-mode prompt **template content** (it legitimately instructs the
  agent on planning specs).
- Historical "replaced by" breadcrumbs to the deleted pre-Vue UI
  (`ui/js/planning-chat.js` in `stores/agentSession.ts` /
  `useChatSession.ts`) and the reference to the archived
  `planning-codex-compat.md` spec - both name removed/archived artifacts
  by their real historical names, like release notes.

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
