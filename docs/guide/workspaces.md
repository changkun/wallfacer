# Workspaces and Git

A workspace is the unit of context in Wallfacer: a named identity that points at one folder on the host machine. The task board, chat sessions, the whiteboard, and analytics are all scoped per workspace. This guide covers the workspace model, day-to-day workspace management, and the git machinery underneath task execution.

## The workspace model

Each workspace has a stable identity (a UUID assigned once) and one folder. The identity, not the folder, owns the task history: renaming a workspace or re-pointing it at a different folder keeps every task, session, and whiteboard scene attached to it. Several workspaces may point at the same folder; each keeps its own history. Workspaces are persisted in `~/.wallfacer/workspaces.json` (older `workspace-groups.json` installations are migrated automatically on startup), and after a restart the most recently used workspace opens again.

Per-workspace settings:

- **Parallel caps**: a maximum number of concurrently running tasks and test runs for the workspace. Leave a cap empty to inherit the global default (`WALLFACER_MAX_PARALLEL`); see [Configuration](configuration.md).
- **Name**: an optional display name; an unnamed workspace shows its folder name.

The folder can be a git repository or a plain directory; both work (see non-git folders below). Work that spans several repositories runs as separate workspaces, one per repository.

### Dormant workspaces

A dormant workspace keeps its task history but has no folder chosen yet, so it cannot open a board. Two kinds of workspace are dormant:

- **Recovered**: the startup migration found task history on disk that no longer matches any live workspace and adopted it, with the original folder recovered on a best-effort basis.
- **Several folders**: a workspace saved by an earlier version that allowed several folders per workspace. It keeps its name, history, and every folder it named, and no folder is picked for it. The server log notes at startup how many such workspaces wait.

In the picker list a dormant workspace carries a **recovered** or **several folders** badge, and clicking it opens the folder step instead of the board. Choosing one folder (for a workspace saved with several, each former folder has a **Keep** button) makes it a normal workspace again, and its tasks reappear on the board. The workspace settings popup offers the same choice.

A task created in a workspace that had several folders keeps a worktree in each of them. It still runs: the agent works in the worktree for the workspace's folder (or the first one when the workspace now points elsewhere), the task timeline notes this once, and committing the task merges the changes in every folder.

## Managing workspaces

### Switching

The workspace chip at the top of the sidebar names the active workspace and shows its connection state; open it to list every workspace. The active one is marked; rows show running and waiting task counts so workspaces with live work stand out. Click a row to switch: the board, streams, and stores swap over within a few seconds. Tasks in the previous workspace keep running in the background. The chip also lists the workspace's branches with Sync, Push, and Rebase actions.

### Creating

Click **Add workspace** in the switcher to open the picker, a two-step wizard:

1. **Choose folder**: browse the filesystem with breadcrumb navigation, a direct path input, a name filter, and a hidden-folders toggle, or open the system folder chooser. Git repositories carry a badge. Choosing another folder replaces the earlier choice.
2. **Name and activate**: give the workspace an optional name, review the folder, and activate.

### Editing

Open the edit control on a workspace row (in the switcher or the picker list) to open the workspace settings popup. It edits the name, changes the folder (via the same folder browser), and sets the parallel caps, and offers deletion. Name changes save on confirm; folder and cap changes persist immediately.

### Deleting

Deleting a workspace permanently removes it and wipes its session data; tasks and history do not survive. The folder on disk is not touched. Deleting the active workspace switches the board to the next usable workspace (or the empty state).

## Git integration

### Per-task worktrees

Every task on a git workspace runs in an isolated git worktree so parallel tasks never interfere:

- When a task starts, Wallfacer creates a branch named `task/<id>` (the first 8 characters of the task id) from the current HEAD of the workspace's repository.
- The worktree lives under `~/.wallfacer/worktrees/<task-id>/<repo-name>/` and is the agent's working directory, with no path translation. The agent always works in the task's worktree, never in the folder itself, even after the workspace is re-pointed.
- When the task completes, its changes are committed, rebased onto the default branch, and fast-forward merged; the worktree and branch are then removed. Cancelled tasks release their worktrees immediately.

See [Git Worktrees](../internals/git-worktrees.md) for the commit pipeline and conflict-resolution internals.

### Status, sync, and push

For a git workspace, the header shows a status chip for the repository: name, current branch, and ahead/behind counts, refreshed by a server-sent stream every few seconds.

- **Push** appears when local commits are ahead of the upstream and runs `git push` with the host's own git configuration and credentials. Wallfacer does not open pull requests; a pull request for a pushed branch is opened on the git host.
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

## See also

[Getting Started](getting-started.md) for first-run setup, [Concepts](concepts.md) for how workspaces relate to tasks and specs, [Board](board.md) for the task lifecycle, [Configuration](configuration.md) for parallelism and push settings, and [Workspaces & Config internals](../internals/workspaces-and-config.md) for the storage layout.
