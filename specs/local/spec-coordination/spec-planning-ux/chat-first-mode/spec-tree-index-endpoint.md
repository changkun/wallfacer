---
title: Add index field to spec tree endpoint for specs/README.md
status: validated
depends_on: []
affects:
  - internal/handler/
  - internal/spec/
  - internal/apicontract/
effort: medium
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Add index field to spec tree endpoint

## Goal

Make `specs/README.md` visible to the UI as a first-class entry alongside the spec tree. Extend `GET /api/specs/tree` to return `{tree, index}`, where `index` describes `specs/README.md` when one exists in the first mounted workspace (deterministic resolution). The spec-tree SSE stream must fire on index changes so the UI transitions layout states correctly.

## What to do

1. In `internal/spec/` add a small helper `ResolveIndex(workspaces []string) (*Index, error)`:
   ```go
   type Index struct {
       Path      string    // "specs/README.md" (relative to workspace root)
       Workspace string    // absolute workspace root
       Title     string    // first H1 in the file, fallback "Roadmap"
       Modified  time.Time // file mtime
   }
   ```
   Iterate `workspaces` in order; return the first match. Return `(nil, nil)` when none exists. Title extraction: scan the first ~200 lines for the first `^# ` line, strip the `#`, trim. Fallback string is `"Roadmap"`.
2. In `internal/handler/specs.go` (or wherever `GetSpecTree` lives), change the response body from the current `tree` array to `{tree, index}`:
   ```json
   {
     "tree": [ /* existing nodes */ ],
     "index": { "path": "specs/README.md", "workspace": "...", "title": "...", "modified": "2026-04-12T12:34:56Z" }
   }
   ```
   `index` is `null` when no README is found.
3. Update the SSE payload emitted by `SpecTreeStream` (same file) so every snapshot carries the `index` alongside `tree`. Ensure the file watcher that feeds the SSE includes `specs/README.md` in its watched set — the current watcher likely scopes to `*.md` under `specs/`, so this may already work; verify and extend if needed.
4. Update the API contract generator — no schema changes needed in `internal/apicontract/routes.go` itself (the route is the same), but regenerate `ui/js/generated/routes.js` and `docs/internals/api-contract.json` via `make api-contract`.
5. Frontend consumers: `ui/js/spec-explorer.js` currently expects the raw tree array. Change the fetch path to consume `{tree, index}`. Store `index` in module-scope state so the pinned Roadmap entry task (follow-up) can render from it. For THIS task, just plumb the data — rendering is the next task's scope.

## Tests

- `internal/spec/index_test.go` (new):
  - `TestResolveIndex_NoReadme`: empty workspace list → `nil, nil`.
  - `TestResolveIndex_WorkspaceWithout`: workspace exists but no `specs/README.md` → `nil, nil`.
  - `TestResolveIndex_FirstMatchWins`: two workspaces, both have README, first one returned.
  - `TestResolveIndex_TitleFromH1`: `# My Roadmap\n...` → title `"My Roadmap"`.
  - `TestResolveIndex_TitleFallback`: no H1 → title `"Roadmap"`.
  - `TestResolveIndex_MtimeSet`: returned `Modified` matches file mtime.
- `internal/handler/specs_test.go` (extend):
  - `TestGetSpecTree_ReturnsIndexField`: response shape includes `{tree, index}`.
  - `TestGetSpecTree_IndexNullWhenMissing`: no README → `index: null`.
  - `TestSpecTreeStream_IncludesIndex`: SSE snapshot includes the index; changing the README file fires a new snapshot.

## Boundaries

- **Do NOT** change the shape of individual `tree` nodes. Only the top-level response gains an `index` field.
- **Do NOT** render the pinned Roadmap entry in the explorer — that's the next task (`explorer-roadmap-entry.md`).
- **Do NOT** add a writable endpoint for `specs/README.md`. Users edit it via the file explorer or agent tools; this spec only exposes read access through the tree endpoint.
- **Do NOT** merge indexes from multiple workspaces — first-wins is explicit per the parent spec's non-goals.
