# Task 8: Backend File Writing

**Status:** Done
**Depends on:** Task 1
**Phase:** Phase 2 — File Editing + Saving
**Effort:** Medium

## Goal

Implement the `ExplorerWriteFile` handler that writes content to workspace files with atomic writes and `.git/` directory protection.

## What to do

1. Add to `internal/handler/explorer.go`:

   a. `ExplorerWriteFile(w http.ResponseWriter, r *http.Request)` handler:
      - Decode JSON body via `decodeJSONBody()`:
        ```go
        var req struct {
            Path      string `json:"path"`
            Workspace string `json:"workspace"`
            Content   string `json:"content"`
        }
        ```
      - Validate workspace via `h.isAllowedWorkspace(req.Workspace)`
      - Validate path via `isWithinWorkspace(req.Path, req.Workspace)`
      - Reject if content exceeds 2 MB
      - Reject if path contains `/.git/` or ends with `/.git`:
        ```go
        func isGitPath(p string) bool {
            return strings.Contains(p, "/.git/") || strings.HasSuffix(p, "/.git") ||
                   strings.Contains(p, "\\.git\\") || strings.HasSuffix(p, "\\.git")
        }
        ```
      - Atomic write: write to temp file in same directory, then `os.Rename()` (same pattern as `internal/store/`)
      - Return `{"status": "ok", "size": <bytes_written>}`

2. Register the route in `internal/apicontract/routes.go`:
   ```go
   {
       Method:      http.MethodPut,
       Pattern:     "/api/explorer/file",
       Name:        "ExplorerWriteFile",
       Description: "Write file contents to a workspace",
       Tags:        []string{"explorer"},
   }
   ```

3. Run `make api-contract` to regenerate routes.

## Tests

Add to `internal/handler/explorer_test.go`:

- `TestExplorerWriteFile_Success` — write content, read back, verify match
- `TestExplorerWriteFile_AtomicWrite` — verify temp file + rename pattern (check that partial writes don't corrupt the target)
- `TestExplorerWriteFile_GitPathRejection` — verify 400 for paths inside `.git/` directory
- `TestExplorerWriteFile_OutsideWorkspace` — verify rejection for path traversal
- `TestExplorerWriteFile_TooLarge` — verify 413 for content exceeding 2 MB
- `TestExplorerWriteFile_WorkspaceNotConfigured` — verify 400 for unconfigured workspace
- `TestExplorerWriteFile_CreateParentDirs` — verify behavior when parent directory doesn't exist (should return error, not create dirs)

## Boundaries

- Do NOT implement frontend edit UI (Task 9)
- Do NOT add file creation or deletion endpoints (Phase 3)
- Do NOT add file locking or conflict detection
- Authentication and CSRF are already handled by middleware — no extra auth logic needed

## Implementation notes

- **Non-existent target files:** `isWithinWorkspace` uses `filepath.EvalSymlinks` which fails for non-existent paths. Since write targets may not exist yet (creating new files), the handler falls back to a manual containment check (`filepath.Clean` + `strings.HasPrefix`) when `isWithinWorkspace` errors, then verifies the parent directory exists. This is the same pattern used by `ExplorerReadFile` for 404 detection.
