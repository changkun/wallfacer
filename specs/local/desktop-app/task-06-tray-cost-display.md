---
title: System Tray -- Cost and Stats Display
status: complete
depends_on:
  - specs/local/desktop-app/task-04-tray-health-polling.md
affects: []
effort: small
created: 2026-03-28
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 6: System Tray — Cost and Stats Display

## Goal

Add today's cost and total cost labels to the tray menu by polling `GET /api/stats` on a slower 30-second cycle.

## What to do

1. Add a separate 30-second poll for `GET /api/stats` in `TrayManager`
2. Extract from the stats response:
   - `total_cost_usd` for the cumulative total
   - Today's cost from `daily_usage` array (find the entry matching today's date)
3. Add a cost/stats section to the menu:
   ```
   ─────────────
   Today: $3.42 · Total: $156
   Uptime: 2h 15m
   ─────────────
   ```
4. Format costs with 2 decimal places and `$` prefix
5. Update the tooltip to include today's cost: `Wallfacer — 2 running · 1 waiting · $3.42 today`
6. If the stats endpoint returns an error, show `—` for cost values

## Tests

- `TestParseStatsResponse`: Mock stats response with daily_usage, verify today's cost is correctly extracted
- `TestCostFormatting`: Verify `0.5` → `$0.50`, `1234.567` → `$1234.57`, `0` → `$0.00`
- `TestStatsErrorFallback`: Mock a failing stats call, verify cost labels show `—`
- `TestTooltipWithCost`: Verify tooltip includes cost when stats are available

## Boundaries

- Do NOT add per-workspace cost breakdowns to the tray menu
- Do NOT add clickable items in the cost section — these are read-only labels
- Keep the 30-second poll interval — stats computation is heavier than health
