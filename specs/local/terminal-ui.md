---
title: "Terminal UI — Full TUI Mode for Wallfacer"
status: drafted
track: local
depends_on: []
affects:
  - internal/tui/
  - internal/cli/tui.go
  - main.go
effort: xlarge
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Terminal UI — Full TUI Mode for Wallfacer

---

## Problem

Wallfacer currently requires either a web browser (`:8080`) or a native desktop app (Wails) to operate. This makes it unusable in environments where a graphical display is unavailable or impractical:

- **Remote servers** accessed via SSH (the most common development setup for many teams).
- **Headless CI/dev containers** where no browser is available.
- **tmux/screen sessions** where developers live full-time and context-switching to a browser is friction.
- **Resource-constrained machines** where a browser tab is expensive.
- **Terminal-native workflows** where developers prefer keyboard-driven tools over mouse-driven UIs.

Beyond environment constraints, many power users simply prefer terminal interfaces for task management tools. Tools like `lazygit`, `k9s`, and `btop` demonstrate that complex, real-time UIs work well in the terminal when designed thoughtfully.

---

## Current State (as of 2026-03-30)

- **Web UI**: Vanilla HTML/JS/Tailwind served from embedded `ui/` directory. Full task board, drag-and-drop, SSE live updates, log streaming, file explorer, terminal panel.
- **Desktop app**: Wails wrapper around the same web UI. macOS `.app`, Windows `.exe`, Linux binary.
- **CLI**: `wallfacer run` starts the server, `wallfacer status` prints a static board snapshot, `wallfacer status -watch` polls and reprints. `wallfacer exec` attaches to containers.
- **HTTP API**: All operations are exposed via REST. The web UI is a pure API client — no server-side rendering logic.
- **SSE streams**: `GET /api/tasks/stream` pushes task state changes, `GET /api/git/stream` pushes git status updates.
- **WebSocket terminal**: `GET /api/terminal/ws` provides interactive shell access.

The existing `wallfacer status -watch` is a minimal terminal view — it polls and reprints a text table. It has no interactivity, no task management, no log viewing.

---

## Design

### Architecture: API Client with Embedded Server

The TUI operates in two modes:

1. **Standalone mode** (default): The TUI binary starts the wallfacer server in-process (same as `wallfacer run`) and connects to it internally. The server does not open a browser. This is the zero-config experience: `wallfacer tui` gives you everything.

2. **Client mode**: The TUI connects to an already-running wallfacer server via `--addr`. Useful when the server runs as a daemon or on a remote host: `wallfacer tui --addr http://remote:8080`. Also useful for multiple views into the same server (e.g., one TUI per tmux pane for different concerns).

In both modes, the TUI is a pure API client — it uses the same HTTP endpoints and SSE streams as the web UI. No new server-side code is needed for core functionality.

### TUI Framework

Use [Bubble Tea](https://github.com/charmbracelet/bubbletea) (charmbracelet) as the TUI framework:

- **Elm architecture** (Model-Update-View) fits well with SSE-driven state updates.
- **Lip Gloss** for styling (borders, colors, alignment).
- **Bubbles** library provides reusable components (text input, viewport, list, table, spinner, paginator).
- Large ecosystem, actively maintained, widely adopted in Go CLI tools.
- Handles terminal resize, mouse events, and alternate screen buffer.

### Layout

The TUI uses a panel-based layout inspired by `lazygit` and `k9s`. The terminal is divided into regions that can be focused and resized:

```
+--[ Wallfacer ]--[ ws: ~/dev/myproject ]--[ 3 running | 2 waiting ]--+
|                              |                                       |
|  BACKLOG                     |  TASK DETAIL / LOGS                   |
|  +--------------------------+|                                       |
|  | #a1b2 Fix auth bug       ||  #c3d4 Add user dashboard            |
|  | #e5f6 Refactor DB layer  ||  Status: in_progress (turn 3)        |
|  +--------------------------+|  Sandbox: claude                      |
|                              |  Cost: $0.42 | Tokens: 12.3k in      |
|  IN PROGRESS                 |  ─────────────────────────────────    |
|  +--------------------------+|  [Logs] [Diff] [Events] [Oversight]  |
|  |>#c3d4 Add user dashboard ||                                      |
|  | #g7h8 Write tests        ||  > Analyzing codebase structure...    |
|  +--------------------------+|  > Reading internal/handler/auth.go   |
|                              |  > Creating internal/handler/users.go |
|  WAITING                     |  > Writing test file...               |
|  +--------------------------+|  > Running go test ./...              |
|  | #i9j0 Review PR feedback ||  > All tests pass.                   |
|  +--------------------------+|  > ▌                                  |
|                              |                                       |
|  DONE                        |                                       |
|  +--------------------------+|                                       |
|  | #k1l2 Setup CI pipeline  ||                                       |
|  +--------------------------+|                                       |
|                              |                                       |
+------------------------------+---------------------------------------+
| [n]ew  [s]tart  [c]ancel  [f]eedback  [d]one  [?]help  [q]uit      |
+---------------------------------------------------------------------+
```

**Regions:**

1. **Header bar** — Workspace name, running/waiting task counts, server connection status.
2. **Board panel** (left) — Task cards grouped by status column (Backlog, In Progress, Waiting, Done, Failed, Cancelled). Vertically scrollable. Focused task highlighted.
3. **Detail panel** (right) — Shows information about the selected task. Tabs switch between sub-views: Logs, Diff, Events, Oversight, Turn Usage.
4. **Status bar** (bottom) — Context-sensitive key hints. Changes based on the focused panel and selected task state.

### Navigation Model

Keyboard-driven with vim-style bindings as primary, arrow keys as fallback:

| Key | Action |
|-----|--------|
| `j` / `k` or `Up` / `Down` | Move selection within board panel |
| `h` / `l` or `Left` / `Right` | Switch focus between board and detail panel |
| `Tab` | Cycle through detail sub-views (Logs, Diff, Events, ...) |
| `1`-`5` | Jump to status column (1=Backlog, 2=In Progress, 3=Waiting, 4=Done, 5=Failed) |
| `Enter` | Open detail for selected task / confirm action |
| `n` | New task (opens prompt input) |
| `s` | Start selected backlog task (move to In Progress) |
| `c` | Cancel selected task |
| `f` | Submit feedback on waiting task |
| `d` | Mark waiting task as done |
| `r` | Resume failed task / retry |
| `y` | Sync task (rebase worktree) |
| `t` | Run test verification |
| `g` | Git operations menu (push, sync, rebase) |
| `w` | Workspace switcher |
| `/` | Search tasks |
| `?` | Help overlay (full keybinding reference) |
| `q` / `Ctrl+C` | Quit |
| `Ctrl+L` | Force redraw |

Mouse support (optional, off by default): click to select tasks, scroll in panels.

### Task Creation

Pressing `n` opens a multi-line text input at the bottom of the screen (similar to vim's command mode or lazygit's commit message editor):

```
+---------------------------------------------------------------------+
| New Task                                                             |
| Prompt: Fix the authentication bug in the login handler.            |
|         The session token is not being refreshed on re-login.       |
|                                                                      |
| Goal:   [optional, Tab to switch]                                   |
| Timeout: 30m   Sandbox: claude   Fresh start: no                    |
| [Enter] create  [Esc] cancel  [Tab] next field                     |
+---------------------------------------------------------------------+
```

For multi-line prompts, the input area expands. `Ctrl+Enter` or a dedicated key submits; `Esc` cancels.

### Feedback Input

When a task is in `waiting` state, pressing `f` opens a similar input area for feedback text. Pressing `d` marks it done (with a confirmation prompt).

### Log Streaming

The Logs sub-view in the detail panel connects to `GET /api/tasks/{id}/logs` via SSE and renders the output in a scrollable viewport:

- Auto-scroll follows new output (like `tail -f`).
- `Shift+G` jumps to bottom, `g g` jumps to top.
- `Page Up` / `Page Down` for fast scrolling.
- ANSI escape sequences are interpreted for colors (Bubble Tea's built-in support via Lip Gloss handles this).
- When the user scrolls up, auto-scroll pauses. A "new output" indicator appears at the bottom. Pressing `Shift+G` or `Enter` resumes auto-scroll.

### Diff View

The Diff sub-view shows `GET /api/tasks/{id}/diff` output with syntax-highlighted diff rendering:

- `+` lines in green, `-` lines in red, `@@` headers in cyan.
- Scrollable viewport with the same navigation as logs.
- File-level folding: headers show file paths, `Enter` toggles fold.

### Git Operations

Pressing `g` opens a floating menu:

```
  Git Operations
  ──────────────
  [p] Push workspace
  [s] Sync workspace
  [r] Rebase on main
  [b] Browse branches
  [Esc] close
```

Each action calls the corresponding `/api/git/` endpoint and shows the result inline.

### Workspace Switcher

Pressing `w` opens a workspace selection dialog. Lists available workspace groups from `GET /api/config`. Selecting one calls `PUT /api/workspaces` and refreshes the board.

### Search

Pressing `/` activates a search bar at the top. Typing filters tasks in real-time (calls `GET /api/tasks/search?q=...`). `Enter` selects the first match, `Esc` clears the search.

### Responsive Layout

The TUI adapts to terminal size:

- **Wide terminals** (>120 cols): Side-by-side board + detail layout as shown above.
- **Narrow terminals** (80-120 cols): Board panel takes full width. Pressing `Enter` on a task switches to a full-screen detail view. `Esc` returns to the board.
- **Minimum** (< 80 cols): Single-column list with abbreviated task info. Detail is always full-screen.

Terminal height affects how many tasks are visible per column. Columns with many tasks get scrollbars (visual indicators at the edges).

---

## SSE Integration

The TUI maintains persistent SSE connections for live updates:

1. **Task stream** (`GET /api/tasks/stream`): Receives task list deltas. On each event, the TUI model updates its task list and triggers a re-render. No polling.
2. **Git stream** (`GET /api/git/stream`): Receives git status updates for the header bar.
3. **Log stream** (`GET /api/tasks/{id}/logs`): Connected when viewing a specific task's logs. Disconnected when switching away.

SSE connections are managed by a background goroutine that feeds events into Bubble Tea's message channel via `tea.Cmd`. This keeps the update loop clean:

```go
func listenSSE(url string) tea.Cmd {
    return func() tea.Msg {
        // Connect to SSE endpoint, return first event as a tea.Msg
        // Re-invoke on next Update cycle for continuous streaming
    }
}
```

Connection failures are retried with exponential backoff. The header bar shows connection status (connected / reconnecting / disconnected).

---

## CLI Integration

### New Subcommand

```
wallfacer tui [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | (none) | Connect to existing server instead of starting embedded |
| `--no-mouse` | `false` | Disable mouse support |
| `--color` | `auto` | Color mode: `auto`, `256`, `truecolor`, `none` |

When `--addr` is not specified, the TUI starts the server in-process on an ephemeral port (no need to bind `:8080`). The server's HTTP listener is internal-only — no browser launch, no external access.

### Interaction with Existing Commands

- `wallfacer run` remains unchanged — starts server + opens browser.
- `wallfacer status` remains unchanged — static/watch text output.
- `wallfacer tui` is the new fully interactive terminal experience.
- `wallfacer desktop` remains unchanged — Wails native app.

---

## Data Model

No new data models are needed. The TUI is a pure client of existing APIs.

The TUI maintains local view state only:

```go
// internal/tui/model.go

type Model struct {
    // Connection
    serverURL   string
    apiKey      string
    connected   bool

    // Board state (from SSE)
    tasks       []store.Task
    gitStatus   map[string]gitutil.Status

    // View state
    focusedPanel  Panel      // board or detail
    selectedTask  int        // index in filtered task list
    activeColumn  TaskStatus // which status column is focused
    detailTab     DetailTab  // logs, diff, events, oversight, usage

    // Sub-models
    logViewer    viewport.Model
    diffViewer   viewport.Model
    taskInput    textinput.Model
    searchInput  textinput.Model
    feedbackInput textarea.Model

    // Layout
    width, height int
}
```

---

## Implementation Phases

### Phase 1 — Scaffold and board view

| File | Change |
|------|--------|
| `internal/tui/` (new package) | Package root |
| `internal/tui/model.go` | Root `Model` struct, `Init`, `Update`, `View` |
| `internal/tui/board.go` | Board panel: task list grouped by status, selection, scrolling |
| `internal/tui/styles.go` | Lip Gloss style definitions (colors, borders, highlights) |
| `internal/tui/keys.go` | Key bindings map |
| `internal/tui/client.go` | HTTP API client (fetch tasks, patch status, create task) |
| `internal/cli/tui.go` (new) | `wallfacer tui` subcommand wiring |
| `main.go` | Register `tui` subcommand |

**Deliverable:** Board panel renders tasks grouped by column. Navigation with `j`/`k`, column jumps with `1`-`5`. No detail panel yet. Reads task list via `GET /api/tasks` on startup.

**Effort:** Medium.

### Phase 2 — SSE live updates

| File | Change |
|------|--------|
| `internal/tui/sse.go` (new) | SSE client for task and git streams |
| `internal/tui/model.go` | Handle SSE messages in `Update` |
| `internal/tui/board.go` | Re-render board on state changes |
| `internal/tui/header.go` (new) | Header bar with workspace, counts, connection status |

**Deliverable:** Board updates in real-time. Tasks appearing/moving/completing are reflected without manual refresh.

**Effort:** Medium.

### Phase 3 — Detail panel and log streaming

| File | Change |
|------|--------|
| `internal/tui/detail.go` (new) | Detail panel with tab bar, task metadata display |
| `internal/tui/logs.go` (new) | Log viewer: SSE connection, ANSI rendering, auto-scroll |
| `internal/tui/layout.go` (new) | Responsive layout manager (wide vs narrow vs minimum) |
| `internal/tui/model.go` | Panel focus switching (`h`/`l`), tab cycling (`Tab`) |

**Deliverable:** Selecting a task shows its details. Logs tab streams live output. Layout adapts to terminal size.

**Effort:** Medium-High. Log streaming with ANSI rendering and auto-scroll is the most complex component.

### Phase 4 — Task actions (create, start, cancel, feedback, done)

| File | Change |
|------|--------|
| `internal/tui/input.go` (new) | Task creation form, feedback input, confirmation dialogs |
| `internal/tui/actions.go` (new) | API calls for task mutations (PATCH, POST, DELETE) |
| `internal/tui/model.go` | Wire action keybindings to input flows |
| `internal/tui/statusbar.go` (new) | Context-sensitive key hints |

**Deliverable:** Full task lifecycle from the TUI: create, start, cancel, feedback, mark done, resume, retry.

**Effort:** Medium.

### Phase 5 — Diff, events, oversight views

| File | Change |
|------|--------|
| `internal/tui/diff.go` (new) | Diff viewer with syntax highlighting and folding |
| `internal/tui/events.go` (new) | Event timeline viewer |
| `internal/tui/oversight.go` (new) | Oversight summary display |
| `internal/tui/usage.go` (new) | Turn usage / cost display |

**Deliverable:** All detail tabs are functional.

**Effort:** Medium.

### Phase 6 — Git operations and workspace switcher

| File | Change |
|------|--------|
| `internal/tui/git.go` (new) | Git operations floating menu |
| `internal/tui/workspace.go` (new) | Workspace selection dialog |
| `internal/tui/search.go` (new) | Search bar with live filtering |

**Deliverable:** Git push/sync/rebase, workspace switching, task search all work from TUI.

**Effort:** Low-Medium.

### Phase 7 — Standalone mode (embedded server)

| File | Change |
|------|--------|
| `internal/cli/tui.go` | Start server in-process on ephemeral port when `--addr` not given |
| `internal/cli/server.go` | Extract server setup into reusable function (used by both `run` and `tui`) |

**Deliverable:** `wallfacer tui` works with zero flags — starts server internally, no browser.

**Effort:** Low. Server startup logic already exists; this just reuses it without the browser-open step.

### Phase 8 — Tests, docs, polish

| File | Change |
|------|--------|
| `internal/tui/*_test.go` | Unit tests for model updates, key handling, layout calculations |
| `docs/guide/configuration.md` | Document `wallfacer tui` flags |
| `docs/guide/getting-started.md` | Mention TUI as an alternative to browser |
| `CLAUDE.md` | Add TUI subcommand to CLI usage section |

**Effort:** Low-Medium.

---

## Key Patterns Reused

| Pattern | Source | Reused For |
|---------|--------|------------|
| SSE client | `ui/js/app.js` (JS SSE handling) | Go SSE client for task/git streams |
| Task list rendering | `ui/js/board.js` | Board panel task grouping and display |
| Log streaming | `ui/js/taskLogs.js` | Log viewer with auto-scroll |
| Diff rendering | `ui/js/taskDiff.js` | Diff viewer with color highlighting |
| API client patterns | `ui/js/generated/routes.js` | Go HTTP client for all endpoints |
| Server startup | `internal/cli/server.go` | Embedded server for standalone mode |
| Status display | `internal/cli/status.go` | Base patterns for terminal task display |

---

## Considerations

### 1. ANSI rendering fidelity

Container logs contain raw ANSI escape sequences (colors, cursor movement, clearing). Bubble Tea's viewport renders these natively since it operates in an alternate screen buffer that understands ANSI. However, some sequences (like cursor positioning or screen clearing) from Claude Code's output may cause rendering artifacts. The log viewer should strip or neutralize cursor-positioning sequences while preserving color codes.

### 2. Terminal capabilities

Not all terminals support the same features:
- **True color** (16M colors): Most modern terminals. Detected via `$COLORTERM`.
- **256 colors**: Older terminals, some SSH clients. Fallback palette needed.
- **No color**: Pipe mode, `$NO_COLOR`, dumb terminals. Must be fully usable without color.
- **Mouse support**: Useful for clicking tasks but conflicts with terminal selection (copy-paste). Off by default, enable with flag.
- **Unicode**: Box-drawing characters, status icons. Fallback to ASCII for terminals that don't support Unicode (detected via locale).

The `--color` flag and runtime detection should handle these gracefully.

### 3. Large task boards

A board with 50+ tasks needs efficient rendering. The board panel should:
- Virtualize rendering (only draw visible rows).
- Collapse completed/cancelled columns by default (expand with `Enter`).
- Show count badges on collapsed columns: `Done (23)`.

### 4. Multi-line prompt editing

Terminal text input for task prompts is inherently limited compared to a browser textarea. Options:
- Use Bubble Tea's `textarea` bubble for multi-line editing within the TUI.
- Offer `$EDITOR` integration: pressing `e` in the prompt input opens the user's editor (vim, nano, etc.) with a temp file. On save+quit, the content is used as the prompt. This is the `git commit` pattern and is familiar to terminal users.

Both should be supported. The inline textarea for quick prompts, `$EDITOR` for complex multi-line prompts.

### 5. SSH and remote access

When connecting to a remote wallfacer server via `wallfacer tui --addr http://remote:8080`:
- **Authentication**: The `--api-key` flag or `WALLFACER_SERVER_API_KEY` env var provides the bearer token.
- **Latency**: SSE events may arrive with delay. The TUI should show "last updated" timestamps and handle reconnection gracefully.
- **Network interruption**: The SSE client should reconnect with backoff. During disconnection, the board shows a "disconnected" indicator and the last-known state.

### 6. Accessibility

- All actions must be keyboard-accessible (no mouse-only interactions).
- Color is never the sole indicator of state — status text labels accompany colored indicators.
- Screen reader compatibility is limited in TUI applications, but semantic structure (clear labels, logical tab order) helps.

### 7. Concurrent terminal usage

Users may run the TUI in one tmux pane while using `wallfacer exec` in another. The TUI must not interfere with other terminal sessions. Since it runs in an alternate screen buffer (Bubble Tea default), switching away and back should restore the display cleanly.

### 8. Startup time

The TUI should render the first frame within 200ms. This means:
- Fetch task list asynchronously after initial render (show spinner or "Loading..." while fetching).
- In standalone mode, start the server in a background goroutine and connect when ready.
- Cache nothing on disk — the server is the source of truth.

### 9. What this does NOT include (potential future extensions)

- **Embedded terminal** (shell inside the TUI): Running a PTY within a Bubble Tea app is possible but complex. Users can use tmux panes or `wallfacer exec` for shell access. Deferred.
- **Split-view multiple tasks**: Showing logs for two tasks side-by-side. Useful but adds significant layout complexity. Deferred.
- **File explorer**: Browsing workspace files in the TUI. The web UI's file explorer is mouse-oriented and doesn't translate well to terminal. Users have `ls`, `tree`, and their editor. Deferred.
- **Drag-and-drop reordering**: Not possible in a terminal. Task ordering is managed via `s` (start) to promote, or PATCH with position field.

---

## Dependencies

- **No new external Go dependencies beyond Bubble Tea ecosystem**: `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/lipgloss`, `github.com/charmbracelet/bubbles`. These are well-maintained, have no CGo, and compile on all platforms.
- **No new server-side changes** for core functionality. The TUI is a pure API client.
- **Existing SSE endpoints** must work with Go's `net/http` client (they do — SSE is just chunked HTTP responses).

---

## Migration & Backward Compatibility

- **Additive only**: New `tui` subcommand. No changes to existing `run`, `status`, `exec`, `desktop` commands.
- **No API changes**: The TUI consumes existing endpoints.
- **No UI changes**: The web UI is unaffected.
- **Go module**: New dependencies (charmbracelet) are added to `go.mod`. These are pure Go, no CGo, no platform restrictions.

---

## Open Questions

1. **Should `wallfacer tui` become the default when no subcommand is given?** Currently `wallfacer` with no args prints help. Making `tui` the default would give a zero-friction experience but changes existing behavior. Could be gated behind a config flag or introduced in a later release.

2. **Notification support?** When a task completes or needs feedback, the TUI could send a desktop notification (via `notify-send` on Linux, `osascript` on macOS, `toast` on Windows). Useful when the TUI is in a background tmux pane. Low effort but platform-specific.

3. **Theme customization?** The Lip Gloss styles could be configurable via a `~/.wallfacer/tui-theme.toml` file. Power users expect this from terminal tools, but it adds scope. Could ship with a sensible default and add theming later.

4. **Batch task creation?** The web UI supports batch task creation with dependency wiring. Replicating the full batch UI in a terminal is complex. Options: support a simpler single-task-at-a-time flow, or allow pasting/piping a JSON batch spec. The `$EDITOR` integration could open a YAML/JSON template for batch creation.

5. **Integration with `wallfacer status -watch`?** The existing watch mode is a simpler, non-interactive terminal view. Should it be deprecated in favor of `tui`, or kept as a lightweight alternative for monitoring without interactivity? Keeping both seems reasonable — `status -watch` for passive monitoring, `tui` for active management.
