---
title: Post-Exec Planning Commit
status: complete
depends_on: []
affects:
  - internal/handler/planning.go
  - internal/handler/planning_git.go
effort: small
created: 2026-04-04
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Post-Exec Planning Commit

## Goal

After `planner.Exec()` returns successfully in `SendPlanningMessage`, commit any
spec file changes in each workspace to git with a `plan: round N — <summary>`
message. This establishes the snapshot invariant that every writing round is a
distinct, undoable git commit.

## What to do

1. **Add a helper function** `commitPlanningRound(ctx context.Context, ws, summary string) error`
   in `internal/handler/planning.go` (or a new `internal/handler/planning_git.go`):

   ```go
   func commitPlanningRound(ctx context.Context, ws, summary string) error {
       // 1. Check if specs/ has changes
       out, err := cmdexec.Git(ws, "status", "--porcelain", "specs/").
           WithContext(ctx).Output()
       if err != nil || strings.TrimSpace(out) == "" {
           return nil // nothing to commit
       }
       // 2. Derive round number: count existing planning commits + 1
       log, _ := cmdexec.Git(ws, "log", "--format=%s",
           "--grep=^plan: round").WithContext(ctx).Output()
       n := strings.Count(strings.TrimSpace(log), "\n") + 1
       if strings.TrimSpace(log) != "" {
           n = len(strings.Split(strings.TrimSpace(log), "\n")) + 1
       }
       // 3. Truncate summary to 80 chars
       if len(summary) > 80 {
           summary = summary[:80]
       }
       msg := fmt.Sprintf("plan: round %d — %s", n, summary)
       // 4. Stage and commit
       if err := cmdexec.Git(ws, "add", "specs/").WithContext(ctx).Run(); err != nil {
           return err
       }
       return cmdexec.Git(ws, "commit", "-m", msg).WithContext(ctx).Run()
   }
   ```

2. **Inject the call** in `SendPlanningMessage`'s background goroutine, immediately
   after `planner.Exec()` returns without error and before `store.AppendMessage()`:

   ```go
   // existing: rawOutput, err = h.planner.Exec(ctx, args...)
   if err == nil {
       summary := planner.ExtractResultText(rawOutput)
       for _, ws := range h.cfg.Workspaces {
           if cerr := commitPlanningRound(ctx, ws, summary); cerr != nil {
               slog.Warn("planning commit failed", "workspace", ws, "err", cerr)
           }
       }
   }
   // existing: store.AppendMessage(...)
   ```

   Commit failures are non-fatal: log at `slog.Warn` and continue. The conversation
   log is still appended regardless.

3. **Round number edge case**: if `git log` returns an error (e.g., repo has no
   commits yet), default to round 1.

## Tests

- `TestCommitPlanningRound_DirtySpecs` — create a temp git repo with a modified
  `specs/foo.md`, call `commitPlanningRound`, verify a `plan: round 1 — ...` commit
  was created with only `specs/` in the diff
- `TestCommitPlanningRound_NoOp` — clean working tree, verify no commit created and
  no error returned
- `TestCommitPlanningRound_RoundNumbering` — seed 2 existing `plan: round N` commits
  in the repo, call helper, verify round 3 is used
- `TestCommitPlanningRound_SummaryTruncation` — summary > 80 chars, verify commit
  message is truncated

## Boundaries

- Do NOT use `git add -A` — only stage `specs/`
- Do NOT fail or return an error to the HTTP client if the commit fails
- Do NOT touch the conversation log (`store.AppendMessage`) ordering
- Do NOT commit files outside `specs/` even if other files are dirty
- Do NOT change any session ID save or extraction logic

## Implementation notes

- **Helper lives in `internal/handler/planning_git.go`** (new file) rather than
  appending to `planning.go`. Keeps `planning.go`'s HTTP handlers in one
  place; matches the repo convention of splitting git-touching helpers out
  (`git.go`, `diffcache.go`). The spec explicitly allowed either location.
- **Workspace list uses `h.currentWorkspaces()`**, not `h.cfg.Workspaces`.
  The `Handler` struct has no `cfg` field — the active workspace paths are
  exposed through the `currentWorkspaces()` accessor, which takes
  `h.snapshotMu.RLock` and returns a clone. The spec's pseudo-code was
  approximate on this point.
- **Round counter**: simplified the spec's two-step pseudo-code (a `strings.Count`
  line immediately overwritten by a `strings.Split` line) to a single form —
  `n := 1; if logOut != "" { n = len(strings.Split(logOut, "\n")) + 1 }`.
  Same semantics, no dead code.
- **`git status` errors are swallowed** (return nil) as the spec prescribed.
  This matches best-effort semantics: a missing git repo or transient git
  failure should not stop the planning pipeline.
- **`git add` / `git commit` errors are returned** wrapped with `fmt.Errorf`,
  so the caller in `SendPlanningMessage` can `slog.Warn` with context.
  Failures never bubble up to the HTTP response.
- **Summary truncation constant** extracted as `commitPlanningRoundSummaryMax`
  (`= 80`) so the test can assert on it by name.
- **Commit context**: used `context.Background()` for the git operations
  rather than the exec goroutine's context (itself already detached from the
  HTTP request). Matches what the spec's pseudo-code implied.
- **Tests**: the four table-stakes cases from the spec — dirty specs,
  clean tree no-op, round numbering across three rounds, and 80-char
  summary truncation — all live in `planning_git_test.go` with a local
  `initPlanningTestRepo` helper that disables GPG signing to keep CI
  hermetic.

## Design evolution — 2026-04-12

The original implementation produced `plan: round N — <summary>` commit
messages, where `<summary>` was the raw agent response truncated at 80 chars.
In practice this looked ugly (e.g. `plan: round 1 — ---` when the agent opened
with YAML frontmatter) and felt out of place next to kanban task commits.

Revised format now matches the kanban commit voice:

```
<primary-path>(plan): <imperative subject>

<body explaining what and why>

Plan-Round: N
```

Mechanics:

- The subject and body are generated by the **same** commit-message agent
  that kanban tasks use. `Runner.GenerateCommitMessage(ctx, data)` was added
  to `runner.Interface` as a task-free flavor of the existing private
  `generateCommitMessage(taskID, ...)`; both call a new shared
  `runCommitContainer` helper. The `commit.tmpl` prompt is augmented with
  a scope-hint instruction so the agent emits `<path>(plan): …` natively.
- `primary-path` is the longest directory prefix shared by the staged
  `specs/` files, so a planning round inside one epic naturally gets
  `specs/local/<epic>(plan):` while cross-track edits collapse to `specs(plan):`.
- `Plan-Round: N` is a git trailer rather than a subject-line token. The
  undo grep moved from `^plan: round` to `^Plan-Round: `, and the subject
  parser accepts any `<path>(plan): …` form.
- On commit-agent failure (timeout, container crash, blank result), a
  deterministic fallback derives the subject from the agent summary text —
  skipping frontmatter blocks, headings, and fenced code — so planning
  commits are never blocked on an LLM hiccup.
- If the commit agent forgot the `(plan)` scope despite the instruction,
  `ensurePlanScope` splices it into the subject as a safety net.

Cost: one extra lightweight LLM container per planning turn that writes to
`specs/`. Accepted as worth it for commit-log consistency; tracked under
`wallfacer.task.activity="commit_message_planning"` container label but
not attributed to any task's usage counters (there is no task to attribute
to — planning has its own session).
