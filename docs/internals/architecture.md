# Architecture

Wallfacer is a host-native Go service that coordinates autonomous coding agents running in ephemeral containers, with per-task git worktree isolation and a web task board for human oversight.

## System Overview

```text
┌─────────────────────────────────────────────────────────────────────┐
│ Browser UI (Vanilla JS + Tailwind + Sortable.js)                  │
│ - Drag-and-drop task board                                         │
│ - SSE streams for live updates                                     │
└──────────────────────────────┬──────────────────────────────────────┘
                               │ HTTP / SSE
┌──────────────────────────────▼──────────────────────────────────────┐
│ Go Server (stdlib net/http, no framework)                          │
│                                                                     │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌───────────────────┐  │
│  │ Handler  │  │  Runner  │  │  Store   │  │ Automation Loops  │  │
│  │ REST API │→ │ orchestr.│→ │ state +  │← │ promote/test/     │  │
│  │ + SSE    │  │ + commit │  │ persist  │  │ submit/sync/retry │  │
│  └──────────┘  └────┬─────┘  └──────────┘  └───────────────────┘  │
└──────────────────────┼────────────────────────────────────────────── ┘
                       │ os/exec
          ┌────────────▼────────────┐    ┌──────────────────────────┐
          │ Sandbox Containers      │    │ Per-task Git Worktrees   │
          │ Claude / Codex images   │←──→│ ~/.wallfacer/worktrees/  │
          │ ephemeral, one per turn │    │ task/<id> branches       │
          └─────────────────────────┘    └──────────────────────────┘
```

## Design Decisions

**Filesystem-first persistence.** No database. Each task is a directory (`data/<uuid>/`) containing `task.json`, traces, outputs, and oversight summaries. Writes are atomic (temp file + rename). Easy to inspect, back up, and debug.

**Container isolation.** Every agent turn runs in a fresh ephemeral container launched via `os/exec`. The container sees only its task's worktree mounted at `/workspace`. Tasks cannot interfere with each other or the host.

**Git worktree isolation.** Each task gets its own worktree on a `task/<id>` branch. Tasks work in parallel without merge conflicts during execution. Rebase/merge happens at commit time.

**Activity-routed sandboxes.** Different activities (implementation, testing, oversight, title, etc.) can route to different sandbox images and models, so cheap operations use smaller models.

**Automation with guardrails.** Background loops handle promotion, testing, submission, and retry — each with explicit controls (toggles, budgets, thresholds).

## Task State Machine

```text
                    ┌──────────────────────────────────────────────────────────┐
                    │                                                          │
 ┌─────────┐  drag/autopilot  ┌─────────────┐  end_turn   ┌───────────┐  commit  ┌──────┐
 │ BACKLOG ├─────────────────→│ IN_PROGRESS ├────────────→│ COMMITTING├────────→│ DONE │
 └────┬────┘                  └──┬──┬───┬────┘             └─────┬─────┘        └──┬───┘
      │                          │  │   │                        │                 │
      │cancel              max_tokens  │   │error/timeout/budget  │fail              │cancel
      │               pause_turn│  │   │                        │                 │
      │                    ┌────┘  │   ▼                        ▼                 │
      │                    │       │ ┌────────┐            ┌────────┐              │
      │                  (loop)    │ │WAITING │            │ FAILED │              │
      │                            │ └┬─┬─┬──┘            └┬──┬─┬──┘              │
      │                            │  │ │ │                │  │ │                  │
      │              empty stop────┘  │ │ │  resume────────┘  │ │                  │
      │              reason           │ │ │                   │ │                  │
      │                               │ │ │  retry/auto_retry─┘ │                  │
      │           feedback────────────┘ │ │  ──→ BACKLOG         │                  │
      │           ──→ IN_PROGRESS       │ │                      │                  │
      │                                 │ │  fork────────────────┘                  │
      │           mark done─────────────┘ │  ──→ new BACKLOG                       │
      │           ──→ COMMITTING → DONE   │                                        │
      │                                   │                                        │
      ▼                                   ▼                                        ▼
 ┌───────────┐                       ┌───────────┐                           ┌───────────┐
 │ CANCELLED │←──────────────────────│ CANCELLED │←──────────────────────────│ CANCELLED │
 └─────┬─────┘                       └───────────┘                           └───────────┘
       │ retry ──→ BACKLOG
```

States: `backlog`, `in_progress`, `waiting`, `committing`, `done`, `failed`, `cancelled`.
`archived` is a boolean flag on done/cancelled tasks, not a separate state.

## Turn Loop

```text
                    ┌──────────────┐
                    │  Start turn  │
                    │ (increment N)│
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │Launch container│
                    │with prompt +  │
                    │session ID     │
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │Save output to│
                    │turn-NNNN.json│
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │ Accumulate   │
                    │ usage/cost   │
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐     over budget
                    │Check budgets ├──────────────────→ FAILED
                    │MaxCost/Tokens│                    (budget_exceeded)
                    └──────┬───────┘
                           │ within budget
                    ┌──────▼──────────────┐
                    │  Parse stop_reason  │
                    └──┬───┬───┬───┬──────┘
                       │   │   │   │
          end_turn ────┘   │   │   └──── error/timeout
          │                │   │                │
          ▼                │   │                ▼
     ┌──────────┐          │   │           ┌────────┐
     │COMMITTING│          │   │           │ FAILED │
     │→ commit  │          │   │           │classify│
     │→ rebase  │          │   │           │category│
     │→ push?   │          │   │           └────────┘
     │→ DONE    │          │   │
     └──────────┘          │   │
                           │   └──── empty/unknown
           max_tokens ─────┘              │
           pause_turn                     ▼
              │                      ┌─────────┐
              │                      │ WAITING │
              └──→ next turn ◄───────│feedback │
                   (same session)    │resumes  │
                                     └─────────┘
```

## Background Automation

```text
  Store (pub/sub on state changes)
       │
       ├──→ Auto-promoter
       │      if autopilot ON
       │        && in_progress < MAX_PARALLEL
       │        && dependencies met
       │        && scheduled time reached
       │      then: backlog → in_progress
       │
       ├──→ Auto-tester
       │      if autotest ON
       │        && task is waiting + untested
       │        && test slots available
       │      then: launch test verification
       │
       ├──→ Auto-submitter
       │      if autosubmit ON
       │        && task is waiting + test passed
       │        && conflict-free + up-to-date
       │      then: waiting → done (commit pipeline)
       │
       ├──→ Waiting-sync
       │      if task is waiting + behind default branch
       │      then: rebase worktree onto latest
       │
       ├──→ Auto-retry
       │      if task just failed
       │        && retry budget for that failure category > 0
       │      then: failed → backlog (fresh session)
       │
       └──→ Ideation watcher
              if ideation ON + interval elapsed
              then: launch idea-agent task
```

## Component Responsibilities

**Store** (`internal/store/`) — In-memory task state guarded by `sync.RWMutex`, backed by per-task directory persistence. Enforces the state machine via a transition table. Provides pub/sub for live deltas and a full-text search index.

**Runner** (`internal/runner/`) — Orchestration engine. Creates worktrees, builds container specs, executes the turn loop, accumulates usage, enforces budgets, runs the commit pipeline, and generates titles/oversight in the background.

**Handler** (`internal/handler/`) — REST API and SSE endpoints organized by concern. Hosts automation toggle controls.

**Frontend** (`ui/`) — Vanilla JS modules. Task board, modals, timeline/flamegraph, diff viewer, usage dashboard. All live updates via SSE.

## Cross-Cutting Concerns

**Concurrency** — `Store.mu` for task map integrity; `Runner.worktreeMu` for filesystem ops; per-repo mutex for rebase serialization; per-task mutex for oversight generation.

**Recovery** — On startup, `RecoverOrphanedTasks` inspects `in_progress` and `committing` tasks against actual container and worktree state, recovering or failing them as appropriate.

**Security** — API key auth, SSRF-hardened gateway URLs, path traversal guards, CSRF protection, request body size limits.

**Observability** — SSE event streams, append-only trace timeline per task, span timing, Prometheus-compatible metrics, webhook notifications.
