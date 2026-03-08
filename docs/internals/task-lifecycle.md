# Task Lifecycle

## State Machine

Tasks progress through a well-defined set of states. Every transition is recorded as an immutable event in `data/<uuid>/traces/`.

```
BACKLOG ──drag / autopilot──→ IN_PROGRESS ──end_turn──────────────────→ DONE
   │                               │                                        │
   │                               ├──max_tokens / pause_turn──→ (loop)     └──set archived=true──→ (Archived column)
   │                               │
   │                               ├──empty stop_reason──→ WAITING ──feedback──→ IN_PROGRESS
   │                               │                              ──mark done──→ COMMITTING → DONE
   │                               │                              ──test──────→ IN_PROGRESS (test run)
   │                               │                              ──sync──────→ IN_PROGRESS (rebase) → WAITING
   │                               │                              ──cancel────→ CANCELLED
   │                               │
   │             IN_PROGRESS (test run) ──end_turn──→ WAITING (+ verdict recorded)
   │                               │
   │                               └──is_error / timeout──→ FAILED ──resume──→ IN_PROGRESS (same session)
   │                                                               ──sync───→ IN_PROGRESS (rebase) → FAILED
   │                                                               ──retry───→ BACKLOG (fresh session)
   │                                                               ──cancel──→ CANCELLED
   │
   └──cancel──→ CANCELLED ──retry──→ BACKLOG
                        └──set archived=true──→ (Archived column)
```

## States

| State | Description |
|---|---|
| `backlog` | Queued, not yet started |
| `in_progress` | Container running, agent executing |
| `waiting` | Claude paused mid-task, awaiting user feedback |
| `committing` | Transient: commit pipeline running after mark-done |
| `done` | Completed; changes committed and merged |
| `failed` | Container error, Claude error, or timeout |
| `cancelled` | Explicitly cancelled; sandbox cleaned up, history preserved |

**Note:** `archived` is a boolean flag (`Archived bool`) on the task, not a separate state. Tasks in `done` or `cancelled` state can have `Archived = true`, which moves them to the Archived column in the UI. The state machine has exactly 7 states (`backlog`, `in_progress`, `waiting`, `committing`, `done`, `failed`, `cancelled`).

## Turn Loop

Each pass through the loop in `runner.go` `Run()`:

1. Increment turn counter
2. Run container with current prompt and session ID
3. Save raw stdout to `data/<uuid>/outputs/turn-NNNN.json`; stderr (if any) to `turn-NNNN.stderr.txt`
4. Parse `stop_reason` from agent JSON output:

| `stop_reason` | `is_error` | Result |
|---|---|---|
| `end_turn` | false | Exit loop → trigger commit pipeline → `done` (or → `waiting` with verdict if this is a test run) |
| `max_tokens` | false | Auto-continue (next iteration, same session) |
| `pause_turn` | false | Auto-continue (next iteration, same session) |
| empty / unknown | false | Set `waiting`; block until user provides feedback |
| any | true | Set `failed` |

5. Accumulate token usage (`input_tokens`, `output_tokens`, cache tokens, `cost_usd`)

## Session Continuity

Claude Code supports `--resume <session-id>` for session continuity. The first turn creates a new session; subsequent turns (auto-continue or post-feedback) pass the same session ID, preserving the full conversation context.

Setting `FreshStart = true` on a task skips `--resume`, starting a brand-new session. This is what happens when a user retries a failed task.

## Feedback & Waiting State

When `stop_reason` is empty, Claude has asked a question or is blocked. The task enters `waiting`:

- Worktrees are **not** cleaned up — the git branch is preserved
- User submits feedback via `POST /api/tasks/{id}/feedback`
- Handler writes a `feedback` event to the trace log, then launches a new `runner.Run` goroutine using the existing session ID
- The task resumes from exactly where it paused, with the feedback message as the next prompt

Alternatively, the user can mark the task done from `waiting`, which skips further Claude turns and jumps straight to the commit pipeline.

## Cancellation

Any task in `backlog`, `in_progress`, `waiting`, or `failed` can be cancelled via `POST /api/tasks/{id}/cancel`. The handler:

1. **Kills the container** (if `in_progress`) — sends `<runtime> kill wallfacer-<uuid>`. The running goroutine detects the cancelled status and exits without overwriting it to `failed`.
2. **Cleans up worktrees** — removes the git worktree and deletes the task branch, discarding all prepared changes.
3. **Sets status to `cancelled`** and appends a `state_change` event.
4. **Preserves history** — `data/<uuid>/traces/` and `data/<uuid>/outputs/` are left intact so execution logs, token usage, and the event timeline remain visible.

From `cancelled`, the user can retry the task (moves it back to `backlog`) to restart from scratch.

## Title Generation

When a task is created, a background goroutine (`runner.GenerateTitle`) launches a lightweight container to generate a short title from the prompt. Titles are stored on the task and displayed on the board cards instead of the full prompt text. `POST /api/tasks/generate-titles` can retroactively generate titles for older untitled tasks.

## Prompt Refinement

Before running a task, users can have an AI agent analyse the codebase and produce a detailed implementation spec (the refined prompt). Only `backlog` tasks can be refined.

```
POST /api/tasks/{id}/refine
  body: { user_instructions? }   // optional additional guidance
  ↓
  Sets CurrentRefinement.Status = "running".
  Launches a sandbox container in the background.
  Returns 202 Accepted immediately.

GET /api/tasks/{id}/refine/logs  (SSE)
  Streams container output in real time.

Container finishes:
  CurrentRefinement.Status = "done", Result = spec text.
  — or —
  CurrentRefinement.Status = "failed", Error = failure message.

POST /api/tasks/{id}/refine/apply
  body: { prompt: string }
  ↓
  Saves the refined prompt as the new task prompt.
  Moves the old prompt to PromptHistory.
  Persists a RefinementSession (recording sandbox result and applied prompt).
  Clears CurrentRefinement.
  Triggers background title regeneration.

POST /api/tasks/{id}/refine/dismiss
  ↓
  Clears CurrentRefinement without changing the prompt.

DELETE /api/tasks/{id}/refine
  ↓
  Kills the running refinement container.
  Sets CurrentRefinement.Status = "failed".
```

Both `RefineSessions []RefinementSession` (past history) and `CurrentRefinement *RefinementJob` (present job) live on the Task struct. `RefineSessions` grows over time as each refinement is applied; `CurrentRefinement` is replaced on each new run and cleared on dismiss.

## Test Verification

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

## Autopilot

When autopilot is enabled, the server automatically promotes backlog tasks to `in_progress` as capacity becomes available, without requiring the user to drag cards manually.

```
PUT /api/config { "autopilot": true }
  ↓
  StartAutoPromoter goroutine subscribes to store change notifications.
  On each state change:
    If autopilot enabled and in_progress count < WALLFACER_MAX_PARALLEL:
      Pick the lowest-position backlog task.
      Promote it to in_progress and launch runner.Run.
```

Concurrency limit is read from `WALLFACER_MAX_PARALLEL` in the env file (default: 5). Autopilot is off by default and does not persist across server restarts.

Tasks whose `DependsOn` list contains any task not yet in `done` status are skipped by the auto-promoter even when the in-progress count is below `WALLFACER_MAX_PARALLEL`.

## Board Context

Each container receives a read-only `board.json` at `/workspace/.tasks/board.json` containing a manifest of all non-archived tasks. The current task is marked `"is_self": true`. This gives agents cross-task awareness to avoid conflicting changes with sibling tasks. The manifest is refreshed before every turn.

When `MountWorktrees` is enabled on a task, eligible sibling worktrees are also mounted read-only at `/workspace/.tasks/worktrees/<short-id>/<repo>/`.

## Data Models

Defined in `internal/store/models.go`:

**Task**
```
ID                 uuid.UUID            // UUID
Title              string               // auto-generated short title
Prompt             string               // current task description (short card label)
PromptHistory      []string             // previous prompt versions (before refinements)
RefineSessions     []RefinementSession  // history of completed sandbox refinement sessions
CurrentRefinement  *RefinementJob       // active or recently completed sandbox refinement job
Status             TaskStatus           // current state
Archived           bool                 // true when moved to archived view (done/cancelled tasks only)
SessionID          *string              // agent session ID (persisted across turns)
FreshStart         bool                 // skip --resume on next run
StopReason         *string              // last stop_reason from Claude
Result             *string              // last result text from Claude
Turns              int                  // number of completed turns
Timeout            int                  // per-turn timeout in minutes
Usage              TaskUsage            // accumulated token counts and cost (all activities)
UsageBreakdown     map[string]TaskUsage // token/cost per sub-agent activity key
Sandbox            string               // container image override for this task
SandboxByActivity  map[string]string    // per-activity image overrides (e.g. "testing" → "wallfacer-codex:latest")
Model              string               // deprecated: retained for migration compatibility; use SandboxByActivity
Position           int                  // sort order within column
CreatedAt          time.Time
UpdatedAt          time.Time
MountWorktrees     bool                 // enable sibling worktree mounts + board context
WorktreePaths      map[string]string    // repo path → worktree path
BranchName         string               // task branch name (e.g. task/a1b2c3d4)
CommitHashes       map[string]string    // repo path → commit hash after merge
BaseCommitHashes   map[string]string    // repo path → base commit hash at branch creation
Kind               TaskKind             // "" or "idea-agent" (TaskKindIdeaAgent)
Tags               []string             // labels for categorisation
ExecutionPrompt    string               // overrides Prompt when invoking the sandbox agent; keeps Prompt as the short card label
DependsOn          []string             // UUIDs of prerequisite tasks; blocks autopilot promotion until all are done

// Test verification
IsTestRun        bool   // true while a test agent is running on this task
LastTestResult   string // "pass", "fail", "unknown" (tested but ambiguous), or "" (untested)
TestRunStartTurn int    // turn count when the test run started (boundary between impl and test turns)
```

**RefinementSession** (one completed sandbox refinement interaction, stored in history)
```
ID           string               // UUID
CreatedAt    time.Time
StartPrompt  string               // prompt text at the start of this session
Result       string               // raw spec produced by the sandbox agent
ResultPrompt string               // prompt the user applied (may differ from Result if edited)
Messages     []RefinementMessage  // kept for backward compatibility with older chat-based sessions
```

**RefinementMessage** (legacy; used in older chat-based sessions)
```
Role      string    // "user" or "assistant"
Content   string
CreatedAt time.Time
```

**RefinementJob** (tracks the active or most-recently-completed sandbox refinement run)
```
ID        string    // UUID
CreatedAt time.Time
Status    string    // "running" | "done" | "failed"
Result    string    // refined prompt/spec text (populated when Status = "done")
Error     string    // error message (populated when Status = "failed")
```

**TaskOversight** (aggregated high-level summary of agent execution)
```
Status       OversightStatus  // "pending" | "generating" | "ready" | "failed"
GeneratedAt  time.Time
Error        string
Phases       []OversightPhase
```

**OversightPhase** (one logical grouping of related agent activities)
```
Timestamp  time.Time
Title      string
Summary    string
ToolsUsed  []string
Commands   []string
Actions    []string
```

**SpanData** (attached to span_start / span_end trace events)
```
Phase  string  // e.g. "worktree_setup", "agent_turn", "container_run", "commit"
Label  string  // differentiates multiple spans of the same phase
```

**TaskEvent** (append-only trace log)
```
ID        int64
TaskID    uuid.UUID
EventType EventType // state_change | output | feedback | error | system | span_start | span_end
Data      json.RawMessage
CreatedAt time.Time
```

**TaskUsage**
```
InputTokens              int
OutputTokens             int
CacheReadInputTokens     int
CacheCreationInputTokens int
CostUSD                  float64
```

**EventType values**

| Value | Description |
|---|---|
| `state_change` | Task moved to a new state |
| `output` | Agent turn output text |
| `feedback` | User-submitted feedback message |
| `error` | Error during execution |
| `system` | Server-inserted note (e.g. crash recovery message, pipeline progress) |
| `span_start` | Start of a named execution phase (data: SpanData) |
| `span_end` | End of a named execution phase (data: SpanData) |

## Persistence

Each task owns a directory under `data/<uuid>/`:

```
data/<uuid>/
├── task.json          # current task state (atomically overwritten on each update)
├── traces/
│   ├── 0001.json      # first event
│   ├── 0002.json      # second event
│   └── ...            # append-only
├── outputs/
│   ├── turn-0001.json        # raw agent JSON output
│   ├── turn-0001.stderr.txt  # stderr (if non-empty)
│   └── ...
└── oversights/
    └── <oversight-id>.json   # generated oversight summary
```

All writes are atomic (temp file + `os.Rename`). On startup, `task.json` files are loaded into memory. See [Architecture](architecture.md#design-choices) for the persistence design rationale.

## Crash Recovery

On startup, `recoverOrphanedTasks` in `server.go` reconciles tasks that were interrupted by a server restart. It first queries the container runtime to determine which containers are still running, then handles each interrupted task as follows:

| Previous status | Container state | Recovery action |
|---|---|---|
| `committing` | any | → `failed` — commit pipeline cannot be safely resumed |
| `in_progress` | still running | Stay `in_progress`; a monitor goroutine watches the container and transitions to `waiting` once it stops |
| `in_progress` | already stopped | → `waiting` — user can review partial output, provide feedback, or mark as done |

**Why `waiting` instead of `failed` for stopped containers?**
The task may have produced useful partial output. Moving to `waiting` lets the user inspect results and choose the next action (resume with feedback, mark as done, or cancel) rather than forcing a retry from scratch.

**Monitor goroutine** (`monitorContainerUntilStopped`):
When a container is found still running after a restart, a background goroutine polls `podman/docker ps` every 5 seconds. Once the container stops it moves the task from `in_progress` to `waiting` with an explanatory output event. If the task was already transitioned by another path (e.g. cancelled by the user) the goroutine exits cleanly.

## Oversight Generation

When a task transitions to `waiting`, `done`, or `failed`, the server launches a background goroutine to generate an oversight summary. The summary is also regenerated periodically if `WALLFACER_OVERSIGHT_INTERVAL` is set to a positive number of minutes.

The generator reads the task's trace events, passes them to the Claude API with a summarisation prompt, and writes the result as a `TaskOversight` (`status`: `pending` → `generating` → `ready` | `failed`). The result is persisted in `data/<uuid>/oversights/<id>.json`.

The UI shows the oversight in the Oversight tab (logical phases with tools/commands used) and as an interactive flamegraph Timeline.

`POST /api/tasks/generate-oversight` can be used to retroactively generate oversight for tasks that completed before this feature existed.

## Ideation / Brainstorm Agent

The ideation feature creates a task with `Kind = "idea-agent"`. The agent runs in a sandbox container, reads the configured workspaces, and calls the wallfacer API to create backlog tasks.

- Each created task gets relevant `Tags` and an `ExecutionPrompt` (full instructions) separate from `Prompt` (the short card label).
- Triggered via `POST /api/ideate`; cancelled via `DELETE /api/ideate`.
- `GET /api/ideate` returns current ideation session state (task ID, status, created task count).
