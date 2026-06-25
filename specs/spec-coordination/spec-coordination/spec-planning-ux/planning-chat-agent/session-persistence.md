---
title: Planning Session Persistence and Recovery
status: stale
depends_on: []
affects:
  - internal/planner/planner.go
  - internal/planner/conversation.go
  - internal/planner/spec.go
  - internal/handler/planning.go
  - frontend/src/stores/planning.ts
  - frontend/src/components/plan/PlanningChatPanel.vue
effort: large
created: 2026-04-04
updated: 2026-06-25
author: changkun
dispatched_task_id: null
---

# Planning Session Persistence and Recovery

> **Status: stale.** The baseline this spec builds on shipped (`SaveSession`/`LoadSession`/`SessionInfo`, `--resume`, and the lossy `BuildHistoryContext` fallback), but none of this spec's five proposed deliverables landed, and the container premise behind several of them is now obsolete. See the Status section at the end before implementing. The design below is preserved for the record and as a starting point if the work is revived.

## Design Problem

The planning chat agent relies on Claude Code's `--resume <session-id>` for multi-turn context. But this mechanism is fragile: the session state lives in the agent's `~/.claude/` directory, and sessions are lost when:

- The planning execution context is recreated
- The agent's config directory is pruned or garbage-collected
- Claude Code internally expires old sessions
- The server restarts and the execution context is rebuilt with a different identity

When a session is lost, the current fallback (`BuildHistoryContext`) prepends a lossy text summary of the last 20 messages. This discards the agent's tool call trajectories, file read/write history, reasoning chains, and internal state, making the "resumed" conversation substantially degraded.

### What exactly is lost

| Context layer | Preserved after session loss? | Impact |
|---------------|-------------------------------|--------|
| User messages | Yes (in `messages.jsonl`) | Low (text is intact) |
| Assistant final text | Yes (in `messages.jsonl`) | Low (but truncated to result text only) |
| Tool calls (Read, Edit, Write, Bash, Grep) | **No** | High (agent forgets what files it read, what edits it made, what commands it ran) |
| Tool results (file contents, command output) | **No** | High (agent must re-read files to rebuild its understanding) |
| Intermediate reasoning | **No** | Medium (agent loses the "why" behind past decisions) |
| Focused spec at each turn | Partially (stored per message) | Low |
| Files the agent wrote | Yes (on disk in `specs/`) | Low (artifacts survive) |

## Design

### 1. Per-Turn NDJSON Storage

Store the complete raw NDJSON output of each planning exec in `~/.wallfacer/planning/<fingerprint>/turns/` (per thread, under `threads/<id>/turns/` given the threads layout):

```
~/.wallfacer/planning/<fingerprint>/threads/<id>/
├── messages.jsonl        # existing: user/assistant message log
├── session.json          # existing: current session ID
└── turns/
    ├── turn-0001.jsonl   # raw stream-json from first exec
    ├── turn-0002.jsonl   # raw stream-json from second exec
    └── ...
```

Each turn file is the complete stdout from one `Planner.Exec()` call, the same NDJSON streamed to the UI via `GET /api/planning/messages/stream`. This captures assistant text, tool calls, tool results, session IDs, usage metrics, and errors.

The handler's exec goroutine already reads all stdout via `io.ReadAll(tee)`. After the exec completes, write `rawStdout` to the next turn file. (Note: the task runner has a `Store.SaveTurnOutput` for board tasks; the planner would need its own equivalent on `ConversationStore`, since the two storage layers are separate.)

### 2. Dedicated Planning Config (originally: dedicated volume)

When the planner ran in a container, the proposal was to give it a planning-specific config volume (`claude-planning-config`) so task-worker container lifecycle could not invalidate planning sessions. **This is now obsolete.** The planner runs as a host process (`internal/planner/spec.go` `buildSpec`), so there is no container and no shared volume to contend for; the planner inherits the host's `~/.claude/` directory. If revived, this item becomes "ensure planning and task agents don't clobber each other's session state on the host", not a volume rename.

### 3. Context Reconstruction from Turns

When `--resume` fails with a stale session error, reconstruct context from stored turns instead of the lossy `BuildHistoryContext()`:

```go
func (s *ConversationStore) ReconstructContext(maxTokens int) string
```

The reconstruction algorithm:

1. **Load all turn files** in order from `turns/`
2. **Extract structured events** from each turn's NDJSON:
   - Assistant text blocks (`type: "assistant"`, `message.content[].type == "text"`)
   - Tool calls (`message.content[].type == "tool_use"`); extract tool name and key input (file path, command, pattern)
   - Tool results (`type: "user"`, `message.content[].type == "tool_result"`); extract truncated output
   - User messages (from `messages.jsonl`, keyed by turn number)
3. **Build a structured context prompt** with three levels of detail:
   - **Full fidelity** (most recent N turns): complete assistant text + tool call details
   - **Summary** (older turns): assistant text only, tool calls listed as `[Read internal/handler/planning.go]` one-liners
   - **Omitted** (oldest turns): just "... N earlier turns omitted ..."
4. **Token budget**: work backwards from the most recent turn, including as much detail as fits within `maxTokens` (default: 30000 tokens, ~120KB of text)

The reconstructed context is prepended to the new prompt as:

```
[Session recovered, previous conversation context follows]

=== Turn 1 (user) ===
Create a new spec for the auth system

=== Turn 1 (assistant) ===
I'll create a new spec file...
[Read specs/README.md]
[Write specs/identity/authentication.md]
Created specs/identity/authentication.md with...

=== Turn 2 (user) ===
Break it down into tasks

=== Turn 2 (assistant) ===
[Read specs/identity/authentication.md]
...
```

### 4. Graceful Session Loss UX

When a session is lost and recovered:

1. **Show a system message** in the chat: "Session was reset, context reconstructed from N previous turns"
2. **Don't retry silently.** The current auto-retry happens in the background and the user sees either nothing or a confusing "No response". Instead, show the recovery status.
3. **Allow manual clear.** The user can clear the thread to start truly fresh if the reconstructed context is unhelpful.

### 5. Prevent Unnecessary Session Loss

Reduce the frequency of session loss. With the host-process planner this is largely moot (no container to recreate, no fingerprint-keyed container name), but workspace-switch handling should still avoid discarding a live session unnecessarily, and the planner should reuse the same `~/.claude/` session directory across server restarts as long as the workspace is unchanged.

## Open Questions

1. **Token budget for reconstruction**: 30K tokens is a guess. Should this be configurable? Should it be a fraction of the model's context window?
2. **Turn file retention**: how many turns to keep? Older turns could be archived or deleted after N days.
3. **Concurrent read during write**: the exec goroutine writes the turn file after completion. If reconstruction runs during an active exec (session loss mid-conversation), it should skip the incomplete current turn.
4. **Threads interaction**: with planning chat threads, turns and reconstruction are per-thread (`threads/<id>/turns/`). This spec predates threads and assumes a single conversation root; revival must account for the thread layout.

## Status

Verified against the code on 2026-06-14. Marked **stale**: the baseline holds, but the proposed work did not ship and part of its premise is gone.

**What exists (baseline, treated as already-there by this spec's Problem section):**

- `internal/planner/conversation.go`: `SessionInfo`, `SaveSession`, `LoadSession`, and `BuildHistoryContext` (the lossy last-20-messages summary).
- `--resume` via the stored session ID (`planner.go` loads `LoadSession`).
- The stale-session fallback in `internal/handler/planning.go` (around the stale-session retry path) still calls `cs.BuildHistoryContext()`, the exact lossy behavior this spec set out to replace.

**What did NOT ship (this spec's five deliverables):**

1. **Per-turn NDJSON storage in the planning dir.** There is no `turns/` directory under `~/.wallfacer/planning/...` and no `SaveTurnOutput` on the planner's `ConversationStore`. The `SaveTurnOutput` symbols in the tree belong to the task `Store` (`internal/store/io.go`) and are unrelated to planning.
2. **`ReconstructContext`.** Does not exist anywhere in `internal/`.
3. **Dedicated `claude-planning-config` volume.** Does not exist, and is moot: the planner is a host process with no container (`internal/planner/spec.go` `buildSpec`), so the volume/container session-loss vectors in the Problem table no longer apply.
4. **Graceful session-loss UX.** No "session recovered" system message in the Vue planning store/panel; the retry remains silent.
5. **`UpdateWorkspaces` should-not-Stop.** Moot under host-process execution; there is no container to keep alive.

**Premise drift:** several of the loss vectors (container recreated on workspace switch, shared `claude-config` volume pruned, task-worker container clobbering the volume, container rebuilt with a different name) assumed a containerized planner. That model is gone. If this work is revived, re-scope around the host-process planner and the per-thread storage layout from the planning-chat-threads spec.

## Affects

- `internal/planner/conversation.go` (turn file storage, `ReconstructContext()`, `SaveTurnOutput()` for the planner)
- `internal/handler/planning.go` (write turn output after exec, reconstruction on stale session, recovery messaging)
- `frontend/src/stores/planning.ts` + `frontend/src/components/plan/PlanningChatPanel.vue` (show "session recovered" system message; the original `ui/js/planning-chat.js` is deleted)
