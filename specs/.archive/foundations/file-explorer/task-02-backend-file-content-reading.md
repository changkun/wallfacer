---
title: "Backend File Content Reading"
status: archived
depends_on:
  - specs/foundations/file-explorer/task-01-backend-path-validation-and-tree-listing.md
affects:
  - internal/handler/explorer.go
effort: medium
created: 2026-03-22
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---


# Task 2: Backend File Content Reading

## Goal

Implement the `ExplorerReadFile` handler that returns file contents for preview, with binary detection and size limits. This completes the read-only backend API.

## What to do

1. Add to `internal/handler/explorer.go`:

   a. `ExplorerReadFile(w http.ResponseWriter, r *http.Request)` handler:
      - Parse query params: `path` (required), `workspace` (required)
      - Validate workspace via `h.isAllowedWorkspace(workspace)`
      - Validate path via `isWithinWorkspace(path, workspace)`
      - `os.Stat()` to get file info; reject directories with 400
      - Check file size against 2 MB limit (`2 * 1024 * 1024`); return 413 with `{"error": "file too large", "size": <n>, "max": 2097152}` if exceeded
      - Read first 8192 bytes to detect binary (check for null bytes)
      - If binary: return JSON `{"binary": true, "size": <n>}` with `Content-Type: application/json` and `X-File-Binary: true` header
      - If text: read full file, set `Content-Type: text/plain; charset=utf-8`, write raw content
      - Always set `X-File-Size: <bytes>` header

   b. Binary detection helper:
      ```go
      func isBinaryContent(data []byte) bool {
          for _, b := range data {
              if b == 0 {
                  return true
              }
          }
          return false
      }
      ```

2. Add the max file size constant to `internal/constants/` if appropriate, or define it locally in explorer.go.

3. Register the route in `internal/apicontract/routes.go`:
   ```go
   {
       Method:      http.MethodGet,
       Pattern:     "/api/explorer/file",
       Name:        "ExplorerReadFile",
       Description: "Read file contents from a workspace",
       Tags:        []string{"explorer"},
   }
   ```

4. Run `make api-contract` to regenerate routes.

## Tests

Add to `internal/handler/explorer_test.go`:

- `TestExplorerReadFile_TextFile` — create a text file, verify raw content returned with correct Content-Type and X-File-Size header
- `TestExplorerReadFile_BinaryFile` — create file with null bytes, verify JSON response with `binary: true` and `X-File-Binary` header
- `TestExplorerReadFile_LargeFile` — create file > 2 MB, verify 413 response with size info
- `TestExplorerReadFile_NotFound` — verify 404 for non-existent file
- `TestExplorerReadFile_Directory` — verify 400 when path points to a directory
- `TestExplorerReadFile_OutsideWorkspace` — verify rejection when path escapes workspace boundary
- `TestExplorerReadFile_MissingParams` — verify 400 when params are missing

## Boundaries

- Do NOT implement file writing (Task 7)
- Do NOT add frontend code
- Do NOT add caching — file reads are direct from disk; caching is a future optimization

## Implementation notes

- **Non-existent path handling:** The spec assumed `isWithinWorkspace` would pass for non-existent paths, but it uses `filepath.EvalSymlinks` which requires the target to exist. Added a fallback in `ExplorerReadFile` that cleans the raw path and checks containment manually when `isWithinWorkspace` fails, returning 404 for paths within the workspace that don't exist.
- **Binary detection:** Used `slices.Contains(data, 0)` instead of a manual loop, per linter suggestion.
- **`ExplorerMaxFileSize` constant:** Added to `internal/constants/` rather than defining locally, since it's referenced by name in tests and follows the existing pattern for size limits.
