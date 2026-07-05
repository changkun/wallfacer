# Configuration

Wallfacer is configured through the Settings page, environment variables in `~/.wallfacer/.env`, CLI flags, and file-based overrides for system prompt templates. The `.env` file is created with a commented template on first run; edit it directly or through the Settings UI. The server re-reads it before each task run, so most changes take effect on the next task without a restart.

## Settings page

Open Settings from the sidebar gear icon or press `Cmd+,` (or `Ctrl+,`). The page has four tabs.

### Execution tab

- **Automation toggles**: the same five toggles as the board header menu (autoimplement, autotest, autosubmit, autosync, autopush). See [Automation](automation.md).
- **Parallel tasks**: maximum concurrent running tasks (1 to 20). Maps to `WALLFACER_MAX_PARALLEL`.
- **Archived tasks**: page size for the archived-task list (1 to 200). Maps to `WALLFACER_ARCHIVED_TASKS_PER_PAGE`.
- **Oversight interval**: minutes between periodic oversight summaries while a task runs (0 to 120; 0 generates summaries only at completion). Maps to `WALLFACER_OVERSIGHT_INTERVAL`.
- **Auto push**: enable automatic `git push` after commits, with a threshold field (push only when the workspace is at least N commits ahead of upstream). Maps to `WALLFACER_AUTO_PUSH` and `WALLFACER_AUTO_PUSH_THRESHOLD`.
- **Task titles** and **Trace oversight**: batch backfill buttons that generate missing titles or oversight summaries for existing tasks, with a selectable batch limit.

### Harness tab

Credentials, models, and routing for the coding CLIs. A warning banner appears on first launch while no credential is configured. Each harness has its own block with a **Test** connectivity button:

- **Claude**: OAuth token (`CLAUDE_CODE_OAUTH_TOKEN`), API key (`ANTHROPIC_API_KEY`), base URL, default and title models, plus a **Sign in with Claude** OAuth button.
- **Codex**: OpenAI API key, base URL, default and title models, plus a **Sign in with OpenAI** button.
- **Cursor**: `CURSOR_API_KEY`, or run `cursor-agent login` once outside Wallfacer.
- **OpenCode**: no key field; the `opencode` CLI manages provider credentials itself (`opencode auth login`).
- **Pi**: no key field; the `pi` CLI reads provider keys (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) from the same environment.

The **Global Harness Routing** section selects the default harness (`WALLFACER_DEFAULT_SANDBOX`). Per-activity overrides route implementation, testing, title, oversight, and commit message generation to different harnesses via the `WALLFACER_SANDBOX_*` variables below. Task-level harness selection (set when creating or editing a card) wins over all of these. Save and Revert buttons apply or discard pending edits; saved values are written to `~/.wallfacer/.env`.

Tasks run as host processes with the account's full permissions; the tab shows this warning while active. Run Wallfacer only on trusted machines.

### GitHub tab

Shows the GitHub connection state. Wallfacer does not run its own GitHub OAuth flow; the connection is borrowed from the signed-in latere.ai account:

- Signed out: an explanation and a **Sign in via latere.ai** button.
- Signed in, not connected: a **Connect GitHub at latere.ai** link that opens the install flow on the account page.
- Connected: the connected login and a **Manage connections at latere.ai** link.

Once connected, tasks can open pull requests and post PR comments from the task detail panel. See [Board](board.md).

### About tab

Version information, the project link, license, and a read-only system status block (goroutines, memory, active agents, circuit breaker state, task counts).

## System prompt templates

Built-in Go templates instruct each agent activity. Nine templates exist:

| Template | Purpose |
|---|---|
| `title.tmpl` | Task title generation |
| `commit.tmpl` | Commit message generation |
| `test.tmpl` | Test verification agent |
| `oversight.tmpl` | Oversight summary generation |
| `conflict.tmpl` | Merge conflict resolution |
| `task_prompt_refine.tmpl` | Task prompt refinement in Plan mode |
| `spec.tmpl` | Spec authoring |
| `spec_system_empty.tmpl` | Plan session system prompt (empty spec tree) |
| `spec_system_nonempty.tmpl` | Plan session system prompt (existing spec tree) |

Overrides live as files under `~/.wallfacer/prompts/`: creating `~/.wallfacer/prompts/title.tmpl` overrides the title template. The templates manager (`/api/system-prompts`) lists each template, shows whether an override exists, and validates edits as Go templates before saving; invalid templates are rejected with a parse error. Delete an override file (or use the delete endpoint) to restore the embedded default.

## CLI reference

### wallfacer run

Start the local task board server and open the web UI.

```
wallfacer run [flags]
```

| Flag | Env var | Default | Description |
|---|---|---|---|
| `-addr` | `ADDR` | `:8080` | Listen address |
| `-data` | `DATA_DIR` | `~/.wallfacer/data` | Task data directory |
| `-env-file` | `ENV_FILE` | `~/.wallfacer/.env` | Env file with credentials and runtime settings |
| `-no-browser` | | `false` | Skip auto-opening the browser |
| `-log-format` | `LOG_FORMAT` | `text` | Log output format: `text` or `json` |

Startup requires the `claude` binary on `PATH` (or `WALLFACER_HOST_CLAUDE_BINARY`); the server exits with an install hint otherwise.

### wallfacer status

Print the current board state to the terminal.

```
wallfacer status [flags]
```

| Flag | Default | Description |
|---|---|---|
| `-addr` | `http://localhost:8080` | Server address |
| `-watch` | `false` | Re-render every 2 seconds until Ctrl-C |
| `-json` | `false` | Emit raw JSON from `/api/tasks` for scripting |

### wallfacer spec

Spec tooling for the [Plan](plan.md) workflow.

```
wallfacer spec new [flags] <path>
wallfacer spec validate [flags] [paths...]
```

`spec new` scaffolds a spec file with valid frontmatter. The path must be under a track directory, for example `specs/local/my-feature.md`. Flags: `-title` (default: title-cased filename), `-status` (default `vague`), `-effort` (default `medium`), `-author` (default: git `user.name`), `-force`.

`spec validate` checks the spec tree against the document model rules. Flags: `-specs-dir` (default `specs`), `-json`, `-warnings` (default `true`). Cross-spec checks (cycle detection, unique dispatch) always run across the full graph; positional paths only filter the output. Exit codes: `0` clean, `1` on validation errors, `2` on usage or tree-build failure.

### wallfacer doctor

Check prerequisites and configuration: config paths, the `.env` file, the Claude credential (required), optional Codex and Cursor credentials, harness binaries with their versions, and git. `wallfacer env` is an alias.

```
wallfacer doctor
```

Output marks passing checks `[ok]`, issues `[!]`, and unconfigured optional items `[ ]`. Credential values are masked.

### wallfacer auth

Sign the CLI (and the local web UI) in to auth.latere.ai using the device-authorization flow. The token is stored at `<UserConfigDir>/latere/token.json`, shared with the `latere` CLI.

```
wallfacer auth login    # Sign in (opens a browser for confirmation)
wallfacer auth logout   # Remove the locally stored token
wallfacer auth whoami   # Print the saved token's expiry
```

Login flags: `-auth-url` (default `https://auth.latere.ai`), `-client-id` (default `wallfacer-cli`), `-scopes`, `-org` (scope the login to an organization), `-personal` (force personal context), `-no-browser` (print the verification URL instead of opening it).

### wallfacer web

Start the cloud-mode server (`wallfacerd`): OIDC-authenticated SPA, coordination WebSocket acceptor, and spec comment store (Postgres via `WALLFACER_DATABASE_URL`, falling back to memory).

```
wallfacer web [flags]
```

| Flag | Env var | Default | Description |
|---|---|---|---|
| `-addr` | `WALLFACERD_ADDR` | `:8080` | Listen address |

Cloud deployment is documented in [Auth & Identity](../internals/auth-and-identity.md).

## Environment variables

All variables live in `~/.wallfacer/.env` unless set in the shell environment, which takes precedence.

### Credentials and models

| Variable | Description |
|---|---|
| `CLAUDE_CODE_OAUTH_TOKEN` | Claude OAuth token from `claude setup-token`; takes precedence over the API key |
| `ANTHROPIC_API_KEY` | Anthropic API key; one Claude credential is required for tasks |
| `ANTHROPIC_AUTH_TOKEN` | Bearer token for a gateway proxy (read-only, managed externally) |
| `ANTHROPIC_BASE_URL` | Custom Anthropic-compatible endpoint |
| `CLAUDE_DEFAULT_MODEL` | Default model for Claude tasks |
| `CLAUDE_TITLE_MODEL` | Model for title generation; falls back to the default model |
| `CLAUDE_CODE_MODEL` | Model passed through to the Claude CLI for a specific run |
| `OPENAI_API_KEY` | OpenAI key for Codex; optional when `~/.codex/auth.json` exists |
| `OPENAI_BASE_URL` | Custom OpenAI-compatible endpoint |
| `CODEX_DEFAULT_MODEL` | Default model for Codex tasks |
| `CODEX_TITLE_MODEL` | Codex title model; falls back to `CODEX_DEFAULT_MODEL` |
| `CODEX_ARGS` | Extra arguments appended to Codex invocations |
| `CURSOR_API_KEY` | Headless credential for `cursor-agent` |
| `OPENCODE_SERVER_PASSWORD` | Reserved for a future OpenCode server-attach path |

### Runtime knobs

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_MAX_PARALLEL` | `1` | Concurrent running tasks. Defaults to 1 because the harness CLIs share state under `~/.claude` and `~/.codex`; set explicitly to opt into more |
| `WALLFACER_MAX_TEST_PARALLEL` | `2` | Concurrent test verification runs |
| `WALLFACER_MAX_AGENTS` | unlimited | Global budget on concurrent agent processes |
| `WALLFACER_AGENT_NICE` | | Niceness applied to agent processes; negative disables |
| `WALLFACER_OVERSIGHT_INTERVAL` | `0` | Minutes between periodic oversight generation (0 = only at completion) |
| `WALLFACER_ARCHIVED_TASKS_PER_PAGE` | `20` | Pagination size for archived tasks |
| `WALLFACER_AUTO_PUSH` | `false` | Automatic `git push` after commits |
| `WALLFACER_AUTO_PUSH_THRESHOLD` | `1` | Minimum commits ahead of upstream before auto-push fires |
| `WALLFACER_AGON_FORKS` | `2` | Independent critic forks per Agon verification run |
| `WALLFACER_AGON_ROUNDS` | `4` | Per-fork debate round cap |
| `WALLFACER_AGON_COST_CAP` | `50000` | Soft token budget per Agon run |
| `WALLFACER_AGENT_SESSION_WINDOW_DAYS` | `30` | Default window for session cost analytics; 0 = all time. `WALLFACER_PLANNING_WINDOW_DAYS` is a deprecated alias |
| `WALLFACER_DEFAULT_SANDBOX` | `claude` | Default harness for all activities |
| `WALLFACER_SANDBOX_IMPLEMENTATION` | | Harness override for implementation |
| `WALLFACER_SANDBOX_TESTING` | | Harness override for test verification |
| `WALLFACER_SANDBOX_TITLE` | | Harness override for title generation |
| `WALLFACER_SANDBOX_OVERSIGHT` | | Harness override for oversight |
| `WALLFACER_SANDBOX_COMMIT_MESSAGE` | | Harness override for commit messages |
| `WALLFACER_HOST_CLAUDE_BINARY` | `$PATH` lookup | Explicit path to the `claude` binary; likewise `_CODEX_`, `_CURSOR_`, `_OPENCODE_`, `_PI_` variants |
| `WALLFACER_TERMINAL_ENABLED` | `true` | Integrated host terminal panel; set `false` to disable |
| `WALLFACER_WORKSPACES` | | Active workspace folders (colon-separated on Unix, semicolon on Windows) |
| `WALLFACER_CLOUD` | `false` | Forces sign-in for HTML navigation; sign-in stays available either way |

### Operational

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_SERVER_API_KEY` | | Require `Authorization: Bearer <key>` on API requests; bypassed when a signed-in identity is present. SSE endpoints accept `?token=` |
| `WALLFACER_DRIFT_TESTER` | off | Experimental spec drift pipeline: on task completion, an assessment agent classifies the linked spec as complete or stale instead of completing it directly |
| `WALLFACER_TOMBSTONE_RETENTION_DAYS` | `7` | Days soft-deleted tasks remain restorable from the Trash |
| `WALLFACER_MAX_TURN_OUTPUT_BYTES` | `8388608` | Per-turn output budget; longer output is truncated (0 = unlimited) |
| `WALLFACER_CONTAINER_CB_THRESHOLD` | `5` | Consecutive agent launch failures before the circuit breaker opens |
| `WALLFACER_CONTAINER_CB_OPEN_SECONDS` | `30` | Seconds the circuit breaker stays open before probing |
| `WALLFACER_WORKTREE_GC_INTERVAL` | `24h` | Interval between worktree garbage collection runs (duration syntax, e.g. `6h`) |
| `WALLFACER_FLOWS_DIR` | `~/.wallfacer/flows` | Directory scanned for user flow descriptors; the loader is partially wired, so treat as experimental |
| `WALLFACER_AGENTS_DIR` | `~/.wallfacer/agents` | Directory scanned for user agent descriptors; same caveat |
| `WALLFACER_PROMPT_HISTORY_LIMIT` | | Cap on retained prompt revisions per task |
| `WALLFACER_RETRY_HISTORY_LIMIT` | | Cap on retained retry records per task |
| `WALLFACER_REFINE_SESSIONS_LIMIT` | | Cap on retained refine sessions per task |
| `WALLFACER_COORDINATION` | on | Set `0` to disable the coordination connector when signed in |
| `WALLFACER_COORDINATION_URL` | derived | Override the coordination endpoint for staging or self-hosted deployments |
| `WALLFACER_DATABASE_URL` | | Postgres DSN for cloud-mode spec comment storage |

### Sign-in and cloud (OIDC)

A plain `wallfacer run` fills these with the public secret-less client against `https://auth.latere.ai`; explicit values take precedence. `AUTH_URL`, `AUTH_CLIENT_ID`, `AUTH_CLIENT_SECRET`, `AUTH_REDIRECT_URL`, `AUTH_COOKIE_KEY` (auto-generated at `~/.wallfacer/cookie-key` for the public client), `AUTH_ISSUER`, and `AUTH_JWKS_URL`. `WALLFACERD_ADDR` sets the cloud server address. Cloud deployment details are in [Auth & Identity](../internals/auth-and-identity.md).

### Flags as environment variables

`LOG_FORMAT`, `ADDR`, `DATA_DIR`, and `ENV_FILE` mirror the `wallfacer run` flags of the same names.

## Files and locations

| Path | Contents |
|---|---|
| `~/.wallfacer/.env` | Credentials and runtime settings |
| `~/.wallfacer/data/` | Task board state and events |
| `~/.wallfacer/workspaces.json` | Workspace definitions |
| `~/.wallfacer/worktrees/` | Per-task git worktrees |
| `~/.wallfacer/prompts/` | System prompt template overrides |
| `~/.wallfacer/agent-sessions/` | Chat and Plan session history |
| `~/.wallfacer/github/` | GitHub connection cache |
| `~/.wallfacer/cookie-key` | Session cookie encryption key |
| `~/.wallfacer/tmp/` | Scratch space |
| `<UserConfigDir>/latere/token.json` | latere.ai sign-in token, shared with the `latere` CLI |

## Keyboard shortcuts

Press `?` anywhere to open this reference in the app. Shortcuts without modifiers are suppressed while focus is in a text input or a modal is open.

### Global

| Key | Action |
|---|---|
| `Cmd+K` / `Ctrl+K` | Command palette (tasks, specs, docs) |
| `Cmd+,` / `Ctrl+,` | Open Settings |
| `` Ctrl+` `` | Toggle terminal panel |
| `/` | Focus search |
| `?` | Show keyboard shortcuts |
| `n` | New task |
| `e` | Toggle file explorer |
| `p` | Switch to Plan mode |
| `Escape` | Close modal / cancel |

### Card navigation (Board)

| Key | Action |
|---|---|
| `Enter` / `Space` | Open the focused task |
| Arrow keys | Move focus between cards |
| `s` | Start a backlog task |
| `d` | Mark a waiting task done |
| `r` | Resume a waiting task with a session, otherwise retry to backlog |
| `t` | Run test verification |
| `p` | Open the task in Plan mode |
| `Escape` | Blur the card |

### Forms

| Key | Action |
|---|---|
| `Ctrl+Enter` / `Cmd+Enter` | Save |
| `Escape` | Cancel |

## See also

- [Getting Started](getting-started.md): installation and first run
- [Concepts](concepts.md): the mental model and primitives
- [Automation](automation.md): toggles, retries, and guard rails
- [Agent Graph](agent-graph.md): custom agents, flows, and harness pinning
- [Workspaces](workspaces.md): folder sets and per-workspace settings
- [Architecture](../internals/architecture.md): internals for contributors
