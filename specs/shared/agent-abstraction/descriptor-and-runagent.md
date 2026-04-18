---
title: AgentRole descriptor and runAgent core
status: archived
depends_on: []
affects:
  - internal/runner/agent.go
  - internal/runner/agent_test.go
  - internal/constants/
effort: medium
created: 2026-04-18
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---


# AgentRole descriptor and runAgent core

## Goal

Introduce the core primitives chosen in the parent spec's Decision
(Option A): the `AgentRole` descriptor, the `MountMode` enum, and a
central `runAgent()` that handles the shared container launch sequence
(build spec → launch → read NDJSON → parse → accumulate usage →
token-limit fallback). Ship the primitive with headless-tier support
only (`MountMode: MountNone`) and a test, but do not migrate any
callers yet — that work lives in the per-tier sibling tasks.

This task also folds in the "incidental drift fixes" from the parent
spec so every subsequent migration inherits normalized flag order,
container naming, and centralized timeouts.

## What to do

1. Create `internal/runner/agent.go`:
   ```go
   type MountMode int
   const (
       MountNone MountMode = iota
       MountReadOnly
       MountReadWrite
   )

   type AgentRole struct {
       Activity    store.SandboxActivity
       PromptTmpl  string
       Timeout     func(*store.Task) time.Duration
       MountMode   MountMode
       MountBoard  bool
       SingleTurn  bool
       ParseResult func(string) (any, error)
       Name        string   // used for container naming: wallfacer-{Name}-{uuid8}
   }
   ```

2. Implement `runAgent(ctx, role AgentRole, task *store.Task, prompt string, opts runAgentOpts) (runAgentResult, error)` that, for `MountMode == MountNone`:
   - Resolves the sandbox via `sandboxForTaskActivity` (existing 4-tier hierarchy).
   - Builds the container spec from `buildBaseContainerSpec`, sets
     `Entrypoint`, `Name = "wallfacer-" + role.Name + "-" + shortUUID()`,
     and command args `{"--verbose", "--output-format", "stream-json", "-p", prompt}`.
   - Registers the container in the appropriate registry (expose a
     `Registry *containerRegistry` on `runAgentOpts` so callers keep
     their existing registry slot).
   - Calls `backend.Launch`, drains stdout/stderr, waits for exit.
   - Parses NDJSON via existing `parseAgentOutput`.
   - Accumulates usage via `AccumulateSubAgentUsage` +
     `AppendTurnUsage`.
   - On token-limit error returned by the parser, retries once against
     the fallback sandbox via the existing `retryWithFallbackSandbox`
     helper (extracted in step 4 below).

3. Extract the token-limit fallback currently duplicated across
   `oversight.go`, `title.go`, `commit.go`, `refine.go`, `ideate.go`
   into a single helper `retryWithFallbackSandbox(ctx, role, task,
   prompt, primary)` in `agent.go`. Each existing caller keeps its
   existing wrapper types for now — they will be removed as they
   migrate in sibling tasks.

4. Centralize the hardcoded timeouts referenced by the parent spec's
   "incidental drift fixes" into `internal/constants/`:
   - `TitleAgentTimeout = 60 * time.Second`
   - `OversightAgentTimeout = 3 * time.Minute`
   - `CommitMessageAgentTimeout = 90 * time.Second`
   - Reuse the existing `RefinementTimeout` and task-level
     `constants.DefaultTaskTimeout` for the inspector / heavyweight
     tiers.

5. Add `Name` and `Timeout` helpers used by callers; no descriptors for
   the seven concrete roles yet — those land in the per-tier tasks.

6. Ensure `runAgent` only supports `MountNone` for this task. Calling
   it with `MountReadOnly` or `MountReadWrite` must return
   `fmt.Errorf("runAgent: mount mode %v not yet implemented", m)` so a
   mis-wired sibling task fails loudly rather than silently running
   without mounts.

## Tests

- `internal/runner/agent_test.go`:
  - `TestAgentRole_ContainerNameShape` — ensures `Name` ->
    `wallfacer-{name}-{uuid8}` with an 8-hex-char suffix.
  - `TestRunAgent_HeadlessHappyPath` — using `MockExecutor` (pattern
    from `executor_mock_test.go`), run a stubbed headless role, assert
    NDJSON parsing, usage accumulation, and returned result.
  - `TestRunAgent_RejectsMountReadOnly` — returns the "not yet
    implemented" error.
  - `TestRunAgent_RejectsMountReadWrite` — returns the "not yet
    implemented" error.
  - `TestRunAgent_TokenLimitFallback` — primary sandbox returns a
    token-limit error; assert one retry on the fallback sandbox.
- `internal/constants/` — no tests needed (constants), but running
  `go test ./...` must stay green.

## Boundaries

- Do not migrate any existing role in this task — `title.go`,
  `oversight.go`, `commit.go`, `refine.go`, `ideate.go`, and
  `execute.go` keep their current duplicated code paths. The point of
  this task is to land the primitive and verify its shape with a
  self-contained test.
- Do not add `MountReadOnly` or `MountReadWrite` support yet — the
  primitive ships headless-only so the migration tasks prove the
  pattern tier by tier.
- Do not touch the runner's turn loop (auto-continue, session
  recovery) — that's phase 3 scope.
- Do not introduce inter-agent communication (Options C/D from the
  parent spec) — deferred indefinitely.
