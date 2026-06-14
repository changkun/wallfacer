# Configuration & Customization

Wallfacer is configured through the Settings UI, environment variables in `~/.wallfacer/.env`, CLI flags, and file-based overrides for system prompts and workspace instructions. The `.env` file is auto-generated on first run with commented-out defaults; edit it directly or use the Settings UI. Most settings take effect on the next task run without restarting the server.

## How tasks run

Wallfacer runs each task as a host process. The runner execs the selected CLI (`claude`, `codex`, `cursor-agent`, `opencode`, or `pi`) directly on your machine, with the task's git worktree as the working directory. Isolation comes from the per-task worktree, so no container runtime is required to install or run. Tasks run with your user account's permissions and can read or write any file your account can. Run Wallfacer only on machines you trust.

This is the current and default runtime. Throughout this document, "the agent" refers to the host CLI process Wallfacer launches for a given task.

---

## Essentials

### Opening Settings

Open the Settings page by clicking the gear icon in the top-right corner of the task board, or press `Cmd+,` (`Ctrl+,`). The page has seven tabs: Appearance, Execution, Harness, Workspace, Prompts, Trash, and About.

### Appearance

**Theme** -- Choose between Light, Dark, or Auto (follows the operating system preference). The theme applies to the current browser session.

**Done Column** -- Toggle "Show archived tasks" to display or hide archived completed tasks on the board.

### Setting Up Credentials

At minimum, you need one of these credentials configured in **Settings > Harness**:

**Claude configuration:**
- **Sign in with Claude** button -- starts an OAuth flow: opens your browser to authenticate, then stores the token automatically. This is the easiest way to set up credentials.
- OAuth Token (`CLAUDE_CODE_OAUTH_TOKEN`) -- alternatively, paste a token from `claude setup-token`; takes precedence when both credentials are set
- API Key (`ANTHROPIC_API_KEY`) -- direct key from console.anthropic.com
- **Test** button -- runs a quick connectivity check to verify your credentials work. If the test detects an invalid or expired token and OAuth is available, a **Sign in again** button appears inline.

**Codex configuration:** similarly, use the **Sign in with OpenAI** button for OAuth or paste an API key manually.

**Cursor configuration:** paste a `CURSOR_API_KEY`, or run `cursor-agent login` once and the CLI reuses that session. Use the **Test** button to verify connectivity.

**OpenCode configuration:** OpenCode manages provider credentials itself. Run `opencode auth login` once and select a provider (Anthropic, OpenAI, OpenRouter, etc.); the CLI stores that credential in its own config, so no API key is needed in `~/.wallfacer/.env`. Use the **Test** button to verify connectivity.

**Pi configuration:** Pi is Armin Ronacher's `pi` coding agent ([pi.dev](https://pi.dev), [earendil-works/pi](https://github.com/earendil-works/pi)), **not** Inflection's Pi chatbot. It has no dedicated key: it reads provider credentials (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, etc.) from the same environment the Claude and Codex blocks configure. Select its model with the two-flag form `--provider <name> --model <id>`; pass a `provider/model` string (e.g. `anthropic/claude-sonnet-4-6`) as the task model and Wallfacer splits it on the first `/`. Use the **Test** button to verify connectivity.

The sign-in buttons are hidden when a custom base URL is configured (custom endpoints don't use standard OAuth). On first launch with no credentials for any provider, a prompt guides you to set up credentials.

All changes are written to `~/.wallfacer/.env` and take effect on the next task run. Leave token fields blank to preserve the existing value.

### Verifying the CLIs

Wallfacer launches the `claude`, `codex`, `cursor-agent`, `opencode`, and `pi` CLIs from your `$PATH`. Install them with:

- `npm i -g @anthropic-ai/claude-code` for Claude.
- `npm i -g @openai/codex` for Codex (optional; skip if you only run Claude tasks).
- `cursor-agent` from [cursor.com/docs/cli](https://cursor.com/docs/cli) for Cursor (optional).
- `opencode` from [opencode.ai/docs/cli](https://opencode.ai/docs/cli) for OpenCode (optional); after install run `opencode auth login` to configure a provider.
- `pi` from [pi.dev](https://pi.dev) for Pi (optional); it reads provider keys from the environment, so no separate login is required.

Run `wallfacer doctor` to confirm the binaries are resolvable and to print their `--version` output. Missing codex, cursor-agent, opencode, or pi is a soft warning: tasks routed to that agent will fail, but claude-only workflows still work.

### Key Environment Variables

All configuration lives in `~/.wallfacer/.env` (auto-generated on first run). The server re-reads this file before each task run, so changes take effect on the next task without a server restart.

| Variable | Required | Description |
|---|---|---|
| `CLAUDE_CODE_OAUTH_TOKEN` | one of these two | OAuth token from `claude setup-token` (Claude Pro/Max subscription) |
| `ANTHROPIC_API_KEY` | one of these two | API key from console.anthropic.com (starts with `sk-ant-...`) |
| `WALLFACER_MAX_PARALLEL` | no | Maximum concurrent tasks auto-promoted to In Progress. Default is `1` in the host runtime (see [Concurrency](#concurrency)). |

### CLI Basics

```bash
wallfacer run                           # Start server, restore last workspace group
wallfacer run -addr :9090 -no-browser   # Custom port, no browser
wallfacer status                        # Print board state to terminal
wallfacer status -watch                 # Live-updating board state
wallfacer doctor                        # Check prerequisites and config
```

See the [CLI Reference](#cli-reference) for the full subcommand and flag list.

### Plan Mode

See [Designing Specs](designing-specs.md) for the full Plan mode guide.

### Planning Chat

See [Exploring Ideas](exploring-ideas.md) for the full planning chat guide.

### Keyboard Shortcuts

This is the canonical shortcut reference. Other guides link here instead of maintaining their own tables.

**Global** (Board and Plan mode):

| Key | Action |
|---|---|
| `?` | Show keyboard shortcuts help |
| `Cmd+,` / `Ctrl+,` | Open Settings |
| `Cmd+K` / `Ctrl+K` | Open command palette |
| `E` | Toggle file explorer |
| `P` | Toggle Board ↔ Plan mode |
| `` Ctrl+` `` | Toggle terminal panel |
| `Escape` | Close topmost modal or blur search bar |

**Board:**

| Key | Context | Action |
|---|---|---|
| `N` | Board | Open new task form |
| `/` | Board | Focus the search bar |
| `Ctrl+Enter` / `Cmd+Enter` | New task form | Save task |
| `Escape` | New task form | Cancel |
| `Enter` / `Space` | Focused card | Open task detail |
| `Arrow keys` | Focused card | Navigate between cards |

**Focused card** (quick actions; each fires only when valid for the card's current status):

| Key | Action |
|---|---|
| `s` | Start the task |
| `d` | Mark a waiting task as done |
| `r` | Resume a waiting task that has a session, otherwise retry (re-queue to backlog) |
| `t` | Run the test-verification agent |
| `p` | Open the task in Plan mode |

**Plan mode:**

| Key | Action |
|---|---|
| `C` | Toggle chat pane |
| `D` | Dispatch focused spec as a task |
| `B` | Break down focused spec into sub-specs |

Board and plan-mode shortcuts are suppressed when focus is in a text input or when a modal is open.

---

## Advanced Topics

### Execution Settings

**Parallel Tasks** -- Set the maximum number of tasks that run concurrently in the In Progress column (1--20). Corresponds to `WALLFACER_MAX_PARALLEL`.

**Archived Tasks** -- Number of archived task items to load per scroll page (1--200). Corresponds to `WALLFACER_ARCHIVED_TASKS_PER_PAGE`.

**Oversight Interval** -- Minutes between periodic oversight summary generation while a task is running (0--120). Setting this to 0 means oversight is only generated when the task reaches a terminal state. Corresponds to `WALLFACER_OVERSIGHT_INTERVAL`.

**Auto Push** -- Enable automatic `git push` after a task's commit pipeline completes. When enabled, an additional threshold field appears: push only triggers when the workspace is at least N commits ahead of upstream. Corresponds to `WALLFACER_AUTO_PUSH` and `WALLFACER_AUTO_PUSH_THRESHOLD`.

**Brainstorm** -- Enable the brainstorm (ideation) agent and set its recurrence interval. Options range from "immediately" to "every 24h". The brainstorm agent analyses repositories and proposes tasks tagged `idea-agent`. A "Run now" button triggers an immediate brainstorm.

**Task Titles** -- Select a batch limit (5, 10, 25, 50, or All) and click "Generate Missing" to auto-generate titles for untitled tasks using a lightweight model call.

**Trace Oversight** -- Select a batch limit and click "Generate Missing" to generate oversight summaries for tasks that lack them.

### Codex Configuration

**Codex configuration:**
- API Key (`OPENAI_API_KEY`) -- optional when host `~/.codex/auth.json` is available
- Base URL (`OPENAI_BASE_URL`) -- optional custom OpenAI-compatible endpoint
- Default Model (`CODEX_DEFAULT_MODEL`) -- model for Codex tasks
- Title Model (`CODEX_TITLE_MODEL`) -- falls back to the Codex default model
- **Test** button -- runs a Codex connectivity check

### Cursor Configuration

**Cursor configuration:**
- API Key (`CURSOR_API_KEY`) -- headless credential for `cursor-agent`; create one in Cursor under Settings > API Keys, or run `cursor-agent login` interactively
- **Test** button -- runs a Cursor connectivity check

### OpenCode Configuration

**OpenCode configuration:**
- Provider auth is managed by the `opencode` CLI itself. Run `opencode auth login` and pick a provider; no API key goes in `~/.wallfacer/.env`.
- **Test** button -- runs an OpenCode connectivity check

### Pi Configuration

**Pi configuration:** Pi is the `pi` coding agent from earendil-works (Armin Ronacher's Pi), **not** Inflection's Pi chatbot.
- No dedicated key: `pi` reads provider credentials (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, etc.) from the same environment the Claude and Codex blocks configure.
- Model selection uses the two-flag form `--provider <name> --model <id>`. Pass a `provider/model` task model (e.g. `anthropic/claude-sonnet-4-6`); Wallfacer splits it on the first `/`. A bare model with no `/` is sent as `--model` alone, letting `pi` pick its default provider.
- **Test** button -- runs a Pi connectivity check

### Agent Routing

**Global Agent Routing** -- Select the default agent and override it for individual activities: Implementation, Testing, Title generation, Oversight summary, Commit message, and Idea agent. Each dropdown offers the available agents (claude, codex, cursor, opencode, pi) or "default".

Wallfacer supports five agents, selected per activity. All run as host processes; the difference is which CLI the runner execs:

- **Claude** -- runs the Claude Code CLI (`WALLFACER_AGENT=claude`). Requires either `CLAUDE_CODE_OAUTH_TOKEN` or `ANTHROPIC_API_KEY`.
- **Codex** -- runs the OpenAI Codex CLI (`WALLFACER_AGENT=codex`). Requires `OPENAI_API_KEY` or host `~/.codex/auth.json`.
- **Cursor** -- runs the Cursor CLI (`WALLFACER_AGENT=cursor`). Requires `CURSOR_API_KEY` or an interactive `cursor-agent login`. Headless runs inject `--force` so edits are applied, not just proposed.
- **OpenCode** -- runs the OpenCode CLI via `opencode run` (`WALLFACER_AGENT=opencode`). Provider auth is managed by `opencode auth login`, not by `~/.wallfacer/.env`. Headless runs inject `--dangerously-skip-permissions` so edits are applied without an interactive prompt.
- **Pi** -- runs the `pi` CLI under `-p --mode json` (`WALLFACER_AGENT=pi`). Reads provider keys from the environment. Headless runs force full tool access (Read, Write, Edit, Bash) so edits are applied. Resolved from `$PATH`.

Each task can be assigned a specific agent when created or edited. The task-level selection overrides the global default for that task's implementation run.

Different activities can be routed to different agents. For example, you could run implementation tasks on Claude but use Codex for title generation. Configure this in **Settings > Harness > Global Agent Routing** or via the `WALLFACER_SANDBOX_*` environment variables.

The six configurable activities are:

1. **Implementation** -- the main task execution
2. **Testing** -- the test-verification agent
3. **Title generation** -- automatic task title generation
4. **Oversight summary** -- trace oversight analysis
5. **Commit message** -- commit message generation
6. **Idea agent** -- brainstorm/ideation agent

When an activity-specific override is not set, it falls back to `WALLFACER_DEFAULT_SANDBOX`. When that is also unset, the agent is determined by the task's own field or the server default (Claude).

Prompt refinement is not an agent activity. It happens in the Plan task-mode chat via the `update_task_prompt` tool. See [Refinement & Ideation](refinement-and-ideation.md).

Route specific activities to different agents (`claude` or `codex`):

| Variable | Description |
|---|---|
| `WALLFACER_DEFAULT_SANDBOX` | Default agent for all activities |
| `WALLFACER_SANDBOX_IMPLEMENTATION` | Override for task implementation |
| `WALLFACER_SANDBOX_TESTING` | Override for test verification |
| `WALLFACER_SANDBOX_TITLE` | Override for title generation |
| `WALLFACER_SANDBOX_OVERSIGHT` | Override for oversight generation |
| `WALLFACER_SANDBOX_COMMIT_MESSAGE` | Override for commit message generation |
| `WALLFACER_SANDBOX_IDEA_AGENT` | Override for the ideation agent |

The `WALLFACER_SANDBOX_*` names are retained for backward compatibility; they select the agent, not a container.

### Full Environment Variables Reference

All configuration lives in `~/.wallfacer/.env` (auto-generated on first run). The server re-reads this file before each task run, so changes take effect on the next task without a server restart.

#### Authentication

| Variable | Required | Description |
|---|---|---|
| `CLAUDE_CODE_OAUTH_TOKEN` | one of these two | OAuth token from `claude setup-token` (Claude Pro/Max subscription) |
| `ANTHROPIC_API_KEY` | one of these two | API key from console.anthropic.com (starts with `sk-ant-...`) |
| `ANTHROPIC_AUTH_TOKEN` | no | Bearer token for LLM gateway proxy authentication |
| `ANTHROPIC_BASE_URL` | no | Custom Anthropic-compatible API endpoint; when set, Wallfacer queries `{base_url}/v1/models` to populate the model dropdown |

#### Models

| Variable | Default | Description |
|---|---|---|
| `CLAUDE_DEFAULT_MODEL` | (Claude Code default) | Default model passed to task agents |
| `CLAUDE_TITLE_MODEL` | (falls back to `CLAUDE_DEFAULT_MODEL`) | Model for background title generation |

#### OpenAI Codex

| Variable | Required | Description |
|---|---|---|
| `OPENAI_API_KEY` | no\* | OpenAI API key; not required when valid host auth cache exists at `~/.codex/auth.json` |
| `OPENAI_BASE_URL` | no | Custom OpenAI-compatible base URL |
| `CODEX_DEFAULT_MODEL` | no | Default model for Codex tasks |
| `CODEX_TITLE_MODEL` | no | Title generation model; falls back to `CODEX_DEFAULT_MODEL` |

\* If host auth cache is unavailable, `OPENAI_API_KEY` plus a successful **Test (Codex)** is required.

#### Cursor

| Variable | Required | Description |
|---|---|---|
| `CURSOR_API_KEY` | no\* | Headless credential for `cursor-agent` |

\* Alternatively, sign in once with an interactive `cursor-agent login`; the CLI then reuses that session.

#### OpenCode

OpenCode manages provider credentials itself (`opencode auth login`), so no provider key is read from `~/.wallfacer/.env`.

| Variable | Required | Description |
|---|---|---|
| `OPENCODE_SERVER_PASSWORD` | no | Basic-auth password for `opencode serve` / `opencode run --attach`; reserved for a future warm-start path |

#### Concurrency

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_MAX_PARALLEL` | `1` | Maximum concurrent tasks auto-promoted to In Progress. The host runtime caps this to `1` unless you set an explicit value, because the `claude` and `codex` CLIs share state under `~/.claude` and `~/.codex` and can race when run in parallel. Set `WALLFACER_MAX_PARALLEL=N` to opt into more. |
| `WALLFACER_MAX_TEST_PARALLEL` | `2` | Maximum concurrent test runs. Fixed default constant; it does not inherit from `WALLFACER_MAX_PARALLEL`. |

#### Agent binaries

These variables are optional; the CLI binaries are resolved via `$PATH` by default.

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_HOST_CLAUDE_BINARY` | `exec.LookPath("claude")` | Explicit path to the Claude CLI binary |
| `WALLFACER_HOST_CODEX_BINARY` | `exec.LookPath("codex")` | Explicit path to the Codex CLI binary (optional; codex-typed tasks require it) |
| `WALLFACER_HOST_CURSOR_BINARY` | `exec.LookPath("cursor-agent")` | Explicit path to the Cursor CLI binary (optional; cursor-typed tasks require it) |
| `WALLFACER_HOST_OPENCODE_BINARY` | `exec.LookPath("opencode")` | Explicit path to the OpenCode CLI binary (optional; opencode-typed tasks require it) |
| `WALLFACER_AGENTS_DIR` | `~/.wallfacer/agents` | Directory scanned for user-authored agent descriptors (`*.yaml`). A missing directory is not an error: Wallfacer falls back to the built-in agent catalog. |
| `WALLFACER_FLOWS_DIR` | `~/.wallfacer/flows` | Directory scanned for user-authored flow descriptors (`*.yaml`). Same fallback semantics as `WALLFACER_AGENTS_DIR`. |

> **Tasks run with your user's permissions.** A task agent can read or write any file your account can, not just its worktree. Run Wallfacer only on machines you trust. The **Settings > Harness** tab shows this warning while active.

**Known limitations:**

- `--resume` is a no-op for codex (codex's `exec` subcommand has no stable resume flag).
- Concurrent tasks default to `WALLFACER_MAX_PARALLEL=1` to avoid races on `~/.claude/__store.db` and `~/.codex/` shared state. Override with an explicit value to opt in.
- Windows is not supported natively; run Wallfacer inside WSL2.
- No write containment: an agent can touch any file your user account can.

#### Circuit Breaker

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_CONTAINER_CB_THRESHOLD` | `5` | Consecutive agent-process launch failures before the circuit breaker opens |
| `WALLFACER_CONTAINER_CB_OPEN_SECONDS` | `30` | Seconds the circuit breaker stays open before probing |

#### Automation

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_OVERSIGHT_INTERVAL` | `0` | Minutes between periodic oversight generation while a task runs (0 = only at completion) |
| `WALLFACER_AUTO_PUSH` | `false` | Enable automatic `git push` after task completion |
| `WALLFACER_AUTO_PUSH_THRESHOLD` | `1` | Minimum commits ahead of upstream before auto-push triggers |
| `WALLFACER_PLANNING_WINDOW_DAYS` | `30` | Default window (in days) for the planning-cost analytics display. `0` means all time. Only affects the UI's default period selection; the server always returns the full record set until the UI requests a narrower window via `?days=N`. |
| `WALLFACER_TERMINAL_ENABLED` | `true` | Enable the integrated host terminal panel. The Terminal button in the status bar opens an interactive shell running on the host machine via WebSocket + PTY. Supports multiple concurrent sessions with a tab bar: click "+" to add sessions, click tabs to switch, click x to close. Set to `false` to disable. |

#### Data & Pagination

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_WORKSPACES` | -- | Workspace paths (colon-separated on Unix, semicolon on Windows); alternative to CLI arguments. |
| `WALLFACER_ARCHIVED_TASKS_PER_PAGE` | `20` | Pagination size for archived tasks |
| `WALLFACER_TOMBSTONE_RETENTION_DAYS` | `7` | Days to retain soft-deleted task data before permanent removal |

#### Security

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_SERVER_API_KEY` | -- | Bearer token for server API authentication; when set, all API requests must include `Authorization: Bearer <key>` |

#### Account Sign-In (latere.ai)

A plain `wallfacer run` offers browser sign-in against [auth.latere.ai](https://auth.latere.ai) with no setup. Sign-in is available, not mandatory: the board stays fully usable signed out, and an absent or expired session never redirects you. A sign-in chip appears in the sidebar; once signed in, the account menu (avatar, org switcher, theme, sign out) replaces it.

Under the hood, `wallfacer run` fills the `AUTH_*` config with the secret-less public "wallfacer" client: `AUTH_URL=https://auth.latere.ai`, a loopback `/callback` redirect, and `openid email profile offline_access` scopes. The session-cookie key is generated once and persisted at `~/.wallfacer/cookie-key`. Any `AUTH_*` value you set yourself takes precedence over these defaults.

| Variable | Default | Description |
|---|---|---|
| `AUTH_URL` | `https://auth.latere.ai` | OIDC auth service base URL |
| `AUTH_CLIENT_ID` | `wallfacer` | OAuth client id. The default is the public client |
| `AUTH_CLIENT_SECRET` | -- | Set to use a confidential client. When set, the cookie key derives from the secret and the auto-generated `cookie-key` file is skipped |
| `AUTH_REDIRECT_URL` | `http://localhost:<port>/callback` | OAuth callback URL. A loopback HTTP callback serves the session cookie insecurely (browsers reject `Secure` cookies over plain HTTP); a non-loopback host is assumed to terminate TLS and uses `https` |
| `AUTH_COOKIE_KEY` | auto-generated | Hex-encoded 32-byte key encrypting the session cookie. Auto-created at `~/.wallfacer/cookie-key` for the public client; set explicitly to pin it |
| `AUTH_ISSUER` | derived from `AUTH_URL` | Override the expected JWT issuer (advanced) |
| `AUTH_JWKS_URL` | derived from `AUTH_URL` | Override the JWKS endpoint for token validation (advanced) |

**Precedence.** Every `AUTH_*` value resolves from the shell environment first, then `~/.wallfacer/.env`, then the public-client default. So `AUTH_CLIENT_ID=other wallfacer run` is a clean one-shot override without editing the file.

**Pointing at a different auth service.** Set `AUTH_URL` and a matching `AUTH_CLIENT_ID` to sign in against your own OIDC deployment. For a confidential client, also set `AUTH_CLIENT_SECRET`; the session-cookie key is then derived from the secret instead of the generated file.

**Forced sign-in.** `WALLFACER_CLOUD` does not control whether sign-in is *available* (it always is). It controls whether sign-in is *required*: with `WALLFACER_CLOUD` set, anonymous HTML navigation is redirected to `/login`. Local mode never installs this gate.

**Headless sign-in.** On machines without a browser, use the device-authorization flow: `wallfacer auth login` (see [`wallfacer auth`](#wallfacer-auth) above). Full cloud-mode deployment (`wallfacer web`) is documented in [`docs/cloud/`](../cloud/); start with [`docs/cloud/README.md`](../cloud/README.md).

### System Prompt Templates

Wallfacer ships built-in Go template files that instruct its agent activities. The most commonly edited ones:

| Template | Purpose |
|---|---|
| `title.tmpl` | Task title generation |
| `commit.tmpl` | Commit message generation |
| `test.tmpl` | Test-verification agent |
| `oversight.tmpl` | Oversight summary generation |
| `ideation.tmpl` | Brainstorm/ideation agent |
| `conflict.tmpl` | Merge conflict resolution |
| `instructions.tmpl` | Workspace instructions (AGENTS.md) generation |
| `task_prompt_refine.tmpl` | Plan task-mode prompt refinement (`update_task_prompt`) |

#### Viewing and Editing

Open **Settings > Prompts > System Prompt Templates > Manage** to view the templates. Each shows whether a user override exists. Click a template name to view its content and edit it.

Overrides are validated as Go templates before saving. If the template is invalid, the save is rejected with a parse error message.

#### Override Storage

User overrides are stored as `.tmpl` files in `~/.wallfacer/prompts/`. For example, overriding the title template creates `~/.wallfacer/prompts/title.tmpl`.

#### Restoring Defaults

Delete a user override via the template editor or the API (`DELETE /api/system-prompts/{name}`) to restore the embedded default.

### Workspace Instructions (AGENTS.md)

#### What AGENTS.md Is

Each unique set of workspaces gets its own `AGENTS.md` file stored in `~/.wallfacer/instructions/`. Wallfacer supplies its path to the agent through the `WALLFACER_INSTRUCTIONS_PATH` environment variable: Claude appends the file as a system prompt, and Codex inlines its contents. The agent picks it up automatically as context.

#### How Fingerprinting Works

The instructions file is identified by a SHA-256 hash of the sorted, absolute workspace paths. This means switching to workspaces `~/a` and `~/b` (in any order) shares the same instructions file, while `~/a`, `~/b`, and `~/c` together gets a separate one.

#### Initial Generation

On first run with a new workspace set, Wallfacer creates the `AGENTS.md` from:

1. A built-in default template with general agent guidance.
2. A reference list of per-repository `AGENTS.md` (or legacy `CLAUDE.md`) file paths so agents can read them on demand.

#### Editing

Open **Settings > Prompts > AGENTS.md > Edit** to modify the instructions in the web UI. Changes are saved to the fingerprinted file in `~/.wallfacer/instructions/`.

#### Re-Initializing

Click **Re-init** (or call `POST /api/instructions/reinit`) to regenerate the instructions from the default template and current repository files, discarding any manual edits.

### Prompt Templates

Prompt templates are reusable named prompt fragments for common task patterns.

#### Creating a Template

Open **Settings > Prompts > Prompt Templates > Manage**. Enter a name and body, then save. Templates are stored in `~/.wallfacer/templates.json`.

#### Using Templates

When creating a new task, select a template from the template picker to insert its body into the prompt field. You can then edit the inserted text before submitting.

#### Managing Templates

From the template manager, you can view all templates sorted by creation date and delete templates you no longer need.

### CLI Reference

#### wallfacer run

Start the task board server and open the web UI.

```
wallfacer run [flags] [workspace...]
```

**Positional arguments:**
- `workspace` -- directories to expose to tasks (default: current directory)

**Flags:**

| Flag | Env Var | Default | Description |
|---|---|---|---|
| `-addr` | `ADDR` | `:8080` | Listen address |
| `-data` | `DATA_DIR` | `~/.wallfacer/data` | Task data directory |
| `-env-file` | `ENV_FILE` | `~/.wallfacer/.env` | Env file with credentials and runtime settings |
| `-no-browser` | -- | `false` | Skip auto-opening the browser |
| `-log-format` | `LOG_FORMAT` | `text` | Log output format: `text` or `json` |

#### wallfacer status

Print the current board state to the terminal.

```
wallfacer status [flags]
```

| Flag | Default | Description |
|---|---|---|
| `-addr` | `http://localhost:8080` | Server address (or `ADDR` env var) |
| `-watch` | `false` | Re-render every 2 seconds until Ctrl-C |
| `-json` | `false` | Emit raw JSON from `/api/tasks` for scripting |

**Examples:**

```bash
wallfacer status                  # Snapshot of current state
wallfacer status -watch           # Live-updating view
wallfacer status -json            # Machine-readable JSON output
wallfacer status -addr :9090      # Connect to a different server
```

#### wallfacer doctor

Check prerequisites and configuration. Displays config paths, then verifies credentials, the `claude` / `codex` binaries on `$PATH`, and Git. Reports the resolved binary paths and `--version` output for each CLI.

```
wallfacer doctor
```

`wallfacer env` is an alias for `wallfacer doctor`; both run the same check.

Output uses `[ok]` for passing checks, `[!]` for issues that need fixing, and `[ ]` for optional items that are not configured. Credential values are masked.

#### wallfacer auth

Sign the CLI (and the local-mode web UI) in to `auth.latere.ai` using the RFC 8628 device-authorization flow. The token is stored at `<UserConfigDir>/latere/token.json`, shared with the `latere` CLI, so a single login carries over between both tools.

```
wallfacer auth login    # Sign in (opens a browser for confirmation)
wallfacer auth logout   # Remove the locally stored token
wallfacer auth whoami   # Print the saved token's expiry
```

**Flags (login):**

| Flag | Default | Description |
|---|---|---|
| `-auth-url` | `https://auth.latere.ai` (or `AUTH_URL`) | Auth service base URL |
| `-client-id` | `wallfacer-cli` (or `AUTH_CLIENT_ID`) | OAuth client id |
| `-scopes` | `openid email profile offline_access` | Space-separated scope list |
| `-org` | `""` | Scope the login to this `org_id` (empty string = personal context) |
| `-personal` | `false` | Force personal context (equivalent to `-org=""`) |
| `-no-browser` | `false` | Do not open the verification URL automatically |

#### wallfacer web

Start the OIDC-authenticated web frontend server (the cloud-mode entry point). Serves the SPA plus `/login`, `/callback`, `/logout`, `/api/me`, and `/healthz` / `/readyz` probes.

```
wallfacer web [flags]
```

| Flag | Env Var | Default | Description |
|---|---|---|---|
| `-addr` | `WALLFACERD_ADDR` | `:8080` | Listen address |

Auth is configured through `AUTH_URL`, `AUTH_CLIENT_ID`, `AUTH_CLIENT_SECRET`, `AUTH_REDIRECT_URL`, and `AUTH_COOKIE_KEY`. OIDC is disabled when `AUTH_CLIENT_ID` is unset.

#### wallfacer spec

Spec tooling for the Plan workflow.

```
wallfacer spec new [flags] <path>      # Scaffold a spec file with valid frontmatter
wallfacer spec validate [flags] [paths...]   # Validate the specs/ tree
```

**`spec new` flags:** `-title`, `-status` (default `vague`), `-effort` (default `medium`), `-author`, `-force`. The path must be under a track directory, e.g. `specs/local/my-feature.md`.

**`spec validate` flags:** `-specs-dir` (default `specs`), `-json`, `-warnings` (default `true`). With no positional paths it walks the whole `specs/` tree; cross-spec checks (cycle detection, unique dispatch) always run across the full graph. Exit code is `1` when any error is reported.

### Security

#### Server API Key Authentication

Set `WALLFACER_SERVER_API_KEY` to require bearer-token authentication on all API requests. When configured, every request must include the header:

```
Authorization: Bearer <your-api-key>
```

SSE (Server-Sent Events) endpoints accept the token as a `?token=` query parameter instead, since EventSource does not support custom headers.

The root page (`GET /`) is exempt from authentication to allow loading the UI, which then uses the token for subsequent API calls.

#### CSRF Protection

Wallfacer validates the `Origin` or `Referer` header on all state-changing requests (POST, PUT, PATCH, DELETE). The header host must match the server's listen address. Requests without either header (e.g., from non-browser clients like `curl`) are allowed through.

#### SSRF Hardening

Custom API base URLs (`ANTHROPIC_BASE_URL`, `OPENAI_BASE_URL`) are validated before being persisted:

- Only HTTPS URLs are accepted
- Bare IP addresses are rejected
- Single-label hostnames (e.g., `localhost`) are rejected
- Hostnames that resolve to private, loopback, or link-local IP addresses are rejected

#### Request Body Size Limits

Request bodies are limited to prevent abuse:

| Endpoint Category | Limit |
|---|---|
| Default | 1 MiB |
| Instructions (AGENTS.md) | 5 MiB |
| Feedback | 512 KiB |

### Trash Management

View soft-deleted tasks that are within the retention window (default: 7 days, controlled by `WALLFACER_TOMBSTONE_RETENTION_DAYS`). Each entry shows the task prompt and deletion timestamp. You can:

- **Restore** a task to return it to its pre-deletion state
- Wait for the retention window to expire for automatic permanent removal

Access via **Settings > Trash**.

### About

Displays version information, the project link (github.com/changkun/wallfacer), and license details.

Access via **Settings > About**.

### Workspace Settings

**Active Workspaces** -- Lists the directories currently exposed to tasks as git worktrees. Click **Change** to open the workspace picker and select different directories.

**Saved Workspace Groups** -- Previously used workspace combinations are saved automatically. Switch back to any saved group without rebuilding the folder set. Each group can carry its own `WALLFACER_MAX_PARALLEL` and `WALLFACER_MAX_TEST_PARALLEL` overrides.

Access via **Settings > Workspace**.

---

## See Also

- [Getting Started](getting-started.md) -- initial setup and first task
- [Usage Guide](usage.md) -- task creation, feedback, autopilot, and results
- [Circuit Breakers](circuit-breakers.md) -- agent launch failure protection
- [Refinement & Ideation](refinement-and-ideation.md) -- prompt refinement and brainstorm agents
- [Architecture](../internals/architecture.md) -- system design for contributors
