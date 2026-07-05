# Chat

Chat is the dedicated conversational surface, reachable from the sidebar at `/chat`. It runs the same workspace-scoped agent session engine that powers the chat panel in [Plan](plan.md): the agent reads the workspace code, writes spec files, and drives the spec lifecycle through slash commands. Chat is the place for open-ended exploration and planning conversations; Plan wraps the identical engine in the spec tree and focused-spec context. See [Concepts](concepts.md) for how planning conversations relate to specs and tasks.

## Sessions and threads

A session sidebar lists every conversation thread for the active workspace:

- **New chat** opens a draft thread. The draft exists only in the browser until the first message is sent, at which point the server creates the real thread; abandoned drafts leave nothing behind.
- Rename a thread inline from its row.
- **Archive** hides a thread from the list while keeping its files on disk; the **Archived (N)** section restores or permanently deletes archived threads.
- A spinner marks the thread with an in-flight agent turn; an unread dot marks background threads that finished while another was active.

Each thread keeps its own agent session and message history. Threads persist on disk under `~/.wallfacer/agent-sessions/<fingerprint>/threads/`, keyed by the active workspace paths, so reloading the page or restarting the server restores the full thread list. The agent process exits between turns; continuity comes from session resume, with history replay as a fallback when a session has expired.

Only one agent turn runs at a time across all threads (they share a single agent runtime). Messages sent to a busy or background thread queue locally, remain editable or removable while queued, and drain oldest-first as turns complete.

## Sending messages and streaming

Type in the composer and press **Enter** to send (**Shift+Enter** for a newline). The arrow next to the send button switches to **Cmd+Enter to send** mode; the preference persists per browser. Responses stream in as the agent produces them, with tool activity (file reads, commands, writes) collapsed under each response. The send button becomes a stop button during streaming; clicking it interrupts the turn while preserving everything streamed so far. Reloading the page mid-turn reattaches to the live stream automatically.

## Quick actions

An empty conversation offers three quick-action chips that pre-fill the composer:

- **Draft a spec** inserts `/create `
- **Break down** inserts `/break-down `
- **Dispatch** inserts `/dispatch`

## Mentions

Type `@` to autocomplete workspace file paths. Mentioned files are attached as context for the agent's next turn. The same mention syntax works in the board's feedback boxes.

## Slash commands

Type `/` for an autocomplete menu of the twelve built-in commands. Each expands server-side into a structured prompt for the agent:

| Command | Description |
|---|---|
| `/summarize [words]` | Produce a structured summary of the focused spec |
| `/create <title>` | Create a new spec file with proper frontmatter |
| `/refine [feedback]` | Update the spec against the current codebase state |
| `/validate` | Check the focused spec against document model rules |
| `/impact` | Analyze what code and specs would be affected |
| `/status <state>` | Update the focused spec's lifecycle status |
| `/break-down [design\|tasks]` | Decompose the focused spec into sub-specs or tasks |
| `/review-breakdown` | Validate a task breakdown for correctness |
| `/dispatch` | Dispatch the focused spec to the task board |
| `/review-impl [commit-range]` | Review implementation against the spec's criteria |
| `/diff [commit-range]` | Compare completed implementation against spec |
| `/wrapup` | Finalize a completed spec with outcome and status |

Most commands operate on the *focused spec*. Chat has no spec tree, so there is no way to focus a spec from this surface; `/create` works anywhere, while the spec-scoped commands are most useful in [Plan](plan.md), where clicking a spec in the explorer sets the focus. The command set is identical on both surfaces, served from the same registry.

## Relationship to Plan

Chat and Plan share one engine: the same threads, the same agent, the same commands. The differences are chrome and context:

- Plan adds the spec explorer and focused view, so slash commands have a target and the agent's system prompt carries the spec-tree context (an empty tree switches the agent into a bootstrap variant that encourages drafting a first spec; the variant is selected per turn on both surfaces).
- Chat shows a per-thread usage rollup in the header; Plan's panel instead offers a **Clear conversation** button that discards the visible history while keeping the underlying session.
- Plan presents threads as horizontal tabs; Chat uses the session sidebar.

## Undo

Every assistant response that produced a planning round (spec files created or edited) carries an **undo** button, enabled for the thread's most recent round. Undo runs `git revert` against that round's commit: a forward revert commit is created rather than rewriting history, so both the original and the revert stay in the log. Dirty edits are stashed across the revert, and a revert conflict aborts cleanly with an error. Undo is scoped per thread.

## Session cost

The conversation header shows a live rollup for the active thread: rounds, input and output tokens, cache-hit rate, and accumulated cost, with a detailed breakdown on hover. Workspace-wide agent-session cost reporting lives on the Analytics page, where the default reporting period is seeded from `WALLFACER_AGENT_SESSION_WINDOW_DAYS` (default 30 days; `0` means all time). See [Oversight](oversight.md) for analytics and [Configuration](configuration.md) for the variable.

## See also

- [Plan](plan.md): the spec-first surface built on the same engine
- [Board](board.md): where dispatched specs execute as tasks
- [Whiteboard](whiteboard.md): free-form sketching alongside conversations
- [Oversight](oversight.md): usage, token, and cost analytics
- [Plan Mode internals](../internals/plan-mode.md): session runtime, streaming protocol, and undo plumbing
