---
title: Undo API Endpoint
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/undo-snapshots/post-exec-commit.md
affects:
  - internal/handler/planning.go
  - internal/apicontract/routes.go
  - server.go
effort: small
created: 2026-04-04
updated: 2026-04-04
author: changkun
dispatched_task_id: null
---

# Undo API Endpoint

## Goal

Add `POST /api/planning/undo` to `internal/handler/planning.go` that resets the
workspace to the state before the last `plan: round N` commit, preserving any
concurrent user edits via stash, and calling `UndispatchSpecs` for any task IDs
that were dispatched in the reverted commit.

## What to do

1. **Add the handler method** `UndoPlanningRound(w http.ResponseWriter, r *http.Request)`
   in `internal/handler/planning.go`:

   ```go
   type undoResult struct {
       Round        int      `json:"round"`
       Summary      string   `json:"summary"`
       FilesReverted []string `json:"files_reverted"`
   }
   ```

   Handler logic (run for the first workspace that has a planning commit; iterate
   over `h.cfg.Workspaces` to find it):

   ```go
   // a. Find last planning commit
   out, err := cmdexec.Git(ws, "log", "--format=%H %s",
       "--grep=^plan: round", "-1").Output()
   if err != nil || strings.TrimSpace(out) == "" {
       http.Error(w, `{"error":"no planning commits to undo"}`, http.StatusConflict)
       return
   }
   parts := strings.SplitN(strings.TrimSpace(out), " ", 2)
   commitHash, subject := parts[0], parts[1]
   // parse round number from "plan: round N — summary"
   // subject format: "plan: round 3 — refine auth spec"

   // b. Capture diff BEFORE reset (for dispatch detection)
   diff, _ := cmdexec.Git(ws, "diff", "HEAD~1", "HEAD", "--", "specs/").Output()

   // c. Capture files changed in the commit
   files, _ := cmdexec.Git(ws, "diff", "--name-only", "HEAD~1", "HEAD").Output()

   // d. Stash dirty working tree
   stashed := gitutil.StashIfDirty(ws)

   // e. Reset
   if err := cmdexec.Git(ws, "reset", "--hard", "HEAD~1").Run(); err != nil {
       if stashed { gitutil.StashPop(ws) }
       http.Error(w, err.Error(), http.StatusInternalServerError)
       return
   }

   // f. Pop stash
   if stashed {
       if err := gitutil.StashPop(ws); err != nil {
           // conflict: return 409, leave stash intact for manual resolution
           http.Error(w, `{"error":"stash pop conflict after undo"}`, http.StatusConflict)
           return
       }
   }

   // g. Dispatch-aware undo: find dispatched_task_id lines added in the reverted commit
   //    Pattern: lines starting with "+" (added) containing "dispatched_task_id: <uuid>"
   taskIDs := extractDispatchedTaskIDs(diff)
   for _, id := range taskIDs {
       h.undispatchByTaskID(r.Context(), ws, id) // best-effort, log on error
   }

   // h. Respond
   writeJSON(w, undoResult{
       Round:         parseRoundNumber(subject),
       Summary:       parseSummary(subject),
       FilesReverted: strings.Fields(files),
   })
   ```

2. **Add `extractDispatchedTaskIDs(diff string) []string`** — parses the diff output
   for lines matching `^\+dispatched_task_id: ([0-9a-f-]{36})$` (UUID format). Returns
   task IDs that were added in the reverted commit.

3. **Add `undispatchByTaskID(ctx, ws, taskID)`** — looks up the spec path from the task
   store (via `h.store.GetTask(ctx, taskID)`), then calls the undispatch logic from
   `internal/handler/specs_dispatch.go` (`UndispatchSpecs`). Best-effort: log errors
   but do not fail the undo.

4. **Register the route** in `internal/apicontract/routes.go`, in the Planning sandbox
   section alongside the existing planning routes:

   ```go
   {
       Method:      "POST",
       Pattern:     "/api/planning/undo",
       Name:        "UndoPlanningRound",
       JSName:      "undoPlanningRound",
       Description: "Undo the last planning round (git reset --hard HEAD~1 on last plan: round commit)",
       Tags:        []string{"planning"},
   },
   ```

5. **Register in `server.go`** — add `mux.HandleFunc("POST /api/planning/undo", h.UndoPlanningRound)`
   alongside the other planning routes.

## Tests

- `TestUndoPlanningRound_Success` — seed a temp repo with one `plan: round 1` commit,
  call `POST /api/planning/undo`, verify 200, the commit is gone, response body contains
  `{"round":1,...}`
- `TestUndoPlanningRound_NoPlanningCommits` — no planning commits in repo, expect 409
  with `{"error":"no planning commits to undo"}`
- `TestUndoPlanningRound_WithDirtyWorkingTree` — planning commit exists + uncommitted
  edit to a different spec, call undo, verify the dirty edit is preserved after stash pop
- `TestUndoPlanningRound_DispatchAware` — planning commit contains a
  `dispatched_task_id:` addition in the diff, verify `undispatchByTaskID` is called with
  the correct UUID (mock the store)
- `TestExtractDispatchedTaskIDs` — unit test for the diff parser with valid and invalid
  UUID lines

## Boundaries

- Only reset the workspace that has the latest planning commit; multi-workspace undo
  is deferred
- Do NOT touch the conversation log — undo is a git operation only, messages are NOT
  removed from the conversation store
- Do NOT implement redo (forward stack)
- Do NOT reset non-planning commits (commits without `plan: round` prefix are never
  targeted by this endpoint)
- `undispatchByTaskID` failures are non-fatal; undo still returns 200 if the git reset
  succeeds
