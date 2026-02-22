# Error Handling Improvements Plan

## Overview

This plan documents every missed or inadequate error-handling branch found across the
wallfacer codebase, rated by severity, and prescribes the minimal targeted fix for each.
The goal is to make failures visible, prevent silent state corruption, and avoid resource
leaks — without over-engineering or changing behavior for happy paths.

Issues are grouped into eight categories and sorted by severity within each group.

---

## Severity Legend

| Severity | Meaning |
|----------|---------|
| **Critical** | Can cause data loss, destroy live work, or crash the server |
| **High** | Causes silent state corruption or wrong pipeline behaviour |
| **Medium** | Reduces observability or leaves resources in a bad state |
| **Low** | Minor noise reduction / best-practice cleanup |

---

## Category 1 — Unchecked Error Returns

### 1.1 `runner/worktree.go:94` — `ListTasks` error in `PruneOrphanedWorktrees` [CRITICAL]

**Problem:** `tasks, _ := s.ListTasks(ctx, true)` discards the error. If the call
fails, `tasks` is nil, `knownIDs` is an empty map, and every directory under
`worktreesDir` is treated as orphaned — destroying all worktrees for live tasks.

**Fix:**
```go
tasks, err := s.ListTasks(ctx, true)
if err != nil {
    logger.Runner.Error("prune: list tasks failed, skipping to avoid data loss", "error", err)
    return
}
```

---

### 1.2 `server.go:155` — `fs.Sub` error discarded [CRITICAL]

**Problem:** `uiFS, _ := fsLib.Sub(uiFiles, "ui")` discards the error. A nil `uiFS`
causes an immediate nil-pointer panic on any request to the UI.

**Fix:**
```go
uiFS, err := fsLib.Sub(uiFiles, "ui")
if err != nil {
    logger.Fatal(logger.Main, "create ui sub-FS", "error", err)
}
```

---

### 1.3 `runner/snapshot.go:31–35` — All git init commands in `setupNonGitSnapshot` [High]

**Problem:** Four consecutive `exec.Command(...).Run()` calls for `git config`,
`git config`, `git add -A`, and `git commit` all discard errors. If any of them fail
(e.g., git identity not set, disk full), the snapshot has no initial commit and the
entire commit pipeline produces no output, silently.

**Fix:** Return an error if the final `git commit` step fails (it is the one everything
else depends on). Check and return errors for all four steps in sequence:
```go
cmds := [][]string{
    {"git", "-C", snapshotPath, "config", "user.email", "wallfacer@local"},
    {"git", "-C", snapshotPath, "config", "user.name", "Wallfacer"},
    {"git", "-C", snapshotPath, "add", "-A"},
    {"git", "-C", snapshotPath, "commit", "--allow-empty", "-m", "wallfacer: initial snapshot"},
}
for _, args := range cmds {
    if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
        os.RemoveAll(snapshotPath)
        return fmt.Errorf("snapshot init %v: %w\n%s", args[2], err, out)
    }
}
```

---

### 1.4 `gitutil/ops.go:21` — Rebase abort error discarded in `RebaseOntoDefault` [High]

**Problem:** `exec.Command("git", "-C", worktreePath, "rebase", "--abort").Run()` result
is silently dropped. If the abort itself fails, the worktree is permanently stuck
mid-rebase, making all subsequent git operations in that worktree fail.

**Fix:**
```go
if abortErr := exec.Command("git", "-C", worktreePath, "rebase", "--abort").Run(); abortErr != nil {
    logger.GitUtil.Error("rebase abort failed — worktree may be stuck mid-rebase",
        "worktree", worktreePath, "error", abortErr)
}
```

---

### 1.5 `runner/execute.go:120` and `runner/execute.go:187` — `GetTask` error discarded in cancelled-status check [High]

**Problem:** Both cancelled-status guard clauses use `cur, _ := r.store.GetTask(...)`.
If the store read fails, `cur` is nil and the guard evaluates to false, causing a
cancelled task to be incorrectly transitioned to `failed`.

**Fix (both locations):**
```go
cur, err := r.store.GetTask(bgCtx, taskID)
if err != nil {
    logger.Runner.Error("get task for cancel check", "task", taskID, "error", err)
    // Do not transition; leave status as-is rather than clobbering it.
    return
}
if cur.Status == "cancelled" {
    return
}
```

---

### 1.6 `runner/commit.go:125,131–132` — `git status --porcelain` and diff/log errors discarded [High]

**Problem:** In `hostStageAndCommit`:
- `out, _ := exec.Command("git", "-C", worktreePath, "status", "--porcelain").Output()`
  — if this fails (e.g., not a git repo), `out` is empty and the worktree is silently
  skipped as "nothing to commit", losing work.
- `statOut, _ := ...` and `logOut, _ := ...` for `diff --cached --stat` and
  `log --format=%s -5` — errors are silently dropped, generating a poor commit message
  with no indication the commands failed.

**Fix:** Check and return the `git status` error; log (don't return) the diff/log errors:
```go
out, err := exec.Command("git", "-C", worktreePath, "status", "--porcelain").Output()
if err != nil {
    return fmt.Errorf("git status in %s: %w", worktreePath, err)
}
// For diff/log used only to build the commit message:
statOut, err := exec.Command("git", "-C", worktreePath, "diff", "--cached", "--stat").Output()
if err != nil {
    logger.Runner.Warn("git diff --cached --stat failed", "worktree", worktreePath, "error", err)
}
logOut, err := exec.Command("git", "-C", worktreePath, "log", "--format=%s", "-5").Output()
if err != nil {
    logger.Runner.Warn("git log failed", "worktree", worktreePath, "error", err)
}
```

---

### 1.7 `gitutil/ops.go:60` and `gitutil/ops.go:73` — `strconv.Atoi` errors discarded [High]

**Problem:**
- In `CommitsBehind`: `n, _ := strconv.Atoi(...)` — parse failure silently returns
  `(0, nil)`, indicating the branch is up-to-date when it is not.
- In `HasCommitsAheadOf`: same pattern — returns `false` (no commits ahead), causing the
  commit pipeline to silently skip the FF-merge.

**Fix:**
```go
// CommitsBehind
n, err := strconv.Atoi(strings.TrimSpace(string(out)))
if err != nil {
    return 0, fmt.Errorf("parse rev-list count in %s: %w (output: %q)", worktreePath, err, out)
}

// HasCommitsAheadOf
n, err := strconv.Atoi(strings.TrimSpace(string(out)))
if err != nil {
    return false, fmt.Errorf("parse rev-list count in %s: %w (output: %q)", worktreePath, err, out)
}
```

---

### 1.8 `server.go:314–390` — Store errors discarded in `recoverOrphanedTasks` and `monitorContainerUntilStopped` [Medium]

**Problem:** All calls to `s.UpdateTaskStatus`, `s.InsertEvent` in the recovery path and
the container monitor loop silently drop their return values.

**Fix:** Wrap each call with a log-on-error helper or inline checks:
```go
if err := s.UpdateTaskStatus(ctx, t.ID, "failed"); err != nil {
    logger.Runner.Error("recover: update task status", "task", t.ID, "error", err)
}
if err := s.InsertEvent(ctx, t.ID, store.EventTypeError, ...); err != nil {
    logger.Runner.Error("recover: insert error event", "task", t.ID, "error", err)
}
```
Apply the same pattern to all discarded store calls in both `recoverOrphanedTasks` and
`monitorContainerUntilStopped`.

---

### 1.9 `runner/execute.go` (throughout main loop) — Pervasive store call errors dropped [Medium]

**Problem:** In `Run`'s main execution loop (lines ~74–195), all calls to
`r.store.UpdateTaskStatus`, `r.store.UpdateTaskResult`, `r.store.InsertEvent`,
`r.store.AccumulateTaskUsage` drop return values. Including inside the safety `defer`
and `SyncWorktrees`.

**Fix:** Introduce a small helper to log-and-ignore store errors in fire-and-forget
update calls (since the task must continue even if a status update fails):
```go
func logStoreErr(op string, taskID uuid.UUID, err error) {
    if err != nil {
        logger.Runner.Error("store op failed", "op", op, "task", taskID, "error", err)
    }
}
// Usage:
logStoreErr("update status", taskID, r.store.UpdateTaskStatus(bgCtx, taskID, "failed"))
```
Apply to every discarded store call in `Run`, `SyncWorktrees`, and the safety `defer`.

---

### 1.10 `handler/execute.go` — `InsertEvent` / `UpdateTaskStatus` errors dropped [Medium]

**Problem:** In `SubmitFeedback`, `CompleteTask`, `CancelTask`, `ArchiveTask`,
`UnarchiveTask`, `SyncTask`, and `UpdateTask` all `InsertEvent` / `UpdateTaskStatus`
calls drop errors, meaning audit events are silently lost on store failure.

**Fix:** Log errors from all `InsertEvent` and `UpdateTaskStatus` calls in handler layer
(same pattern as above — log but do not fail the HTTP response since status was already
written):
```go
if err := h.store.InsertEvent(r.Context(), id, store.EventTypeStateChange, data); err != nil {
    logger.Handler.Error("insert event", "task", id, "error", err)
}
```

---

### 1.11 `runner/commit.go:442` — `SaveTurnOutput` in `resolveConflicts` dropped [Medium]

**Problem:** `r.store.SaveTurnOutput(taskID, turns, rawStdout, rawStderr)` — return value
discarded. Unlike the main loop where this is at least logged, here it is completely
dropped.

**Fix:**
```go
if err := r.store.SaveTurnOutput(taskID, turns, rawStdout, rawStderr); err != nil {
    logger.Runner.Error("save conflict resolver output", "task", taskID, "error", err)
}
```

---

### 1.12 `runner/commit.go:54` — `GetTask` error discarded in `commit` [Medium]

**Problem:** `task, _ := r.store.GetTask(bgCtx, taskID)` — error silently dropped. If
the task is not found, a nil-guarded fallback of empty `taskPrompt` is used, generating
a commit message with no context and no log entry indicating why.

**Fix:**
```go
task, err := r.store.GetTask(bgCtx, taskID)
if err != nil {
    logger.Runner.Warn("commit: task not found, prompt will be empty", "task", taskID, "error", err)
}
taskPrompt := ""
if task != nil {
    taskPrompt = task.Prompt
}
```

---

### 1.13 `runner/commit.go:437` and `runner/execute.go:437` — `GetTask` error discarded in `resolveConflicts` [Medium]

**Problem:** `task, _ := r.store.GetTask(context.Background(), taskID)` in
`resolveConflicts` — if `GetTask` fails, `turns` defaults to 0, potentially overwriting
turn-1 output from the original run.

**Fix:**
```go
task, err := r.store.GetTask(ctx, taskID)
if err != nil {
    logger.Runner.Warn("resolveConflicts: task not found", "task", taskID, "error", err)
}
turns := 0
if task != nil {
    turns = task.Turns + 1
}
```
Also pass `ctx` (the already-existing timeout context) instead of creating a new
`context.Background()`.

---

### 1.14 `main.go:191` — Browser `exec.Start()` error discarded [Low]

**Problem:** `exec.Command(cmd, url).Start()` error is completely discarded. Silent
failure on systems where the browser binary is missing.

**Fix:**
```go
if err := exec.Command(cmd, url).Start(); err != nil {
    logger.Main.Warn("open browser", "url", url, "error", err)
}
```

---

### 1.15 `gitutil/worktree.go:59,66` — `git worktree prune` and `branch -D` errors discarded [Low]

**Problem:** Best-effort cleanup calls in `RemoveWorktree` silently swallow errors,
leaving no visibility into stale branch accumulation.

**Fix:** Log errors at `Warn` level:
```go
if err := exec.Command("git", "-C", repoPath, "worktree", "prune").Run(); err != nil {
    logger.GitUtil.Warn("git worktree prune", "repo", repoPath, "error", err)
}
if err := exec.Command("git", "-C", repoPath, "branch", "-D", branchName).Run(); err != nil {
    logger.GitUtil.Warn("git branch -D", "repo", repoPath, "branch", branchName, "error", err)
}
```

---

### 1.16 `runner/container.go:143,158–159` and `runner/commit.go:208` and `runner/title.go:27` — Container `rm -f` / `kill` cleanup errors [Low]

**Problem:** Pre-cleanup `exec.Command(r.command, "rm", "-f", containerName).Run()` and
kill calls in `RunContainer`, `KillContainer`, `generateCommitMessage`, and
`GenerateTitle` all discard errors. Debugging container lifecycle issues is needlessly
opaque.

**Fix:** Log at `Debug`/`Warn` level (not `Error` since cleanup is best-effort):
```go
if err := exec.Command(r.command, "rm", "-f", containerName).Run(); err != nil {
    logger.Runner.Debug("pre-cleanup container rm", "container", containerName, "error", err)
}
```

---

### 1.17 `runner/worktree.go:109` — `os.RemoveAll` error discarded in `PruneOrphanedWorktrees` [Low]

**Problem:** Orphaned worktree directory removal errors are silently swallowed.

**Fix:**
```go
if err := os.RemoveAll(orphanDir); err != nil {
    logger.Runner.Warn("remove orphaned worktree", "dir", orphanDir, "error", err)
}
```

---

## Category 2 — Resource Leaks

### 2.1 `handler/stream.go:95–127` — Goroutine and pipe leak in `StreamLogs` [Medium]

**Problem:** When the HTTP client disconnects (`r.Context().Done()`), `pr.Close()` is
called but the scanner goroutine (`go func() { scanner := bufio.NewScanner(pr) ... }`)
may be blocked sending to the `lines` channel. It leaks until the subprocess exits.
For long-lived containers this is a goroutine leak.

**Fix:** Signal the scanner goroutine via context-driven `pr.Close`:
```go
ctx := r.Context()
go func() {
    <-ctx.Done()
    pr.Close() // unblocks scanner
    pw.Close() // unblocks cmd.Wait goroutine
}()
```
Remove the manual `pr.Close()` from the `select` arm so the single goroutine above
handles cleanup on both disconnect and normal exit.

---

### 2.2 `store/io.go:42–51` — Temp file not cleaned up on partial write [Medium]

**Problem:** In `atomicWriteJSON`, if `os.WriteFile` partially fails (disk full mid-write),
the `.tmp` file is left on disk indefinitely.

**Fix:**
```go
if err := os.WriteFile(tmp, raw, 0644); err != nil {
    os.Remove(tmp) // best-effort; ignore secondary error
    return err
}
```

---

### 2.3 `server.go:331` — `monitorContainerUntilStopped` goroutine with no lifetime bound [Medium]

**Problem:** The polling goroutine has no timeout or cancellation signal. If the container
runtime hangs or the polling logic hits a permanent error path that still returns, the
goroutine leaks indefinitely.

**Fix:** Pass a context derived from a server-level `Done` channel (or from task
cancellation) so the polling loop can exit:
```go
go monitorContainerUntilStopped(ctx, s, r, t.ID) // ctx tied to server shutdown
```
Inside `monitorContainerUntilStopped`, select on `ctx.Done()` alongside the tick:
```go
select {
case <-ctx.Done():
    return
case <-ticker.C:
    // existing poll logic
}
```

---

## Category 3 — HTTP Handler Gaps

### 3.1 `handler/git.go:173–221` — Diff errors return HTTP 200 with empty body [Medium]

**Problem:** In `TaskDiff`, all `exec.CommandContext(...).Output()` error returns use `_`.
Git diff failures (missing ref, deleted repo, etc.) produce empty output and HTTP 200,
giving the client no way to distinguish "no changes" from "diff computation failed".

**Fix:** Collect errors and return HTTP 500 if all diff attempts fail:
```go
out, err := exec.CommandContext(r.Context(), "git", "-C", repoPath,
    "diff", baseHash, commitHash).Output()
if err != nil {
    logger.Handler.Warn("git diff by hash", "repo", repoPath, "error", err)
    // fall through to branch-based diff
}
```
If the branch-based fallback also fails, return a 500:
```go
out, err = exec.CommandContext(r.Context(), "git", "-C", repoPath,
    "diff", base, task.BranchName).Output()
if err != nil {
    http.Error(w, "git diff failed", http.StatusInternalServerError)
    return
}
```

---

### 3.2 `handler/tasks.go:75` — All `GetTask` errors mapped to 404 [Medium]

**Problem:** In `UpdateTask`, any store error (including disk I/O failure) is returned as
`404 Not Found` instead of `500 Internal Server Error`.

**Fix:** Inspect the error message (or add a typed sentinel error `ErrNotFound` to the
store) and return 404 only for not-found, 500 otherwise:
```go
task, err := h.store.GetTask(r.Context(), id)
if err != nil {
    if strings.Contains(err.Error(), "not found") {
        http.Error(w, "task not found", http.StatusNotFound)
    } else {
        http.Error(w, "internal error", http.StatusInternalServerError)
    }
    return
}
```
The cleaner long-term approach is to introduce `store.ErrNotFound` sentinel and use
`errors.Is`.

---

### 3.3 `handler/stream.go:185–186` — Write errors not checked in `serveStoredLogs` [Medium]

**Problem:** `w.Write(content)` and `fmt.Fprintln(w)` errors are dropped. If the client
disconnects, the server continues iterating all output files with no early exit.

**Fix:**
```go
if _, err := w.Write(content); err != nil {
    return // client disconnected
}
if _, err := fmt.Fprintln(w); err != nil {
    return
}
```

---

### 3.4 `handler/execute.go:82–100` — Goroutine in `CompleteTask` has no `recover()` [Medium]

**Problem:** The goroutine that calls `h.runner.Commit(...)` and then updates task status
has no `recover()`. A panic inside `Commit` (e.g., unexpected nil pointer) would crash
the entire server.

**Fix:**
```go
go func() {
    defer func() {
        if p := recover(); p != nil {
            logger.Handler.Error("CompleteTask goroutine panic", "task", id, "panic", p)
            if err := h.store.UpdateTaskStatus(bgCtx, id, "failed"); err != nil {
                logger.Handler.Error("update task status after panic", "task", id, "error", err)
            }
        }
    }()
    // existing commit + status logic
}()
```

---

## Category 4 — Nil Pointer / Type Assertion Risks

### 4.1 `server.go:135` — Unchecked type assertion `(*net.TCPAddr)` [Medium]

**Problem:** `ln.Addr().(*net.TCPAddr)` panics if `ln.Addr()` does not return a
`*net.TCPAddr` (e.g., a Unix socket listener). While unlikely for `net.Listen("tcp", ...)`,
it is not guaranteed.

**Fix:**
```go
tcpAddr, ok := ln.Addr().(*net.TCPAddr)
if !ok {
    logger.Fatal(logger.Main, "unexpected listener address type", "addr", ln.Addr())
}
actualPort := tcpAddr.Port
```

---

### 4.2 `server.go:125` — `net.SplitHostPort` error discarded [Low]

**Problem:** `host, _, _ := net.SplitHostPort(*addr)` — if `*addr` is not a valid
`host:port` string, `host` is empty with no warning, producing an unexpected fallback
behaviour for browser launch URL construction.

**Fix:**
```go
host, _, err := net.SplitHostPort(*addr)
if err != nil {
    logger.Main.Warn("parse listen address", "addr", *addr, "error", err)
    host = "localhost"
}
```

---

## Category 5 — Context Propagation

### 5.1 `runner/commit.go:437` — `resolveConflicts` creates `context.Background()` instead of using `ctx` param [Medium]

**Problem:** The `ctx` timeout context is already passed into `resolveConflicts` but the
function creates a fresh `context.Background()` for the `GetTask` and `InsertEvent`
store calls, bypassing the task-level timeout.

**Fix:** Replace all inline `context.Background()` in `resolveConflicts` with the `ctx`
parameter that is already in scope.

---

### 5.2 `runner/execute.go` — All store calls use `context.Background()` [Low]

**Problem:** The `Run` goroutine uses `bgCtx := context.Background()` for the entire
task lifecycle. This is acceptable for now (long-running background work), but blocks
future graceful shutdown support.

**Fix (deferred):** Pass a server-level shutdown context into `Run` and use it for store
operations. Mark as a TODO comment for now:
```go
// TODO: replace bgCtx with a server-level context for graceful shutdown.
bgCtx := context.Background()
```

---

## Category 6 — Silent State Corruption

### 6.1 `runner/execute.go:82–84` — Worktree path persistence failure [High]

**Problem:** `UpdateTaskWorktrees` error is logged but execution continues. If this fails,
the task runs in a container with worktrees that the store has no record of. After a
server restart, the task cannot be resumed and worktrees cannot be cleaned up.

**Fix:** Treat this as a fatal worktree setup error (like failing to create the worktrees
in the first place):
```go
if err := r.store.UpdateTaskWorktrees(bgCtx, taskID, worktreePaths, branchName); err != nil {
    logger.Runner.Error("save worktree paths", "task", taskID, "error", err)
    r.cleanupWorktrees(taskID, worktreePaths, branchName)
    r.store.UpdateTaskStatus(bgCtx, taskID, "failed")
    r.store.InsertEvent(bgCtx, taskID, store.EventTypeError,
        map[string]string{"error": "failed to persist worktree paths: " + err.Error()})
    r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange,
        map[string]string{"from": "in_progress", "to": "failed"})
    return
}
```

---

### 6.2 `gitutil/ops.go` — `handler/git.go:138` — Rebase abort errors in sync handler [Medium]

**Problem:** In the `GitSyncWorkspace` HTTP handler, the rebase abort on conflict is
silently discarded. The worktree is left mid-rebase on the server if the abort fails.

**Fix:** Same pattern as 1.4 — log abort failure at `Error` level and surface it in the
HTTP response:
```go
if abortErr := exec.Command("git", "-C", req.Workspace, "rebase", "--abort").Run(); abortErr != nil {
    logger.Handler.Error("rebase abort failed in sync", "workspace", req.Workspace, "error", abortErr)
    http.Error(w, "rebase abort failed — workspace may be stuck mid-rebase", http.StatusInternalServerError)
    return
}
```

---

## Category 7 — Missing Context in Error Messages

### 7.1 `store/store.go` — No typed `ErrNotFound` sentinel [Medium]

**Problem:** All "task not found" conditions are returned as plain `fmt.Errorf("task not
found: %s", id)` strings. Callers must do fragile `strings.Contains` checks (as noted in
3.2 above).

**Fix:** Define and use a typed sentinel error:
```go
// In store/errors.go (new file, ~5 lines)
var ErrNotFound = errors.New("not found")

// In GetTask, GetTaskEvents, etc.:
return nil, fmt.Errorf("task %s: %w", id, ErrNotFound)

// Callers:
if errors.Is(err, store.ErrNotFound) {
    http.Error(w, "task not found", http.StatusNotFound)
    return
}
http.Error(w, "internal error", http.StatusInternalServerError)
```

---

### 7.2 `runner/commit.go:394` — `GetCommitHash` failure only warns, not errors [Medium]

**Problem:** After a successful FF-merge, `GetCommitHash` failure is only warned about.
The task's `CommitHashes` map entry is missing for that repo, and the `TaskDiff`
endpoint falls back to a worse branch-based diff with no user-visible indication.

**Fix:** Upgrade to `Error` log level and add a note in the event log:
```go
if err != nil {
    logger.Runner.Error("get commit hash after merge", "task", taskID, "repo", repoPath, "error", err)
    r.store.InsertEvent(bgCtx, taskID, store.EventTypeError,
        map[string]string{"error": fmt.Sprintf("commit hash unavailable for %s: %s", repoPath, err)})
}
```

---

## Implementation Order

Implement fixes in the following order to handle the most critical risks first:

### Phase 1 — Critical (must fix first)

1. **1.1** `runner/worktree.go:94` — Guard `ListTasks` error in `PruneOrphanedWorktrees`
2. **1.2** `server.go:155` — Fatal on `fs.Sub` error
3. **6.1** `runner/execute.go:82–84` — Treat `UpdateTaskWorktrees` failure as fatal setup error
4. **1.5** `runner/execute.go:120,187` — Guard `GetTask` error in cancelled-status checks

### Phase 2 — High (fix before next release)

5. **1.3** `runner/snapshot.go:31–35` — Check all git init commands in `setupNonGitSnapshot`
6. **1.4** `gitutil/ops.go:21` — Log rebase abort error in `RebaseOntoDefault`
7. **1.6** `runner/commit.go:125,131–132` — Check `git status --porcelain` error
8. **1.7** `gitutil/ops.go:60,73` — Propagate `strconv.Atoi` errors in `CommitsBehind` / `HasCommitsAheadOf`

### Phase 3 — Medium (address in follow-up)

9.  **2.1** `handler/stream.go` — Fix goroutine/pipe leak in `StreamLogs`
10. **2.2** `store/io.go:42–51` — Clean up temp file on partial write error
11. **3.1** `handler/git.go` — Return HTTP 500 on diff command failure
12. **3.2** `handler/tasks.go:75` — Introduce `store.ErrNotFound` sentinel
13. **3.3** `handler/stream.go:185–186` — Check write errors in `serveStoredLogs`
14. **3.4** `handler/execute.go:82–100` — Add `recover()` to `CompleteTask` goroutine
15. **4.1** `server.go:135` — Check `(*net.TCPAddr)` type assertion
16. **5.1** `runner/commit.go:437` — Use `ctx` param instead of `context.Background()` in `resolveConflicts`
17. **1.8** `server.go:314–390` — Log store errors in recovery / monitor paths
18. **1.9** `runner/execute.go` — Log all store call errors in main execution loop
19. **1.10** `handler/execute.go` — Log `InsertEvent`/`UpdateTaskStatus` errors in handlers
20. **1.11** `runner/commit.go:442` — Check `SaveTurnOutput` in `resolveConflicts`
21. **1.12** `runner/commit.go:54` — Log `GetTask` error in `commit`
22. **1.13** `runner/commit.go:437` — Log `GetTask` error in `resolveConflicts`
23. **6.2** `handler/git.go:138` — Surface rebase abort failure in sync handler
24. **7.1** `store/store.go` — Add `store.ErrNotFound` sentinel
25. **7.2** `runner/commit.go:394` — Upgrade commit hash warning to error event

### Phase 4 — Low (cleanup pass)

26. **1.14** `main.go:191` — Log browser launch error
27. **1.15** `gitutil/worktree.go:59,66` — Log cleanup errors in `RemoveWorktree`
28. **1.16** Container `rm -f` / `kill` cleanup — Debug-log in `RunContainer`, `KillContainer`, etc.
29. **1.17** `runner/worktree.go:109` — Log `os.RemoveAll` error
30. **4.2** `server.go:125` — Log `SplitHostPort` error and default to `localhost`
31. **2.3** `server.go:331` — Bound `monitorContainerUntilStopped` goroutine lifetime with context
32. **5.2** `runner/execute.go` — Add TODO comment about graceful shutdown context

---

## Testing Strategy

For each fix, add or extend tests to verify the corrected behaviour:

- **1.1**: Mock `ListTasks` to return an error; assert that the worktrees directory is NOT
  modified (no `os.RemoveAll` called).
- **1.7**: Unit-test `CommitsBehind` and `HasCommitsAheadOf` with non-numeric git output;
  assert an error is returned.
- **3.2**: Add `store.ErrNotFound` tests and handler tests asserting 404 vs 500 for
  not-found vs store error.
- **2.2**: Mock `os.WriteFile` to return `syscall.ENOSPC`; assert the `.tmp` file is
  removed.
- **2.1**: Cancel the request context mid-stream; assert no goroutine leak (via
  `goleak` or a counter).
- **General**: Existing `go test ./...` suite must remain green after every change.

---

## Files to Modify

| File | Phases |
|------|--------|
| `internal/runner/worktree.go` | 1, 4 |
| `server.go` | 1, 3, 4 |
| `internal/runner/execute.go` | 1, 2, 3 |
| `internal/gitutil/ops.go` | 2 |
| `internal/runner/snapshot.go` | 2 |
| `internal/runner/commit.go` | 2, 3 |
| `internal/handler/stream.go` | 3 |
| `internal/store/io.go` | 3 |
| `internal/handler/git.go` | 3 |
| `internal/handler/tasks.go` | 3 |
| `internal/handler/execute.go` | 3 |
| `internal/store/store.go` | 3 |
| `internal/store/errors.go` (new) | 3 |
| `internal/gitutil/worktree.go` | 4 |
| `internal/runner/container.go` | 4 |
| `internal/runner/title.go` | 4 |
| `main.go` | 4 |
