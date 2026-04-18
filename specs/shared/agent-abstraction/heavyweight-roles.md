---
title: Migrate heavyweight roles (implementation, testing) to runAgent
status: complete
depends_on:
  - specs/shared/agent-abstraction/inspector-roles.md
affects:
  - internal/runner/agent.go
  - internal/runner/container.go
  - internal/runner/execute.go
  - internal/runner/execute_testrun.go
effort: large
created: 2026-04-18
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---

# Migrate heavyweight roles to runAgent

## Goal

Extend `runAgent()` with read-write workspace mounts + board context +
sibling worktrees, then migrate the implementation and testing
container-launch code paths onto it. This is the most intricate tier
because the turn loop (auto-continue, session recovery, verdict
inference) wraps the launch — we keep that wrapping in
`execute.go` and only swap the inner launch call.

## What to do

1. In `internal/runner/agent.go`, extend `runAgent` with
   `MountMode == MountReadWrite`:
   - Mount each task worktree under `/workspace/<basename>` RW,
     matching the existing `buildBaseContainerSpec` layout.
   - When `MountBoard == true`:
     - Write the board manifest and mount it at `/workspace/board.json`.
     - Mount sibling worktrees from `siblingMounts` read-only at
       `/workspace/<basename>/`.
   - Mount the workspace instructions file read-only as today.
   - Set `spec.Cmd` to include session-specific flags from
     `RunAgentOpts` (e.g. `--resume sessionID` when provided).

2. Define two heavyweight descriptors:
   ```go
   var roleImplementation = AgentRole{
       Activity:    store.SandboxActivityImplementation,
       PromptTmpl:  "",                 // caller supplies rendered prompt directly
       Name:        "impl",
       Timeout:     func(t *store.Task) time.Duration {
           return time.Duration(t.Timeout) * time.Minute
       },
       MountMode:   MountReadWrite,
       MountBoard:  true,
       SingleTurn:  false,
       ParseResult: parseImplementationTurnResult,
   }
   var roleTesting = AgentRole{
       Activity:    store.SandboxActivityTesting,
       PromptTmpl:  "",
       Name:        "test",
       Timeout:     func(t *store.Task) time.Duration {
           return time.Duration(t.Timeout) * time.Minute
       },
       MountMode:   MountReadWrite,
       MountBoard:  true,
       SingleTurn:  false,
       ParseResult: parseTestTurnResult,
   }
   ```

3. `internal/runner/container.go`:
   - Refactor the private spec builder so `runAgent` (for
     `MountReadWrite`) and the existing `buildTaskContainerSpec` share
     the same mount/env logic. One helper, two entry points.

4. `internal/runner/execute.go`:
   - The top-level `Run` / `runContainer` turn loop stays here — it
     owns auto-continue, session recovery, verdict inference, and
     failure classification.
   - Each per-turn container launch inside the loop calls
     `r.runAgent(ctx, roleImplementation, task, prompt, opts)` where
     `opts` carries `SessionID`, turn index, and mountBoard worktree
     info.
   - `execute_testrun.go` likewise migrates the test-run launch to
     `runAgent(roleTesting, ...)` while keeping the outer test-run
     orchestration.

5. Remove orphaned code revealed by the migration:
   - `buildImplementationContainerSpec` / `buildTestContainerSpec`
     (whatever names exist today) collapse into the shared helper
     above.
   - Per-role fallback retry wrappers if any remain.

6. Verify existing end-to-end behavior — the turn loop's
   observable behavior (event stream, usage records, session IDs,
   resume on "no conversation") must stay identical. This task's
   acceptance gate is a clean pass of `make test-backend` plus the
   e2e lifecycle script for both sandboxes.

## Tests

- All existing tests in `execute_test.go`, `commit_test.go`,
  `recovery_test.go`, `execute_testrun` / `execute_stderr_test.go`
  must remain green.
- Add `TestRunAgent_MountReadWrite_MountsWorktreesAndBoard` — asserts
  the generated spec carries RW worktree mounts, the board.json mount,
  and the sibling read-only mounts when `MountBoard: true`.
- Add `TestRun_ImplementationUsesDescriptor` and
  `TestRun_TestRunUsesDescriptor` — spy on the runAgent call, assert
  descriptor identity per turn.

## Boundaries

- Do not refactor the turn loop itself — session recovery,
  auto-continue, and verdict inference stay in `execute.go`.
- Do not change worktree creation / sync / cleanup logic.
- Do not touch the commit pipeline — commit.go's worktree /
  rebase / push handling is unrelated.
- Do not change `interface.go` / mock.go public surfaces beyond
  what the new opts struct requires.
- Cleanup (dead code removal across the package) lives in the
  sibling cleanup task.
