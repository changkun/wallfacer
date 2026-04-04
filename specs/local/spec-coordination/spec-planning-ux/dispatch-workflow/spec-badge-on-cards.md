---
title: Spec Badge on Task Cards
status: complete
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/dispatch-workflow/task-spec-source-field.md
affects:
  - ui/js/render.js
  - ui/css/
effort: small
created: 2026-04-04
updated: 2026-04-04
author: changkun
dispatched_task_id: null
---

# Spec Badge on Task Cards

## Goal

Render a small badge on task cards that were dispatched from a spec. The badge shows the spec name and clicking it navigates to spec mode with that spec focused. This provides visual indication that a task is spec-driven and a quick way to trace back to the source spec.

## What to do

1. In `ui/js/render.js`, in the `updateCard()` function (around line 948 where other badges are rendered), add a spec badge:
   - Check if `t.spec_source_path` is truthy
   - If so, render a badge: `<span class="badge badge-spec" data-spec-path="${escapeHtml(t.spec_source_path)}" title="From spec: ${escapeHtml(t.spec_source_path)}">${specName}</span>`
   - `specName` = last segment of the path without `.md` extension (e.g., `specs/local/foo.md` → `foo`)
   - Place the badge after the dependency badge and before the scheduled badge

2. Add click handler for the spec badge (in the event delegation section or directly on the badge):
   - On click, switch to spec mode and focus the spec: call `switchToSpecMode()` and `focusSpec(specPath, workspace)` from `spec-mode.js`
   - Prevent the card's default click handler (opening the task modal) from firing

3. Update `_cardFingerprint()` (around line 831) to include `t.spec_source_path` so the card re-renders when the field changes.

4. Add minimal CSS styling for `.badge-spec` — use a distinct color (e.g., purple/indigo) to differentiate from other badge types. Follow existing badge styling patterns.

## Tests

- Frontend test: verify `updateCard()` renders spec badge when `spec_source_path` is set, and does not render it when empty

## Boundaries

- Do NOT add spec badge to the task modal (separate task)
- Do NOT modify the Task model or API
- Do NOT add any backend changes
