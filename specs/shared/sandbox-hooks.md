# Sandbox Hooks

## Status

Not started

## Track

Shared

## Problem

Wallfacer launches AI agents (Claude Code, Codex) in sandbox containers but has no mechanism to intercept, modify, or observe agent behavior at the tool-call level. All control is limited to CLI flags (`--dangerously-skip-permissions`, `--append-system-prompt`), environment variables, and the mounted AGENTS.md file.

Both Claude Code and Codex now expose hook systems — lifecycle event callbacks that fire before/after tool execution, on session start/stop, on prompt submission, etc. These hooks can block dangerous operations, compress tool output, inject context, auto-approve permissions, and report telemetry — all without modifying the agent's source code.

Wallfacer should design a hook framework that:
1. Works across sandbox types (Claude Code and Codex today, others in the future).
2. Gives the wallfacer server observability into what agents are doing inside containers.
3. Enables token optimization (output compression, context injection) at the harness level.
4. Remains decoupled from any specific agent's hook format.

---

## Background: Agent Hook Systems

### Claude Code Hooks

Configured via `settings.json` (user, project, or local scope). Rich event model with 25+ event types.

**Key events relevant to wallfacer:**

| Event | Blocking | Wallfacer Use Case |
|-------|----------|--------------------|
| `SessionStart` | No | Record session metadata, inject initial context |
| `PreToolUse` | Yes | Compress Bash output (RTK), block dangerous commands, rewrite commands |
| `PostToolUse` | No | Record tool results for telemetry, detect anomalies |
| `PostToolUseFailure` | No | Record failures, detect flaky patterns |
| `Stop` | Yes | Prevent premature stops, enforce completion criteria |
| `SubagentStart/Stop` | Mixed | Track sub-agent lifecycle for cost attribution |
| `PreCompact/PostCompact` | No | Detect context compaction (cache break signal) |
| `StopFailure` | No | Detect rate limits, auth failures, billing errors |
| `UserPromptSubmit` | Yes | Validate/transform prompts before execution |
| `Notification` | No | Forward permission prompts to wallfacer UI |

**Hook handler types:** `command` (shell script), `http` (webhook to external server), `prompt` (LLM evaluation), `agent` (spawn sub-agent).

**Output control:** Hooks return JSON with `permissionDecision` (allow/deny/ask), `updatedInput` (rewrite tool args), `additionalContext` (inject into conversation), or `decision: "block"` (prevent action).

**Matcher system:** Regex on tool name (`Bash`, `Edit|Write`, `mcp__.*`), session source (`startup|resume`), error type, etc.

### Codex CLI Hooks

Configured via `hooks.json` at `~/.codex/hooks.json` or `<repo>/.codex/hooks.json`. Currently experimental (`codex_hooks = true` in config.toml). Smaller event surface than Claude Code.

**Supported events:**

| Event | Blocking | Matcher | Notes |
|-------|----------|---------|-------|
| `SessionStart` | No | `source` (startup/resume) | Adds context, records metadata |
| `PreToolUse` | Yes | `tool_name` (Bash only) | Can block or rewrite commands |
| `PostToolUse` | No | `tool_name` (Bash only) | Feedback on executed commands |
| `UserPromptSubmit` | Yes | None | Block or augment prompts |
| `Stop` | Yes | None | Prevent premature completion |

**Format:** Nearly identical JSON schema to Claude Code (same `hookSpecificOutput` structure, same `permissionDecision` values, same exit code semantics). This is not coincidental — Codex adopted Claude Code's hook format.

**Limitations:** Only `Bash` tool matching; no `Edit`, `Write`, `Read` interception. No sub-agent events. No compaction events. Feature-flagged and experimental.

### Comparison

| Capability | Claude Code | Codex CLI |
|------------|-------------|-----------|
| Event types | 25+ | 5 |
| Tool matching | All tools + MCP | Bash only |
| Handler types | command, http, prompt, agent | command |
| Input rewriting | Yes (`updatedInput`) | Limited |
| HTTP webhooks | Yes | No |
| Sub-agent events | Yes | No |
| Compaction events | Yes | No |
| Config location | `settings.json` | `hooks.json` |
| Production status | Stable | Experimental |

**Key insight:** The hook schemas are intentionally compatible. Codex adopted Claude Code's JSON format. A wallfacer hook framework can target the shared subset and extend per-sandbox where needed.

---

## Design

### Architecture Overview

```
┌─────────────────────────────────────────────────┐
│ Wallfacer Server                                │
│                                                 │
│  ┌──────────────┐    ┌──────────────────────┐   │
│  │ Hook Registry │    │ Hook HTTP Server     │   │
│  │ (per sandbox) │    │ :0 (ephemeral port)  │   │
│  └──────┬───────┘    └──────────┬───────────┘   │
│         │                       │               │
│         │  generates            │  receives     │
│         ▼                       ▼               │
│  ┌──────────────┐    ┌──────────────────────┐   │
│  │ settings.json │    │ Hook Event Handler   │   │
│  │ or hooks.json │    │ (dispatch per event) │   │
│  └──────┬───────┘    └──────────────────────┘   │
│         │                                       │
└─────────┼───────────────────────────────────────┘
          │ mounted into container
          ▼
┌─────────────────────────────────────────────────┐
│ Sandbox Container                               │
│                                                 │
│  Agent (claude/codex) reads settings/hooks.json │
│  → fires hooks as HTTP POSTs to server          │
│  → receives JSON responses (allow/deny/rewrite) │
│                                                 │
└─────────────────────────────────────────────────┘
```

Two components:

1. **Hook config generator**: Produces the agent-specific config file (`settings.json` for Claude Code, `hooks.json` for Codex) with HTTP hooks pointing back to the wallfacer server.

2. **Hook HTTP server**: Listens inside the wallfacer process, receives hook events from containers, dispatches to registered handlers, returns decisions.

### Why HTTP Hooks

Both Claude Code and Codex support command-type hooks (shell scripts). But HTTP hooks (Claude Code native, Codex via wrapper script) are superior for wallfacer because:

- **Centralized logic**: Hook behavior lives in the Go server, not in shell scripts scattered across container images. Easier to update, test, and version.
- **Bidirectional communication**: The server can make decisions based on global state (other tasks, budget, circuit breakers) that a shell script inside the container cannot see.
- **Structured telemetry**: Hook events flow directly into the server's event system without parsing script output.
- **No container image changes**: Hook config is mounted at launch time; the container image stays generic.

For Codex (which lacks native HTTP hooks), a thin wrapper script translates: receive JSON on stdin → POST to wallfacer server → print response JSON to stdout.

### Hook Config Generation

At container launch time, `buildContainerSpecForSandbox()` generates the appropriate hook config and mounts it.

**Claude Code** — generate `.claude/settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{
          "type": "http",
          "url": "http://host.containers.internal:8080/api/hooks/pre-tool-use",
          "headers": { "Authorization": "Bearer $WALLFACER_HOOK_TOKEN" },
          "allowedEnvVars": ["WALLFACER_HOOK_TOKEN"],
          "timeout": 30
        }]
      }
    ],
    "PostToolUse": [
      {
        "matcher": ".*",
        "hooks": [{
          "type": "http",
          "url": "http://host.containers.internal:8080/api/hooks/post-tool-use",
          "headers": { "Authorization": "Bearer $WALLFACER_HOOK_TOKEN" },
          "allowedEnvVars": ["WALLFACER_HOOK_TOKEN"],
          "timeout": 10
        }]
      }
    ],
    "SessionStart": [
      {
        "matcher": ".*",
        "hooks": [{
          "type": "http",
          "url": "http://host.containers.internal:8080/api/hooks/session-start",
          "headers": { "Authorization": "Bearer $WALLFACER_HOOK_TOKEN" },
          "allowedEnvVars": ["WALLFACER_HOOK_TOKEN"],
          "timeout": 10
        }]
      }
    ],
    "Stop": [
      {
        "hooks": [{
          "type": "http",
          "url": "http://host.containers.internal:8080/api/hooks/stop",
          "headers": { "Authorization": "Bearer $WALLFACER_HOOK_TOKEN" },
          "allowedEnvVars": ["WALLFACER_HOOK_TOKEN"],
          "timeout": 10
        }]
      }
    ],
    "StopFailure": [
      {
        "hooks": [{
          "type": "http",
          "url": "http://host.containers.internal:8080/api/hooks/stop-failure",
          "headers": { "Authorization": "Bearer $WALLFACER_HOOK_TOKEN" },
          "allowedEnvVars": ["WALLFACER_HOOK_TOKEN"],
          "timeout": 5
        }]
      }
    ],
    "PreCompact": [
      {
        "hooks": [{
          "type": "http",
          "url": "http://host.containers.internal:8080/api/hooks/pre-compact",
          "headers": { "Authorization": "Bearer $WALLFACER_HOOK_TOKEN" },
          "allowedEnvVars": ["WALLFACER_HOOK_TOKEN"],
          "timeout": 5
        }]
      }
    ],
    "PostCompact": [
      {
        "hooks": [{
          "type": "http",
          "url": "http://host.containers.internal:8080/api/hooks/post-compact",
          "headers": { "Authorization": "Bearer $WALLFACER_HOOK_TOKEN" },
          "allowedEnvVars": ["WALLFACER_HOOK_TOKEN"],
          "timeout": 5
        }]
      }
    ]
  }
}
```

**Codex** — generate `.codex/hooks.json` with the same structure (Codex supports the same JSON format for the events it implements). For events Codex doesn't support, the config simply omits them.

**Network considerations:**
- With `--network=host` (default): hooks URL is `http://localhost:<port>/api/hooks/...`
- With custom network: hooks URL uses the host gateway (`host.containers.internal` or `host-gateway`)
- Hook token (`WALLFACER_HOOK_TOKEN`) is a per-session random token passed via env to authenticate hook calls

### Hook Config vs Named Volume Conflict

Currently, `.claude` is backed by a named volume (`claude-config:/home/claude/.claude`). If we mount a generated `settings.json` into this path, it conflicts with the volume.

**Options:**

| Option | Approach | Trade-off |
|--------|----------|-----------|
| A. Bind-mount settings.json over volume | `--mount type=bind,src=...,dst=/home/claude/.claude/settings.json` | Bind mount takes precedence over volume content at that specific path. Works with podman/docker. |
| B. Write settings.json via entrypoint | Entrypoint script writes settings.json before exec-ing claude | No mount conflict; but couples config to entrypoint logic |
| C. Use project-level settings | Mount at `/workspace/.claude/settings.json` | Claude Code reads project settings from CWD's `.claude/settings.json`. No volume conflict. |
| D. Drop named volume | Replace `claude-config` volume with per-task bind mounts | Breaking change to container reuse model |

**Recommended: Option C** (project-level settings). Mount the generated config at `/workspace/.claude/settings.json` (or the task's working directory). Claude Code reads project-level settings automatically. No conflict with the user-level named volume. For multi-workspace setups where CWD is `/workspace`, mount at `/workspace/.claude/settings.json`.

For Codex: mount at `/workspace/.codex/hooks.json` (repo-level discovery).

---

## Hook Handlers

### Handler 1: Output Compression (PreToolUse → Bash)

**Goal:** Reduce token consumption from verbose shell output (test results, build errors, git operations).

**Mechanism:** Rewrite the Bash command to pipe through a compressor before the agent sees the output. Uses `updatedInput` to modify the command.

```
Agent wants to run: go test ./...
Hook rewrites to:   go test ./... 2>&1 | rtk compress --type=test
Agent sees:         compressed output (90% fewer tokens)
```

**Implementation:**

The hook handler receives the `PreToolUse` event, inspects `tool_input.command`, and decides whether to compress:

```go
func handlePreToolUse(w http.ResponseWriter, r *http.Request) {
    var event HookEvent
    json.NewDecoder(r.Body).Decode(&event)

    if !shouldCompress(event.ToolInput.Command) {
        w.WriteHeader(200) // pass through
        return
    }

    compressed := wrapWithCompression(event.ToolInput.Command)
    json.NewEncoder(w).Encode(HookResponse{
        HookSpecificOutput: &PreToolUseOutput{
            HookEventName:   "PreToolUse",
            UpdatedInput:    map[string]any{"command": compressed},
            AdditionalContext: "Output was compressed for token efficiency.",
        },
    })
}
```

**Compression strategies** (can ship RTK binary in image, or use built-in Go compressor):

| Command Pattern | Compression | Savings |
|----------------|-------------|---------|
| `go test`, `npm test`, `pytest`, `cargo test` | Strip passing tests, keep failures + summary | 80-90% |
| `git status`, `git diff`, `git log` | Group by directory, truncate large diffs | 70-80% |
| `ls`, `find` | Group by extension, collapse deep paths | 60-70% |
| `make`, `cargo build` | Deduplicate repeated warnings, keep errors | 70-80% |

**Opt-out:** Some commands should never be compressed (e.g., `cat` for reading files, commands with `| jq`, commands the agent explicitly needs full output from). The handler maintains a skip-list.

**Alternative to RTK:** Instead of installing RTK in the image, wallfacer can implement its own Go-based compressor that the hook handler applies server-side. The hook receives the command, the server runs a modified version that captures and compresses output, and returns it via `updatedInput`. However, this requires the command to execute on the server side, which breaks the sandbox isolation model. Therefore, in-container compression (RTK or equivalent) is the correct approach, with the hook merely rewriting the command to include the compressor pipe.

### Handler 2: Telemetry Collection (PostToolUse, StopFailure, PreCompact, PostCompact)

**Goal:** Fine-grained observability into agent behavior without parsing container logs.

**PostToolUse handler:**
- Record tool name, execution time (from timestamps), and result size.
- Detect patterns: excessive file reads (context stuffing), repeated failed commands (agent stuck), large tool results (token waste).
- Emit as `EventTypeSystem` events into the task's event timeline.

**StopFailure handler:**
- Record rate limit hits, auth failures, billing errors.
- Feed into circuit breaker logic (existing `WALLFACER_CONTAINER_CB_THRESHOLD`).
- Rate limit events can trigger automatic backoff before the next turn.

**PreCompact/PostCompact handlers:**
- Record when context compaction occurs (signals context is getting large).
- Track compaction frequency per task — high frequency means the agent is consuming context rapidly.
- Feed into the token cost optimization spec's cache break detection.

### Handler 3: Stop Guard (Stop)

**Goal:** Prevent the agent from stopping prematurely when wallfacer knows there's more work to do.

**Use cases:**
- Task has a validation barrier (test suite must pass) but the agent stopped without running tests.
- Agent stopped but the oversight summary indicates incomplete work.
- Task has dependent tasks waiting — ensure the agent committed clean, buildable code.

**Implementation:** The `Stop` hook checks the agent's `last_assistant_message` against task completion criteria. If criteria aren't met, return `{"decision": "block", "reason": "Tests have not been run yet. Please run the test suite before finishing."}`.

This replaces the current pattern where wallfacer detects incomplete work only after the agent has already stopped, requiring a new feedback turn (expensive — new container invocation, potential cache miss).

### Handler 4: Session Metadata (SessionStart)

**Goal:** Inject task-specific context and record session metadata.

**On session start:**
- Record the session source (`startup` vs `resume`) — feeds into cache analysis.
- Inject `additionalContext` with task-specific guidance that doesn't fit in AGENTS.md (e.g., "This task depends on task X which completed with the following summary: ...").
- Record the model being used (confirms the container is using the expected model).

### Handler 5: Dangerous Command Guard (PreToolUse → Bash)

**Goal:** Prevent agents from executing commands that could damage the host or other tasks.

Even with `--dangerously-skip-permissions`, wallfacer may want to block specific operations:
- `rm -rf /workspace` (delete all worktrees)
- `git push --force` on shared branches
- `kill`, `pkill` on system processes
- Network operations to internal services
- Writes outside `/workspace`

This is defense-in-depth — the container sandbox provides isolation, but an extra hook-level guard catches mistakes in the sandbox configuration.

---

## API Routes

New hook endpoints on the wallfacer server:

```
POST /api/hooks/session-start     — SessionStart event
POST /api/hooks/pre-tool-use      — PreToolUse event  
POST /api/hooks/post-tool-use     — PostToolUse event
POST /api/hooks/stop              — Stop event
POST /api/hooks/stop-failure      — StopFailure event
POST /api/hooks/pre-compact       — PreCompact event
POST /api/hooks/post-compact      — PostCompact event
```

All routes:
- Authenticated via `WALLFACER_HOOK_TOKEN` (per-session, passed in `Authorization: Bearer` header).
- Accept JSON body (the hook event payload from the agent).
- Must include task identification. Since the agent doesn't know its wallfacer task ID, the hook URL includes it as a query parameter: `/api/hooks/pre-tool-use?task=<uuid>`.
- Return JSON response conforming to the agent's expected hook output schema.
- Must respond within the configured timeout (default 30s for PreToolUse, 10s for others).

---

## Configuration

```
WALLFACER_HOOKS_ENABLED          — enable sandbox hooks (default: true)
WALLFACER_HOOKS_COMPRESSION      — enable output compression hook (default: true)
WALLFACER_HOOKS_STOP_GUARD       — enable stop guard hook (default: false)
WALLFACER_HOOKS_TELEMETRY        — enable telemetry collection hook (default: true)
WALLFACER_HOOKS_COMMAND_GUARD    — enable dangerous command guard (default: true)
```

Per-task overrides possible via task metadata (e.g., disable compression for tasks that need exact output).

---

## Implementation Phases

### Phase 1: Infrastructure

- Hook config generator (produces `settings.json` / `hooks.json` per sandbox type).
- Mount generated config into containers (Option C: project-level settings).
- Hook HTTP server with routing and authentication.
- Basic `SessionStart` handler (metadata recording only).

### Phase 2: Telemetry

- `PostToolUse` handler recording tool events.
- `StopFailure` handler feeding circuit breaker.
- `PreCompact`/`PostCompact` handlers for context tracking.
- Surface hook-sourced events in task timeline UI.

### Phase 3: Output Compression

- Install RTK (or equivalent compressor) in sandbox images.
- `PreToolUse` handler that rewrites Bash commands with compression pipes.
- Compression skip-list for commands needing full output.
- Token savings tracking and dashboard.

### Phase 4: Active Control

- `Stop` guard enforcing completion criteria.
- Dangerous command guard.
- `PreToolUse` command rewriting for non-compression purposes (e.g., adding `--no-color` to commands that produce ANSI escape sequences).

---

## Dependencies

- Depends on sandbox images being rebuilt with hook config mount points.
- Output compression (Phase 3) depends on RTK or equivalent being added to Dockerfiles.
- Benefits from [token-cost-optimization.md](token-cost-optimization.md) for telemetry integration, but can ship independently.

## Relationship to Token Cost Optimization

The [token-cost-optimization.md](token-cost-optimization.md) spec identifies the *what* (cache observability, output compression, regression modeling). This spec provides the *how* — hooks are the primary mechanism for implementing several of those optimizations:

| Token Cost Optimization Item | Hook Mechanism |
|------------------------------|---------------|
| Cache break detection (§1.2) | `PreCompact`/`PostCompact` events |
| Shell output compression (§2.1) | `PreToolUse` Bash rewriting |
| AGENTS.md size tracking (§2.3) | `SessionStart` context recording |
| Anomaly detection (§3.1) | `PostToolUse` + `StopFailure` telemetry |
| Stop prevention (§4.2) | `Stop` guard |

## Acceptance Criteria

1. Claude Code sandbox containers receive a generated `settings.json` with HTTP hooks pointing to the wallfacer server.
2. Codex sandbox containers receive a generated `hooks.json` with equivalent configuration (for supported events).
3. Hook events are received and recorded in the task event timeline.
4. Output compression reduces Bash tool output tokens by ≥50% on test/build commands.
5. Stop guard can prevent premature agent termination based on configurable criteria.
6. Dangerous command guard blocks `rm -rf /workspace` and `git push --force` on protected branches.
7. All hooks are individually toggleable via env vars.
8. Hook latency does not add >100ms to agent tool execution (p99).

## Non-Goals

- Modifying agent internals or forking Claude Code / Codex.
- Intercepting non-Bash tools in Codex (blocked by Codex's current Bash-only matcher limitation).
- Building a general-purpose hook marketplace or plugin system.
- Real-time streaming of hook events to the UI (batch via task event timeline is sufficient).

## Open Questions

1. **Named volume conflict:** Does Option C (project-level `.claude/settings.json`) work reliably when the workspace already has a `.claude/` directory in the repo? Need to test precedence: does the bind-mounted file override the repo's checked-in settings?
2. **Codex HTTP hooks:** Codex only supports `command`-type hooks natively. The wrapper script approach (stdin JSON → curl → stdout JSON) adds ~50ms latency. Is this acceptable, or should we wait for native Codex HTTP hook support?
3. **Compression accuracy:** Does compressing test output cause agents to miss important failure details? Need to test with wallfacer's typical Go test output and verify the agent can still diagnose failures.
4. **Hook ordering with user hooks:** If users configure their own hooks in the named volume's `settings.json`, how do wallfacer's project-level hooks interact? Claude Code merges hooks from multiple sources — need to verify both fire without conflict.
5. **Container reuse (task workers):** With task workers, the container persists across turns. Does the hook config need to be refreshed between turns, or is it read once at session start?
