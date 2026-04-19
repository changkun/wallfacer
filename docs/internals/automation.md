# Automation

Wallfacer runs 7 background watchers that form an autonomous pipeline: **auto-promoter**, **auto-retrier**, **auto-tester**, **auto-submitter**, **auto-refiner**, **ideation watcher**, and **waiting-sync watcher**. Together with oversight generation and circuit breakers, these watchers allow the task board to operate hands-free — promoting backlog tasks, running tests, submitting results, retrying failures, refining prompts, syncing worktrees, and generating oversight summaries without manual intervention.

## Background Goroutine Model

No message queue, no worker pool. Concurrency is plain Go goroutines:

```go
// Task execution (new or resumed)
go h.runner.Run(id, prompt, sessionID, freshStart)

// Post-feedback resumption
go h.runner.Run(id, feedbackMessage, sessionID, false)

// Commit pipeline after mark-done
go func() {
    h.runner.Commit(id)
    store.UpdateStatus(id, "done")
}()
```

Tasks are long-running and IO-bound (container execution, git operations), so goroutines are appropriate — no CPU contention, and Go's scheduler handles the rest.

## Watcher Initialization & Startup

### Startup Sequence in `server.go`

The server starts watchers and recovery routines in a specific order after constructing the `Runner` and `Handler`:

```mermaid
flowchart TD
    subgraph "Runner Construction (NewRunner)"
        A1["Initialize circuit breaker<br/>(DefaultCBThreshold=5, 30s open)"]
        A2["Start board subscription loop<br/>(cache invalidation)"]
    end

    subgraph "Pre-watcher Recovery"
        B1["r.PruneUnknownWorktrees()"]
        B2["runner.RecoverOrphanedTasks(ctx, s, r)"]
        B3["go r.StartWorktreeGC(ctx)"]
        B4["go r.StartWorktreeHealthWatcher(ctx)"]
    end

    subgraph "Handler Watchers"
        C1["h.StartAutoPromoter(ctx)"]
        C2["h.StartAutoRetrier(ctx)"]
        C3["h.StartIdeationWatcher(ctx)"]
        C4["h.StartWaitingSyncWatcher(ctx)"]
        C5["h.StartAutoTester(ctx)"]
        C6["h.StartAutoSubmitter(ctx)"]
    end

    A1 --> A2 --> B1 --> B2 --> B3 --> B4
    B4 --> C1 --> C2 --> C3 --> C4 --> C5 --> C6
```

### Recovery Scans

Before watchers begin, two recovery operations run synchronously:

- **`PruneUnknownWorktrees()`**: Scans the `worktrees/` directory and removes any worktree directories that do not correspond to a known task. Also runs `git worktree prune` on each workspace repository to clean up stale Git worktree references.
- **`RecoverOrphanedTasks()`**: Scans all tasks in `in_progress` or `committing` status. For each, it checks whether a corresponding container is still running. If so, it starts a monitoring goroutine. If not (container crashed while the server was down), it transitions the task to `failed`.

### Subscription Patterns

All handler-level watchers follow one of two patterns:

**Store-driven (SubscribeWake)**: The auto-promoter, auto-retrier, auto-tester, and auto-submitter call `store.SubscribeWake()` to get a capacity-1 channel that signals "something changed." They react to the signal by scanning tasks and taking action if conditions are met.

```go
// Auto-promoter pattern
subID, ch := h.store.SubscribeWake()
ticker := time.NewTicker(60 * time.Second)
go func() {
    defer h.store.UnsubscribeWake(subID)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case <-ch:         h.tryAutoPromote(ctx)
        case <-ticker.C:   h.tryAutoPromote(ctx)
        }
    }
}()
```

The supplementary ticker (60 seconds for the promoter) ensures scheduled tasks are promoted even when no other state change occurs.

**Startup recovery scan**: The auto-retrier additionally performs a startup scan — immediately after subscribing, it lists all failed tasks and attempts to retry any that match the transient failure categories (`container_crash`, `worktree`, `sync_error`). This catches tasks that failed while the server was down.

### Circuit Breaker Initialization

The container circuit breaker is initialized in `NewRunner()` with:
- **Threshold**: `WALLFACER_CONTAINER_CB_THRESHOLD` (default: 5 consecutive failures).
- **Open duration**: `WALLFACER_CONTAINER_CB_OPEN_SECONDS` (default: 30 seconds).

After the threshold is exceeded, the circuit opens and rejects further launches. After the open duration, it enters half-open state and allows a single probe. A successful probe resets the breaker; a failed probe re-opens it.

## Autopilot (Auto-Promotion)

When autopilot is enabled, the server automatically promotes backlog tasks to `in_progress` as capacity becomes available, without requiring the user to drag cards manually.

```mermaid
flowchart TD
    Enable["PUT /api/config<br/>autopilot: true"] --> Subscribe["StartAutoPromoter<br/>subscribes to store changes"]
    Subscribe --> Check{"On each state change:<br/>autopilot enabled?"}
    Check -->|no| Skip[Skip]
    Check -->|yes| Capacity{"in_progress count<br/>< MAX_PARALLEL?"}
    Capacity -->|no| Skip
    Capacity -->|yes| Pick["Pick lowest-position<br/>backlog task"]
    Pick --> Deps{"DependsOn met?<br/>ScheduledAt reached?"}
    Deps -->|no| Skip
    Deps -->|yes| Promote["Promote to in_progress<br/>launch runner.Run"]
```

`WALLFACER_MAX_PARALLEL` defaults to 5. The lock ensures two simultaneous state changes cannot both promote tasks, which would exceed the limit. Autopilot state is toggled via `PUT /api/config {"autopilot": true/false}` and does not persist across restarts.

Concurrency limit is read from `WALLFACER_MAX_PARALLEL` in the env file (default: 5). Autopilot is off by default and does not persist across server restarts.

Tasks whose `DependsOn` list contains any task not yet in `done` status are skipped by the auto-promoter even when the in-progress count is below `WALLFACER_MAX_PARALLEL`.

Tasks whose `ScheduledAt` is in the future are also skipped.

## Test Verification

`POST /api/tasks/{id}/test` runs a separate verification agent on a `waiting` task without committing:

```mermaid
flowchart TD
    Click["User clicks Test<br/>on waiting task"] --> Setup["Set IsTestRun=true<br/>clear LastTestResult"]
    Setup --> Transition["waiting to in_progress"]
    Transition --> Launch["Launch fresh container<br/>(no --resume, new session)<br/>with test prompt"]
    Launch --> Loop["Runner loop<br/>(isTestRun=true)"]
    Loop --> StopReason{"stop_reason?"}
    StopReason -->|"max_tokens / pause_turn"| Loop
    StopReason -->|end_turn| Parse["parseTestVerdict()<br/>extract PASS / FAIL"]
    Parse --> Record["IsTestRun = false<br/>LastTestResult = verdict<br/>in_progress to waiting<br/>(no commit pipeline)"]

    Note1["TestRunStartTurn marks boundary<br/>between implementation and test output"]
    style Note1 fill:none,stroke-dasharray: 5 5
```

The UI splits the live output panel into "Implementation" and "Test" sections using `TestRunStartTurn` as the boundary.

Once a task has reached `waiting` (Claude finished but the user hasn't committed yet), a test verification agent can be triggered to check whether the implementation meets acceptance criteria.

```
POST /api/tasks/{id}/test
  body: { criteria?: string }   // optional additional acceptance criteria
  ↓
  Sets IsTestRun = true, clears LastTestResult.
  Transitions waiting → in_progress.
  Launches a fresh container (separate session, no --resume) with a test prompt.

Test agent runs (IsTestRun = true):
  Container executes: inspect code, run tests, verify requirements.
  Agent must end its response with **PASS** or **FAIL**.

On end_turn:
  parseTestVerdict() extracts "pass", "fail", or "unknown" from the result.
  Records verdict in LastTestResult.
  Transitions in_progress → waiting (no commit).
  Test output is shown separately from implementation output in the task detail panel.
```

The test verdict is displayed as a badge on the task card and in the task detail panel. Multiple test runs are allowed; each overwrites the previous verdict. The `TestRunStartTurn` field records which turn the test started so the UI can split implementation vs. test output.

After reviewing the verdict, the user can:
- Mark the task done (commit pipeline runs) if the verdict is PASS
- Provide feedback to fix issues, then re-test
- Cancel the task

## Auto-Submit

Auto-submit is part of the autopilot pipeline. When enabled via `PUT /api/config {"autosubmit": true}`, the `StartAutoSubmitter` watcher monitors tasks that reach `waiting` state with a passing test verdict. It automatically marks them as done, triggering the commit-and-push pipeline without manual intervention.

This completes the autonomous loop: autopilot promotes → agent executes → auto-tester verifies → auto-submit commits.

## Auto-Retry

Tasks can have an `AutoRetryBudget map[FailureCategory]int` that specifies how many automatic retries are allowed for each failure category. When a task fails:

1. The failure is classified into a `FailureCategory`
2. If the budget for that category has remaining retries, the count is decremented
3. The task is automatically reset to `backlog` for a fresh run
4. `AutoRetryCount` tracks the total number of auto-retries consumed

A global cap (`constants.MaxAutoRetries`, currently 3) prevents infinite retry loops regardless of per-category budgets.

Failure categories:

| Category | Description |
|---|---|
| `timeout` | Per-turn timeout exceeded |
| `budget_exceeded` | Cost or token budget limit reached |
| `worktree_setup` | Git worktree creation failed |
| `container_crash` | Container exited unexpectedly |
| `agent_error` | Agent reported an error in its output |
| `sync_error` | Rebase/sync operation failed |
| `unknown` | Unclassifiable failure |

The `StartAutoRetrier` watcher performs a startup recovery scan for tasks that failed with transient categories (`container_crash`, `worktree`, `sync_error`) while the server was down, then subscribes to store changes for ongoing monitoring.

See [Task Lifecycle](task-lifecycle.md#auto-retry) for retry history and data models.

## Tip-Sync (Auto-Sync)

The `StartWaitingSyncWatcher` monitors tasks in `waiting` or `failed` state and rebases their worktrees onto the latest default branch when upstream changes are detected. This keeps task branches up-to-date without merging, reducing conflict risk when the commit pipeline eventually runs.

Multiple workspace paths can be passed at startup or switched at runtime via `PUT /api/workspaces`. For each workspace:

- Git status is polled independently and shown in the UI header
- A separate worktree is created per task per workspace
- The commit pipeline runs phases 1-3 for each workspace in sequence

Non-git directories are supported as plain mount targets (no worktree, no commit pipeline for that workspace).

## Task-Mode Planning (Replaces the Retired Refine Subsystem)

Prompt iteration is now driven from Plan task-mode threads (pinned to a specific task via the **Send to Plan** card action or the explorer **Task Prompts** section). The planning agent edits the task's prompt through the `POST /api/planning/tool/update_task_prompt` tool endpoint; every call is a committed round that `POST /api/planning/undo` can rewind with `git revert`. There is no longer a `POST /api/tasks/{id}/refine*` family, no auto-refine watcher, and no `autorefine` config flag — see [Refinement & Ideation](../guide/refinement-and-ideation.md) for the user-facing surface.

## Oversight Generation

Oversight is generated asynchronously whenever a task transitions to `waiting`, `done`, or `failed`. It is also regenerated periodically during execution if `WALLFACER_OVERSIGHT_INTERVAL > 0` (minutes).

`POST /api/tasks/generate-oversight` triggers generation for tasks that are missing summaries.

```mermaid
flowchart TD
    Trigger["Task reaches<br/>waiting / done / failed"] --> BG["Background goroutine:<br/>GenerateOversight(taskID)"]
    BG --> Status1["TaskOversight.Status<br/>= generating"]
    Status1 --> Read["Read trace events<br/>from traces/NNNN.json"]
    Read --> Send["Send to Claude with<br/>summarisation prompt"]
    Send --> Parse["Parse response into<br/>OversightPhase list"]
    Parse --> Status2["TaskOversight.Status<br/>= ready"]
    Status2 --> Store["Store in<br/>oversights/id.json"]
```

Served by:
- `GET /api/tasks/{id}/oversight` — implementation run summary
- `GET /api/tasks/{id}/oversight/test` — test-run summary (if a test was run)

The UI renders phases in the Oversight tab and as an interactive flamegraph Timeline.

The generator reads the task's trace events, passes them to the Claude API with a summarisation prompt, and writes the result as a `TaskOversight` (`status`: `pending` → `generating` → `ready` | `failed`). The result is persisted in `data/<uuid>/oversights/<id>.json`.

`POST /api/tasks/generate-oversight` can be used to retroactively generate oversight for tasks that completed before this feature existed.

## Ideation / Brainstorm

Ideation is the built-in `brainstorm` flow wrapping a single `ideate` agent. It runs via the standard task-and-flow dispatch path rather than a special case. The system-ideation routine is a routine card tagged `system:ideation` with `spawn_flow: brainstorm`; each fire spawns a fresh task whose `FlowID` is `brainstorm`. Legacy records written before the flow rewrite carry `Kind = "idea-agent"` and resolve to `brainstorm` via the legacy-kind mapper.

```mermaid
flowchart TD
    Trigger["POST /api/ideate"] --> Fire["Fire system:ideation routine"]
    Fire --> Create["Create task with<br/>FlowID = brainstorm"]
    Create --> Launch["Launch ideate agent<br/>in sandbox container"]
    Launch --> Analyse["Read workspace contents<br/>analyse code structure"]
    Analyse --> Generate["Create backlog tasks<br/>via wallfacer API<br/>(each tagged)"]
    Generate --> Done["Container completes<br/>tasks appear on board"]

    Status["GET /api/ideate<br/>returns session state"]
    Cancel["DELETE /api/ideate<br/>kills container"]
```

- Each created task gets relevant `Tags` and an `ExecutionPrompt` (full instructions) separate from `Prompt` (the short card label).
- Triggered via `POST /api/ideate` (fires the routine); cancelled via `DELETE /api/ideate`.
- `GET /api/ideate` returns current ideation session state (task ID, status, created task count).
- See [Agents & Flows](../guide/agents-and-flows.md) for how to clone the `ideate` agent with a different harness or a custom system prompt.

## Conflict Resolution

When `git rebase` fails during the commit pipeline:

```mermaid
flowchart TD
    Rebase["git rebase default-branch"] --> Result{"Rebase<br/>succeeded?"}
    Result -->|yes| FFMerge["git merge --ff-only<br/>task branch into default"]
    Result -->|no| Invoke["Invoke agent<br/>(same session ID)<br/>with conflict details"]
    Invoke --> Resolve["Agent resolves conflicts<br/>and stages files"]
    Resolve --> Continue["git rebase --continue"]
    Continue --> Retry{"Still<br/>failing?"}
    Retry -->|"no (resolved)"| FFMerge
    Retry -->|"yes (attempt < 3)"| Invoke
    Retry -->|"yes (attempts exhausted)"| Failed["Mark task failed<br/>clean up worktrees"]
```

Using the same session ID means the agent has full context of the original task when making conflict resolution decisions.

## Circuit Breakers

Container launches are protected by a circuit breaker. After a configurable number of consecutive failures (`WALLFACER_CONTAINER_CB_THRESHOLD`, default: 5), the circuit opens and rejects further launches until it resets. This prevents cascading failures when the container runtime is unhealthy.

The circuit breaker lifecycle:

1. **Closed** (normal): All container launches proceed. Each failure increments a counter; each success resets it.
2. **Open** (tripped): After the threshold is exceeded, all launches are rejected for the open duration (`WALLFACER_CONTAINER_CB_OPEN_SECONDS`, default: 30 seconds).
3. **Half-open** (probing): After the open duration expires, a single probe launch is allowed. Success resets the breaker to closed; failure re-opens it.

See [Circuit Breakers](../guide/circuit-breakers.md) for full details.

## See Also

- [Task Lifecycle](task-lifecycle.md) — State machine, turn loop, data models, auto-retry details
- [API & Transport](api-and-transport.md) — HTTP API, SSE streams, container execution, environment configuration
- [Git Worktrees](git-worktrees.md) — Per-task worktree isolation and commit pipeline
- [Architecture](architecture.md) — High-level design decisions and persistence model
