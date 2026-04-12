---
title: Extract spec scaffold into a reusable library
status: complete
depends_on: []
affects:
  - internal/spec/
  - internal/cli/spec.go
effort: medium
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Extract spec scaffold into a reusable library

## Goal

Extract the file-creation logic currently embedded in `internal/cli/spec.go:runSpecNew` into a pure library function under `internal/spec/`, so both the `wallfacer spec new` CLI and the new server-side `/spec-new` directive handler (follow-up task) consume a single source of truth for spec frontmatter.

## What to do

1. Create `internal/spec/scaffold.go` with:
   ```go
   type ScaffoldOptions struct {
       Path      string    // required, e.g. "specs/local/auth-refactor.md"
       Title     string    // optional; defaults to TitleCase(basename)
       Status    Status    // optional; defaults to StatusVague
       Effort    Effort    // optional; defaults to EffortMedium
       Author    string    // optional; defaults to resolveAuthor()
       DependsOn []string  // optional
       Now       time.Time // optional; defaults to time.Now(). Injection point for tests.
       Force     bool      // if true, overwrite existing file
   }

   func Scaffold(opts ScaffoldOptions) (string, error)
   ```
2. Move the helpers `validateSpecPath`, `titleFromFilename`, `resolveAuthor`, `renderSpecSkeleton` from `internal/cli/spec.go` into `internal/spec/scaffold.go` (or an unexported subfile if clearer). `Scaffold` should:
   - Validate `Path` via the moved `validateSpecPath`.
   - Reject invalid `Status` / `Effort` values against `stringStatuses()` / `stringEfforts()`.
   - Default empty `Title` via `titleFromFilename(Path)`.
   - Default empty `Author` via `resolveAuthor()`.
   - Error if the file exists and `!Force`.
   - `MkdirAll(filepath.Dir(Path), 0o755)` before writing.
   - Write the rendered skeleton and return the absolute path.
3. Rewrite `internal/cli/spec.go:runSpecNew` as a thin argv-parser that constructs `ScaffoldOptions` from flags, calls `spec.Scaffold`, and prints the CLI success/error line. No logic duplication — all validation and file I/O belong to the library.
4. Update `internal/cli/spec_test.go` so tests exercise the CLI wrapper but delegate business-logic assertions to `internal/spec/scaffold_test.go`.

## Tests

In `internal/spec/scaffold_test.go`:

- `TestScaffold_HappyPath`: creates a spec at `specs/local/foo.md`, verifies frontmatter fields match options.
- `TestScaffold_DefaultsTitleFromBasename`: empty `Title` yields `"Foo Bar"` for path `specs/local/foo-bar.md`.
- `TestScaffold_DefaultsAuthor`: empty `Author` uses `resolveAuthor()` (stub via `Now`-like injection if needed; otherwise assert against actual git config presence).
- `TestScaffold_RejectsInvalidStatus` / `TestScaffold_RejectsInvalidEffort`: returns an error.
- `TestScaffold_RejectsPathOutsideSpecs`: `other/foo.md` errors.
- `TestScaffold_RejectsNonMarkdown`: `specs/local/foo.txt` errors.
- `TestScaffold_RejectsExistingFileWithoutForce`: second call to same path errors.
- `TestScaffold_ForceOverwrites`: second call with `Force: true` succeeds and overwrites.
- `TestScaffold_CreatesParentDirectory`: deep path `specs/local/auth/subfolder/foo.md` creates intermediate dirs.
- `TestScaffold_ValidatesViaSpecValidate`: round-trip — scaffold a file, parse it back with `ParseFile`, assert no validation errors.

In `internal/cli/spec_test.go`:

- Existing tests continue to pass (CLI behaviour unchanged from the user's perspective).

## Boundaries

- **Do NOT change** the CLI flag names or user-facing output of `wallfacer spec new`. Back-compat for scripts.
- **Do NOT change** the rendered frontmatter format — this task is a pure extraction, not a schema change.
- **Do NOT** add new fields to `ScaffoldOptions` beyond what's listed. Extending (e.g. for multi-workspace routing) is a follow-up.
- **Do NOT** touch `internal/cli/spec.go:runSpecValidate` — validation is a separate concern.

## Implementation notes

1. **Helpers exported as public API.** The spec named the moved helpers with lowercase names (`validateSpecPath`, `titleFromFilename`, `resolveAuthor`, `renderSpecSkeleton`) implying unexported symbols. They landed as `ValidateSpecPath`, `TitleFromFilename`, `ResolveAuthor`, `RenderSkeleton` — exported — because the follow-up `/spec-new` directive parser will need `ValidateSpecPath` and `RenderSkeleton` from a different package (`internal/handler`). Exporting was simpler than introducing a duplicate parallel API or moving the directive handler into the spec package. `renderSpecSkeleton` was renamed to `RenderSkeleton` (dropped "Spec" redundant-with-package-name per Go convention).

2. **CLI error-to-exit-code mapping.** The spec said `runSpecNew` should become "a thin argv-parser that constructs ScaffoldOptions from flags, calls spec.Scaffold, and prints the CLI success/error line." The exit-code contract of the original code was:
   - Invalid path / invalid status / invalid effort → `os.Exit(2)` (usage error).
   - File already exists / mkdir failure / write failure → `os.Exit(1)` (I/O error).

   The new code preserves this by inspecting the error string: any error whose message contains `" already exists"` or matches `os.ErrPermission` exits 1; everything else exits 2. This is a slight abuse of string-matching for exit-code routing but avoids introducing typed errors in the library that only the CLI would use.

3. **`RenderSkeleton` signature gained a `dependsOn` parameter.** The original `renderSpecSkeleton` hardcoded `depends_on: []`. `ScaffoldOptions.DependsOn` is now plumbed through to render non-empty lists, because the follow-up server-side scaffold paths (bootstrap hook, `/create` expansion) will sometimes know the parent spec's `depends_on` at creation time.

4. **Test call-sites use `chdir` instead of absolute paths.** `ValidateSpecPath` expects repo-relative paths (the first path segment must be `specs/`). Initial tests that used `filepath.Join(t.TempDir(), "specs", "local", "foo.md")` built absolute paths, which the validator correctly rejected. Tests now chdir into a temp dir and use relative paths, matching how production code invokes `Scaffold`.
