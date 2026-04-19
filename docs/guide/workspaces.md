# 📁 Workspaces & Git

Workspaces are the directories containing your source code that Wallfacer mounts into task containers. Each workspace is an independent project directory on your host machine. Wallfacer supports mounting multiple workspaces simultaneously, organising them into switchable groups, and providing full git integration -- branch management, sync, push, worktree isolation, and automatic conflict resolution -- all from the web UI.

---

## ⚡ Essentials

### 🔑 Key Concepts

| Concept | Description |
|---|---|
| **Workspace** | An absolute host directory mounted read-write into every task container under `/workspace/<dirname>/`. Can be a git repository or a plain directory. |
| **Workspace group** | A saved combination of one or more workspaces. Groups appear as tabs in the header bar, similar to VS Code workspace tabs. Switching groups switches the entire task board. |
| **Default branch** | The branch currently checked out in a workspace (e.g. `main`, `develop`). Task branches are created from the default branch HEAD and merged back into it when the task completes. |

### ⚙️ Setting Up Workspaces

#### From the command line

On startup, Wallfacer restores the most recently used workspace group from your previous session. If no saved group exists, it starts with no active workspaces — select them from the UI workspace picker.

#### Workspace browser

The workspace browser is a modal dialog for selecting workspace directories from the UI. Open it from the header **+** tab or **Settings > Workspace**.

Features:

- **Breadcrumb navigation** -- click any segment of the current path to jump to that directory
- **Path input** -- type or paste an absolute path and press Enter to navigate directly
- **Directory listing** -- shows all subdirectories with a **git** badge on those that are git repositories
- **Hidden files toggle** -- show or hide dotfiles (directories starting with `.`)
- **Filter** -- type in the filter field to narrow the directory list by name
- **Add button** -- click **+ Add** next to any directory to add it to the selection, or click **Add current folder** to add the directory you are currently browsing
- **Selection summary** -- the left panel shows all selected directories with remove buttons

Click **Apply** to switch to the selected workspaces. The server validates every path and creates the necessary data directories and instructions file.

### 🔄 Git Status and Basic Operations

#### Git status display

When workspaces are git repositories, the header bar shows compact status chips for each workspace:

- **Repository name** -- links to the remote URL (GitHub, GitLab, etc.) when a remote is configured
- **Branch name** -- a clickable dropdown for switching branches
- **Ahead badge** (e.g. `3 up-arrow`) -- the number of local commits not yet pushed
- **Behind badge** (e.g. `2 down-arrow`) -- the number of upstream commits not yet pulled

Status updates are streamed via Server-Sent Events (SSE) and refresh every 5 seconds, so the display stays current without manual polling.

#### Push

When a workspace has commits ahead of the upstream, a **Push** button appears next to the ahead badge. Clicking it runs `git push` on the workspace. If the push fails due to non-fast-forward, the UI suggests syncing first.

#### Sync

When a workspace is behind the upstream, a **Sync** button appears. Clicking it runs `git fetch` followed by `git rebase @{u}` on the workspace. If a rebase conflict occurs, the operation is aborted and you are asked to resolve it manually.

Sync is blocked while tasks with worktrees in that workspace are in progress, waiting, committing, or failed (with worktrees still on disk).

#### Branch switching

Click the branch name in a workspace chip to open the branch dropdown. Select a branch from the list to switch, or type in the search field to filter. Branch switching is blocked while tasks are in progress, waiting, committing, or failed with worktrees still on disk.

#### Open folder

Click a workspace name (when no remote URL is configured) or use the context menu to open the workspace directory in your OS file manager (Finder on macOS, `xdg-open` on Linux).

### 📋 Reviewing Task Changes

Each task works on an isolated branch named `task/<id>`, created from the default branch HEAD. The task's agent makes changes on this branch inside a dedicated git worktree. When a task reaches **Done**, its changes are automatically committed, rebased onto the default branch, and merged via fast-forward. You can view the diff of any task's changes against the default branch from the task detail panel.

---

## 🔧 Advanced Topics

### 🗂️ Workspace Groups

Workspace groups let you save and switch between different combinations of workspaces without restarting the server.

#### Header tabs

Each saved group appears as a tab in the header bar. The currently active group is highlighted. Click a different tab to switch to that group. Wallfacer stops any active SSE streams, resets the board, loads the new group's task store, and reconnects the streams -- all within a few seconds.

When the window is too narrow to fit all tabs, overflow tabs are automatically hidden and accessible via the **+** menu. The active tab is never auto-hidden.

#### Naming groups

By default, tabs show the basename(s) of the workspace directories (e.g. `repo-a + repo-b`). You can assign a short, readable name to any group:

- **Double-click** the active tab in the header to rename it inline. Press Enter to confirm or Escape to cancel.
- Open **Settings > Workspace** and click **Rename** next to a group to set a name via a dialog.

Named groups display the custom name on the tab. Hover over a tab to see the full workspace paths. To clear a custom name, rename it to an empty string -- the tab reverts to the basename fallback.

#### Managing tabs

| Action | How |
|---|---|
| Switch to a group | Click its tab in the header |
| Rename a group | Double-click the active tab, or click **Rename** in Settings |
| Hide a tab | Click the X on an inactive tab |
| Restore a hidden tab | Click the **+** button and select from the list |
| Add a new group | Click **+** and choose "New workspace group..." to open the workspace picker |
| Edit a group | Open **Settings > Workspace** and click **Edit** next to the group |
| Remove a group | Open **Settings > Workspace** and click **Remove** |

Groups are saved automatically to `~/.wallfacer/workspace-groups.json` whenever a group becomes active. The most recently used group is promoted to the front of the list.

#### Concurrent workspace groups

You can switch workspace groups at any time, even while tasks are running. Tasks in the previous group continue executing in the background -- their stores and worktrees are kept alive until all tasks complete. The header tabs show per-group task count badges (**N running**, **N waiting**) so you can see at a glance which groups have active work.

### 🌿 Branch Management

Click the branch name in a workspace chip to open the branch dropdown:

| Action | How |
|---|---|
| Switch branch | Select a branch from the list |
| Filter branches | Type in the search field |
| Create a new branch | Type a name that does not match any existing branch, then click "Create branch" or press Enter |

Branch switching and creation are blocked while tasks are in progress, waiting, committing, or failed with worktrees still on disk.

#### Rebase on Main

When you are on a feature branch, a **Rebase on main** button appears. It fetches the remote default branch (e.g. `origin/main`) and rebases your current branch on top of it. This is useful when you want to incorporate upstream changes from the main branch into a feature branch. The button shows the behind-main count when your branch is behind.

Like Sync, this operation is blocked while tasks depend on the workspace's git state.

### 🌳 Git Worktrees

Every task runs on an isolated git branch and worktree, so multiple tasks can work on the same repository simultaneously without conflicts.

#### How worktrees are created

```
main branch ─────────────────────────
  ├── task-abc worktree (isolated)
  ├── task-def worktree (isolated)
  └── task-ghi worktree (isolated)
```

When a task moves to **In Progress**:

1. Wallfacer creates a new branch named `task/<first-8-chars-of-task-id>` from the current HEAD of each workspace
2. A git worktree is created at `~/.wallfacer/data/<workspace-key>/worktrees/<task-id>/<repo-name>/`
3. The worktree is mounted read-write into the task container under `/workspace/<repo-name>/`

For non-git workspaces, a snapshot copy is created instead, with a local git repository initialised for change tracking. When the task completes, the diff is captured from the snapshot before changes are extracted back to the original directory, so the diff view works for non-git workspaces too.

#### Worktree lifecycle

- **In Progress / Waiting / Failed** -- the worktree exists on disk and can be inspected
- **Done** -- after the commit pipeline completes, the worktree and branch are deleted
- **Cancelled** -- the worktree and branch are deleted immediately

If the server restarts while tasks are in progress, it recovers worktrees by reattaching to existing branches.

### 📤 The Commit Pipeline

When a task reaches **Done** (either by the agent finishing its work or by the user clicking "Mark Done"), Wallfacer runs a three-phase commit pipeline:

#### Phase 1: Stage and commit

1. All uncommitted changes in every worktree are staged (`git add -A`)
2. A commit message is generated by a lightweight sandbox agent that analyses the diff stats, the task prompt, and the repository's recent commit style
3. Changes are committed on the task branch in each worktree

If commit message generation fails, a fallback message is constructed from the task prompt.

#### Phase 2: Rebase and merge

For each workspace (serialised per repository to avoid races):

1. The task branch is rebased onto the default branch
2. If the rebase succeeds, the default branch is fast-forward merged to the task branch tip
3. Commit hashes are recorded for later reference

If a rebase conflict occurs, Wallfacer invokes a conflict-resolution agent (see below). Up to 3 rebase attempts are made.

#### Phase 3: Cleanup

1. The git worktree is removed
2. The task branch is deleted
3. The task's worktree directory is removed from disk

### 🔀 Conflict Resolution

When the rebase in Phase 2 encounters a merge conflict, Wallfacer handles it automatically:

1. The failed rebase is aborted, leaving the worktree in a clean state on the task branch
2. A sandbox container is launched with the conflicted worktree mounted
3. The agent is given a specialised conflict-resolution prompt instructing it to start the rebase, resolve all conflicts, and complete the rebase with `git rebase --continue`
4. If the agent succeeds, the commit pipeline retries the rebase

This process repeats for up to 3 attempts. If all attempts fail, the task is marked **Failed**. You can then inspect the task's event timeline to see what went wrong.

Conflict resolution is triggered in two contexts:

- **Commit pipeline** -- when a completed task's branch conflicts with the default branch during the final merge
- **Task sync** -- when rebasing a waiting or failed task's worktree onto the latest default branch (see below)

### 🔄 Syncing Tasks

While a task is in the **Waiting** or **Failed** state, you can sync its worktrees to incorporate changes that other tasks have merged into the default branch since this task started. Click **Sync** in the task detail panel.

Syncing runs `git rebase` on the task's worktree against the default branch. If conflicts are encountered, the same agent-driven conflict resolution described above is used (up to 3 attempts).

The **Catch Up** automation toggle (in the Automation menu) can automatically rebase waiting tasks onto the latest branch whenever it advances, preventing merge conflicts.

### 🚀 Auto-Push

After the commit pipeline completes, Wallfacer can optionally push each workspace to its remote. Auto-push is controlled by the `WALLFACER_AUTO_PUSH` and `WALLFACER_AUTO_PUSH_THRESHOLD` environment variables; see [Configuration → Full Environment Variables Reference](configuration.md#full-environment-variables-reference) for defaults. It can also be toggled from the **Automation** menu in the header.

### 📝 Workspace Instructions (AGENTS.md)

Each workspace group has its own `AGENTS.md` file that provides instructions to every agent running in that group. The file is identified by a SHA-256 fingerprint of the sorted workspace paths, so switching to workspaces `~/a` and `~/b` (in any order) shares the same instructions file.

#### Where instructions are stored

Instructions files live in `~/.wallfacer/instructions/<fingerprint>.md`. They are mounted read-only into every task container at `/workspace/AGENTS.md`.

#### Default content

When a workspace group is activated for the first time, an `AGENTS.md` is created automatically with:

1. A default template with general coding guidance
2. A workspace layout section listing the mounted directories
3. References to any per-repository `AGENTS.md` or `CLAUDE.md` files found in the workspace directories

#### Editing from the UI

Open **Settings > Workspace Instructions** to view and edit the current instructions. Changes are saved immediately and take effect for the next task that starts.

#### Re-initialising

Click **Re-init** to rebuild the instructions file from scratch using the default template and the current per-repo instruction files. This overwrites any manual edits.

### Environment Variable

Set `WALLFACER_WORKSPACES` in `~/.wallfacer/.env` to persist workspaces across restarts. Paths are separated by the OS path-list separator (`:` on macOS/Linux, `;` on Windows):

```
WALLFACER_WORKSPACES=/Users/you/project-a:/Users/you/project-b
```

When you switch workspaces in the UI, this variable is updated automatically.

For the full HTTP API reference (workspace, git, and instruction endpoints), see [API & Transport](../internals/api-and-transport.md). For the `WALLFACER_WORKSPACES`, `WALLFACER_AUTO_PUSH`, and `WALLFACER_AUTO_PUSH_THRESHOLD` env vars, see [Configuration → Full Environment Variables Reference](configuration.md#full-environment-variables-reference).

---

## See Also

[Board & Tasks](board-and-tasks.md) for task lifecycle details, [Automation](automation.md) for autopilot and auto-sync settings, [Configuration](configuration.md) for the full environment variable reference.
