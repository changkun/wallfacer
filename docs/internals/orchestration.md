# Orchestration Flows

## HTTP API

All state changes flow through `handler.go`. The handler never blocks — long-running work is always handed off to a goroutine.

### Routes

| Method + Path | Handler action |
|---|---|
| `GET /api/config` | Return workspace paths, instructions file path, autopilot state, available models |
| `PUT /api/config` | Update server config (`{autopilot: bool}`); re-triggers auto-promotion if enabled |
| `GET /api/env` | Return current env config (tokens masked) |
| `PUT /api/env` | Update env config (token, base URL, default model, title model, max parallel); writes `~/.wallfacer/.env` atomically |
| `GET /api/instructions` | Get workspace CLAUDE.md content |
| `PUT /api/instructions` | Save workspace CLAUDE.md (`{content}`) |
| `POST /api/instructions/reinit` | Rebuild workspace CLAUDE.md from default + repo files |
| `GET /api/tasks` | List all tasks (from in-memory store) |
| `POST /api/tasks` | Create task, assign UUID, persist to disk |
| `PATCH /api/tasks/{id}` | Update status / position / prompt / timeout — may launch `runner.Run` goroutine |
| `DELETE /api/tasks/{id}` | Delete task + cleanup worktrees |
| `POST /api/tasks/{id}/feedback` | Write feedback event → launch `runner.Run` (resume) goroutine |
| `POST /api/tasks/{id}/done` | Set `committing` → launch commit pipeline goroutine |
| `POST /api/tasks/{id}/cancel` | Kill container (if running), clean up worktrees, set `cancelled`; traces/logs kept |
| `POST /api/tasks/{id}/resume` | Resume failed task, same session → launch `runner.Run` goroutine |
| `POST /api/tasks/{id}/sync` | Rebase task worktrees onto latest default branch (waiting/failed only) |
| `POST /api/tasks/{id}/test` | Start test verification agent on a waiting task; records pass/fail verdict |
| `POST /api/tasks/{id}/refine` | Launch sandbox refinement container for a backlog task |
| `DELETE /api/tasks/{id}/refine` | Cancel active sandbox refinement container |
| `GET /api/tasks/{id}/refine/logs` | SSE: stream refinement container logs |
| `POST /api/tasks/{id}/refine/apply` | Apply refined prompt; persists result as `RefinementSession` |
| `POST /api/tasks/{id}/refine/dismiss` | Discard refinement result without applying |
| `GET /api/tasks/{id}/oversight` | Get aggregated oversight summary for a task |
| `GET /api/tasks/{id}/oversight/test` | Get test-run oversight summary |
| `GET /api/tasks/{id}/spans` | Get span timing data for a task |
| `POST /api/tasks/{id}/archive` | Move done or cancelled task to archived |
| `POST /api/tasks/{id}/unarchive` | Restore archived task |
| `POST /api/tasks/archive-done` | Archive all done and cancelled tasks in one operation |
| `GET /api/tasks/stream` | SSE: push task list on any state change |
| `GET /api/tasks/{id}/events` | Return full event trace log |
| `GET /api/tasks/{id}/diff` | Git diff for task worktrees vs default branch |
| `GET /api/tasks/{id}/outputs/{filename}` | Serve raw turn output file |
| `GET /api/tasks/{id}/logs` | SSE: stream live container logs (`podman/docker logs -f`) |
| `POST /api/tasks/generate-titles` | Trigger background title generation for untitled tasks |
| `POST /api/tasks/generate-oversight` | Generate oversight summaries for tasks missing them |
| `GET /api/containers` | List all wallfacer sandbox containers (running and stopped) |
| `GET /api/git/status` | Current branch / remote status for all workspaces |
| `GET /api/git/stream` | SSE: poll git status every few seconds |
| `POST /api/git/push` | Run `git push` on a workspace |
| `POST /api/git/sync` | Fetch from remote and rebase workspace onto upstream |
| `POST /api/git/rebase-on-main` | Fetch remote default branch and rebase current workspace branch onto it (blocked while tasks in_progress) |
| `GET /api/git/branches` | List local branches for a workspace (`?workspace=<path>`) |
| `POST /api/git/checkout` | Switch the active branch for a workspace |
| `POST /api/git/create-branch` | Create a new branch and check it out |
| `GET /api/ideate` | Get current ideation session state |
| `POST /api/ideate` | Launch brainstorm/ideation agent |
| `DELETE /api/ideate` | Cancel running ideation agent |
| `GET /api/usage` | Aggregate usage statistics across all tasks |
| `GET /api/files` | File listing for @ mention autocomplete |
| `POST /api/env/test` | Validate sandbox credentials via test container |
| `GET /api/debug/health` | Health check |
| `GET /api/debug/spans` | Aggregate span timing statistics |

### Triggering Task Execution

When a `PATCH /api/tasks/{id}` request moves a task to `in_progress`, the handler:

1. Updates the task record (status, session ID)
2. Launches a background goroutine: `go h.runner.Run(id, prompt, sessionID, false)`
3. Returns `200 OK` immediately — the client does not wait for execution

The same pattern applies to feedback resumption and commit-and-push.

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

## Container Execution (`runner.go` `runContainer`)

Each turn launches an ephemeral container via the configured runtime (Podman or Docker):

```
<podman|docker> run --rm \
  --name wallfacer-<uuid> \
  --env-file ~/.wallfacer/.env \
  -v claude-config:/home/claude/.claude \
  -v <worktree-path>:/workspace/<repo-name> \
  -v ~/.gitconfig:/home/claude/.gitconfig:ro \
  wallfacer:latest \
  claude -p "<prompt>" \
         --model <model> \
         --resume <session-id> \
         --verbose \
         --output-format stream-json
```

- `--rm` — container is destroyed on exit; no state leaks between tasks
- `--env-file` — injects `CLAUDE_CODE_OAUTH_TOKEN` (or `ANTHROPIC_API_KEY`), `ANTHROPIC_BASE_URL`, and any other variables from `~/.wallfacer/.env` into the container environment
- `--model` — per-task model takes priority; falls back to `CLAUDE_DEFAULT_MODEL` from the env file; the server re-reads the file on every container launch so changes take effect immediately without a restart
- `--resume` — omitted on the first turn or when `FreshStart` is set
- Output is captured as NDJSON, parsed, and saved to disk
- Stderr is saved separately if non-empty

The container name `wallfacer-<uuid>` lets the server stream logs with `<runtime> logs -f wallfacer-<uuid>` while the container is running.

### Container Runtime Auto-Detection

The `-container` flag defaults to auto-detection (`detectContainerRuntime()` in `main.go`):

1. `/opt/podman/bin/podman` — preferred explicit Podman installation
2. `podman` on `$PATH`
3. `docker` on `$PATH`

Override with `CONTAINER_CMD` env var or `-container` flag. Both Podman and Docker are fully supported — the server handles their different JSON output formats transparently (Podman emits a JSON array from `ps --format json`; Docker emits NDJSON with one object per line).

### Board Context

Each container receives a read-only board context at `/workspace/.tasks/board.json`. This JSON manifest lists all non-archived tasks on the board — their prompts, statuses, results, branch names, and usage — so agents have cross-task awareness and can avoid conflicting changes.

The current task is marked with `"is_self": true`. The manifest is regenerated before every turn to reflect the latest state.

When `MountWorktrees` is enabled on a task, eligible sibling worktrees (from tasks in `waiting`, `failed`, or `done` status) are also mounted read-only under `/workspace/.tasks/worktrees/<short-id>/<repo>/`, allowing the agent to reference other tasks' in-progress code.

## SSE Live Update Flow

Both task state and git status use the same SSE push pattern:

```
UI opens EventSource → GET /api/tasks/stream
  handler registers subscriber channel
  ↓
any store.Write() call → notify() sends signal (non-blocking, coalesced)
  ↓
handler wakes, serialises full task list as JSON
  sends: data: <json>\n\n
  ↓
UI receives event → re-renders board
```

`notify()` uses a buffered channel of size 1. If a signal is already pending (UI hasn't drained yet), the new signal is dropped — the subscriber will still get the latest state on the next drain. This coalesces bursts of rapid state changes into a single UI update.

The same pattern applies to `GET /api/git/stream`, except the source is a time-based ticker (polling `git status` every few seconds) rather than a store write signal.

Live container logs use a different mechanism: `GET /api/tasks/{id}/logs` opens a process pipe to `<runtime> logs -f <name>` and streams its stdout line-by-line as SSE events.

## Store Concurrency

`store.go` manages an in-memory `map[string]*Task` behind a `sync.RWMutex`:

- Reads (`List`, `Get`) acquire a read lock
- Writes (`Create`, `Update`, `UpdateStatus`) acquire a write lock, mutate memory, then atomically persist to disk (temp file + `os.Rename`)
- After every write, `notify()` is called to wake SSE subscribers

Event traces are append-only. Each event is written as a separate file (`traces/NNNN.json`) using the same atomic write pattern. Files are never modified after creation.

## Token Tracking & Cost

Per-turn usage is extracted from the agent JSON output and accumulated on the `Task`:

```
TaskUsage {
  InputTokens              int
  OutputTokens             int
  CacheReadInputTokens     int
  CacheCreationInputTokens int
  CostUSD                  float64
}
```

Usage is displayed on task cards and aggregated in the Done column header. It persists in `task.json` across server restarts.

In addition to the aggregate `TaskUsage`, each task records a `UsageBreakdown map[string]TaskUsage` keyed by activity: `implementation`, `testing`, `refinement`, `title`, `oversight`, `commit_message`, `idea_agent`. This lets the Usage tab in the task detail panel show cost per sub-agent rather than a single lump sum.

## Multi-Workspace Support

Multiple workspace paths can be passed at startup (see [Architecture — Configuration](architecture.md#configuration)). For each workspace:

- Git status is polled independently and shown in the UI header
- A separate worktree is created per task per workspace
- The commit pipeline runs phases 1–3 for each workspace in sequence

Non-git directories are supported as plain mount targets (no worktree, no commit pipeline for that workspace).

## Conflict Resolution Flow

When `git rebase` fails during the commit pipeline:

```
rebase fails with conflict
  ↓
wallfacer invokes agent (same session ID) with conflict details
  ↓
agent resolves conflicts, stages files
  ↓
wallfacer runs `git rebase --continue`
  ↓
if still failing: repeat up to 3 times
  ↓
if all retries exhausted: mark task failed, clean up worktrees
```

Using the same session ID means the agent has full context of the original task when making conflict resolution decisions.

## Test Verification Flow

`POST /api/tasks/{id}/test` runs a separate verification agent on a `waiting` task without committing:

```
waiting task + user clicks Test
  ↓
handler sets IsTestRun=true, clears LastTestResult
  ↓
task status: waiting → in_progress
  ↓
fresh container (no --resume) runs with a structured test prompt:
  "Examine the code, run tests, verify requirements.
   End your response with **PASS** or **FAIL**."
  ↓
runner loop (isTestRun=true):
  - tracks turn count separately (TestRunStartTurn boundary)
  - does NOT update the implementation sessionID or result
  - handles max_tokens / pause_turn by resuming the test session (not impl session)
  ↓
on end_turn:
  parseTestVerdict() extracts verdict from last line or **PASS**/**FAIL** markers
  verdict: "pass" | "fail" | "unknown"
  IsTestRun set back to false
  LastTestResult = verdict
  task status: in_progress → waiting (no commit pipeline)
```

The UI splits the live output panel into "Implementation" and "Test" sections using `TestRunStartTurn` as the boundary.

## Autopilot (Auto-Promotion) Flow

Autopilot automatically promotes backlog tasks without user drag-and-drop:

```
StartAutoPromoter():
  subscribe to store change notifications (buffered channel, coalesced)
  on each notification:
    if autopilot disabled → skip
    lock promoteMu (serialise concurrent notifications)
    count in_progress tasks
    if count < WALLFACER_MAX_PARALLEL and backlog not empty:
      pick lowest-position backlog task
      update status: backlog → in_progress
      go runner.Run(task)
```

`WALLFACER_MAX_PARALLEL` defaults to 5. The lock ensures two simultaneous state changes cannot both promote tasks, which would exceed the limit. Autopilot state is toggled via `PUT /api/config {"autopilot": true/false}` and does not persist across restarts.

## Refinement Flow

`POST /api/tasks/{id}/refine` launches a sandbox container to analyse the codebase and produce a detailed implementation spec:

```
POST /api/tasks/{id}/refine
  body: { user_instructions? }   // optional additional instructions
  ↓
  Sets CurrentRefinement.Status = "running".
  Launches sandbox container in background (runner.RunRefinementBackground).
  Returns immediately with 202 Accepted.
  ↓
GET /api/tasks/{id}/refine/logs  (SSE)
  Client streams container output in real time.
  ↓
Container finishes:
  CurrentRefinement.Status = "done", Result = refined prompt/spec text.
  — or —
  CurrentRefinement.Status = "failed", Error = failure message.

POST /api/tasks/{id}/refine/apply
  body: { prompt }
  ↓
  Saves a RefinementSession (recording the sandbox result and the applied prompt).
  Moves current Prompt to PromptHistory.
  Sets task.Prompt = refined prompt.
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

## Oversight Generation Flow

Oversight is generated asynchronously whenever a task transitions to `waiting`, `done`, or `failed`. It is also regenerated periodically during execution if `WALLFACER_OVERSIGHT_INTERVAL > 0` (minutes).

`POST /api/tasks/generate-oversight` triggers generation for tasks that are missing summaries.

```
Task reaches waiting/done/failed
  ↓
background goroutine: runner.GenerateOversight(taskID)
  ↓
TaskOversight.Status → "generating"
reads trace events from traces/NNNN.json
sends to Claude (via configured credentials) with a summarisation prompt
  ↓
response parsed into []OversightPhase (logical groupings of work)
  ↓
TaskOversight.Status → "ready"
stored in oversights/<id>.json
```

Served by:
- `GET /api/tasks/{id}/oversight` — implementation run summary
- `GET /api/tasks/{id}/oversight/test` — test-run summary (if a test was run)

The UI renders phases in the Oversight tab and as an interactive flamegraph Timeline.

## Ideation / Brainstorm Agent Flow

```
POST /api/ideate
  ↓
  Creates a special task with Kind = "idea-agent".
  Launches a sandbox container that:
    - Reads workspace directory contents
    - Analyses code structure and identifies opportunities
    - Creates backlog tasks via the wallfacer API, each tagged appropriately
  Container runs to completion; created tasks appear on the board.

GET /api/ideate  — returns current ideation session state (task ID, status, created task count)
DELETE /api/ideate  — kills running ideation container, marks task cancelled
```

## Span Instrumentation

Key execution phases are instrumented with `span_start` / `span_end` trace events. Each span carries a `SpanData` payload with a `Phase` (e.g. `worktree_setup`, `agent_turn`, `container_run`, `commit`) and an optional `Label` to differentiate multiple spans of the same phase.

- `GET /api/tasks/{id}/spans` — returns all span events for a task, useful for profiling turn latency
- `GET /api/debug/spans` — aggregate span timing statistics across all tasks
