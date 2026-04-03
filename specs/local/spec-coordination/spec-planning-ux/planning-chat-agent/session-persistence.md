---
title: Planning Session Persistence and Recovery
status: drafted
track: local
depends_on: []
affects:
  - internal/planner/planner.go
  - internal/planner/conversation.go
  - internal/planner/spec.go
  - internal/handler/planning.go
  - ui/js/planning-chat.js
effort: large
created: 2026-04-04
updated: 2026-04-04
author: changkun
dispatched_task_id: null
---

# Planning Session Persistence and Recovery

## Design Problem

The planning chat agent relies on Claude Code's `--resume <session-id>` for multi-turn context. But this mechanism is fragile: the session state lives inside the container's `~/.claude/` directory (mapped to a shared `claude-config` named volume), and sessions are lost when:

- The planning container is recreated (workspace switch calls `UpdateWorkspaces` → `Stop`)
- The shared volume is pruned by container runtime garbage collection
- Claude Code internally expires old sessions
- A task worker container writes to the same volume and triggers session cleanup
- The server restarts and the container is rebuilt with a different name

When a session is lost, the current fallback (`BuildHistoryContext`) prepends a lossy text summary of the last 20 messages. This discards the agent's tool call trajectories, file read/write history, reasoning chains, and internal state — making the "resumed" conversation substantially degraded.

### What exactly is lost

| Context layer | Preserved after session loss? | Impact |
|---------------|-------------------------------|--------|
| User messages | Yes (in `messages.jsonl`) | Low — text is intact |
| Assistant final text | Yes (in `messages.jsonl`) | Low — but truncated to result text only |
| Tool calls (Read, Edit, Write, Bash, Grep) | **No** | High — agent forgets what files it read, what edits it made, what commands it ran |
| Tool results (file contents, command output) | **No** | High — agent must re-read files to rebuild its understanding |
| Intermediate reasoning | **No** | Medium — agent loses the "why" behind past decisions |
| Focused spec at each turn | Partially (stored per message) | Low |
| Files the agent wrote | Yes (on disk in `specs/`) | Low — artifacts survive |

## Design

### 1. Per-Turn NDJSON Storage

Store the complete raw NDJSON output of each planning exec in `~/.wallfacer/planning/<fingerprint>/turns/`:

```
~/.wallfacer/planning/<fingerprint>/
├── messages.jsonl        # existing: user/assistant message log
├── session.json          # existing: current session ID
└── turns/
    ├── turn-0001.jsonl   # raw stream-json from first exec
    ├── turn-0002.jsonl   # raw stream-json from second exec
    └── ...
```

Each turn file is the complete stdout from one `Planner.Exec()` call — the same NDJSON that gets streamed to the UI via `GET /api/planning/messages/stream`. This captures assistant text, tool calls, tool results, session IDs, usage metrics, and errors.

The handler's exec goroutine already reads all stdout via `io.ReadAll(tee)`. After the exec completes, write `rawStdout` to the next turn file.

### 2. Dedicated Planning Volume

Replace the shared `claude-config` volume with a planning-specific volume to prevent cross-contamination with task workers:

- **Planning container**: mounts `claude-planning-config:/home/claude/.claude`
- **Task containers**: continue using `claude-config:/home/claude/.claude`
- **Refinement/ideation via planner**: inherits the planning volume

This ensures that task worker container lifecycle operations (stop, remove, recreate) cannot invalidate the planning agent's Claude Code sessions.

Implementation: change the volume name in `internal/planner/spec.go` `buildContainerSpec()`.

### 3. Context Reconstruction from Turns

When `--resume` fails with a stale session error, reconstruct context from stored turns instead of the lossy `BuildHistoryContext()`:

```go
func (s *ConversationStore) ReconstructContext(maxTokens int) string
```

The reconstruction algorithm:

1. **Load all turn files** in order from `turns/`
2. **Extract structured events** from each turn's NDJSON:
   - Assistant text blocks (`type: "assistant"`, `message.content[].type == "text"`)
   - Tool calls (`message.content[].type == "tool_use"`) — extract tool name and key input (file path, command, pattern)
   - Tool results (`type: "user"`, `message.content[].type == "tool_result"`) — extract truncated output
   - User messages (from `messages.jsonl`, keyed by turn number)
3. **Build a structured context prompt** with three levels of detail:
   - **Full fidelity** (most recent N turns): include complete assistant text + tool call details
   - **Summary** (older turns): assistant text only, tool calls listed as `[Read internal/handler/planning.go]` one-liners
   - **Omitted** (oldest turns): just "... N earlier turns omitted ..."
4. **Token budget**: work backwards from the most recent turn, including as much detail as fits within `maxTokens` (default: 30000 tokens, ~120KB of text)

The reconstructed context is prepended to the new prompt as:

```
[Session recovered — previous conversation context follows]

=== Turn 1 (user) ===
Create a new spec for the auth system

=== Turn 1 (assistant) ===
I'll create a new spec file...
[Read specs/README.md]
[Write specs/shared/authentication.md]
Created specs/shared/authentication.md with...

=== Turn 2 (user) ===
Break it down into tasks

=== Turn 2 (assistant) ===
[Read specs/shared/authentication.md]
...
```

### 4. Graceful Session Loss UX

When a session is lost and recovered:

1. **Show a system message** in the chat: "Session was reset — context reconstructed from N previous turns"
2. **Don't retry silently** — the current auto-retry happens in the background and the user sees either nothing or a confusing "No response". Instead, show the recovery status.
3. **Allow manual clear** — the user can click "Clear" to start truly fresh if the reconstructed context is unhelpful.

### 5. Prevent Unnecessary Session Loss

Reduce the frequency of session loss:

- **`UpdateWorkspaces` should not Stop** — when workspace configuration changes, mark the planner as needing a restart on next exec, but don't kill the current container. Only stop if the workspace fingerprint actually changed.
- **Container name stability** — the planning container name is `wallfacer-plan-<fingerprint12>`. As long as the fingerprint doesn't change, the same container is reused. Document that workspace reordering (same dirs, different order) produces a different fingerprint and triggers container recreation.

## Open Questions

1. **Token budget for reconstruction**: 30K tokens is a guess. Should this be configurable? Should it be a fraction of the model's context window?
2. **Turn file retention**: how many turns to keep? Older turns could be archived or deleted after N days. The task runner uses 7-day tombstone retention — should planning turns follow the same policy?
3. **Concurrent read during write**: the exec goroutine writes the turn file after completion. If the reconstruction runs during an active exec (e.g. session loss mid-conversation), it should skip the incomplete current turn.

## Affects

- `internal/planner/conversation.go` — turn file storage, `ReconstructContext()`, `SaveTurnOutput()`
- `internal/planner/spec.go` — dedicated volume name
- `internal/handler/planning.go` — write turn output after exec, reconstruction on stale session
- `ui/js/planning-chat.js` — show "session recovered" system message
