---
title: Panels and Overlays
status: complete
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

## Outcome

**What shipped** (`6d45e5ac`, `a47db930`, `fdc3daad`, `53ebaac8`). `modal.css`
defines the two floating shapes beside the sheet: `.pop` (a lifted card with
`--sh-pop`, radius `--r-xl`) and `.dialog` (centred, 440 wide, `.dialog--wide`
720, with `.dialog-head`, `.dialog-body` and `.dialog-foot`). The command
palette is a 640px `.pop` at the top of the viewport: a borderless field with
an `esc` key in the head, sections under `.eyebrow` titles, rows at the row
radius with the selected one on `--bg-sunk`, task actions as small ghost
buttons and a key legend in the foot. Toasts are `.pop` rows with a tone dot.
Confirm, keyboard shortcuts and device sign-in are 440 dialogs; the trash and
the workspace picker and editor are 720 dialogs of `.rows` cards. The folder
browser the picker and editor duplicated is one `FolderBrowser.vue` fed the
parent's `useFolderBrowser` state. The dock regions sit on `--bg-sunk` with a
hairline toward the editor and accent gutters; the terminal tab bar is the
underline tab strip (its styles had been lost with the status bar); the
explorer reuses the plan tree row; editor tabs are underline tabs with a warn
dot for dirty files; `FileEditor` reads `lib/editorTheme.ts`, a CodeMirror
theme and highlight style on the tokens, and `@codemirror/theme-one-dark` is
gone. `lib/statusPill.ts` is the one status to pill map for the sheet, the
palette, the trash and the explorer. Tests: `CommandPalette.test.ts`,
`ConfirmDialog.test.ts`, `editorTheme.test.ts`, `statusPill.test.ts`; the
guard covers every listed file. Scenes `palette` and `dock` are new, `picker`
asserts the 720 width, and every scene now runs in its own browser context.
Snapshots gain `picker`, `terminal` and `explorer`.

**Decisions made during implementation.**
- The kbd hints are `kbd.key` from the primitives rather than `.pill-neutral`:
  a key cap is a glyph, not a state.
- The confirm dialog keeps no head when the request carries no title; the
  message is then the first thing in the card.
- The scene runner isolates every scene in a fresh browser context. The dock
  scene had failed only after the chat scene, whose persisted popup covered
  the terminal controls. Isolation is the root fix rather than closing that
  popup.
- The `e` shortcut is guarded by focus, so the explorer snapshot clicks the
  collapsed explorer rail instead.

**Deviations from the spec.** `WorkspaceEditModal` stays at the wide dialog
with cards of rows, as specified, but the picker's list view keeps its rows
in a card rather than bare `.rows` so it reads like the editor beside it.

**Surprises.** `frontend/package-lock.json` is stale since the switch to bun
and still pins latere-ui 1.9.12; running `npm` against it downgraded
`node_modules/latere-ui` and broke the typecheck until `bun install` restored
it. The lockfile is a leftover for a follow-up.

**Follow-ups.** Delete `frontend/package-lock.json` in a housekeeping commit.
