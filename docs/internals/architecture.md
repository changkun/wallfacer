# Architecture

Wallfacer is a host-native Go service that coordinates autonomous coding agents running in ephemeral containers, with per-task git worktree isolation and a web task board for human oversight.

## System Overview

```mermaid
graph TB
    subgraph Browser
        UI["Browser UI<br/>(Vanilla JS + Tailwind + Sortable.js)<br/>Drag-and-drop task board, SSE live updates"]
    end

    subgraph Server["Go Server (stdlib net/http)"]
        Handler["Handler<br/>REST API + SSE"]
        Runner["Runner<br/>orchestration + commit"]
        Store["Store<br/>state + persistence"]
        Automation["Automation Loops<br/>promote / test / submit<br/>sync / retry / refine / ideation"]

        Handler --> Runner
        Runner --> Store
        Automation --> Store
        Store -.->|pub/sub| Automation
    end

    subgraph Infra["Host Infrastructure"]
        Containers["Sandbox Containers<br/>Claude / Codex images<br/>ephemeral, one per turn"]
        Worktrees["Per-task Git Worktrees<br/>~/.wallfacer/worktrees/<br/>task/ID branches"]
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
    in_progress --> waiting : end_turn
    in_progress --> waiting : empty stop_reason
    in_progress --> failed : error / timeout / budget

    committing --> done : commit success
    committing --> failed : commit failure

    waiting --> in_progress : feedback
    waiting --> in_progress : test (IsTestRun)
    waiting --> committing : mark done
    waiting --> cancelled : cancel

    failed --> backlog : retry / auto_retry
    failed --> cancelled : cancel

    done --> cancelled : cancel
    cancelled --> backlog : retry

    note right of waiting
        sync: rebase onto default branch
    end note
```

States: `backlog`, `in_progress`, `waiting`, `committing`, `done`, `failed`, `cancelled`.
`archived` is a boolean flag on done/cancelled tasks, not a separate state.

## Turn Loop

```mermaid
flowchart TD
    Start["Start turn<br/>(increment N)"] --> Launch["Launch container<br/>with prompt + session ID"]
    Launch --> Save["Save output to<br/>turn-NNNN.json"]
    Save --> Usage["Accumulate<br/>usage/cost"]
    Usage --> Budget{"Check budgets<br/>MaxCost / MaxTokens"}

    Budget -->|over budget| WaitingBudget["WAITING<br/>(budget_exceeded)"]
    Budget -->|within budget| Parse{"Parse stop_reason"}

    Parse -->|end_turn| Waiting2["WAITING<br/>awaiting review"]
    Parse -->|"max_tokens / pause_turn"| Start
    Parse -->|"empty / unknown"| Waiting["WAITING<br/>blocks until<br/>user feedback"]
    Parse -->|"error / timeout"| FailedError["FAILED<br/>(classify failure<br/>category)"]

    Waiting -->|feedback received| Start
```

## Background Automation

```mermaid
flowchart LR
    PubSub["Store<br/>pub/sub on<br/>state changes"]

    PubSub --> Promoter["Auto-promoter<br/>backlog to in_progress<br/>when capacity available<br/>+ deps met + scheduled"]
    PubSub --> Tester["Auto-tester<br/>launch test verification<br/>on untested waiting tasks"]
    PubSub --> Submitter["Auto-submitter<br/>waiting to done<br/>when test passed<br/>+ conflict-free"]
    PubSub --> Sync["Waiting-sync<br/>rebase worktrees<br/>behind default branch"]
    PubSub --> Retry["Auto-retry<br/>failed to backlog<br/>if retry budget > 0"]
    PubSub --> Refiner["Auto-refiner<br/>launch refinement agent<br/>on unrefined backlog tasks"]
    PubSub --> Ideation["Ideation watcher<br/>launch idea-agent<br/>on interval"]
```

## Component Responsibilities

**Store** (`internal/store/`) — In-memory task state guarded by `sync.RWMutex`, backed by per-task directory persistence. Enforces the state machine via a transition table. Provides pub/sub for live deltas and a full-text search index.

**Runner** (`internal/runner/`) — Orchestration engine. Creates worktrees, builds container specs, executes the turn loop, accumulates usage, enforces budgets, runs the commit pipeline, and generates titles/oversight in the background.

**Handler** (`internal/handler/`) — REST API and SSE endpoints organized by concern. Hosts automation toggle controls.

**Frontend** (`ui/`) — Vanilla JS modules. Task board, modals, timeline/flamegraph, diff viewer, usage dashboard. All live updates via SSE.

**Workspace Manager** (`internal/workspace/`) — Manages workspace configuration, workspace groups, and hot-swapping between workspace sets without server restart.

## Cross-Cutting Concerns

**Concurrency** — `Store.mu` for task map integrity; `Runner.worktreeMu` for filesystem ops; per-repo mutex for rebase serialization; per-task mutex for oversight generation.

**Recovery** — On startup, `RecoverOrphanedTasks` inspects `in_progress` and `committing` tasks against actual container and worktree state, recovering or failing them as appropriate.

**Security** — API key auth, SSRF-hardened gateway URLs, path traversal guards, CSRF protection, request body size limits.

**Circuit breakers** — Per-watcher exponential backoff suppresses individual automation loops on failure; container-level circuit breaker blocks launches when the runtime is unavailable. See [Circuit Breakers](../guide/circuit-breakers.md).

**Observability** — SSE event streams, append-only trace timeline per task, span timing, Prometheus-compatible metrics, webhook notifications.
