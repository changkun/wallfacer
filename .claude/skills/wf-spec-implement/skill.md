---
name: wf-spec-implement
description: Implement a design spec from specs/ — reads the spec, plans the work, implements each item with tests and docs, commits, then finalizes (full implementation delegates to wrap-up; partial implementation records running notes). Use when the user says "implement spec", "build spec", or references a spec file to implement.
argument-hint: <spec-file> [items to focus on...]
user-invocable: true
---

# Implement a Spec

Implement the design spec at `$ARGUMENTS`. The first token is the spec file path
(e.g., `specs/04-file-explorer.md`). Remaining tokens are optional focus
instructions — if provided, implement only the specified items/sections instead
of the full spec.

## Step 0: Parse arguments and read the spec

1. Extract the spec file path (first token of `$ARGUMENTS`).
2. Read the spec file in full. If the path doesn't exist, check `specs/` for a
   matching filename.
3. **Parse YAML frontmatter** — extract structured fields between `---` fences:
   `title`, `status`, `depends_on`, `affects`, `effort`, `created`,
   `updated`, `author`, `dispatched_task_id`. These drive readiness checks and
   completion updates below.
4. Read `specs/README.md` to understand where this spec sits in the track
   organization and dependency graph.
5. Extract any focus instructions from the remaining tokens.

## Step 1: Assess readiness

Before writing any code, verify:

1. **Spec lifecycle gate** — check the frontmatter `status` field:
   - `validated` → ready to implement. Proceed.
   - `drafted` → warn the user that the spec has not been reviewed/validated.
     Ask whether to proceed anyway.
   - `vague` → stop. The spec is not ready for implementation.
   - `testing` → the implementation already landed and the drift verdict is
     pending. Do not re-implement; run `/wf-spec-wrapup` to render the verdict.
   - `complete` → already done. Confirm with the user before re-implementing.
   - `stale` → warn the user the spec may not match reality. Suggest `/wf-spec-refine`
     first.
2. **Dependencies are met** — read the `depends_on` list from frontmatter.
   For each dependency path, read that spec's frontmatter and confirm its
   `status` is `complete`. If any dependency is not complete, report which
   ones block this spec and ask the user how to proceed.
3. **Spec is current** — use the `affects` list from frontmatter to locate the
   relevant code files. Skim the spec for file paths, function names, and API
   references. If any look stale, update them (or flag to the user) before
   proceeding.
4. **No conflicts** — run `git status` to confirm the working tree is clean.
   If dirty, ask the user how to proceed.

## Step 2: Build a plan

Break the spec into an ordered list of implementation tasks. For each task:

- State what will be built or changed
- List the files that will be created or modified
- Note any test files needed
- Note any doc files that need updating

Present this plan to the user using `EnterPlanMode`. Group tasks into logical
commits (small, focused). Order tasks so each commit leaves the project in a
working state.

Wait for user approval before proceeding. The user may adjust scope, reorder
items, or skip sections.

**Autonomous mode (goal-driven / driven by `/wf-spec-drive`):** plan-mode approval
is an interactive gate — it *hangs* an unattended `/goal` loop. So when this skill
is invoked by `/wf-spec-drive` under a goal, or with an explicit `auto` token in
the arguments, the goal itself is the standing approval: **skip `EnterPlanMode`
and the approval wait**, and go straight to Step 3. Stay conservative — keep
commits small, and if the plan turns out ambiguous, risky, or larger than a
single focused leaf, stop and report (surfacing it to the goal loop / user)
rather than guessing. Reserve autonomous mode for leaf specs you can build in one
pass; anything needing real design judgment should still pause for a human.

## Step 3: Implement

For each task in the approved plan:

### 3a. Write the code

- Read all files you plan to modify before changing them.
- Follow existing code patterns — match style, naming, error handling, and
  structure of surrounding code.
- Keep changes minimal and focused on what the spec requires.
- For new API routes: add them to `internal/apicontract/routes.go` first, then
  run `make api-contract` to regenerate the JS route file.

### 3b. Write tests

- Every new or changed behavior must have tests.
- Backend: add Go tests in the same package (`_test.go` files).
- Frontend: add vitest tests next to the source in `frontend/src/` (e.g.
  `frontend/src/lib/foo.test.ts`, `frontend/src/components/Foo.test.ts`).
- Tests must cover the happy path and at least one error/edge case.

### 3c. Verify

After implementing each task:

1. Run `make fmt` and `make lint` — fix any issues.
2. Run `go vet ./...`
3. Run `go test ./...` for backend changes.
4. Run `make test-frontend` for frontend changes.
5. Fix any failures before moving on.

### 3d. Update docs

If the task adds, removes, or modifies any API route, CLI flag, env variable,
data model field, or user-visible behavior:

- Update the relevant guide in `docs/guide/`.
- Update `docs/internals/` if internal architecture changed.
- Update `CLAUDE.md` if new routes, env vars, or conventions were added.

### 3e. Commit

- Stage only the files for this task.
- Write a scoped, imperative commit message matching the repo style
  (e.g., `internal/handler: add file content endpoint`).
- Do NOT push unless the user explicitly asks.

### 3f. Update progress

After each commit, mark the completed task done and show the user a brief
status update: what was done, what's next.

## Step 4: Final verification

After all tasks are implemented:

1. Run the full test suite: `make test`
2. Run `make build` to confirm the binary builds cleanly.
3. If any tests fail, diagnose and fix before finishing.

## Step 5: Finalize the spec

How you finalize depends on whether the **whole** spec shipped or only a subset
(focus instructions, deferred/blocked items).

### 5a. Full implementation → delegate to wrap-up

If every item in the spec was implemented, do not hand-roll the completion
write-up here — invoke **`/wf-spec-wrapup <spec-file>`**. It owns the canonical
finalization: driving the spec through the `testing` gate to `complete` (or
`stale` on significant drift) — never a raw `validated → complete` jump — writing
the `## Outcome` section (What Shipped + Design Evolution + the
decisions/surprises/follow-ups detail), updating `specs/README.md`, the
reverse-dependency scan, and the single spec commit. This keeps direct-implement and dispatch converging on one finalizer and
one section convention, instead of two skills writing divergent sections.

Hand wrap-up the knowledge you accumulated this session as the Outcome source —
the commit SHAs, the judgment calls, the deviations from the spec, the gotchas —
so it documents what actually happened rather than reconstructing it from git.
If a deviation made the spec body itself wrong, fix the body inline (wrap-up's
Outcome explains the change; the body must read as current reality).

### 5b. Partial implementation → lightweight in-place notes

If only a subset shipped, do NOT mark the spec complete (that would let wrap-up
close it). Instead:

1. Leave `status` at `validated` (the lifecycle has no `in_progress` /
   `implemented` state — a spec stays `validated` until it goes through `testing`
   to `complete`, which only the full-completion wrap-up does); set `updated` to
   today; record `dispatched_task_id` if this is a dispatched leaf.
2. Append an `## Implementation notes` section capturing the running state, with
   tight bullets (omit a subsection only if genuinely empty):
   - **Status** — commit SHAs/PR, date, and that the spec is *partially* done.
   - **What was done** — concrete changes that shipped, grouped by area
     (backend / frontend / docs / tests), linking primary files or commits.
   - **What was not done** — spec items skipped/deferred/descoped, each with
     *why* (out of scope this pass, blocked by X, user deferred, found
     unnecessary) and whether a follow-up is expected.
   - **Decisions made during implementation** — choices not spelled out in the
     spec (naming, error semantics, defaults, schema shapes, ordering, UX
     micro-details, test strategy), each with its reasoning. Most valuable
     subsection — captures judgment that would otherwise be lost.
   - **Deviations from the spec** — where the implementation intentionally
     differs (signatures, renamed fields, reordered/removed/added items); what
     the spec said vs. what was done, and why. Fix a now-wrong spec body inline.
   - **Surprises / gotchas** — hidden coupling, fragile assumptions, perf
     cliffs, test-infra quirks, undocumented external behavior.
   - **Follow-ups** — concrete next steps not done here; link new specs/issues,
     or write "None."
3. Update the `specs/README.md` status column to the in-progress state and run
   the reverse-`depends_on` scan for factual corrections to dependents only.
4. Commit the spec + README updates as one small commit
   (e.g., `specs: record partial progress on <spec-name>`).

Run `/wf-spec-wrapup` later, once the remaining items land, to do the full
completion write-up.

## Step 6: Summary

Report to the user:
- What was implemented (list of commits with one-line descriptions)
- What was deferred or skipped (if any), and why
- Any follow-up work or known limitations
- Whether the spec is now fully done or if items remain

## Guidelines

- **Read before writing** — never modify a file you haven't read in this session.
- **One logical change per commit** — don't bundle unrelated changes.
- **No over-engineering** — implement exactly what the spec says. Don't add
  features, abstractions, or configurability beyond what's specified.
- **Ask when ambiguous** — if the spec is unclear or contradicts the codebase,
  ask the user rather than guessing.
- **Preserve existing patterns** — match the conventions in `CLAUDE.md` and
  the surrounding code. This project uses stdlib `net/http` (no framework) on
  the backend, a Vue 3 + TypeScript SPA in `frontend/` (Pinia stores, vitest),
  and per-task directory storage.
- **Follow the implementation checklist** — every task must have tests, docs,
  and a quick refactoring pass (per CLAUDE.md).
