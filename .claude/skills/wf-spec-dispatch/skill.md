---
name: wf-spec-dispatch
description: Dispatch a validated spec to the kanban task board. Validates prerequisites, resolves dependency wiring, creates the task, and updates the spec's dispatched_task_id atomically. Also supports undispatching (cancel + clear link). Use when a spec is ready for execution.
argument-hint: <spec-file.md> [undispatch]
allowed-tools: Read, Grep, Glob, Edit, Agent, Bash(ls *), Bash(curl *)
---

# Dispatch Spec to Task Board

Send a validated spec to the kanban board as a task, or undispatch a previously
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
     — the kanban task will block on the dependency's task via `DependsOn`.
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

## Step 4: Execute

### Dispatch:

Call `POST /api/specs/dispatch` with the spec path(s). If the endpoint doesn't
exist yet (pre-implementation), fall back to:

1. Call `POST /api/tasks` (or `POST /api/tasks/batch` for multi-dispatch) with:
   - `prompt`: the spec body (everything below the frontmatter)
   - `goal`: the spec title
   - `depends_on`: the resolved task UUIDs from Step 3
2. On success, update the spec file's frontmatter: set `dispatched_task_id` to
   the returned task UUID and `updated` to today's date.

### Undispatch:

1. Cancel the linked task via `POST /api/tasks/{id}/cancel`.
2. Clear the spec's `dispatched_task_id` (set to `null`).
3. Set the spec's `status` back to `validated` if it was changed.
4. Update `updated` to today's date.

## Step 5: Update spec file

Use Edit to update the spec file's YAML frontmatter in place. Only modify the
fields that changed (`dispatched_task_id`, `updated`, optionally `status`). Do
not touch the markdown body.

## Step 6: Summary

Report to the user:
- What was dispatched/undispatched and the task UUID
- Dependency wiring: which task dependencies were resolved, which were skipped
- Any warnings (unresolved dependencies, non-leaf dispatch, running task cancel)
- Next steps: "Monitor on the task board" or "Dispatch dependencies first"

## Guidelines

- This skill is the bridge between the spec world and the task board. It should
  feel like a single action, not a multi-step process.
- Prefer the atomic `POST /api/specs/dispatch` endpoint when available. Fall
  back to manual task creation + spec update only if the endpoint isn't
  implemented yet.
- For batch dispatch (multiple specs), resolve all dependency wiring before
  creating any tasks, then create them atomically via `POST /api/tasks/batch`.
- Do NOT push or commit. The spec file update is a local file change. The user
  decides when to commit.
