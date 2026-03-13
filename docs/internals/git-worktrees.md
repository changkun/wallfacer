# Git Operations & Worktree Lifecycle

## Core Principle

Every task gets its own git worktree. The agent operates in an isolated copy of each repository on a dedicated branch, leaving the main working tree untouched and allowing multiple tasks to run concurrently without interfering with each other.

```
Main repo (~/projects/myapp)          Task worktree
  branch: main                          branch: task/a1b2c3d4
  working tree: clean                   working tree: mounted into container
                                        path: ~/.wallfacer/worktrees/<uuid>/myapp
```

## Worktree Setup

Called by `setupWorktrees()` in `runner.go` when a task enters `in_progress`.

For each configured workspace:

```
1. git rev-parse --git-dir
       └─ verify the path is a git repository

2. git worktree add -b task/<uuid8> \
       ~/.wallfacer/worktrees/<task-uuid>/<repo-basename>
       └─ creates a new branch and a new working tree simultaneously

3. store worktree path + branch name on the Task struct
```

Branch naming uses the first 8 characters of the task UUID: `task/a1b2c3d4`.

Multiple workspaces → multiple worktrees, all grouped under `~/.wallfacer/worktrees/<task-uuid>/`:

```
~/.wallfacer/worktrees/
└── <task-uuid>/
    ├── myapp/       # worktree for ~/projects/myapp
    └── mylib/       # worktree for ~/projects/mylib
```

## Container Mounts

The sandbox container sees worktrees, not the live main working directory:

```
~/.wallfacer/worktrees/<uuid>/<repo>  →  /workspace/<repo>   (read-write)
~/.wallfacer/.env                      →  /run/secrets/.env   (read-only)
~/.gitconfig                           →  /home/claude/.gitconfig (read-only)
claude-config (named volume)           →  /home/claude/.claude
```

The agent operates on `/workspace/<repo>` — the isolated worktree branch — so all edits land on `task/<uuid8>` and never touch `main`.

## Commit Pipeline

Triggered automatically after `end_turn`, or manually when a user marks a `waiting` task as done. Runs four sequential phases in `runner.go`.

### Phase 1 — Claude Commits (in container)

A new container run is launched with a commit prompt. Claude executes:
```
git add -A
git commit -m "<meaningful message>"
```
in each worktree. This happens inside the sandbox with the same user identity as the main run.

### Phase 2 — Rebase & Merge (host-side, `git.go`)

```
git rebase <default-branch>
  └─ rebases task branch on top of the current default branch HEAD
  └─ on conflict: retry up to 3 times, invoking Claude's conflict resolver each time

git merge --ff-only <task-branch>
  └─ fast-forward merges the rebased task branch into the default branch
  └─ collect resulting commit hashes
```

`defaultBranch()` resolves the target branch by checking, in order:
1. `origin/HEAD` (remote default)
2. Current `HEAD` branch name
3. Falls back to `"main"`

**Conflict resolution loop:** If `git rebase` exits non-zero, Wallfacer invokes the agent again — using the original task's session ID — passing it the conflict details. The agent resolves the conflicts and stages the result. The rebase is then continued and retried. Up to 3 attempts are made before the task is marked `failed`.

### Phase 3 — Cleanup

```
git worktree remove --force   ← remove worktree directory
git branch -D task/<uuid8>    ← delete task branch
rm -rf ~/.wallfacer/worktrees/<uuid>/   ← remove task worktree directory
```

Note: `data/<uuid>/` (task record, traces, outputs, oversights, summary) is **preserved** after cleanup so execution history remains accessible in the UI.

Cleanup is idempotent and safe to call multiple times (errors are logged, not fatal).

## Task Forking

`POST /api/tasks/{id}/fork` creates a new backlog task that inherits the source task's worktree state:

1. The source task's worktree is copied to a new location under `~/.wallfacer/worktrees/<new-uuid>/`
2. A new branch `task/<new-uuid8>` is created from the current worktree state
3. The new task records `ForkedFrom = source_task_uuid` for lineage tracking
4. The forked task can be independently executed, tested, and merged

Forking is available from `waiting`, `failed`, or `done` tasks.

## Orphan Pruning

`pruneOrphanedWorktrees()` runs on every server startup:

1. Scan `~/.wallfacer/worktrees/` for subdirectories
2. For each directory, check if a task with matching UUID exists in the store
3. If no matching task found: remove the directory + run `git worktree prune` on all workspaces to clear stale refs from `.git/worktrees/`

This handles crashes where cleanup never ran.

## Worktree Sync (Rebase Without Merge)

Tasks in `waiting` or `failed` status can be synced with the latest default branch via `POST /api/tasks/{id}/sync`. This rebases the task worktree onto the current default branch HEAD without merging, keeping the task's changes on top.

```
POST /api/tasks/{id}/sync
  ↓
task status → in_progress (temporarily)
  ↓
for each worktree:
  git fetch origin
  git rebase <default-branch>
    └─ on conflict: invoke agent (same session) to resolve, up to 3 retries
  ↓
task status → previous status (waiting or failed)    [conflicts resolved]
  ↓
if rebase fails after retries:
  agent (Run) invoked with conflict resolution prompt → task stays in_progress
  └─ agent resolves conflict → task status → waiting (or done on end_turn)
```

This is useful when other tasks have merged changes to the default branch and you want the current task to pick them up before continuing.

## Task Diff

`GET /api/tasks/{id}/diff` returns the diff of a task's changes against the default branch. It handles multiple scenarios:

- **Active worktrees** — uses `merge-base` to diff only the task's changes since it diverged, including untracked files
- **Merged tasks** (worktree cleaned up) — falls back to stored commit hashes or branch names to reconstruct the diff
- Returns `behind_counts` per repo indicating how many commits the default branch has advanced since the task branched off

## Git Helper Functions (`internal/gitutil/`)

Git operations are organized in the `internal/gitutil` package:

| File | Purpose |
|---|---|
| `repo.go` | Repository queries: `IsGitRepo`, `DefaultBranch`, `MergeBase`, `CommitsBehind` |
| `worktree.go` | Worktree lifecycle: `CreateWorktree`, `RemoveWorktree`, `PruneWorktrees` |
| `ops.go` | Git operations: `RebaseOnto`, `FFMerge`, `HasCommitsAheadOf`, `GetCommitHash` |
| `stash.go` | Stash operations for conflict resolution |
| `status.go` | Workspace git status for the UI header bar |

## Git Status & Branch Management API

The server exposes git status and branch management for the UI header bar. See [Orchestration](orchestration.md) for the full API route list.

- `GET /api/git/status` — current branch, remote tracking, ahead/behind counts per workspace
- `GET /api/git/stream` — SSE endpoint pushing git status updates
- `POST /api/git/push` — run `git push` on a workspace
- `POST /api/git/sync` — fetch from remote and rebase workspace onto upstream
- `GET /api/git/branches?workspace=<path>` — list all local branches for a workspace; returns `{branches: [...], current: "main"}`
- `POST /api/git/checkout` — switch the active branch (`{workspace, branch}`); refuses while tasks are in progress
- `POST /api/git/create-branch` — create and checkout a new branch (`{workspace, branch}`); refuses while tasks are in progress

### Branch Switching

The UI header displays a branch switcher dropdown for each workspace. Users can:

1. **Switch branches** — select an existing branch from the dropdown. The server runs `git checkout` on the workspace. All future task worktrees branch from the new HEAD.
2. **Create branches** — type a new branch name in the search field and select "Create branch". The server runs `git checkout -b` on the workspace.

Both operations are blocked while any task is `in_progress` to prevent worktree conflicts.
