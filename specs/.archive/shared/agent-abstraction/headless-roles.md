---
title: Migrate headless roles (title, oversight, commit message) to runAgent
status: archived
depends_on:
  - specs/shared/agent-abstraction/descriptor-and-runagent.md
affects:
  - internal/runner/title.go
  - internal/runner/oversight.go
  - internal/runner/commit.go
  - internal/runner/agent.go
effort: medium
created: 2026-04-18
updated: 2026-04-19
author: changkun
dispatched_task_id: null
---


# Migrate headless roles to runAgent

## Goal

Collapse the three nearly-identical headless-role launch sequences
(title generation, oversight summary, commit-message generation) onto
the shared `runAgent()` primitive introduced in the sibling
descriptor-and-runagent task. After this task, those three role
files carry descriptors + a thin wrapper that calls `runAgent`, not
duplicated container-launch plumbing.

## What to do

1. In `internal/runner/agent.go`, define the three headless role
   descriptors:
   ```go
   var roleTitle = AgentRole{
       Activity:    store.SandboxActivityTitle,
       PromptTmpl:  "title",
       Name:        "title",
       Timeout:     func(*store.Task) time.Duration { return constants.TitleAgentTimeout },
       MountMode:   MountNone,
       SingleTurn:  true,
       ParseResult: parseTitleResult, // existing helper
   }
   var roleOversight = AgentRole{
       Activity:    store.SandboxActivityOversight,
       PromptTmpl:  "oversight",
       Name:        "oversight",
       Timeout:     func(*store.Task) time.Duration { return constants.OversightAgentTimeout },
       MountMode:   MountNone,
       SingleTurn:  true,
       ParseResult: parseOversightResult,
   }
   var roleCommitMessage = AgentRole{
       Activity:    store.SandboxActivityCommitMessage,
       PromptTmpl:  "commit",
       Name:        "commit-msg",
       Timeout:     func(*store.Task) time.Duration { return constants.CommitMessageAgentTimeout },
       MountMode:   MountNone,
       SingleTurn:  true,
       ParseResult: parseCommitMessageResult,
   }
   ```
   `parseTitleResult` / `parseOversightResult` / `parseCommitMessageResult`
   wrap the existing role-specific output-extraction code so it slots
   into `AgentRole.ParseResult`.

2. `internal/runner/title.go`:
   - `GenerateTitle(ctx, taskID, prompt)` now calls
     `r.runAgent(ctx, roleTitle, task, renderedPrompt, runAgentOpts{...})`
     and treats the parsed result as the title string.
   - Remove: private container-spec builder, manual NDJSON read loop,
     inline fallback retry.
   - Keep: the caller's public surface and the "title also writes
     system event" behavior.

3. `internal/runner/oversight.go`:
   - `runOversightAgent(ctx, taskID, data)` migrates to
     `runAgent(roleOversight, ...)`.
   - Remove: `buildOversightContainerSpec`, `oversightRunResult`
     wrapper (fold its fields into the generic result), inline
     fallback logic.
   - Keep: the outer `GenerateOversight` scheduler, the per-task
     coalescing mutex, and all handler-facing signatures.

4. `internal/runner/commit.go` (commit-message section only):
   - `generateCommitMessage(ctx, task, data)` migrates to
     `runAgent(roleCommitMessage, ...)`.
   - Do not touch the commit pipeline proper (worktree rebase, git
     commit, push, retry) — only the commit-message-agent subroutine.

5. Normalize container names to the new `wallfacer-title-{uuid8}` /
   `wallfacer-oversight-{uuid8}` / `wallfacer-commit-msg-{uuid8}`
   pattern via the descriptor's `Name` field.

6. Verify existing unit tests continue to pass unchanged — behavioral
   equivalence is the acceptance criterion. Any test that reaches
   into the removed helpers directly should migrate to asserting the
   observable behavior (usage accumulated, event emitted, parsed
   result correct) rather than the internal plumbing.

## Tests

- Existing tests in `internal/runner/title_test.go`,
  `oversight_test.go`, `commit_test.go` must all pass.
- Add one regression test per role:
  - `TestRunAgent_TitleUsesDescriptor` — mock the runAgent path, call
    `GenerateTitle`, assert the descriptor's template and container
    name prefix were used.
  - `TestRunAgent_OversightUsesDescriptor` — same shape.
  - `TestRunAgent_CommitMessageUsesDescriptor` — same shape.
- These prevent a future regression where someone bypasses the
  descriptor and reintroduces bespoke launch logic.

## Boundaries

- Do not add `MountReadOnly` or `MountReadWrite` support to
  `runAgent` here — inspector and heavyweight migrations are
  separate tasks.
- Do not change the prompt templates themselves.
- Do not change the outer `GenerateOversight` scheduling/coalescing
  loop.
- Leave the commit pipeline's worktree / git plumbing alone.
- Do not touch `ideate.go`, `refine.go`, or `execute.go` turn loops.
