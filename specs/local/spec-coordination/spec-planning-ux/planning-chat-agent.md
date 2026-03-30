---
title: Planning Chat Agent
status: drafted
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/spec-mode-ui-shell.md
  - specs/local/spec-coordination/spec-planning-ux/planning-sandbox.md
affects:
  - ui/js/
  - internal/handler/
  - internal/runner/
  - internal/prompts/
effort: xlarge
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Planning Chat Agent

## Design Problem

How does the chat-driven iteration model work end-to-end? The right pane of spec mode is a conversation stream where the user types directives and the agent executes immediately — reading the codebase, writing spec files, breaking down specs, updating entry-point documents. This is fundamentally different from the existing task execution model (fire-and-forget prompt → wait for completion). The planning agent is interactive: each user message gets a response, the agent's writes appear live in the explorer and focused view, and the conversation persists globally across spec switches and sessions.

Key constraints:
- The agent runs inside the planning sandbox (per the planning-sandbox sub-design)
- Zero permission prompts — the agent writes to `specs/` autonomously
- Global conversation persistence across spec switches and across sessions (stored in `~/.wallfacer/`)
- The agent has full read access to the workspace codebase (not just specs)
- Agent has spec-creation skills (API endpoint, refactor, bug fix, migration patterns)
- Agent proactively updates entry-point documents on spec status changes
- "Summarize" action produces structured summaries under a user-chosen word limit

## Context

The existing chat-like interaction in Wallfacer is the task execution model:
- User writes a prompt, task enters `in_progress`, agent runs autonomously
- Output streams via SSE (`GET /api/tasks/{id}/logs`)
- User can provide feedback when task reaches `waiting` state
- No back-and-forth conversation — it's prompt → execution → result

The planning chat is conversational: user sends message → agent responds + may modify files → user sends another message → agent responds, all within one continuous session. This is closer to a REPL than a batch job.

The existing `internal/prompts/` package manages system prompt templates. The planning agent needs its own system prompt that establishes its role (spec writer, not code implementer), its permissions (read all, write specs only), and its available skills.

## Options

**Option A — Extend task execution model.** The planning session is a special long-running task in `waiting` state. Each user message is "feedback" that resumes execution. The agent runs one turn, writes files, then returns to `waiting`. The chat stream is the task's event timeline.

- Pro: Reuses existing infrastructure (task store, event sourcing, SSE streaming, feedback mechanism). Usage tracking and cost attribution work out of the box.
- Con: Awkward fit. Tasks have a lifecycle (backlog → done) that doesn't map to an open-ended conversation. A "planning task" that never completes pollutes the kanban board. The waiting → feedback → resume cycle adds latency to what should be instant back-and-forth.

**Option B — New conversation model.** A dedicated `internal/planner/conversation.go` manages the planning session as a stateful conversation (not a task). Messages are stored in `~/.wallfacer/planning/<fingerprint>/`. The planning sandbox runs the agent in a conversational mode (each user message → one agent response). The UI streams responses via a new SSE endpoint or WebSocket.

- Pro: Purpose-built for interactive conversation. No task lifecycle overhead. Session persistence is straightforward (append messages to a file or SQLite). Can use a different streaming model optimized for chat (e.g., token-by-token streaming).
- Con: New infrastructure — doesn't reuse task store, event sourcing, or SSE streaming. Usage tracking must be built separately.

**Option C — Hybrid: conversation wrapper over container exec.** The planning container is a long-lived worker. Each user message is sent as a `podman exec` command with the message as input. The agent processes the message, writes files, and returns a response. The conversation history is maintained client-side and prepended to each exec as context.

- Pro: Simple container interaction model. No custom streaming infrastructure — each exec has stdout/stderr. Conversation state is stateless on the server (client manages).
- Con: Context window limits — prepending full conversation history to each exec is expensive and eventually exceeds the model's context. No server-side persistence means closing the browser loses the conversation.

## Open Questions

1. How does the agent receive the "focused spec" context? Is it passed as part of each message, or does the container maintain state about which spec is focused?
2. How does live file update work? When the agent writes a spec file, the UI needs to detect the change and re-render the focused view and explorer. Options: agent response includes a list of modified files, or the UI polls/watches for filesystem changes, or the server sends file-change events via SSE.
3. What system prompt does the planning agent use? It needs to know: its role (spec writer), available skills (break down, summarize, update status), the spec document model conventions (frontmatter schema, lifecycle states), and the current spec tree state.
4. How does the "Summarize" action work? Is it a special chat message ("Summarize this spec in 100 words") or a separate API call that bypasses the conversation?
5. How does entry-point auto-update work? When a spec status changes, who triggers the agent to update the README — the server (as a hook), the UI (as a follow-up message), or the agent itself (proactively within the same turn)?
6. How is conversation history managed for token efficiency? Long planning sessions could produce thousands of messages. Options: sliding window, summarization of old messages, or server-side context management.

## Affects

- `internal/prompts/` — new system prompt template for the planning agent
- `internal/handler/` — new endpoints for sending/receiving planning messages, streaming responses
- `internal/runner/` or `internal/planner/` — planning session lifecycle, message dispatch to sandbox
- `ui/js/` — new chat stream component (message input, response rendering, live file change indicators)
- `~/.wallfacer/planning/` — conversation persistence storage
