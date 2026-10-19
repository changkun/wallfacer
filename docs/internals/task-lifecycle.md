# Task Lifecycle

## State Machine

Tasks progress through a well-defined set of states. Every transition is recorded as an immutable event in `data/<uuid>/traces/`.

```mermaid
stateDiagram-v2
    [*] --> backlog

    backlog --> in_progress : start

    in_progress --> in_progress : auto-continue
    in_progress --> waiting : end_turn
    in_progress --> failed : error
    in_progress --> backlog : reset
    in_progress --> cancelled : cancel

    committing --> done : commit ok
    committing --> failed : commit fail

    waiting --> in_progress : feedback
    waiting --> committing : mark done
    waiting --> cancelled : cancel

    failed --> backlog : retry
    failed --> cancelled : cancel

    done --> cancelled : cancel
    cancelled --> backlog : retry

    note right of waiting
        sync: rebase onto default branch
    end note
    note right of failed
        sync: rebase onto default branch
    end note
    note right of done
        archived flag can be set
    end note
    note right of cancelled
        archived flag can be set
    end note
```

Transition labels are shortened for legibility. In full:

| Transition | Trigger |
|---|---|
| `backlog` to `in_progress` | User drag or **Start task**, refused with 409 at the parallel limit |
| `in_progress` to `in_progress` | `stop_reason` of `max_tokens` or `pause_turn`, auto-continued in the same session |
| `in_progress` to `waiting` | `stop_reason` of `end_turn`, or an empty/unknown `stop_reason` |
| `in_progress` to `failed` | Process error, agent error, timeout, or a budget limit exceeded |
| `waiting` to `in_progress` | User feedback |
| `committing` to `done` / `failed` | Commit pipeline success or failure |
| `failed` to `backlog` | Retry |

## States

| State | Description |
|---|---|
| `backlog` | Queued, not yet started |
| `in_progress` | Host process running, agent executing |
| `waiting` | Claude paused mid-task, awaiting user feedback |
| `committing` | Transient: commit pipeline running after mark-done |
| `done` | Completed; changes committed and merged |
| `failed` | Process error, Claude error, timeout, or budget exceeded |
| `cancelled` | Explicitly cancelled; worktree cleaned up, history preserved |

**Note:** `archived` is a boolean flag (`Archived bool`) on the task, not a separate state. Tasks in `done` or `cancelled` state can have `Archived = true`, which moves them to the Archived column in the UI. The state machine has exactly 7 states (`backlog`, `in_progress`, `waiting`, `committing`, `done`, `failed`, `cancelled`).

## Turn Loop

Each pass through the loop in `runner.go` `Run()`:

1. Increment turn counter
2. Exec the selected CLI as a host process with the current prompt and session ID, using the task's git worktree as CWD
3. Save raw stdout to `data/<uuid>/outputs/turn-NNNN.json`; stderr (if any) to `turn-NNNN.stderr.txt`
4. Parse `stop_reason` from agent JSON output:

| `stop_reason` | `is_error` | Result |
|---|---|---|
| `end_turn` | false | Exit loop → `waiting` (awaiting review; the user marks done to trigger the commit pipeline) |
| `max_tokens` | false | Auto-continue (next iteration, same session) |
| `pause_turn` | false | Auto-continue (next iteration, same session) |
| empty / unknown | false | Set `waiting`; block until user provides feedback |
| any | true | Set `failed` with classified `FailureCategory` |

5. Accumulate token usage (`input_tokens`, `output_tokens`, cache tokens, `cost_usd`)
6. Record per-turn usage as `TurnUsageRecord`

The CWD comes from the task's worktree map (`agentWorkDir`, `internal/runner/container.go`): the worktree for the workspace folder when the task has one, else the first worktree in sorted folder order, and the live workspace folder only when the task has no worktree. A task created in one folder and resumed after the workspace was re-pointed therefore keeps working in its own worktree. A task stored with worktrees in several folders (from before a workspace became one folder) runs the same way; before its first turn `noteSeveralWorktrees` writes one system event on its timeline naming the folder the agent works in, and the commit pipeline still visits every folder's worktree.
7. Check budget limits (`MaxCostUSD`, `MaxInputTokens`); if exceeded → `failed` with `FailureCategory = budget_exceeded`

## Session Continuity

Claude Code supports `--resume <session-id>` for session continuity. The first turn creates a new session; subsequent turns (auto-continue or post-feedback) pass the same session ID, preserving the full conversation context.

Setting `FreshStart = true` on a task skips `--resume`, starting a brand-new session. This is what happens when a user retries a failed task.

## Feedback & Waiting State

When `stop_reason` is empty, Claude has asked a question or is blocked. The task enters `waiting`:

- Worktrees are **not** cleaned up, the git branch is preserved
- User submits feedback via `POST /api/tasks/{id}/feedback`
- Handler writes a `feedback` event to the trace log, then launches a new `runner.Run` goroutine using the existing session ID
- The task resumes from exactly where it paused, with the feedback message as the next prompt

Alternatively, the user can mark the task done from `waiting`, which skips further Claude turns and jumps straight to the commit pipeline.

## Cancellation

Any task in `backlog`, `in_progress`, `waiting`, or `failed` can be cancelled via `PATCH /api/tasks/{id}` with `{"status": "cancelled"}`. The handler:

1. **Kills the host process** (if `in_progress`). Sends `SIGTERM`, escalating to `SIGKILL` after 5 s if the process is still running (`internal/executor/host.go`). The running goroutine detects the cancelled status and exits without overwriting it to `failed`.
2. **Cleans up worktrees**, removes the git worktree and deletes the task branch, discarding all prepared changes.
3. **Sets status to `cancelled`** and appends a `state_change` event.
4. **Preserves history**, `data/<uuid>/traces/` and `data/<uuid>/outputs/` are left intact so execution logs, token usage, and the event timeline remain visible.

From `cancelled`, the user can retry the task (moves it back to `backlog`) to restart from scratch.

## Failure Categorization

When a task transitions to `failed`, the runner classifies the failure into one of these categories:

| Category | Description |
|---|---|
| `timeout` | Per-turn timeout exceeded |
| `budget_exceeded` | Cost or token budget limit reached |
| `worktree_setup` | Git worktree creation failed |
| `container_crash` | Host process exited unexpectedly (legacy category name) |
| `agent_error` | Agent reported an error in its output |
| `sync_error` | Rebase/sync operation failed |
| `unknown` | Unclassifiable failure |

The category is stored in `Task.FailureCategory` and included in `RetryRecord` when the task is reset for retry.

## Retry History

Each time a task is reset for retry, a `RetryRecord` is appended to `Task.RetryHistory`:

```
RetryRecord {
  RetiredAt        time.Time
  Prompt           string
  Status           TaskStatus
  Result           string           // truncated to 2000 chars
  SessionID        string
  Turns            int
  CostUSD          float64
  FailureCategory  FailureCategory
}
```

The list is capped at `DefaultRetryHistoryLimit` (10) entries. This allows operators to inspect the history of failed attempts.

## Titles

A task's title is set when the task is created and never changes afterwards. A `title` sent with `POST /api/tasks` is kept as given (trimmed). Otherwise `store.DeriveTitle` takes the prompt's first non-blank line, removes a leading Markdown heading marker (one to six `#` and a space), collapses runs of whitespace, and, when the line is longer than `store.TitleMaxRunes` (60) runes, cuts it at the last word boundary that fits and appends an ellipsis. Spec dispatch derives the title the same way from the spec body, so a spec's `# Heading` becomes its task's title. A chat thread still named `Chat N` after its first turn is renamed by the same rule from its opening message. Titles are displayed on the board cards instead of the full prompt text; titles stored by earlier releases are kept as they are.

## Prompt Refinement

Prompt refinement is the Plan task-mode chat. There are no dedicated refine routes or background refinement jobs. The agent session converses with the user about the task, and when both agree on an improved prompt the agent calls the `update_task_prompt` tool, which the console forwards as `POST /api/agent/tool/update_task_prompt` (`internal/handler/agentsession_tool.go`). The handler moves the prior prompt into history and sets the task's prompt to the new text. See [Plan Mode](plan-mode.md) for the task-mode chat flow.

## Board Context

Before each turn the runner writes a `board.json` manifest of all non-archived tasks to a host directory (the BoardDir) and passes its path to the agent via the `WALLFACER_BOARD_JSON` environment variable (`internal/runner/{container.go,execute.go}` `writeBoardDir`). The current task is marked `"is_self": true`. This gives agents cross-task awareness to avoid conflicting changes with sibling tasks. The manifest is refreshed before every turn. It names no other task's worktree: an agent sees the other tasks' prompts, status and branches, and works only in its own worktree.

## Data Models

See [Data & Storage](data-and-storage.md) for data model definitions.

## Persistence

See [Data & Storage](data-and-storage.md).

## Crash Recovery

On startup, `RecoverOrphanedTasks` in `runner/recovery.go` reconciles tasks that were interrupted by a server restart. It does not look for agent processes from the previous server; it decides from the stored status and the worktrees on disk:

| Previous status | Recovery action |
|---|---|
| `committing` | Inspect each worktree's branch tip: if a commit landed after `UpdatedAt` → `done`; otherwise → `failed` |
| `in_progress` | → `waiting`, so the user can review partial output, provide feedback, or mark the task done |

If a worktree is missing during recovery, the task is marked `failed` with `FailureCategory = worktree_setup`.

**Why `waiting` instead of `failed` for interrupted tasks?**
The task may have produced useful partial output. Moving to `waiting` lets the user inspect results and choose the next action (resume with feedback, mark as done, or cancel) rather than forcing a retry from scratch.

## Oversight Generation

See [Background Work and Recovery](background-work.md#oversight-generation).

## Output Truncation

Server-side output truncation is controlled by `WALLFACER_MAX_TURN_OUTPUT_BYTES` (default 8 MB). When a turn's stdout or stderr exceeds this limit, the output is truncated and a sentinel is appended. Truncated turn numbers are recorded in `Task.TruncatedTurns` so the UI can surface warnings.

---

## Storage Layer Deep-Dive

See [Data & Storage](data-and-storage.md).

## Migration System

See [Data & Storage](data-and-storage.md).

## Search Index

See [Data & Storage](data-and-storage.md).

## Soft Delete Implementation

See [Data & Storage](data-and-storage.md).

---

## Board Context Generation

Board context gives each running agent visibility into sibling tasks on the board, enabling cross-task coordination and conflict avoidance.

### What Board Context Contains

The board context is a `BoardManifest` JSON file written to a host BoardDir and handed to the agent by path via `WALLFACER_BOARD_JSON` (see [Board Context](#board-context) above):

```go
type BoardManifest struct {
    GeneratedAt time.Time   `json:"generated_at"`
    SelfTaskID  string      `json:"self_task_id"`
    Tasks       []BoardTask `json:"tasks"`
}
```

Each `BoardTask` entry includes:

| Field | Description |
|---|---|
| `id` | Full UUID string |
| `short_id` | First 8 characters of the UUID |
| `title` | Task display title |
| `prompt` | Task prompt (truncated to 500 chars for siblings) |
| `status` | Current task status |
| `is_self` | `true` for the task's own entry |
| `turns` | Turn count (0 for siblings to reduce noise) |
| `result` | Last result text (truncated to 1000 chars for siblings) |
| `stop_reason` | Last stop reason |
| `usage` | Token/cost usage |
| `branch_name` | Git branch name |
| `created_at` / `updated_at` | Timestamps |

`SessionID` is deliberately excluded from `BoardTask` to prevent session hijacking between tasks.

### How Board Context Is Assembled

`generateBoardContext` in `internal/runner/board.go` produces the `board.json` bytes:

1. Lists all non-archived tasks via `store.ListTasks`.
2. Builds the set of workspace repo paths the self task operates on (the keys of its worktree map, or the workspace folder when it has no worktree yet).
3. Filters siblings to only those sharing that folder, so tasks from before the workspace was re-pointed drop out.
4. For the self task: includes full `Prompt`, `Result`, and `Turns`.
5. For siblings: truncates `Prompt` to 500 chars, `Result` to 1000 chars, zeros `Turns`.
6. Marshals to JSON with indent; logs a warning if the manifest exceeds 64 KB.

### Caching

Results are cached by `(boardChangeSeq, selfTaskID)`. The cache is invalidated when any store mutation increments the change sequence. Per-turn calls cost nearly nothing when no task has changed since the last generation.

### API Endpoints

- `GET /api/tasks/{id}/board`, Returns the `BoardManifest` as it appears to a specific task (with that task marked `is_self: true`).
- `GET /api/debug/board`, Returns the `BoardManifest` as seen by a hypothetical new task (no task marked as self, `selfTaskID` = `uuid.Nil`).

---

## Task Summaries

See [Data & Storage](data-and-storage.md).

---

## See Also

- [Internals Index](internals.md), Overview and reading order for all internals docs
- [Architecture](architecture.md), System design, component overview, and design decisions
- [Data & Storage](data-and-storage.md), Data models, persistence, storage layer, migrations, search index, soft delete, task summaries
- [Background Work and Recovery](background-work.md), startup recovery, oversight generation
- [Git Worktrees](git-worktrees.md), Worktree lifecycle, branch naming, cleanup
- [API & Transport](api-and-transport.md), HTTP routes, SSE, metrics, middleware
