---
title: Sessions That Spawn and Fork
status: vague
depends_on:
  - specs/shared/platform-native.md
  - specs/shared/topos-native-harness.md
affects:
  - internal/runner/
  - internal/store/
  - frontend/src/
effort: xlarge
created: 2026-10-02
updated: 2026-10-02
author: changkun
dispatched_task_id: null
---

# Sessions That Spawn and Fork

Decision 3 of [platform-native](../platform-native.md). A direction and the
questions it raises, not a design. It is recorded so the board, the hosted
board and the harness migration are not designed against the old picture.

## The observation

The maintainer's, on 2026-10-02:

- An execution board of tasks is useful, and it has problems that grow with
  scale: tasks do not share context, cannot message each other, and are not
  aware of each other.
- In Claude Code an agent can spawn a subagent for a piece of work, and can
  fork itself so the new agent inherits the entire context thread.
- Wallfacer should become an application that spawns and creates agents on
  the fly, the way a Claude Code workflow does, instead of one where a person
  arranges agents in advance.
- Hosting the board is something to think through for scale before building
  it.

## What the board is today

A task is an isolated unit: one prompt, one worktree, one agent run that
starts from nothing but the prompt, the repository and a board manifest. The
ways tasks relate are static: a dependency edge between tasks, and sibling
worktrees a task may read. A spec breakdown produces many tasks, and each
agent rediscovers what the planner already knew. Nothing a running task
learns reaches another running task.

## What the harness side offers

The rebuilt Topos project specifies the primitives this direction needs.
Their state is what a design here has to check first; none of it is assumed
built.

| Primitive | What it is | Where it is specified |
|---|---|---|
| Threads and subagents | A session's own graph: an agent spawns a subagent, sends it a message, optionally in a worktree of its own | The project's threads and subagents spec |
| Fork | A new session that starts from another's log at a turn boundary, with that turn's files | The project's fork and handoff spec; a hosted fork route is in the published API document |
| Checkpoints | Each turn's working directory, committed, so a fork or a rewind has files to restore | The project's checkpoints spec |
| One session schema | The same event log locally and hosted | The project's architecture |

## The questions a design has to answer

1. **Task or session as the unit.** Does the board stay a board of tasks,
   each backed by one session that may spawn and fork inside it, or does it
   become a view over a graph of sessions in which "task" is one node kind?
2. **What a spawn is on the board.** When a task's agent spawns a subagent,
   is that a child card, a row inside the parent's sheet, or invisible outside
   the trace? What the user can stop, redirect or review follows from it.
3. **Fork as a user action.** "Continue this from here, another way" is a
   fork at a turn boundary. Is it offered on a task, and what happens to the
   worktree: a second worktree from the fork point's checkpoint?
4. **Shared context without a shared mess.** What is shared between sibling
   work: the planner's context by fork, a common memory, messages between
   threads? And what stays isolated so two agents do not overwrite each other:
   today the worktree boundary does this.
5. **Git.** Wallfacer's commit, rebase and merge pipeline assumes one branch
   per task. Spawned work in its own worktree needs a rule for how it lands:
   merged into the parent's branch by the parent agent, or by the pipeline.
6. **Oversight and limits.** A budget, a turn limit, test verification and
   oversight are per task today. Which of them a spawned agent inherits,
   shares, or gets its own.
7. **Scale and hosting.** Whether many concurrent sessions run as hosted
   sessions, with the local machine as one more place to run them, and what
   the board then needs from a server beyond the coordination plane.
8. **Planning.** A spec breakdown that forks the planning session per leaf
   would hand each implementer the planner's full context. Whether that
   replaces dispatching a prompt, and what it costs in tokens.

## What this is not

- Not a fleet editor in another form. Nothing is authored in advance; the
  agent decides at run time.
- Not a commitment to a hosted board. It decides what a hosted board would
  host.

## Depends on

The harness migration. Until wallfacer runs on the rebuilt harness, none of
the primitives above is reachable from it.
