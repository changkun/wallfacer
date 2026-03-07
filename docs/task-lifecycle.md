# Task Lifecycle

## State Machine

Tasks progress through a well-defined set of states. Every transition is recorded as an immutable event in `data/<uuid>/traces/`.

```
BACKLOG ──drag / autopilot──→ IN_PROGRESS ──end_turn──────────────────→ DONE
   │                               │                                        │
   │                               ├──max_tokens / pause_turn──→ (loop)     └──drag──→ ARCHIVED
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
| `archived` | Done task moved off the active board |

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

Before running a task, users can chat with an AI assistant to iteratively improve the prompt. Only `backlog` tasks can be refined.

```
POST /api/tasks/{id}/refine
  body: { message: string, conversation: [{role, content}] }
  ↓
  On first call (empty conversation): primes with the task prompt and asks an
  opening clarifying question.
  On subsequent calls: appends the user message and continues the conversation.
  ↓
  Returns: { message: string, refined_prompt?: string }
  When Claude has gathered enough information it outputs "REFINED PROMPT: ..."
  which is extracted and returned separately for the UI to show an apply button.

POST /api/tasks/{id}/refine/apply
  body: { prompt: string, conversation: [{role, content}] }
  ↓
  Saves the refined prompt as the new task prompt.
  Moves the old prompt to PromptHistory.
  Persists the full conversation as a RefinementSession on the task.
  Triggers background title regeneration.
```

The refinement assistant calls the Anthropic Messages API directly (not via a container), using the configured credential from the env file. It uses `WALLFACER_DEFAULT_MODEL` (falling back to `claude-haiku-4-5`) and a 1,024-token response budget.

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

## Board Context

Each container receives a read-only `board.json` at `/workspace/.tasks/board.json` containing a manifest of all non-archived tasks. The current task is marked `"is_self": true`. This gives agents cross-task awareness to avoid conflicting changes with sibling tasks. The manifest is refreshed before every turn.

When `MountWorktrees` is enabled on a task, eligible sibling worktrees are also mounted read-only at `/workspace/.tasks/worktrees/<short-id>/<repo>/`.

## Data Models

Defined in `internal/store/models.go`:

**Task**
```
ID               string               // UUID
Title            string               // auto-generated short title
Prompt           string               // current task description
PromptHistory    []string             // previous prompt versions (before refinements)
RefineSessions   []RefinementSession  // history of prompt refinement chat sessions
Status           string               // current state
SessionID        string               // agent session ID (persisted across turns)
StopReason       string               // last stop_reason from Claude
Result           string               // last result text from Claude
Turns            int                  // number of completed turns
Timeout          int                  // per-turn timeout in minutes
FreshStart       bool                 // skip --resume on next run
MountWorktrees   bool                 // enable sibling worktree mounts + board context
Model            string               // per-task model override
Usage            TaskUsage            // accumulated token counts and cost
WorktreePaths    map[string]string    // repo path → worktree path
BranchName       string               // task branch name (e.g. task/a1b2c3d4)
CommitHashes     map[string]string    // repo path → commit hash after merge
BaseCommitHashes map[string]string    // repo path → base commit hash at branch creation

// Test verification
IsTestRun        bool   // true while a test agent is running on this task
LastTestResult   string // "pass", "fail", "unknown" (tested but ambiguous), or "" (untested)
TestRunStartTurn int    // turn count when the test run started (boundary between impl and test turns)
```

**RefinementSession** (one chat-based refinement interaction)
```
ID           string               // UUID
CreatedAt    time.Time
StartPrompt  string               // prompt text at the start of this session
Messages     []RefinementMessage  // full conversation
ResultPrompt string               // applied prompt (empty if discarded)
```

**RefinementMessage**
```
Role      string    // "user" or "assistant"
Content   string
CreatedAt time.Time
```

**TaskEvent** (append-only trace log)
```
ID        int64
TaskID    uuid.UUID
EventType EventType // state_change | output | feedback | error | system
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

## Persistence

Each task owns a directory under `data/<uuid>/`:

```
data/<uuid>/
├── task.json          # current task state (atomically overwritten on each update)
├── traces/
│   ├── 0001.json      # first event
│   ├── 0002.json      # second event
│   └── ...            # append-only
└── outputs/
    ├── turn-0001.json        # raw agent JSON output
    ├── turn-0001.stderr.txt  # stderr (if non-empty)
    └── ...
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
