---
name: implement-spec
description: Implement a design spec from specs/ — reads the spec, plans the work, implements each item with tests and docs, then commits. Use when the user says "implement spec", "build spec", or references a spec file to implement.
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
   `title`, `status`, `track`, `depends_on`, `affects`, `effort`, `created`,
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
   - `complete` → already done. Confirm with the user before re-implementing.
   - `stale` → warn the user the spec may not match reality. Suggest `/refine`
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
- Frontend: add tests in `ui/js/__tests__/` using vitest.
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

## Step 5: Update specs and epic index

After all tasks are implemented (or partially implemented if focus instructions
were given), update the spec file, related specs, and the epic index:

### 5a. Update the implemented spec frontmatter and body

1. **Update frontmatter `status`** — set to `complete` (or leave at `validated`
   and add a note if only focused items were implemented).
2. **Update `updated` date** — set to today's date.
3. **Update `dispatched_task_id`** — if this is a leaf spec dispatched to the
   kanban board, ensure the task ID is recorded.
4. **Document deviations** — if the implementation differs from what the spec
   prescribed (different signatures, renamed fields, extra/fewer methods,
   reordered steps, skipped items, etc.), add an `## Implementation notes`
   section at the end of the spec documenting each deviation:
   - What the spec said vs. what was actually done
   - Why the deviation was necessary (codebase constraint, user decision,
     discovered during implementation, etc.)
   If the implementation matched the spec exactly, omit this section.

### 5b. Update `specs/README.md`

1. Read `specs/README.md`.
2. Update the **Status** column for this spec's row (e.g., `Not started` →
   `**Complete**`, or `**In progress** (N/M tasks done)`).
3. If the implementation changes any dependency relationships, ordering
   rationale, or milestone descriptions, update those too.

### 5c. Update related specs (reverse dependency / impact analysis)

Check whether the implementation affects other specs:

1. **Reverse `depends_on` scan** — grep all spec files for `depends_on` entries
   that reference this spec's path. These are the specs that depend on this one.
   If this spec is now `complete`, those dependents may be unblocked.
2. If this spec introduces or changes interfaces that downstream specs reference
   (e.g., new types, new API routes, renamed fields), update those specs to
   reflect the actual implementation.
3. If another spec lists this one in `depends_on` and the dependency is now
   satisfied, note that the dependent is unblocked.
4. Only make factual corrections — do not redesign other specs.

### 5d. Commit

Commit all spec and index updates together as a single small commit
(e.g., `specs: mark 01-sandbox-backends as complete, update README`).

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
  the surrounding code. This project uses stdlib `net/http` (no framework),
  vanilla JS (no framework), and per-task directory storage.
- **Follow the implementation checklist** — every task must have tests, docs,
  and a quick refactoring pass (per CLAUDE.md).
