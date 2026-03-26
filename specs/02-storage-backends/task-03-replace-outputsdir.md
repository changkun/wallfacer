# Task 3: Replace OutputsDir with Backend Methods

**Status:** Not started
**Depends on:** Task 2
**Effort:** Small

## Goal

Remove `Store.OutputsDir()` which returns a filesystem path. Replace all callers with `ReadOutput`/`SaveOutput` on the backend, so output access works for non-filesystem backends.

## What to do

1. Add a `ReadOutput(taskID uuid.UUID, filename string) ([]byte, error)` method to `Store` that delegates to the backend.

2. Find all callers of `OutputsDir` and replace them:
   - `internal/handler/execute.go` — serves output files to the UI
   - `internal/handler/files.go` — file listing for output directory
   - Any runner code that reads back outputs

3. For the handler serving output files, change from `http.ServeFile(w, r, filepath)` to reading via the backend and writing the response body.

4. Remove `OutputsDir` from `Store`.

## Key files

- `internal/store/store.go` — remove `OutputsDir`
- `internal/handler/execute.go` — update output serving
- `internal/handler/files.go` — update file listing

## Acceptance criteria

- `OutputsDir` method removed
- Handlers serve outputs via backend `ReadOutput`
- No direct filesystem path construction for outputs outside the backend
- All tests pass
