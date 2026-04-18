---
title: Migrate inspector roles (refinement, ideation) to runAgent
status: validated
depends_on:
  - specs/shared/agent-abstraction/descriptor-and-runagent.md
  - specs/shared/agent-abstraction/headless-roles.md
affects:
  - internal/runner/agent.go
  - internal/runner/refine.go
  - internal/runner/ideate.go
  - internal/runner/ideate_workspace.go
effort: medium
created: 2026-04-18
updated: 2026-04-18
author: changkun
dispatched_task_id: null
---

# Migrate inspector roles to runAgent

## Goal

Add read-only workspace mount support to `runAgent()` and migrate the
refinement and ideation roles onto it. Depends on the headless
migration only because that task validates the descriptor pattern
end-to-end before we add mount complexity.

## What to do

1. In `internal/runner/agent.go`, extend `runAgent` to handle
   `MountMode == MountReadOnly`:
   - Walk `r.currentWorkspaces()` and append a read-only
     `sandbox.VolumeMount` per workspace (same layout helper used by
     `buildBaseContainerSpec`; check if there's an existing helper
     for the read-only variant).
   - Mount the workspace instructions file (CLAUDE.md / AGENTS.md)
     read-only, following the planner's
     `appendInstructionsMount` pattern.
   - No board.json, no sibling worktrees, no write access — inspector
     roles operate on a frozen snapshot of the workspace.

2. Define the two inspector descriptors in `internal/runner/agent.go`:
   ```go
   var roleRefinement = AgentRole{
       Activity:    store.SandboxActivityRefinement,
       PromptTmpl:  "refine",
       Name:        "refine",
       Timeout:     func(*store.Task) time.Duration { return constants.RefinementTimeout },
       MountMode:   MountReadOnly,
       SingleTurn:  true,
       ParseResult: parseRefinementResult,
   }
   var roleIdeaAgent = AgentRole{
       Activity:    store.SandboxActivityIdeaAgent,
       PromptTmpl:  "ideation",
       Name:        "ideate",
       Timeout:     func(t *store.Task) time.Duration {
           return time.Duration(t.Timeout) * time.Minute
       },
       MountMode:   MountReadOnly,
       SingleTurn:  true,
       ParseResult: parseIdeationResult,
   }
   ```

3. `internal/runner/refine.go`:
   - `RunRefinement(ctx, taskID, sessionID, prompt)` migrates to
     `runAgent(roleRefinement, ...)`.
   - Delete `buildRefinementContainerSpec`.
   - Keep: refinement job persistence (write result to task),
     `--resume sessionID` threading (add a `RunAgentOpts.SessionID`
     field to pass through).

4. `internal/runner/ideate.go`:
   - The planner-exec path (`runIdeationViaPlanner`) stays as-is —
     it's a different lifecycle. This task only migrates the
     ephemeral path (`runIdeationEphemeral`).
   - `runIdeationEphemeral` migrates to `runAgent(roleIdeaAgent, ...)`.
   - Delete `buildIdeationContainerSpec`.
   - Keep: the ideation history loading, prompt templating via
     `buildIdeationPrompt`, and the idea-output parser.

5. Rename ideation containers from the legacy
   `wallfacer-ideate-{timestamp_ms}` to
   `wallfacer-ideate-{uuid8}` via the descriptor's `Name`.

6. Verify existing tests continue to pass. Add two descriptor-used
   regression tests following the headless-task pattern.

## Tests

- Existing `refine_test.go` and `ideate_test.go` must stay green.
- New: `TestRunAgent_MountReadOnly_MountsWorkspaces` — asserts the
  generated spec carries read-only volume mounts for every configured
  workspace.
- New: `TestRunAgent_RefinementUsesDescriptor` and
  `TestRunAgent_IdeationEphemeralUsesDescriptor` — both assert the
  descriptor is consulted (via spy or prompt-template capture).

## Boundaries

- Planner-backed ideation (`runIdeationViaPlanner`) is out of scope —
  it uses the planner's own long-lived container, which has different
  lifecycle semantics. Leave it untouched.
- Heavyweight roles (implementation, testing) remain unmigrated.
- Do not change the ideation or refinement prompt templates.
- Do not merge the ideation output parser and the refinement output
  parser — they remain role-specific.
