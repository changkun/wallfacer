# Terminal: Multiple Sessions with Tabs

**Status:** Not started
**Date:** 2026-03-28

---

## Problem

Phase 1 of the host terminal ([host-terminal.md](../foundations/host-terminal.md)) provides a single shell session per browser tab. Users who need multiple shells (e.g., one for builds, one for logs, one for git) must open separate browser tabs. A tabbed terminal — like VS Code's — would allow multiple sessions within the same panel.

## Goal

Add a tab bar above the terminal panel supporting multiple concurrent shell sessions per browser tab.

## Design Sketch

- **Tab bar** above the xterm.js canvas inside `#status-bar-panel`.
- **Session registry** in the handler: `map[string]*terminalSession` keyed by session ID.
- **New WebSocket messages**: `create_session`, `switch_session`, `close_session`.
- Each tab shows a label (numbered, or named by cwd basename).
- Switching tabs detaches xterm from the current session's PTY output and attaches to the new one.
- Closing the last tab disconnects the WebSocket.

## Dependencies

- Requires host terminal (complete).
