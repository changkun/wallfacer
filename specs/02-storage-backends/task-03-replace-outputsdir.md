# Task 3: Replace OutputsDir with Backend Methods

**Status:** Done
**Depends on:** Task 2
**Effort:** Small

## Goal

Remove `Store.OutputsDir()` which returns a filesystem path. Replace all callers with `ReadOutput`/`SaveOutput` on the backend, so output access works for non-filesystem backends.

## What to do

1. Add a `ReadBlob(taskID uuid.UUID, key string) ([]byte, error)` convenience method to `Store` that delegates to `backend.ReadBlob`.

2. Find all callers of `OutputsDir` and replace them:
   - `internal/handler/execute.go` — serves output files to the UI; change from `http.ServeFile(w, r, filepath)` to `store.ReadBlob(id, "outputs/"+filename)` and write the response body
   - `internal/handler/files.go` — file listing for output directory; may need a `ListBlobs(taskID, prefix)` method or scan via known turn numbers
   - Any runner code that reads back outputs

3. Remove `OutputsDir` from `Store`.

## Key files

- `internal/store/store.go` — remove `OutputsDir`
- `internal/handler/execute.go` — update output serving
- `internal/handler/files.go` — update file listing

## Acceptance criteria

- `OutputsDir` method removed
- Handlers serve outputs via backend `ReadOutput`
- No direct filesystem path construction for outputs outside the backend
- All tests pass

## Implementation notes

- **Added `ListBlobs(taskID, prefix)` to `StorageBackend` interface** — required by `serveStoredLogsRange` and `buildActivityLog` which need to enumerate turn files. The spec mentioned this might be needed ("may need a ListBlobs method").
- **Convenience methods `Store.ReadBlob` and `Store.ListBlobs`** added as thin delegates to the backend, so callers don't need to access the backend directly.
- **`serveStoredLogsRange` now returns 200 (not 404) for missing outputs** — when no turn files exist, it writes a "no output saved" message. Previously it returned 404 when the outputs directory didn't exist. The new behavior is correct for backend-agnostic access where "missing directory" has no meaning.
- **`ServeOutput` no longer uses `http.ServeFile`** — reads the full blob into memory and writes it directly. This loses Range request support and If-Modified-Since, which is acceptable since turn output files are typically small (bounded by `WALLFACER_MAX_TURN_OUTPUT_BYTES`).
