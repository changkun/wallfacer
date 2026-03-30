---
title: "Backend Path Validation and Tree Listing"
status: complete
depends_on: []
affects:
  - internal/handler/explorer.go
  - internal/apicontract/routes.go
effort: medium
created: 2026-03-22
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Task 1: Backend Path Validation and Tree Listing

## Goal

Implement the `isWithinWorkspace()` security validation function and the `ExplorerTree` handler that lists one level of a directory within a configured workspace. This is the foundation all explorer endpoints depend on.

## What to do

1. Create `internal/handler/explorer.go` with:

   a. `isWithinWorkspace(requestedPath, workspace string) (string, error)` function:
      - Resolve symlinks on both paths via `filepath.EvalSymlinks()`
      - Clean both paths via `filepath.Clean()`
      - Verify `requestedPath == workspace` or `strings.HasPrefix(requestedPath, workspace + string(filepath.Separator))`
      - Validate `workspace` passes `h.isAllowedWorkspace()` check
      - Return the cleaned, resolved path or an error

   b. `ExplorerTree(w http.ResponseWriter, r *http.Request)` handler method on `*Handler`:
      - Parse query params: `path` (required), `workspace` (required)
      - Validate workspace via `h.isAllowedWorkspace(workspace)`
      - Validate path via `isWithinWorkspace(path, workspace)`
      - Call `os.ReadDir(path)` for one-level listing
      - Build response entries with: `name`, `type` ("dir" or "file"), `size` (files only), `modified` (RFC3339)
      - Sort: directories first, then files, each group case-insensitive alphabetical (use `slices.SortFunc`)
      - Return via `writeJSON(w, http.StatusOK, ...)`

   c. Response struct:
      ```go
      type explorerEntry struct {
          Name     string    `json:"name"`
          Type     string    `json:"type"`
          Size     int64     `json:"size,omitempty"`
          Modified time.Time `json:"modified"`
      }
      ```

2. Register the route in `internal/apicontract/routes.go`:
   ```go
   {
       Method:      http.MethodGet,
       Pattern:     "/api/explorer/tree",
       Name:        "ExplorerTree",
       Description: "List one level of a workspace directory",
       Tags:        []string{"explorer"},
   }
   ```

3. Run `make api-contract` to regenerate `ui/js/generated/routes.js`.

## Tests

Create `internal/handler/explorer_test.go` with:

- `TestExplorerTree_Basic` — create temp workspace with dirs and files, verify correct listing with proper sorting (dirs first, case-insensitive)
- `TestExplorerTree_HiddenEntries` — verify dot-prefixed entries are included in results
- `TestExplorerTree_MissingParams` — verify 400 when `path` or `workspace` is missing
- `TestExplorerTree_WorkspaceNotConfigured` — verify 400 when workspace is not in active set
- `TestIsWithinWorkspace_Valid` — paths within workspace pass validation
- `TestIsWithinWorkspace_TraversalAttack` — paths with `../` that escape workspace are rejected
- `TestIsWithinWorkspace_SymlinkEscape` — symlink pointing outside workspace is rejected (create symlink in temp dir pointing outside, verify rejection)
- `TestIsWithinWorkspace_ExactWorkspaceRoot` — requesting the workspace root itself is allowed

## Boundaries

- Do NOT implement file content reading (Task 2)
- Do NOT implement file writing (Task 7)
- Do NOT add frontend code in this task
- Do NOT use the `skipDirs` map for filtering — the explorer shows all entries including node_modules etc. (the spec says hidden entries are included but dimmed)
