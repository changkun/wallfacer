---
title: Planning Chat Agent
status: drafted
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/spec-mode-ui-shell.md
  - specs/local/spec-coordination/spec-planning-ux/planning-sandbox.md
affects:
  - ui/js/
  - internal/handler/
  - internal/planner/
  - internal/runner/
  - internal/prompts/
effort: xlarge
created: 2026-03-30
updated: 2026-04-03
author: changkun
dispatched_task_id: null
---

# Planning Chat Agent

## Design Problem

How does the chat-driven iteration model work end-to-end? The right pane of spec mode is a conversation stream where the user types directives and the agent executes immediately — reading the codebase, writing spec files, breaking down specs, updating entry-point documents. This is fundamentally different from the existing task execution model (fire-and-forget prompt → wait for completion). The planning agent is interactive: each user message gets a response, the agent's writes appear live in the explorer and focused view, and the conversation persists globally across spec switches and sessions.

Key constraints:
- The agent runs inside the planning sandbox (per the planning-sandbox sub-design, already implemented in `internal/planner/`)
- Zero permission prompts — the agent writes to `specs/` autonomously
- Global conversation persistence across spec switches and across sessions (stored in `~/.wallfacer/planning/`)
- The agent has full read access to the workspace codebase (not just specs)
- Agent has spec-creation skills (API endpoint, refactor, bug fix, migration patterns)
- Agent proactively updates entry-point documents on spec status changes
- Slash commands (`/summarize`, `/break-down`, `/create`, etc.) trigger structured agent skills, similar to Claude Code's `/command` pattern

## Current State

The following infrastructure is already implemented:

- **Planning sandbox container** (`internal/planner/`): Long-lived worker container keyed by workspace fingerprint. `Planner.Exec()` runs commands inside the container via the `sandbox.Backend` worker mechanism. Full workspace mounted read-only with `specs/` read-write override. API: `GET/POST/DELETE /api/planning`.
- **Spec mode UI shell** (`ui/js/spec-mode.js`, `ui/css/spec-mode.css`): Three-pane layout with explorer, focused markdown view, and chat stream area. Mode switching between Board and Specs. Deep-linking via `#spec/<path>`.
- **Ideation agent** (`internal/runner/ideate.go`, `internal/handler/ideate.go`): Runs brainstorm analysis in ephemeral containers. Parses JSON results, manages ideation history in `{dataDir}/ideation-history.jsonl`.
- **SSE streaming** (`internal/handler/stream.go`): Task log streaming, refinement log streaming, task list push updates.

## Design

### Approach: Conversation Wrapper over Container Exec

The planning container is the existing long-lived worker from `internal/planner/`. Each user message is sent as a `podman exec` invocation with the message as input. The agent processes the message, writes files, and returns a response. Conversation history is maintained server-side in `~/.wallfacer/planning/<fingerprint>/` for persistence across browser reloads and application restarts.

This approach is chosen because:
- The planning container already exists as a long-lived worker — adding exec-based conversation is a thin layer on top
- Server-side persistence in `~/.wallfacer/` means the user sees prior conversation when reopening the application
- No new streaming infrastructure needed — each exec produces stdout that can be streamed via SSE
- Context window management is explicit: the server controls what history is prepended to each exec

### Unified Planning Worker

The planning worker container and the ideation agent should share a single long-lived worker. Currently ideation runs in ephemeral containers (`internal/runner/ideate.go` builds a `ContainerSpec` per run). Instead:

- The `Planner` manages the single long-lived container for both planning conversation and ideation runs
- Ideation uses `Planner.Exec()` with the ideation prompt instead of spawning its own container
- This eliminates container startup overhead for ideation (~2-5s per run) and consolidates the container lifecycle into one place
- The planner container already has the correct mounts (full workspace RO, specs RW) — ideation only needs RO, which is a subset
- Ideation history (`ideation-history.jsonl`) continues to be managed by the runner; only the container launch path changes

### Conversation Persistence

Conversation history is stored in `~/.wallfacer/planning/<fingerprint>/`:

- **`messages.jsonl`** — append-only log of all messages (role, content, timestamp, focused spec path, modified files list)
- **`context.json`** — current session metadata (last active timestamp, focused spec, token count estimate)
- One directory per workspace fingerprint, matching the planning container keying
- On application startup, the server loads the conversation history and serves it to the UI on connect
- Old messages beyond a configurable token budget are summarized into a `summary.md` that is prepended to new exec calls instead of the full history

### Message Flow

1. User types message in the chat stream pane
2. `POST /api/planning/messages` sends the message to the server
3. Server appends user message to `messages.jsonl`
4. Server builds the exec payload: system prompt + conversation context (recent messages or summary + recent messages) + focused spec content + user message
5. `Planner.Exec()` runs the agent inside the planning container
6. Agent stdout streams back via SSE (`GET /api/planning/messages/stream`)
7. Agent writes spec files during execution; response includes a list of modified files
8. Server appends assistant message to `messages.jsonl`
9. UI receives the modified-files list and triggers explorer refresh + focused view re-render for affected paths

### System Prompt

A new `planning.tmpl` in `internal/prompts/` establishes the agent's role:

- **Identity**: Spec writer and planning assistant, not a code implementer
- **Permissions**: Read all workspace files, write only to `specs/` directories
- **Skills**: Slash-command skill set (see "Slash Commands" section below)
- **Conventions**: Spec document model (frontmatter schema, lifecycle states, DAG rules), naming conventions, track organization
- **Context injection**: Current spec tree state (from `GET /api/specs/tree` data), focused spec content, recent conversation summary

### Live File Update Detection

File change detection reuses existing infrastructure — no new filesystem watching needed:

- **`ExplorerStream`** (`GET /api/explorer/stream`) already polls workspace directories every 3s and sends `refresh` events when file fingerprints change. When the planning agent writes spec files, this stream automatically detects the changes.
- **`SpecTreeStream`** (`GET /api/specs/stream`) already polls spec directories every 3s and sends `snapshot` events when the spec tree changes. Spec status updates, new spec files, and frontmatter edits are all picked up automatically.
- The planning messages stream (`GET /api/planning/messages/stream`) only needs to carry the agent's response text — it does not duplicate file-change detection. It is closer in nature to `StreamLogs` (live container stdout) than to the explorer/spec SSE endpoints.
- Optionally, the agent response can include a `modified_files` hint in a structured footer so the UI can trigger an immediate refresh instead of waiting for the next 3-second poll cycle.

### Slash Commands

The chat input supports `/command` syntax, modeled after Claude Code's skills pattern. When the user types a slash command, the UI shows an autocomplete menu of available commands. The selected command expands into a structured prompt that the agent executes with specific instructions and output format expectations.

Built-in slash commands for the planning agent:

| Command | Description |
|---------|-------------|
| `/summarize [words]` | Produce a structured summary of the focused spec under the given word limit (default 200) |
| `/break-down` | Decompose the focused spec into sub-specs or implementation tasks |
| `/create <title>` | Create a new spec file with proper frontmatter in the appropriate track |
| `/status <state>` | Update the focused spec's status and propagate changes to `specs/README.md` |
| `/validate` | Check the focused spec against the document model rules (frontmatter, DAG, naming) |
| `/impact` | Analyze what existing code and specs the focused spec would affect |
| `/dispatch` | Prepare the focused spec for dispatch to the task board (set `dispatched_task_id`) |

**Implementation**: Each slash command is defined as a template in `internal/prompts/planning/` (e.g., `summarize.tmpl`, `breakdown.tmpl`). The server expands the command into a full prompt before passing it to `Planner.Exec()`. The UI sends the raw `/command args` text; the server intercepts the leading `/`, looks up the matching template, renders it with the current context (focused spec, tree state), and prepends it to the exec payload. From the agent's perspective, a slash command is just a well-structured user message.

**Extensibility**: New slash commands are added by dropping a template file and registering it in a command registry. The `GET /api/planning/commands` endpoint returns the available commands for UI autocomplete.

### Entry-Point Auto-Update

When a slash command or free-form directive changes a spec's status (e.g., `drafted` → `validated`), the agent proactively updates `specs/README.md` in the same turn. The system prompt instructs the agent to maintain consistency between individual spec frontmatter and the entry-point status tables. This is enforced as a convention in the prompt, not a server-side hook.

### Context Window Management

For long planning sessions:

- The server maintains a running token estimate for the conversation
- When the estimate exceeds a threshold (configurable, default ~80k tokens), the server triggers a summarization pass: older messages are replaced by a condensed summary stored in `summary.md`
- Each exec receives: summary (if any) + last N messages (sliding window) + focused spec + system prompt
- The `context.json` file tracks the current window boundaries so the server can resume efficiently after restart

## New API Endpoints

- `GET /api/planning/messages` — Retrieve conversation history (supports pagination via `?before=<timestamp>`)
- `POST /api/planning/messages` — Send a user message or slash command, triggers agent exec
- `GET /api/planning/messages/stream` — SSE: stream the agent's response tokens + modified-files events
- `DELETE /api/planning/messages` — Clear conversation history (with confirmation)
- `GET /api/planning/commands` — List available slash commands with descriptions (for UI autocomplete)

## Open Questions

1. **Ideation migration path**: Should the ideation runner switch to using `Planner.Exec()` immediately, or should it fall back to ephemeral containers when the planning container is not running? The planner auto-starts on server init, so fallback may be unnecessary.
2. **Multi-turn within a single message**: Can the agent ask clarifying questions back to the user within a single exec, or is it strictly one-message-in, one-response-out? The simpler model (single turn per exec) is recommended to start.
3. **Concurrent access**: If the user sends a new message while the agent is still responding to the previous one, should the server queue it, reject it, or cancel the in-flight exec? Queuing with a depth limit of 1 seems safest.
4. **Token budget defaults**: What's the right default for the context window threshold before summarization kicks in? Depends on the model's context window size and the cost tolerance for planning sessions.

## Affects

- `internal/planner/` — conversation dispatch, history management, context windowing
- `internal/prompts/` — new `planning.tmpl` system prompt
- `internal/handler/` — new endpoints for planning messages and streaming
- `internal/runner/` — ideation refactored to use `Planner.Exec()` instead of ephemeral containers
- `ui/js/` — chat stream component (message input, response rendering, live file change indicators)
- `~/.wallfacer/planning/` — conversation persistence storage
