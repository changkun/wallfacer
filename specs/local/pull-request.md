---
title: "Pull Request Creation — Agent-Generated PR from Current Branch"
status: drafted
depends_on: []
affects:
  - internal/runner/pullrequest.go
  - internal/handler/git.go
  - internal/apicontract/routes.go
  - internal/prompts/pullrequest.tmpl
  - internal/store/models.go
  - ui/js/git.js
  - ui/js/render.js
effort: medium
created: 2026-04-01
updated: 2026-04-01
author: changkun
dispatched_task_id: null
---

# Pull Request Creation — Agent-Generated PR from Current Branch

---

## Problem

After tasks complete and changes are pushed, the user must manually create a GitHub pull request — writing a title and description that summarizes the branch's changes. Wallfacer already has the machinery to run lightweight sandbox containers for text generation (title generation, commit message generation) and knows exactly which commits and diffs the branch contains, but there is no way to turn that into a PR from within the UI.

---

## Goal

1. Add a "Create PR" action in the git panel for workspaces that have commits ahead of the remote default branch.
2. Collect the branch's commit log and diff deterministically on the host.
3. Run a lightweight sandbox container to generate a PR title and description from that data.
4. Create the PR via `gh pr create` on the host.
5. Return the PR URL to the UI.

---

## Design

### Prerequisites

- The workspace must be a git repo with a remote.
- The current branch must differ from the remote default branch (i.e., `AheadCount > 0` or the branch is not the default branch with commits ahead of `origin/<default>`).
- The branch must be pushed to the remote (`git push -u origin <branch>` if needed).
- `gh` CLI must be installed and authenticated. The server checks this at startup (like the existing `wallfacer doctor` checks) and exposes the status via `/api/config`.

### API

Two new endpoints:

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/git/pr` | Create a pull request for a workspace branch |
| `GET` | `/api/git/pr/status` | Check PR creation prerequisites (gh auth, branch state) |

#### GET /api/git/pr/status?workspace=\<path\>

Returns prerequisite checks so the UI can show/hide the PR button and display actionable messages.

```json
{
  "eligible": true,
  "gh_installed": true,
  "gh_authenticated": true,
  "branch": "feature/my-change",
  "default_branch": "main",
  "ahead_count": 3,
  "pushed": true,
  "existing_pr_url": null
}
```

- `eligible`: all prerequisites met; the "Create PR" button should be active.
- `gh_installed` / `gh_authenticated`: whether `gh` is available and logged in.
- `pushed`: whether the local branch has been pushed to the remote.
- `existing_pr_url`: if a PR already exists for this branch, its URL (from `gh pr view --json url`). The UI shows "View PR" instead of "Create PR" in this case.

#### POST /api/git/pr

Request body:

```json
{
  "workspace": "/path/to/repo",
  "base": "main",
  "draft": false
}
```

- `workspace` (required): the workspace path.
- `base` (optional): target branch; defaults to the remote default branch.
- `draft` (optional): create as draft PR; defaults to `false`.

Response: `202 Accepted` with `{"status": "generating"}` while the sandbox generates the PR text, then the handler creates the PR synchronously and returns:

```json
{
  "url": "https://github.com/owner/repo/pull/42",
  "title": "Add task revert with agent-assisted conflict resolution",
  "number": 42
}
```

Error responses:
- `400`: workspace not configured, not a git repo, or branch is the default branch.
- `409`: a PR already exists for this branch (returns `{"existing_pr_url": "..."}`).
- `422`: `gh` not installed or not authenticated.
- `500`: sandbox or `gh` command failure.

### Pipeline

The PR creation pipeline is a synchronous request handler (not a background goroutine) because it is fast: ~10s for the sandbox call + ~2s for `gh pr create`. The handler performs all steps within the HTTP request timeout.

#### Step 1: Collect Branch Context (deterministic, host-side)

All data collection is plain git commands — no sandbox involved.

```go
// Inputs gathered deterministically:
type PRContext struct {
    Workspace     string
    Branch        string
    BaseBranch    string
    CommitLog     string // git log --format="%h %s" <base>..HEAD
    DiffStat      string // git diff --stat <base>..HEAD
    Diff          string // git diff <base>..HEAD (truncated to 100KB)
    CommitCount   int    // number of commits
    FilesChanged  int    // from diffstat
    RecentCommits string // git log --format="%h %s%n%n%b" <base>..HEAD (full messages)
}
```

1. `git log --format="%h %s" origin/<base>..HEAD` — one-line commit summary.
2. `git diff --stat origin/<base>..HEAD` — file change summary.
3. `git diff origin/<base>..HEAD` — full diff, truncated to 100 KB to fit sandbox context.
4. `git log --format="%h %s%n%n%b" origin/<base>..HEAD` — full commit messages with bodies.

#### Step 2: Generate PR Title and Description (sandbox)

Launch a lightweight container following the same pattern as title generation:

- Container name: `wallfacer-pr-<workspace-hash8>`
- Timeout: 90 seconds (same as commit message generation).
- Sandbox: route via a new `SandboxActivity`:

```go
SandboxActivityPR SandboxActivity = "pull_request"
```

Falls back through the standard sandbox routing chain (`WALLFACER_SANDBOX_PULL_REQUEST` → `WALLFACER_DEFAULT_SANDBOX` → Claude). Codex fallback on token limit, same as title generation.

Prompt template (`internal/prompts/pullrequest.tmpl`):

```
Write a GitHub pull request title and description for the following branch changes.

Rules:
- Output format: first line is the PR title, then a blank line, then the description body.
- Title: concise, imperative, max 72 characters. No prefix like "PR:" or "feat:".
- Description: markdown format. Include:
  - A "## Summary" section with 2-5 bullet points describing what changed and why.
  - A "## Changes" section listing key changes grouped by area.
  - If there are breaking changes or migration steps, include a "## Breaking Changes" section.
- Output ONLY raw text, no markdown fences wrapping the whole output.
- Do not repeat the commit messages verbatim; synthesize them.

Branch: {{.Branch}} → {{.BaseBranch}}
Commits ({{.CommitCount}}):
{{.CommitLog}}

Commit details:
{{.RecentCommits}}

Changed files:
{{.DiffStat}}

Diff (may be truncated):
{{.Diff}}
```

Parse the output: first non-empty line = title, rest = description body.

#### Step 3: Push Branch if Needed (host-side)

If the branch has not been pushed or is behind the local HEAD:

```
git push -u origin <branch>
```

This ensures the remote has the latest commits before PR creation.

#### Step 4: Create PR (host-side)

```
gh pr create --base <base> --head <branch> --title "<title>" --body "<body>" [--draft]
```

Parse the output URL from `gh pr create` stdout. If `gh` returns an error indicating a PR already exists, fetch and return the existing URL.

### Runner Method

```go
// GeneratePRContent runs a lightweight sandbox to produce a PR title and
// description from the branch's commit log and diff.
// Returns (title, body, error).
func (r *Runner) GeneratePRContent(ctx context.Context, prCtx PRContext) (string, string, error)
```

This follows the exact same pattern as `generateCommitMessage()`:
1. Build container spec via `buildBaseContainerSpec()`.
2. Set `spec.Cmd = buildAgentCmd(prompt, model)`.
3. Launch, read stdout/stderr, wait.
4. Parse with `parseOutput()`.
5. Split result into title + body.
6. Accumulate usage (no task ID — this is a workspace-level operation, so usage is logged but not attributed to a task).

### Usage Tracking

Since PR creation is a workspace-level operation (not tied to a specific task), token usage is:
- Logged to the server log for debugging.
- Not attributed to any task's `UsageBreakdown`.
- Optionally: tracked in a new lightweight `PRUsage` log file per workspace. This is a nice-to-have and can be deferred.

### UI

#### Git Panel

The git status panel already shows per-workspace branch state (branch name, ahead/behind counts, push/sync buttons). Add:

- **"Create PR" button:** shown when the branch is not the default branch and has commits ahead. Disabled with tooltip when `gh` is not installed/authenticated.
- **"View PR" link:** shown when `existing_pr_url` is non-null (a PR already exists for this branch). Opens in the OS browser.
- **Loading state:** while the sandbox generates PR content, show a spinner on the button with "Generating PR...".
- **Result:** on success, show a toast notification with the PR URL (clickable). The button changes to "View PR" with the returned URL.
- **Error:** show toast with the error message.

#### Settings

Add a note in the configuration panel under "Prerequisites" showing `gh` CLI status (installed, authenticated), similar to how sandbox image status is shown.

### Doctor Check

`wallfacer doctor` already checks for podman/docker. Add an optional (non-blocking) check:

```
✓ gh CLI installed (version 2.x.x)
✓ gh authenticated (github.com)
```

or:

```
⚠ gh CLI not found — PR creation will not be available
```

This is informational only — `gh` is not required for core functionality.

---

## Scope Boundaries

**In scope:**
- PR creation for a single workspace from the current branch.
- Lightweight sandbox for title/description generation.
- `gh pr create` execution on the host.
- Prerequisite checks (gh installed, authenticated, branch state).
- Existing PR detection (show "View PR" instead of "Create PR").
- Draft PR support.
- Push branch before PR creation if needed.

**Out of scope:**
- PR creation across multiple workspaces in a single action (each workspace gets its own PR).
- PR review, merge, or close from within Wallfacer.
- PR templates from the repo's `.github/PULL_REQUEST_TEMPLATE.md` (could be a follow-up enhancement to include in the prompt context).
- GitLab/Bitbucket support (only GitHub via `gh` CLI).
- Interactive editing of the generated title/description before creation (the user can edit on GitHub after creation; a follow-up could add a preview/edit step).

---

## Testing

- **Unit tests** for `PRContext` collection: mock git commands, verify commit log and diff are correctly gathered and truncated.
- **Unit tests** for PR content parsing: given a sandbox output string, verify title and body are correctly split.
- **Unit tests** for prerequisite checks: mock `gh` presence/auth status, verify eligibility logic.
- **Integration test** with a real git repo: create a branch with commits, verify the full pipeline (context collection → prompt rendering → PR creation via mock `gh`).
- **Frontend tests** for button visibility based on `pr/status` response, loading states, and result display.
