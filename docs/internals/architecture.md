# Architecture

Wallfacer is a host-native Go service that coordinates autonomous coding agents running in ephemeral containers, with per-task git worktree isolation and a web task board for human oversight.

## System Overview

```mermaid
graph TB
    subgraph Browser
        UI["Browser UI\n(Vanilla JS + Tailwind + Sortable.js)\nDrag-and-drop task board, SSE live updates"]
    end

    subgraph Server["Go Server (stdlib net/http)"]
        Handler["Handler\nREST API + SSE"]
        Runner["Runner\norchestration + commit"]
        Store["Store\nstate + persistence"]
        Automation["Automation Loops\npromote / test / submit\nsync / retry / ideation"]

        Handler --> Runner
        Runner --> Store
        Automation --> Store
        Store -.->|pub/sub| Automation
    end

    subgraph Infra["Host Infrastructure"]
        Containers["Sandbox Containers\nClaude / Codex images\nephemeral, one per turn"]
        Worktrees["Per-task Git Worktrees\n~/.wallfacer/worktrees/\ntask/ID branches"]
        Containers --- Worktrees
    end

    UI -->|"HTTP / SSE"| Handler
    Runner -->|os/exec| Containers
```

## Design Decisions

**Filesystem-first persistence.** No database. Each task is a directory (`data/<uuid>/`) containing `task.json`, traces, outputs, and oversight summaries. Writes are atomic (temp file + rename). Easy to inspect, back up, and debug.

**Container isolation.** Every agent turn runs in a fresh ephemeral container launched via `os/exec`. The container sees only its task's worktree mounted at `/workspace`. Tasks cannot interfere with each other or the host.

**Git worktree isolation.** Each task gets its own worktree on a `task/<id>` branch. Tasks work in parallel without merge conflicts during execution. Rebase/merge happens at commit time.

**Activity-routed sandboxes.** Different activities (implementation, testing, oversight, title, etc.) can route to different sandbox images and models, so cheap operations use smaller models.

**Automation with guardrails.** Background loops handle promotion, testing, submission, and retry — each with explicit controls (toggles, budgets, thresholds).

## Task State Machine

```mermaid
stateDiagram-v2
    [*] --> backlog

    backlog --> in_progress : drag / autopilot
    backlog --> cancelled : cancel

    in_progress --> in_progress : max_tokens / pause_turn (auto-continue)
    in_progress --> committing : end_turn
    in_progress --> waiting : empty stop_reason
    in_progress --> failed : error / timeout / budget

    committing --> done : commit success
    committing --> failed : commit failure

    waiting --> in_progress : feedback
    waiting --> in_progress : test (IsTestRun)
    waiting --> committing : mark done
    waiting --> cancelled : cancel

    failed --> in_progress : resume (same session)
    failed --> backlog : retry / auto_retry
    failed --> cancelled : cancel

    done --> cancelled : cancel
    cancelled --> backlog : retry

    note right of waiting
        fork: creates new backlog task
    end note
    note right of failed
        fork: creates new backlog task
    end note
```

States: `backlog`, `in_progress`, `waiting`, `committing`, `done`, `failed`, `cancelled`.
`archived` is a boolean flag on done/cancelled tasks, not a separate state.

## Turn Loop

```mermaid
flowchart TD
    Start["Start turn\n(increment N)"] --> Launch["Launch container\nwith prompt + session ID"]
    Launch --> Save["Save output to\nturn-NNNN.json"]
    Save --> Usage["Accumulate\nusage/cost"]
    Usage --> Budget{"Check budgets\nMaxCost / MaxTokens"}

    Budget -->|over budget| FailedBudget["FAILED\n(budget_exceeded)"]
    Budget -->|within budget| Parse{"Parse stop_reason"}

    Parse -->|end_turn| Commit["COMMITTING\ngit add, commit,\nrebase, push, DONE"]
    Parse -->|"max_tokens / pause_turn"| Start
    Parse -->|"empty / unknown"| Waiting["WAITING\nblocks until\nuser feedback"]
    Parse -->|"error / timeout"| FailedError["FAILED\n(classify failure\ncategory)"]

    Waiting -->|feedback received| Start
```

## Background Automation

```mermaid
flowchart LR
    PubSub["Store\npub/sub on\nstate changes"]

    PubSub --> Promoter["Auto-promoter\nbacklog to in_progress\nwhen capacity available\n+ deps met + scheduled"]
    PubSub --> Tester["Auto-tester\nlaunch test verification\non untested waiting tasks"]
    PubSub --> Submitter["Auto-submitter\nwaiting to done\nwhen test passed\n+ conflict-free"]
    PubSub --> Sync["Waiting-sync\nrebase worktrees\nbehind default branch"]
    PubSub --> Retry["Auto-retry\nfailed to backlog\nif retry budget > 0"]
    PubSub --> Ideation["Ideation watcher\nlaunch idea-agent\non interval"]
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
