---
title: Panels and Overlays
status: drafted
depends_on:
  - specs/shared/console-redesign/shell.md
affects:
  - frontend/src/components/CommandPalette.vue
  - frontend/src/components/WorkspacePicker.vue
  - frontend/src/components/WorkspaceEditModal.vue
  - frontend/src/components/WorkspaceRequired.vue
  - frontend/src/components/ConfirmDialog.vue
  - frontend/src/components/Toaster.vue
  - frontend/src/components/KeyboardShortcutsModal.vue
  - frontend/src/components/DeviceSignInModal.vue
  - frontend/src/components/TrashModal.vue
  - frontend/src/components/DockWorkspace.vue
  - frontend/src/components/TerminalPanel.vue
  - frontend/src/components/ExplorerPanel.vue
  - frontend/src/components/editor/EditorTabStrip.vue
  - frontend/src/components/editor/FileEditor.vue
  - frontend/src/styles/command-palette.css
  - frontend/src/styles/workspace-picker.css
  - frontend/src/styles/explorer.css
  - frontend/src/styles/dock.css
  - frontend/src/styles/modal.css
effort: large
created: 2026-09-05
updated: 2026-09-05
author: changkun
dispatched_task_id: null
---

# Panels and Overlays

## Overview

Everything that floats over or docks beside a page: the command palette, the
workspace picker and editor, confirm dialogs, toasts, the shortcuts and
device sign-in and trash modals, and the dock workspace with its terminal,
explorer and file editor. They share three shapes: a popover `.card`, a
centered dialog, and a docked panel. This child defines those three once and
moves every overlay onto them.

## Current State

- `CommandPalette.vue` (597 lines, `command-palette.css` 210 with 5
  `backdrop-filter`): input, grouped results, kbd hints.
- `WorkspacePicker.vue` (702 lines, 127 scoped, `workspace-picker.css` 459):
  folder browser, group composition, limits. `WorkspaceEditModal.vue`,
  `WorkspaceRequired.vue`.
- `ConfirmDialog.vue` (5 hex), `Toaster.vue` (2 hex),
  `KeyboardShortcutsModal.vue`, `DeviceSignInModal.vue` (3 hex),
  `TrashModal.vue`.
- `DockWorkspace.vue` + `dock.css` (163): regions, gutters, maximize;
  `TerminalPanel.vue` (xterm theme from `--terminal-*` tokens);
  `ExplorerPanel.vue` (19 scoped, `explorer.css` 195 with 3
  `backdrop-filter`); `EditorTabStrip.vue` (146 scoped), `FileEditor.vue`
  (89 scoped, CodeMirror theme from `data-theme`).
- `modal.css` overlay and `.modal-card` (the wide variant is owned by the
  task-detail child).

## Components

### Popover

`.pop`: `.card` with `--sh-pop`, radius `--r-xl`, `--bg-card`, max height and
internal scroll, `.rows` content. Used by the command palette (centered top,
640px, the input as a borderless `.field` in `.card-head`, results as `.rows`
with a group `.eyebrow`, selected row `--bg-sunk`, kbd hints `.pill-neutral`),
mention and select popovers from other children, and the toaster (bottom
right stack of `.pop` rows with a state dot and an `.icon-btn` dismiss).

### Dialog

`.dialog`: `--glass-dim` scrim without blur, `.card` centered at 440px
(confirm, shortcuts, device sign-in) or 720px (workspace picker and editor,
trash), radius `--r-main`, `--sh-pop`, `.card-head` title with `.icon-btn`
close, body, foot with `.btn` primary and `.btn.ghost` cancel, danger confirm
as `.btn.ghost.danger`. The workspace picker's folder browser is `.rows` with
chevrons; limits are `.rows` with `.field` numbers. The shortcuts modal is a
two-column `.rows` list with `.pill-neutral` keys. The trash modal lists
tasks as `.rows` with a Restore `.btn.sm.ghost`.

### Docked panel

`.dock-region` on `--bg-sunk` with a 1px `--rule` toward the editor; gutters
4px, `--accent-line` on hover; the maximized state fills the main card. Panel
headers (terminal tabs, explorer title) are `.tabs`. The explorer tree reuses
the plan child's tree row (34px, radius 12, `--bg-card` active). The terminal
reads the new `--terminal-*` tokens; the CodeMirror theme in `FileEditor`
maps to the ramp and `--bg-sunk` instead of one-dark. Editor tabs are `.tabs`
with a dirty dot in `--warn` and a close `.icon-btn` on hover.

## Testing Strategy

- Existing `stores/dock.test.ts`, `dialog.test.ts`, `toast.test.ts`,
  `editorTabs.test.ts`, `useDeviceSignIn.test.ts` stay. Add
  `components/CommandPalette.test.ts`: groups render, arrow keys move the
  selected row, Enter navigates; `components/ConfirmDialog.test.ts`: danger
  variant class.
- `tests/designSystem.test.ts`: all listed components and styles have no hex
  literal and no `backdrop-filter`; `FileEditor.vue` does not import
  `@codemirror/theme-one-dark`.
- `checks.mjs` scenes `palette` (opens on ⌘K, 640 wide, inside the viewport,
  first row selected), `picker` (existing scene kept, dialog 720), `dock`
  (toggle terminal, region on the bottom edge of the main card, gutter drag
  changes height, maximize fills the main card). Screenshots `palette`,
  `picker`, `terminal`, `explorer`, light and dark.
