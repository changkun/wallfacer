---
title: Auto-create specs/README.md on first scaffold and append rows thereafter
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/chat-first-mode/spec-new-directive-parser.md
affects:
  - internal/spec/
  - internal/handler/planning.go
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Auto-create specs/README.md on first scaffold

## Goal

When a `/spec-new` directive successfully scaffolds a spec, ensure `specs/README.md` exists in the same workspace. If it doesn't, create a minimal one listing the new spec in the appropriate track table. If it does, append a row (or add a new track section) without modifying user-authored content outside the tables.

## What to do

1. Add `internal/spec/readme.go`:
   ```go
   // EnsureReadme guarantees specs/README.md exists in the given workspace
   // and references the newly-created spec. Creates the file with a minimal
   // template if missing; otherwise appends a row to the track table
   // matching the spec's track (first path segment under specs/).
   func EnsureReadme(workspace string, newSpec Meta) error

   type Meta struct {
       Path    string  // e.g. "specs/local/auth-refactor.md"
       Title   string
       Status  Status
       Summary string  // one-line summary; may be empty ("(agent will fill this in)")
   }
   ```
2. Implementation outline:
   - Resolve track from `filepath.Dir(newSpec.Path)`: the first segment under `specs/` (e.g., `local`, `foundations`).
   - Readme path: `filepath.Join(workspace, "specs", "README.md")`.
   - If file doesn't exist, write the minimal template from the parent spec with the new spec's row under the right track heading.
   - If file exists:
     - Parse it line-by-line looking for a heading `## <Track Display Name>` (`Local Product`, `Foundations`, etc.) followed by a markdown table.
     - If the table exists, append a new row.
     - If the heading exists but no table, create a new table beneath it.
     - If neither exists, append a new `## <Track Display Name>` section at the bottom of the file with a fresh table.
   - Writes are atomic (temp file + rename).
3. In `internal/handler/planning.go`, after each successful `spec.Scaffold` call in the directive-processing loop, call `spec.EnsureReadme(workspace, newSpec)`. Errors are logged and surfaced as system bubbles but do NOT abort the turn.
4. The agent's system prompt (from `agent-system-prompts.md`) is extended with a closing sentence: *"If your `/spec-new` directive creates the first spec in the repo, also provide a brief summary sentence in your response body; the server will patch it into `specs/README.md` for you."* The server extracts the first sentence of the directive's body to use as the `Summary` field.

## Tests

- `internal/spec/readme_test.go` (new):
  - `TestEnsureReadme_CreatesWhenMissing`: workspace has no README → template is written with the new spec under `## Local Product`.
  - `TestEnsureReadme_AppendsWhenExists`: existing README with `## Local Product` table → new row appended at the end of that table.
  - `TestEnsureReadme_PreservesUserContent`: README has custom paragraphs before, between, and after the track tables — all preserved byte-for-byte. Only the matching track table is modified.
  - `TestEnsureReadme_AddsNewTrackSection`: new spec in `specs/foundations/...` when the README has no `## Foundations` heading → a new section is appended to the bottom.
  - `TestEnsureReadme_AtomicWrite`: simulated write failure mid-rename leaves the original file intact.
  - `TestEnsureReadme_TrackDisplayNames`: `local` → "Local Product", `foundations` → "Foundations", `cloud` → "Cloud Platform", `shared` → "Shared Design". Unknown tracks use title-cased directory name.
- `internal/handler/planning_test.go` (extend):
  - `TestSendPlanningMessage_FirstScaffold_CreatesReadme`: directive + body on empty repo → both the spec file AND `specs/README.md` exist, README references the spec.
  - `TestSendPlanningMessage_SecondScaffold_AppendsRow`: two directives in sequence (across turns) → README has two rows in the table, user's headers/paragraphs untouched.

## Boundaries

- **Do NOT** modify `specs/README.md` outside the track tables. User-authored prose, images, or non-table content is sacred.
- **Do NOT** sort or re-order existing rows in a track table. Only append.
- **Do NOT** touch this logic when a `/spec-new` fails — README update only on successful scaffold.
- **Do NOT** add a UI surface for editing the README auto-update behaviour. It's implicit; users who don't want it can `git revert` or edit the README directly.
- **Do NOT** create `specs/README.md` on any path other than post-successful-scaffold. The parent spec explicitly forbids rewriting an existing README.
