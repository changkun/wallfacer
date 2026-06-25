---
title: "Archival: relocate archived specs to specs/.archive/"
status: complete
depends_on:
  - specs/spec-coordination/spec-coordination/spec-archival/archive-api.md
affects:
  - internal/spec/tree.go
  - internal/spec/archive.go
  - internal/handler/specs.go
  - internal/handler/specs_dispatch.go
  - internal/cli/spec.go
created: 2026-06-25
updated: 2026-06-25
author: changkun
dispatched_task_id: null
effort: large
---

# Archival: Relocate Archived Specs to `specs/.archive/`

## Goal

Archived specs currently stay at their original path with only a `status:
archived` frontmatter flag. When an agent explores the codebase, the archived
files sit interleaved with live specs and read as current work. Move every
archived spec into a parallel `specs/.archive/` tree that mirrors the original
structure, so the live `specs/` tree contains only live specs — while the
explorer and every spec reference keep working exactly as before.

## The crux: physical path vs logical path

Every reference in the system uses the **logical** path: `depends_on`,
`affects`, `dispatched_task_id` linkage, the spec tree keys, the focused-view
route, spec-comments anchoring. If an archived spec moves to
`specs/.archive/local/foo.md`, none of those references may change.

So we separate two notions:

- **Logical path** — `specs/local/foo.md`. The tree key. What every reference
  and the UI use. Unchanged by archival.
- **Physical path** — where the bytes live on disk: `specs/local/foo.md` when
  live, `specs/.archive/local/foo.md` when archived.

The mapping inserts/removes a single `.archive` segment right after `specs/`:

```
logical:  specs/local/foo.md
physical: specs/.archive/local/foo.md   (archived)
```

A spec's archived-ness is determined by its physical location (under
`.archive/`); the `status: archived` frontmatter stays as the redundant,
authoritative lifecycle marker so validation and the model are unchanged.

## BuildTree

`scanDir` (internal/spec/tree.go) currently walks `specs/` recursively. Changes:

1. **Skip `.archive/` in the normal walk.** When scanning the top-level
   `specs/` dir, do not recurse into the `.archive` directory.
2. **Second pass over `specs/.archive/`.** Walk it separately. For each spec
   file at physical `specs/.archive/<rel>`, add a node keyed by the **logical**
   path `specs/<rel>` (strip the `.archive/` segment). Force
   `status: archived` on the node regardless of frontmatter, and record the
   physical path so reads/writes resolve.
3. **Parent attachment.** An archived subtree attaches under its logical parent.
   Archiving a whole subtree (`foo.md` + `foo/`) moves it as a unit, so the
   archived nodes form a self-consistent subtree under the logical parent key.
4. **Collision guard.** If both `specs/local/foo.md` and
   `specs/.archive/local/foo.md` exist (should never happen — archival moves,
   not copies), prefer the live one and emit a `tree.Errs` warning.

The `Spec` model gains a derived physical-path field (not from YAML), analogous
to `Path` (logical). `Path` stays the logical key; a new `PhysicalPath` (or a
`tree`-level resolver) gives callers the on-disk location.

## Path resolution

A single helper pair in `internal/spec`:

```go
// ArchivePath maps a logical spec path to its physical location under .archive/.
func ArchivePath(logical string) string   // specs/local/foo.md -> specs/.archive/local/foo.md
// LogicalPath maps a physical .archive path back to its logical path.
func LogicalPath(physical string) string   // inverse; no-op if not under .archive/
```

Callers that resolve a spec file to an absolute path
(`findSpecFile`, the content-read endpoint, archive/unarchive) consult the
physical location: try `specs/<rel>` first, then `specs/.archive/<rel>`.
`findSpecFile` already searches workspaces; extend it to also probe the
`.archive/` location so an archived spec resolves by its logical path.

## Archive action (move, not just flag)

`ArchiveSpec` (internal/handler/specs.go) today flips `status` on the primary +
descendants and commits. New behavior:

1. Collect the archive targets as today (primary spec + companion-dir subtree).
2. `git mv` the primary `foo.md` and its companion directory `foo/` from their
   live location into `specs/.archive/<same rel>`, creating intermediate
   `.archive/` directories as needed. One `git mv` per top-level moved entry.
3. Flip `status: archived` on every moved spec (the bytes are already at the
   new physical path; update frontmatter in place there).
4. Single commit (`<path>: archive` subject, unchanged) capturing the move +
   the status writes, so `git revert` reverses both.
5. Non-git workspaces: fall back to `os.Rename` + frontmatter writes, no commit.

Cascade is naturally handled by moving the whole subtree directory.

## Unarchive action

`UnarchiveSpec` today reverts the archive commit (or falls back to a single
status flip). With moves:

- The **`git revert`** path already reverses the `git mv` + status writes
  together — keep it as the primary path.
- The **fallback** path: `git mv` the spec (and companion dir) back from
  `specs/.archive/<rel>` to `specs/<rel>`, flip `status: drafted`, commit.

## Reading an archived spec

The focused view loads spec content by logical path through the file/spec read
endpoint. That resolution must fall through to the `.archive/` physical
location when the live path is absent, so opening an archived spec (to view or
unarchive) still works. UI code is unchanged — it only ever passes logical
paths.

## Migration (runs last)

A one-time relocation of the specs already archived in place:

- Add a `wallfacer spec migrate-archive [--specs-dir specs]` subcommand (next to
  `spec validate`) that finds every `status: archived` spec **not** already
  under `.archive/`, `git mv`s it (and its companion dir) into `.archive/`,
  preserving relative structure, and commits.
- Idempotent: specs already under `.archive/` are skipped.
- Must run **after** the BuildTree change ships, or the moved specs disappear
  from the tree until it does.

## UI invariance

No frontend changes. Tree keys remain logical, so:

- The explorer renders archived specs at their logical position, muted, behind
  the existing "Show archived" toggle.
- The focused view, dependency minimap, spec-comments, and dispatch all keep
  using logical paths.

## Edge cases

1. **Companion directory.** Archiving `foo.md` moves both `foo.md` and `foo/`.
   Unarchive moves both back.
2. **Nested archive.** Archiving a spec already inside an archived subtree is a
   no-op (already under `.archive/`).
3. **`depends_on` an archived spec.** The archived target still resolves at its
   logical key, so `checkArchivedDependencies` and `depends-on-exist` are
   unaffected.
4. **Dispatched leaf.** Archiving a leaf with a live `dispatched_task_id` stays
   blocked exactly as today (guard runs before the move).
5. **Windows path separators.** Use forward-slash logical keys; convert at the
   filesystem boundary.
6. **`.archive` in `git status`/diff scoping.** Planning-round and spec-commit
   staging globs (`specs/`) already include `.archive/` since it is under
   `specs/`; confirm the chat-edit fan-out and stale scan skip archived specs
   (they already prune archived).

## Tests

- `BuildTree`: an archived spec physically at `specs/.archive/local/foo.md`
  appears at logical key `specs/local/foo.md` with `status: archived`; a live
  spec at `specs/local/bar.md` is unaffected; `.archive/` is not double-scanned.
- `ArchivePath`/`LogicalPath` round-trip, including no-op on non-archive paths.
- `findSpecFile` resolves an archived spec by its logical path.
- Archive moves the file (and companion dir) under `.archive/`, flips status,
  one commit; the live path no longer exists.
- Unarchive (revert path and fallback path) moves it back and restores status.
- `depends_on` from a live spec to an archived spec still resolves in the tree.
- Migration command relocates an in-place archived spec and is idempotent.

## Acceptance

- Archiving a spec moves it under `specs/.archive/` (same relative subpath) and
  the live `specs/` tree no longer contains it.
- The explorer and every reference render identically — no UI change, no
  `.archive` path visible in the product.
- Unarchive restores the spec to its live path.
- `git revert` of an archive commit reverses the move and the status together.
- The migration command relocates all already-archived specs.

## Outcome

**Complete (2026-06-25).** Implemented as designed, in tested stages:

- `ArchivePath`/`LogicalPath`/`IsArchivedPath` + `Spec.PhysicalPath`
  (`internal/spec/archive.go`).
- `BuildTree` skips `.archive/` in the live scan and adds a second pass keying
  archived specs by their logical path, forcing archived status, recording the
  physical path, and attaching each to its logical parent (live or archived).
- `findSpecFile` and the explorer file read/stream endpoints fall through to
  `.archive/`, so the focused view loads an archived spec by its logical path.
- `ArchiveSpec` git-mv's the spec + companion dir into `.archive/` and commits
  the move + status together; `UnarchiveSpec`'s revert reverses both, and its
  fallback moves the file back.
- The 196 already-archived specs were relocated in one commit
  (`specs: relocate archived specs into specs/.archive/`).
- `checkDependsOnExist` resolves archived dependencies via `.archive/` (the
  relocation otherwise flagged ~247 archived deps as missing).

**Decisions that changed from the draft:**

1. **No permanent migration command.** The one-time bulk move was run via a
   throwaway and removed — not shipped as `wallfacer spec migrate-archive` —
   since the archive action relocates going forward and a standing command
   would be one-time dead code. Status frontmatter stays authoritative for the
   lifecycle; physical location drives the tree.
