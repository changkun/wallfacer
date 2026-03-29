---
title: System Tray -- Automation Toggles
status: complete
track: local
depends_on:
  - specs/local/desktop-app/task-04-tray-health-polling.md
affects: []
effort: medium
created: 2026-03-28
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 5: System Tray — Automation Toggles

## Goal

Add checkable menu items for the automation flags (autopilot, auto-test, auto-submit, auto-sync) that read from `GET /api/config` and toggle via `PUT /api/config`.

## What to do

1. Add a config poll to `TrayManager` — fetch `GET /api/config` on the same 5-second cycle as health (or piggyback on the health poll)
2. Parse the config response to extract: `autopilot`, `autotest`, `autosubmit`, `autosync` booleans
3. Add a new menu section between the status counts and the uptime/cost section:
   ```
   ─────────────
   ✓ Autopilot
   ✓ Auto-test
     Auto-submit    (unchecked = off)
     Auto-sync
   ─────────────
   ```
4. Each item is a checkable menu item:
   - Checked state reflects the current config value
   - Clicking sends `PUT /api/config` with only the toggled field (e.g., `{"autopilot": false}`)
   - After the PUT succeeds, refresh the menu check state
   - If the PUT fails, log the error and don't change the check state
5. Update check states on each config poll cycle (handles external changes via the web UI)

## Tests

- `TestParseConfigToggles`: Mock config response, verify all four toggle states are correctly extracted
- `TestToggleSendsCorrectPayload`: Mock HTTP server, trigger a toggle, verify the PUT request body contains only the toggled field with the inverted value
- `TestToggleFailurePreservesState`: Mock a failing PUT, verify the menu item retains its original state

## Boundaries

- Do NOT add new config fields or API endpoints — use existing `GET/PUT /api/config`
- Do NOT add ideation toggle or other config items not in the spec menu
- Do NOT modify the web UI's config handling
