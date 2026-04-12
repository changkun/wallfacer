---
title: Default mode resolution on app open
status: validated
depends_on: []
affects:
  - ui/js/spec-mode.js
  - ui/js/app-init.js
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Default mode resolution on app open

## Goal

Resolve the initial mode when Wallfacer opens, in priority order: saved user preference → Board if any task exists → Plan (chat-first) otherwise. Brand-new workspace groups always open in Plan regardless of saved preference. Saved preference updates only on explicit user action (nav click, keyboard shortcut), never on auto-transitions like the dispatch-complete toast's "View on Board" button.

## What to do

1. In `ui/js/spec-mode.js` (or wherever the mode-switching lives), add a `resolveDefaultMode()` function:
   ```js
   function resolveDefaultMode({ savedMode, taskCount, workspaceIsNew }) {
     if (workspaceIsNew) return "plan";
     if (savedMode === "board" || savedMode === "plan") return savedMode;
     if (taskCount > 0) return "board";
     return "plan";
   }
   ```
2. Saved mode storage key: `wallfacer-mode` in localStorage. Values: `"board"` or `"plan"`. Written only by:
   - Sidebar nav button click (explicit choice).
   - Keyboard shortcut `B` or `P`.
3. NOT written by:
   - The dispatch-complete toast's `[View on Board →]` button (see `plan-to-board-bridges.md`).
   - Any programmatic mode change (e.g., focusing a spec via URL hash).
4. Task count source: reuse the existing `/api/tasks` fetch that already runs on app init. Pass the resulting count into `resolveDefaultMode`.
5. Brand-new workspace detection: track whether the current workspace group was just activated via `PUT /api/workspaces`. If the last `PUT /api/workspaces` happened within this session and no user interaction has occurred since, treat as new. Store a session flag `workspaceIsNew` that clears on first substantive action (task created, message sent, spec focused).
6. In `ui/js/app-init.js` (or the main bootstrap), call `resolveDefaultMode` after fetching tasks and config, use the result to set the initial mode.

## Tests

- `ui/js/tests/default-mode.test.js` (new):
  - `TestResolve_SavedPreferenceWins`: `savedMode: "board"`, `taskCount: 0`, `workspaceIsNew: false` → `"board"`.
  - `TestResolve_NoSavedFallsBackToTaskCount`: `savedMode: null`, `taskCount: 5` → `"board"`.
  - `TestResolve_EmptyBoardNoSaved`: `savedMode: null`, `taskCount: 0` → `"plan"`.
  - `TestResolve_NewWorkspaceOverridesSaved`: `savedMode: "board"`, `taskCount: 5`, `workspaceIsNew: true` → `"plan"`.
  - `TestResolve_InvalidSavedValueIgnored`: `savedMode: "garbage"` → falls through to task count.
  - `TestSavedMode_WrittenOnExplicitClick`: simulate click on sidebar Plan button → `wallfacer-mode` is `"plan"`.
  - `TestSavedMode_WrittenOnKeyboardShortcut`: simulate `P` keydown → `wallfacer-mode` is `"plan"`.
  - `TestSavedMode_NotWrittenOnToastFollowThrough`: simulate the dispatch toast's "View on Board" click → `wallfacer-mode` is unchanged.

## Boundaries

- **Do NOT** persist `workspaceIsNew` across reloads — it's a session-only signal. On reload, saved preference applies.
- **Do NOT** add a user-facing "default mode" preference in Settings. The automatic resolution is the feature.
- **Do NOT** extend this to account for the Chat mode from `chat-mode.md` — that spec handles its own entries. Default mode here is a Board/Plan binary.
- **Do NOT** migrate any existing localStorage keys. This is a net-new key.
