---
name: wf-spec-dispatch
description: Dispatch a validated spec to the task board. Validates prerequisites, resolves dependency wiring, creates the task, and updates the spec's dispatched_task_id atomically. Also supports undispatching (cancel + clear link). Use when a spec is ready for execution.
argument-hint: <spec-file.md> [undispatch]
allowed-tools: Read, Grep, Glob, Edit, Agent, Bash(ls *), Bash(curl *)
---

# Dispatch Spec to Task Board

Send a validated spec to the board as a task, or undispatch a previously
dispatched spec.

## Step 0: Parse arguments

$ARGUMENTS has the form: `<spec-file.md> [undispatch]`

- The **first token** is the spec file path.
- If the second token is `undispatch`, perform an undispatch (cancel the linked
  task and clear `dispatched_task_id`).

## Step 1: Read the spec

1. Read the spec file in full. **Parse YAML frontmatter** — extract `title`,
   `status`, `depends_on`, `affects`, `effort`, `dispatched_task_id`.
2. Read the spec body to use as the task prompt.

## Step 2: Validate prerequisites

### For dispatch:

1. **Status check** — spec must be `validated`. If `drafted`, suggest
   `/wf-spec-validate` first. If `stale`, suggest `/wf-spec-refine` first.
2. **Already dispatched check** — if `dispatched_task_id` is non-null, warn the
   user and stop. They must undispatch first or confirm re-dispatch.
3. **Dependency check** — for each path in `depends_on`, read that spec's
   frontmatter:
   - If the dependency has `status: complete`, it's satisfied.
   - If the dependency has `dispatched_task_id` set (task in progress), note it
     — the board task will block on the dependency's task via `DependsOn`.
   - If the dependency is neither complete nor dispatched, warn: the spec has
     unresolved dependencies. The user can proceed (the task will block) or
     resolve dependencies first.
4. **Leaf check** — determine if the spec is a leaf (no child directory with
   specs). Non-leaf specs can be dispatched, but warn the user: dispatching a
   parent spec means "implement this entire design as one task." If children
   exist, suggest dispatching the children individually instead.

### For undispatch:

1. **Has dispatch link** — `dispatched_task_id` must be non-null.
2. **Task status** — check the linked task's status. If it's `in_progress` or
   `committing`, warn: undispatching will cancel a running task.

## Step 3: Resolve dependencies

For dispatch, build the task's `DependsOn` list:

1. For each spec in `depends_on` that has a non-null `dispatched_task_id`, add
   that task UUID to `DependsOn`.
2. For dependencies that are `complete` (no active task), omit them from
   `DependsOn` — the work is already done.
3. For dependencies that are neither complete nor dispatched, omit them but
   flag a warning — the task won't have a dependency edge for these.

## Step 4: Execute (via the server transition API)

The server owns dispatch atomically — it creates the board task with a
pre-assigned UUID, resolves dependency edges, sets the spec `validated`, writes
`dispatched_task_id`, and commits the frontmatter, all in one transaction (a
folder/non-leaf path expands into its subtree leaves and promotes drafted
ancestors to `validated`). Do not hand-roll task creation or edit frontmatter
yourself when the server is reachable.

### Dispatch:

`POST /api/specs/transition` with:
```json
{ "action": "dispatch", "paths": ["<workspace-relative spec path>"], "run": false }
```
- `paths` takes one or more specs (batch). `run: true` also moves the created
  task to `in_progress` immediately; default `false` leaves it queued.
- The response carries the created task UUID(s). The server has already written
  `dispatched_task_id` + `status: validated` and committed — you do not edit the
  file or commit.

### Undispatch:

`POST /api/specs/transition` with `{ "action": "undispatch", "paths": ["<path>"] }`.
The server cancels the linked task if still active, clears `dispatched_task_id`,
resets `status` to `validated`, and commits.

### Fallback (server unreachable only):

If `POST /api/specs/transition` is not reachable, fall back to `POST /api/tasks`
(or `/api/tasks/batch`) with `prompt` = the spec body, `goal` = the title,
`depends_on` = the resolved task UUIDs (Step 3); then edit the spec frontmatter
(`dispatched_task_id`, `updated`) by hand and commit. Flag clearly that this path
loses the server's atomicity (a failed task create can leave a dangling link).

## Step 5: Update spec file

Normally there is nothing to do here — the transition API already wrote and
committed the frontmatter (`dispatched_task_id`, `status`, `updated`). Only on the
unreachable-server fallback do you edit the YAML frontmatter in place (changed
fields only — `dispatched_task_id`, `updated`, optionally `status`), leaving the
markdown body untouched.

## Step 6: Summary

Report to the user:
- What was dispatched/undispatched and the task UUID
- Dependency wiring: which task dependencies were resolved, which were skipped
- Any warnings (unresolved dependencies, non-leaf dispatch, running task cancel)
- Next steps: "Monitor on the task board" or "Dispatch dependencies first"

## Guidelines

- This skill is the bridge between the spec world and the task board. It should
  feel like a single action, not a multi-step process.
- Prefer the atomic `POST /api/specs/transition` (`action: dispatch`) endpoint —
  it creates the task, sets `validated` + `dispatched_task_id`, and commits in one
  transaction. Fall back to manual task creation + spec edit only when the server
  is unreachable.
- For batch dispatch, pass multiple specs in one `paths` array so the server wires
  dependencies and creates tasks together.
- The transition API commits its own frontmatter change. Do not push. On the
  unreachable-server fallback only, the hand-edited frontmatter is a local change
  the user decides when to commit.
