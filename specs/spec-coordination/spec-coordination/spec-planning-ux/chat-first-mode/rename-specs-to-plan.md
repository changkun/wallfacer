---
title: Rename "Specs" mode to "Plan" in the UI
status: complete
depends_on: []
affects:
  - ui/partials/sidebar.html
  - ui/js/spec-mode.js
  - ui/js/api.js
  - ui/js/events.js
  - ui/partials/keyboard-shortcuts-modal.html
  - docs/guide/designing-specs.md
  - docs/guide/exploring-ideas.md
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Rename "Specs" mode to "Plan"

## Goal

Change the user-facing label from "Specs" to "Plan" across the sidebar nav, keyboard shortcut, and hash deeplink. Internal symbols (IDs, CSS classes, JS state variable names) stay unchanged — this is a label-only change.

## What to do

1. In `ui/partials/sidebar.html`, find the nav button labelled "Specs" and change its visible text to `Plan`. Keep the element's ID (`sidebar-nav-spec` or equivalent) as-is.
2. In `ui/js/keyboard-shortcuts.js` (or wherever key bindings live), rebind `S` → `P` for the mode-switch shortcut. Remove the `S` binding entirely — no deprecation alias.
3. In `ui/partials/keyboard-shortcuts-modal.html`, update the help row: `S — Switch to Specs mode` → `P — Switch to Plan mode`.
4. In `ui/js/hash-deeplink.js`:
   - Parser: accept both `#spec/<path>` and `#plan/<path>` on incoming URLs. Same decoder applied to either.
   - Writer: always emit `#plan/<path>` for new deep links.
5. Any in-UI string mentioning "Specs mode" / "the Specs pane" in tooltips or help text → "Plan mode" / "the Plan pane". Run `rg -n "Specs mode|specs mode" ui/ --glob '!js/generated/'` to find them all.
6. Update `docs/guide/designing-specs.md` and `docs/guide/exploring-ideas.md` with a one-line preface: `Plan mode (formerly Specs) is where you …`

## Tests

- `ui/js/tests/spec-mode-deeplink.test.js` (extend):
  - `TestHashDeeplink_AcceptsLegacySpecForm`: `#spec/specs/local/foo.md` parses identically to `#plan/specs/local/foo.md`.
  - `TestHashDeeplink_WritesPlanForm`: programmatic navigation to focus a spec produces `#plan/<path>` in the URL.
- `ui/js/tests/keyboard-shortcuts.test.js` (or equivalent):
  - `TestShortcut_P_TogglesPlanMode`: pressing `P` in Board mode switches to Plan.
  - `TestShortcut_P_Reverses`: pressing `P` in Plan switches to Board.
  - `TestShortcut_S_NoOp`: pressing `S` in Board mode produces no mode change.

## Boundaries

- **Do NOT** rename internal identifiers: `specModeState`, `spec-mode.js`, `spec-chat-*` CSS classes, `sidebar-nav-spec`, `spec-focused-body`, etc. Full code rename is a separate, larger refactor.
- **Do NOT** touch API routes, backend handler names, or `specs/` directory layout.
- **Do NOT** add a deprecation toast or console warning for the `S` shortcut — clean cutover per the parent spec's non-goal list.
- **Do NOT** change `#spec/` behaviour on write — this is only about the reading path accepting both, while writes go to `#plan/`.

## Implementation notes

- The spec referenced `ui/js/hash-deeplink.js` and `ui/js/keyboard-shortcuts.js` as the homes for deep-link parsing and the mode-switch shortcut, but those concerns actually live in `ui/js/spec-mode.js` + `ui/js/api.js` (deep-link read/write) and `ui/js/events.js` (global keyboard shortcut). The edits were applied to the actual files; `ui/js/keyboard-shortcuts.js` is only the modal controller.
- The keyboard-shortcuts modal did not previously list the mode-switch shortcut at all, so the spec's directive to "update" the row was implemented as adding a new `p — Switch to Plan mode` row under the Global section. The legacy `s` row never existed there.
- Tests were added across `ui/js/tests/spec-mode-deeplink.test.js`, `ui/js/tests/events.test.js`, and `ui/js/tests/api-coverage.test.js` (rather than a single `keyboard-shortcuts.test.js`) because the affected code is split across `events.js` (shortcut), `spec-mode.js` (writer / mode-exit clear), and `api.js` (initial-load parser).
