---
title: System Tray -- Health Polling and Dynamic State
status: archived
depends_on:
  - specs/local/desktop-app/task-03-tray-skeleton.md
affects: []
effort: medium
created: 2026-03-28
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---


# Task 4: System Tray — Health Polling and Dynamic State

## Goal

Add a 5-second poll loop to the tray that fetches health data from the server and updates the icon state, tooltip, and status count labels in the menu.

## What to do

1. Add a `poll()` method to `TrayManager` that calls `GET /api/debug/health` via a local HTTP client (hitting `http://localhost:<port>/api/debug/health`)
2. Start a 5-second ticker in `TrayManager.Start()` that calls `poll()` on each tick
3. Parse the health response to extract:
   - `tasks_by_status` map (backlog, in_progress, waiting, done, failed, committing)
   - `uptime_seconds`
4. Update the tray icon based on task state:
   - **Idle** (static icon): no in_progress and no committing tasks
   - **Active** (badge dot): 1+ in_progress or committing tasks
   - **Attention** (orange dot): 1+ waiting or failed tasks
   - Prepare three icon variants: `tray.png`, `tray-active.png`, `tray-attention.png`
5. Update the tooltip text: `Wallfacer — N running · M waiting · uptime Xh Ym` (or `Wallfacer — Idle`)
6. Add read-only status labels to the menu between "Open Dashboard" and "Quit":
   ```
   Open Dashboard
   ─────────────
   ● 2 In Progress     (bold when > 0)
     1 Waiting
     4 Backlog
   ─────────────
   Uptime: 2h 15m
   ─────────────
   Quit
   ```
7. Update these labels on each poll cycle
8. Stop the ticker in `TrayManager.Stop()`

## Tests

- `TestPollHealthResponse`: Mock an HTTP server returning a health JSON response, verify `poll()` correctly parses task counts and uptime
- `TestIconStateIdle`: tasks_by_status with all zeros → idle icon
- `TestIconStateActive`: in_progress=2, committing=1 → active icon
- `TestIconStateAttention`: waiting=1, failed=0 → attention icon
- `TestTooltipFormatting`: Verify tooltip string matches expected format for various states

## Boundaries

- Do NOT add cost display (that uses /api/stats, handled in Task 6)
- Do NOT add automation toggles yet (Task 5)
- Do NOT implement platform-specific icon behaviors yet (Task 7)
- The HTTP client should respect the server API key if configured (pass `Authorization: Bearer` header)

## Implementation notes

- **Systray library:** Continues using `fyne.io/systray` from Task 3 (Wails v2 has no public systray API).
- **Icon variants:** Generated programmatically — `tray-active.png` has a green (#22C55E) dot in the bottom-right corner, `tray-attention.png` has an orange (#F97316) dot. Both at 1x (22px) and 2x (44px).
- **ServerComponents.ServerAPIKey:** Added this field to `ServerComponents` so `RunDesktop` can pass the API key to the tray manager for authenticated health polling.
- **Poll timeout:** HTTP client uses a 3-second timeout to avoid blocking the tray if the server is slow.
- **Initial poll:** `poll()` is called immediately on startup, then every 5 seconds via ticker.
