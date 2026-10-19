# Configuration

Wallfacer is configured through the Settings page, environment variables in `~/.wallfacer/.env`, and CLI flags. The `.env` file is created with a commented template on first run; edit it directly or through the Settings UI. The server re-reads it before each task run, so most changes take effect on the next task without a restart.

## Settings page

Open Settings from the bottom of the sidebar. The page has four tabs.

### Execution tab

- **Parallel tasks**: maximum concurrent running tasks (1 to 20). Maps to `WALLFACER_MAX_PARALLEL`.
- **Archived tasks**: page size for the archived-task list (1 to 200). Maps to `WALLFACER_ARCHIVED_TASKS_PER_PAGE`.
- **Oversight interval**: minutes between periodic oversight summaries while a task runs (0 to 120; 0 generates summaries only at completion). Maps to `WALLFACER_OVERSIGHT_INTERVAL`.
- **Trace oversight**: a batch backfill button that generates missing oversight summaries for existing tasks, with a selectable batch limit.

### Appearance tab

- **Mode**: Light, Dark, or Auto (follow the operating system). Stored at `wallfacer-theme` in browser storage.
- **Color theme**: named palettes applied to the whole workspace in both light and dark mode. `Clay` (the clay accent on a neutral canvas) is the default; `Paper` is the original cream canvas, and `Indigo`, `Amber`, `Rose`, and `Copper` are alternatives. Stored at `wallfacer-palette`; the choice is per browser.

### Harness tab

Credentials, models, and routing for the coding CLIs. A warning banner appears on first launch while no credential is configured. Each harness has its own block with a **Test** connectivity button:

- **Claude**: OAuth token (`CLAUDE_CODE_OAUTH_TOKEN`), API key (`ANTHROPIC_API_KEY`), base URL, and default model. With no token or key set, Claude tasks use the sign-in of the `claude` CLI on the machine.
- **Codex**: OpenAI API key, base URL, and default model. With no key set, Codex tasks use the sign-in of the `codex` CLI (`~/.codex/auth.json`).
- **Cursor**: `CURSOR_API_KEY`, or run `cursor-agent login` once outside Wallfacer.
- **OpenCode**: no key field; the `opencode` CLI manages provider credentials itself (`opencode auth login`).
- **Pi**: no key field; the `pi` CLI reads provider keys (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) from the same environment.

The **Global Harness Routing** section selects the default harness (`WALLFACER_DEFAULT_SANDBOX`). Per-activity overrides route implementation, testing, title, oversight, and commit message generation to different harnesses via the `WALLFACER_SANDBOX_*` variables below. Task-level harness selection (set when creating or editing a card) wins over all of these. Save and Revert buttons apply or discard pending edits; saved values are written to `~/.wallfacer/.env`.

Tasks run as host processes with the account's full permissions; the tab shows this warning while active. Run Wallfacer only on trusted machines.

#### Organization connectors in Claude

Wallfacer runs the installed Claude CLI with its normal home and configuration.
For claude.ai organization connectors, open `claude` in a terminal, use `/login`
to sign in with the subscription account, and confirm the connections in `/mcp`.
Remove `CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_API_KEY`, and `ANTHROPIC_AUTH_TOKEN`
from the Wallfacer environment when choosing that login method. API keys and
tokens from `claude setup-token` do not grant connector discovery. Organization
tool restrictions still apply. See [Claude's connector authentication guide](https://code.claude.com/docs/en/mcp#use-mcp-servers-from-claudeai).

#### Ollama and OpenRouter

Select **OpenCode** as the task harness to use providers configured in the local
OpenCode CLI. For local models, configure the Ollama provider in OpenCode and
verify it there first; [Ollama's OpenCode setup](https://docs.ollama.com/integrations/opencode)
describes the connection. For OpenRouter, run `opencode auth login`, choose
OpenRouter, and select a provider model using
[OpenRouter's OpenCode setup](https://openrouter.ai/docs/cookbook/coding-agents/opencode-integration).
Use the configured default or enter its provider/model identifier on the task.
Wallfacer passes the model through to OpenCode.

### About tab

Version information, the project link, license, and a read-only system status block (goroutines, memory, active agents, task counts).

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

The command takes flags only. A folder named on the command line is refused with exit status 2: the board reopens the most recently used workspace, and workspaces are created and switched in the workspace picker.

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
| `CLAUDE_CODE_MODEL` | Model passed through to the Claude CLI for a specific run |
| `OPENAI_API_KEY` | OpenAI key for Codex; optional when `~/.codex/auth.json` exists |
| `OPENAI_BASE_URL` | Custom OpenAI-compatible endpoint |
| `CODEX_DEFAULT_MODEL` | Default model for Codex tasks |
| `CURSOR_API_KEY` | Headless credential for `cursor-agent` |
| `OPENCODE_SERVER_PASSWORD` | Reserved for a future OpenCode server-attach path |

### Runtime knobs

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_MAX_PARALLEL` | `1` | Concurrent running tasks. Defaults to 1 because the harness CLIs share state under `~/.claude` and `~/.codex`; set explicitly to opt into more |
| `WALLFACER_MAX_AGENTS` | unlimited | Global budget on concurrent agent processes |
| `WALLFACER_AGENT_NICE` | | Niceness applied to agent processes; negative disables |
| `WALLFACER_OVERSIGHT_INTERVAL` | `0` | Minutes between periodic oversight generation (0 = only at completion) |
| `WALLFACER_ARCHIVED_TASKS_PER_PAGE` | `20` | Pagination size for archived tasks |
| `WALLFACER_DEFAULT_SANDBOX` | `claude` | Default harness for all activities |
| `WALLFACER_SANDBOX_IMPLEMENTATION` | | Harness override for implementation |
| `WALLFACER_SANDBOX_OVERSIGHT` | | Harness override for oversight |
| `WALLFACER_SANDBOX_COMMIT_MESSAGE` | | Harness override for commit messages |
| `WALLFACER_HOST_CLAUDE_BINARY` | `$PATH` lookup | Explicit path to the `claude` binary; likewise `_CODEX_`, `_CURSOR_`, `_OPENCODE_`, `_PI_` variants |

### Operational

| Variable | Default | Description |
|---|---|---|
| `WALLFACER_SERVER_API_KEY` | generated at `~/.wallfacer/server-api-key` | Require `Authorization: Bearer <key>` on API requests. SSE endpoints accept `?token=`. A local run without a configured key generates one on first start and logs the file path; `wallfacer status` reads it from there. The browser receives the key through the page only over a loopback connection, so a client on another host opens `http://<host>:<port>/?token=<key>` once |
| `WALLFACER_DRIFT_TESTER` | off | Experimental spec drift pipeline: on task completion, an assessment agent classifies the linked spec as complete or stale instead of completing it directly |
| `WALLFACER_TOMBSTONE_RETENTION_DAYS` | `7` | Days soft-deleted tasks remain restorable from the Trash |
| `WALLFACER_MAX_TURN_OUTPUT_BYTES` | `8388608` | Per-turn output budget; longer output is truncated (0 = unlimited) |
| `WALLFACER_WORKTREE_GC_INTERVAL` | `24h` | Interval between worktree garbage collection runs (duration syntax, e.g. `6h`) |
| `WALLFACER_PROMPT_HISTORY_LIMIT` | | Cap on retained prompt revisions per task |
| `WALLFACER_RETRY_HISTORY_LIMIT` | | Cap on retained retry records per task |
| `WALLFACER_REFINE_SESSIONS_LIMIT` | | Cap on retained refine sessions per task |

### Flags as environment variables

`LOG_FORMAT`, `ADDR`, `DATA_DIR`, and `ENV_FILE` mirror the `wallfacer run` flags of the same names.

## Files and locations

| Path | Contents |
|---|---|
| `~/.wallfacer/.env` | Credentials and runtime settings |
| `~/.wallfacer/data/` | Task board state and events |
| `~/.wallfacer/workspaces.json` | Workspace definitions |
| `~/.wallfacer/worktrees/` | Per-task git worktrees |
| `~/.wallfacer/agent-sessions/` | Chat and Plan session history |
| `~/.wallfacer/tmp/` | Scratch space |

## See also

- [Getting Started](getting-started.md): installation and first run
- [Concepts](concepts.md): the mental model and primitives
- [Workspaces](workspaces.md): the workspace folder and per-workspace settings
- [Architecture](../internals/architecture.md): internals for contributors

### System keyring storage

In **Settings → Harnesses → Credential storage**, choose **System keyring** and
save. Wallfacer moves saved provider tokens into the operating system keyring
and leaves only `WALLFACER_SECRET_STORE=keyring` and a
`WALLFACER_SECRET_BUNDLE` reference in `.env`. Ordinary settings remain in the
file. This covers Claude, Anthropic gateway, OpenAI, Cursor, and OpenCode
credentials saved by Wallfacer. Native CLI sign-ins and the server key have
their own storage.

Unlock the keyring before saving or starting agents. On Linux, a running Secret
Service and session bus are required. If a credential cannot be read, Wallfacer
reports an error and prevents agent launch. It does not fall back to plaintext.
Settings connection tests use temporary credentials without changing saved ones.

For headless deployments, configuration-file storage remains the default. To
move credentials back into the private `.env` file, explicitly select
**Configuration file** and save while the keyring is available. Moving a
keyring-backed `.env` file to another machine also requires transferring or
recreating its credential bundle; the reference alone is insufficient.
