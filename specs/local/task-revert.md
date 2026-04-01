---
title: "Task Revert — Agent-Assisted Undo of Merged Task Changes"
status: drafted
depends_on: []
affects:
  - internal/runner/revert.go
  - internal/handler/revert.go
  - internal/apicontract/routes.go
  - internal/gitutil/ops.go
  - internal/store/models.go
  - internal/prompts/revert.tmpl
  - ui/js/modal.js
  - ui/js/render.js
effort: medium
created: 2026-04-01
updated: 2026-04-01
author: changkun
dispatched_task_id: null
---

# Task Revert — Agent-Assisted Undo of Merged Task Changes

---

## Problem

Once a task completes and its changes are merged into the default branch, there is no way to undo those changes from within Wallfacer. The user must manually run `git revert` in a terminal, which is error-prone when:

1. **The task touched multiple workspaces.** Each workspace has its own commit hash, so the user must revert in each repo separately.
2. **Later tasks have been merged on top.** A naive `git revert <hash>` may produce conflicts because subsequent commits depend on or overlap with the reverted changes.
3. **The original commit was a squash merge.** The commit pipeline rebases and fast-forward merges, so the merge commit may bundle multiple logical changes.

Users need a one-click revert that handles these cases, including agent-assisted conflict resolution when the revert is non-trivial.

---

## Goal

1. Add a "Revert" action on done/cancelled tasks whose `CommitHashes` are recorded.
2. Perform `git revert` on each workspace's merged commit.
3. When the revert applies cleanly, commit automatically with a descriptive message.
4. When conflicts arise, launch a sandbox agent to resolve them, using the original task context (prompt, diff, oversight) as guidance.
5. Track the revert as a first-class operation with its own event trail and UI state.

---

## Design

### Eligibility

A task is revertible when all of the following hold:

- Status is `done` or `cancelled` (terminal states that went through the commit pipeline).
- `CommitHashes` is non-empty (the task actually merged changes).
- The commit hashes still exist in the repo history (`git cat-file -t <hash>` succeeds). If the repo has been force-pushed or the commit is unreachable, the revert is not possible.
- No revert is already in progress for this task.

The UI shows a "Revert" button on eligible task cards. The button is hidden or disabled otherwise.

### Data Model

Add fields to `Task` in `internal/store/models.go`:

```go
// RevertStatus tracks the lifecycle of a revert operation.
// Empty string means no revert has been attempted.
RevertStatus    RevertState       `json:"revert_status,omitempty"`
RevertCommits   map[string]string `json:"revert_commits,omitempty"`   // repoPath → revert commit hash
RevertError     string            `json:"revert_error,omitempty"`     // last error message if revert failed
RevertStartedAt *time.Time        `json:"revert_started_at,omitempty"`
RevertDoneAt    *time.Time        `json:"revert_done_at,omitempty"`
```

```go
type RevertState string

const (
    RevertStateNone       RevertState = ""          // no revert attempted
    RevertStateInProgress RevertState = "reverting"  // revert running
    RevertStateDone       RevertState = "reverted"   // revert completed successfully
    RevertStateFailed     RevertState = "revert_failed" // revert failed (conflict unresolvable or error)
)
```

### API

Two new endpoints in `internal/apicontract/routes.go`:

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/tasks/{id}/revert` | Start a revert operation for a completed task |
| `DELETE` | `/api/tasks/{id}/revert` | Cancel an in-progress revert |

**POST /api/tasks/{id}/revert**

- Validates eligibility (status, commit hashes exist, no revert in progress).
- Sets `RevertStatus = "reverting"`, records `RevertStartedAt`.
- Launches the revert pipeline in a background goroutine.
- Returns `202 Accepted` with `{"status": "reverting"}`.
- If already reverted, returns `409 Conflict`.
- If not eligible, returns `422 Unprocessable Entity` with reason.

**DELETE /api/tasks/{id}/revert**

- Cancels an in-progress revert (kills agent container if running).
- Aborts any in-progress `git revert` / `git rebase` state.
- Sets `RevertStatus = ""` (reset to no revert).
- Returns `200 OK`.

### Revert Pipeline

Implemented in `internal/runner/revert.go`. The pipeline runs per-workspace, sequentially:

```
For each workspace in task.CommitHashes:
  1. Verify commit is reachable
  2. git revert <commit_hash> --no-edit
  3. If clean → done for this workspace
  4. If conflicts → launch agent to resolve
  5. If agent resolves → commit the revert
  6. If agent fails → abort, mark revert_failed
```

#### Phase 1: Trivial Revert

For each `(repoPath, commitHash)` in `task.CommitHashes`:

1. `git cat-file -t <commitHash>` — verify the commit exists.
2. `git revert <commitHash> --no-edit` on the default branch.
3. If exit code 0: record the new revert commit hash in `RevertCommits[repoPath]`.
4. If conflicts: proceed to Phase 2.

#### Phase 2: Agent-Assisted Conflict Resolution

When `git revert` produces conflicts:

1. Abort the failed revert: `git revert --abort`.
2. Create a temporary worktree on a branch `revert/<task-uuid8>` from the current default branch HEAD.
3. In the worktree, attempt `git revert <commitHash> --no-edit` again (to get the conflict state in the worktree, not on the default branch).
4. Launch a sandbox agent container with:
   - The worktree mounted at `/workspace/<basename>`.
   - A system prompt from `internal/prompts/revert.tmpl` containing:
     - The original task prompt and goal.
     - The original diff (`task.CommitMessage` or regenerated from `BaseCommitHashes..CommitHashes`).
     - The list of conflicted files.
     - The conflict markers in each file.
     - Instruction: resolve all conflicts so the revert cleanly undoes the original task's intent, while preserving changes from later commits that are unrelated.
5. The agent resolves conflicts and stages the result.
6. On agent completion:
   - Verify no conflict markers remain.
   - Commit the resolution: `Revert "<task title>"\n\nReverts commit <hash>.\nConflicts resolved by agent.`
   - Rebase + fast-forward merge onto default branch (same as the regular commit pipeline).
   - Record the revert commit hash.
   - Clean up the worktree and branch.
7. On agent failure or timeout:
   - Abort any in-progress revert/rebase state.
   - Clean up the worktree.
   - Set `RevertStatus = "revert_failed"`, `RevertError = <reason>`.

#### Atomicity

If the task has multiple workspaces and a revert succeeds in some but fails in others:
- Already-committed reverts in successful workspaces remain (they are valid git commits).
- `RevertCommits` records the partial progress.
- `RevertStatus = "revert_failed"` with `RevertError` explaining which workspace(s) failed.
- The user can retry (`POST /api/tasks/{id}/revert` again), which skips workspaces already reverted.

### System Prompt Template

Add `internal/prompts/revert.tmpl`:

```
You are resolving git revert conflicts. A previous task made changes that are being
reverted, but later commits have created conflicts.

## Original Task
Prompt: {{.Prompt}}
Goal: {{.Goal}}

## Original Changes
{{.OriginalDiff}}

## Conflicted Files
{{range .ConflictedFiles}}
- {{.}}
{{end}}

## Instructions
1. Resolve all conflict markers in the listed files.
2. The goal is to undo the original task's changes while preserving unrelated
   changes from later commits.
3. If a later commit depends on the original task's changes, make a judgment call:
   adapt the later code to work without the reverted changes.
4. Stage all resolved files with `git add`.
5. Do NOT run `git commit` — the host will handle that.
```

### Events

Emit events to the task's event trail:

- `system` event: "Revert started" / "Revert completed" / "Revert failed: <reason>"
- `system` event: "Revert conflict in <workspace> — launching agent" (when Phase 2 triggers)
- `span_start` / `span_end` for the revert operation (for timing in the spans API)

These appear in the task timeline alongside existing events.

### Usage Tracking

Add a new `SandboxActivity`:

```go
SandboxActivityRevert SandboxActivity = "revert"
```

Token usage from the conflict resolution agent is attributed to this activity in `UsageBreakdown`.

### UI

#### Task Card

- Done/cancelled tasks with non-empty `CommitHashes` show a "Revert" button in the action menu (kebab menu or detail modal).
- If `RevertStatus == "reverting"`: show a spinner and "Reverting..." label. Disable the button. Show "Cancel Revert" option.
- If `RevertStatus == "reverted"`: show a "Reverted" badge on the card. The revert button changes to "Reverted" (disabled). Show `RevertCommits` in the detail modal.
- If `RevertStatus == "revert_failed"`: show a warning badge. The button becomes "Retry Revert". Show `RevertError` in the detail modal.

#### Detail Modal

Add a "Revert" section in the task detail modal (below the diff section):

- **Not reverted:** "Revert" button.
- **Reverting:** Progress indicator + "Cancel" link.
- **Reverted:** Revert commit hashes (per workspace), timestamp. Link to view the revert diff.
- **Failed:** Error message, "Retry" button.

#### SSE Updates

The existing SSE delta system (`/api/tasks/stream`) already pushes full task state on changes. Adding `RevertStatus` / `RevertCommits` to the `Task` model means the UI receives updates automatically — no new SSE channel needed.

### Container Logs

While the revert agent is running, its container logs are streamable via the existing `/api/tasks/{id}/logs` endpoint (reusing the same infrastructure as implementation/test/refinement containers).

---

## Scope Boundaries

**In scope:**
- Revert of done/cancelled tasks with recorded commit hashes.
- Automatic clean revert when no conflicts exist.
- Agent-assisted conflict resolution when revert is non-trivial.
- Multi-workspace revert with partial progress tracking.
- Revert cancellation.
- Event trail and usage tracking for the revert operation.

**Out of scope:**
- Revert of in-progress or waiting tasks (use "Cancel" instead).
- Revert of tasks that never merged (no `CommitHashes`).
- Batch revert of multiple tasks at once (revert one at a time; the user can chain them).
- "Undo revert" (reverting a revert) — handled by creating a new task if needed.
- Interactive conflict resolution by the user (the agent resolves; if it fails, the user can fix manually in the terminal).

---

## Testing

- **Unit tests** for eligibility checks (commit exists, status is terminal, no revert in progress).
- **Unit tests** for the trivial revert path (mock git commands, verify revert commit recorded).
- **Integration test** with a real git repo: create a commit, create a later commit, revert the first, verify the repo state.
- **Integration test** for conflict scenario: create commit A, create commit B that modifies the same lines, revert A, verify conflict is detected and escalated to agent (mock the agent response).
- **Frontend tests** for revert button visibility, state transitions, and SSE-driven UI updates.
