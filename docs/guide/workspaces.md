# Workspaces and Git

A workspace is the unit of context in Wallfacer: a named identity that points at one or more folders on the host machine. The task board, chat sessions, the whiteboard, and analytics are all scoped per workspace. This guide covers the workspace model, day-to-day workspace management, the git machinery underneath task execution, and the GitHub integration.

## The workspace model

Each workspace has a stable identity (a UUID assigned once) and a mutable folder set. The identity, not the folder list, owns the task history: renaming a workspace or re-pointing it at different folders keeps every task, session, and whiteboard scene attached to it. Workspaces are persisted in `~/.wallfacer/workspaces.json` (older `workspace-groups.json` installations are migrated automatically on startup).

Per-workspace settings:

- **Parallel caps**: a maximum number of concurrently running tasks and test runs for the workspace. Leave a cap empty to inherit the global default (`WALLFACER_MAX_PARALLEL`); see [Configuration](configuration.md).
- **Name**: an optional display name; unnamed workspaces show their folder basenames.

Folders can be git repositories or plain directories; both work (see non-git folders below).

### Dormant workspaces

When the startup migration finds task history on disk that no longer matches any live workspace, it adopts that history as a dormant workspace, with the original folders recovered on a best-effort basis. A dormant workspace keeps its recovered history but is excluded from activation until it is pointed at folders again; editing its folder set reactivates it.

## Managing workspaces

### Switching

The switcher at the top of the sidebar lists every workspace. The active one is marked; rows show running and waiting task badges so workspaces with live work stand out. Click a row to switch: the board, streams, and stores swap over within a few seconds. Tasks in the previous workspace keep running in the background.

### Creating

Click **Add workspace** in the switcher to open the picker, a two-step wizard:

1. **Choose folders**: browse the filesystem with breadcrumb navigation, a direct path input, a name filter, and a hidden-folders toggle. Git repositories carry a badge. Add one or more folders to the selection.
2. **Name and activate**: give the workspace an optional name, review the folder list, and activate.

### Editing

Open the edit control on a workspace row (in the switcher or the picker list) to open the workspace settings popup. It edits the name, the folder set (via the same folder browser), and the parallel caps, and offers deletion. Name changes save on confirm; folder and cap changes persist immediately.

### Deleting

Deleting a workspace permanently removes it and wipes its session data; tasks and history do not survive. Deleting the active workspace switches the board to the next usable workspace (or the empty state).

### Environment variable

`WALLFACER_WORKSPACES` in `~/.wallfacer/.env` records the active folder set (OS path-list separated) and is updated automatically on switch, so the same workspace comes back after a restart.

## Git integration

### Per-task worktrees

Every task on a git workspace runs in an isolated git worktree so parallel tasks never interfere:

- When a task starts, Wallfacer creates a branch named `task/<id>` (the first 8 characters of the task id) from the current HEAD of each repository.
- The worktree lives under `~/.wallfacer/worktrees/<task-id>/<repo-name>/` and is the agent's working directory, with no path translation.
- When the task completes, its changes are committed, rebased onto the default branch, and fast-forward merged; the worktree and branch are then removed. Cancelled tasks release their worktrees immediately.

See [Git Worktrees](../internals/git-worktrees.md) for the commit pipeline and conflict-resolution internals.

### Status, sync, and push

For git workspaces, the header shows a status chip per repository: name, current branch, and ahead/behind counts, refreshed by a server-sent stream every few seconds.

- **Push** appears when local commits are ahead of the upstream and runs `git push`.
- **Sync** appears when the workspace is behind and runs a fetch plus rebase onto the upstream.
- **Rebase on main** appears on feature branches and rebases the current branch onto the remote default branch.

Sync, rebase, branch switching, and branch creation are refused (with the list of blocking tasks) while active tasks still hold worktrees in the workspace, since rewriting the base under a running task would corrupt its rebase later. A rebase that hits conflicts is aborted and reported for manual resolution.

### Branches

Click the branch name in a workspace chip to open the branch dropdown: switch to an existing branch, filter by name, or type a new name and confirm to create a branch.

### Open folder

Use the workspace chip's open action to reveal the folder in the OS file manager (Finder on macOS, Explorer on Windows, `xdg-open` on Linux).

### Worktree housekeeping

Two background mechanisms keep the worktree directory healthy:

- A garbage collector (daily by default; tune with `WALLFACER_WORKTREE_GC_INTERVAL`) removes worktrees whose tasks are done, cancelled, or archived.
- A health watcher scans every two minutes and restores missing worktrees for active tasks, recording a restore event on the task timeline.

### Non-git folders

Folders that are not git repositories still get change tracking: the task works on a snapshot copy backed by a local git repository, the diff is captured from the snapshot, and changes are extracted back to the original folder on completion. The Changes tab works the same way as for git repositories.

## GitHub integration

Wallfacer does not run its own GitHub OAuth flow. It borrows the GitHub connection from the signed-in latere.ai account, where connections are managed centrally.

Gate: GitHub features require both a signed-in latere.ai account and a GitHub connection on that account. Without either, the GitHub surface stays disabled.

### Settings > GitHub

The GitHub tab in Settings reflects the borrowed connection:

- Not signed in: a prompt to sign in via latere.ai.
- Signed in, not connected: a link to connect GitHub on the latere.ai account page.
- Signed in and connected: the connected GitHub login and a link to manage connections at latere.ai.

The API mirrors this: `GET /api/github/auth/status` reports availability, connection state, login, permissions, and expiry; `POST /api/github/auth/connect` returns the install URL for the latere.ai GitHub App flow; `POST /api/github/auth/disconnect` clears the borrowed token.

### What it enables

With a connection in place, the task detail modal shows a PR panel for tasks whose branch was pushed to a github.com repository:

- **Create PR** opens a pull request for the task branch; the title defaults from the task and the body from its commit message.
- The panel shows the PR's link and state, and posts comments to the PR from the task view.

Repository-level endpoints for creating pulls and comments exist for automation (`POST /api/github/pulls`, `POST /api/github/comments`).

## See also

[Getting Started](getting-started.md) for first-run setup, [Concepts](concepts.md) for how workspaces relate to tasks and specs, [Board](board.md) for the task lifecycle, [Configuration](configuration.md) for parallelism and push settings, and [Workspaces & Config internals](../internals/workspaces-and-config.md) for the storage layout.
