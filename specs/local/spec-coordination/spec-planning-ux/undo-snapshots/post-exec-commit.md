---
title: Post-Exec Planning Commit
status: validated
depends_on: []
affects:
  - internal/handler/planning.go
effort: small
created: 2026-04-04
updated: 2026-04-04
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
