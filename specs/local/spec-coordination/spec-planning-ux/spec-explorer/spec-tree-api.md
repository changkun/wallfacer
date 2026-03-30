---
title: Spec tree API endpoint
status: validated
depends_on: []
affects:
  - internal/handler/
  - internal/apicontract/routes.go
  - internal/spec/
  - internal/cli/server.go
effort: medium
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Spec tree API endpoint

## Goal

Add a `GET /api/specs/tree` endpoint that returns the full spec tree with frontmatter metadata, recursive progress counts, and dependency edges. This single response gives the frontend everything it needs to render the spec explorer and dependency minimap without per-node API calls.

## What to do

1. In `internal/spec/model.go`, add JSON tags to the `Spec` struct fields:
   ```go
   Title            string   `yaml:"title" json:"title"`
   Status           Status   `yaml:"status" json:"status"`
   DependsOn        []string `yaml:"depends_on" json:"depends_on"`
   Affects          []string `yaml:"affects" json:"affects"`
   Effort           Effort   `yaml:"effort" json:"effort"`
   Created          Date     `yaml:"created" json:"created"`
   Updated          Date     `yaml:"updated" json:"updated"`
   Author           string   `yaml:"author" json:"author"`
   DispatchedTaskID *string  `yaml:"dispatched_task_id" json:"dispatched_task_id"`
   Path             string   `yaml:"-" json:"path"`
   Track            string   `yaml:"-" json:"track"`
   Body             string   `yaml:"-" json:"-"` // exclude body from API response (large)
   ```
   Add `MarshalJSON` for `Date` to output `"YYYY-MM-DD"` format.

2. In `internal/spec/`, add a `SerializeTree()` function in a new `serialize.go`:
   ```go
   type TreeResponse struct {
       Nodes    []NodeResponse `json:"nodes"`
       Progress map[string]Progress `json:"progress"`
   }

   type NodeResponse struct {
       Path     string   `json:"path"`
       Spec     *Spec    `json:"spec"`
       Children []string `json:"children"` // paths of child specs
       IsLeaf   bool     `json:"is_leaf"`
       Depth    int      `json:"depth"`
   }

   func SerializeTree(tree *Tree) TreeResponse
   ```
   Walk the tree, collect all nodes with their children paths, compute `TreeProgress()`, and bundle into the response.

3. In `internal/apicontract/routes.go`, add:
   ```go
   {Method: http.MethodGet, Pattern: "/api/specs/tree", Name: "GetSpecTree",
       JSName: "tree", Description: "Get the full spec tree with metadata and progress.", Tags: []string{"specs"}},
   ```

4. Run `make api-contract` to regenerate `ui/js/generated/routes.js`.

5. Create `internal/handler/specs.go` with the handler:
   ```go
   func (h *Handler) GetSpecTree(w http.ResponseWriter, r *http.Request) {
       // For each workspace, find specs/ directory, call spec.BuildTree()
       // Collect trees from all workspaces into a combined response
       // Include workspace prefix for multi-repo forests
       workspaces := h.workspaceManager.ActiveWorkspaces()
       var allNodes []spec.NodeResponse
       allProgress := make(map[string]spec.Progress)
       for _, ws := range workspaces {
           specsDir := filepath.Join(ws, "specs")
           tree, err := spec.BuildTree(specsDir)
           if err != nil { continue } // workspace has no specs/
           resp := spec.SerializeTree(tree)
           // prefix paths with workspace basename for multi-repo
           allNodes = append(allNodes, resp.Nodes...)
           for k, v := range resp.Progress {
               allProgress[k] = v
           }
       }
       httpjson.Write(w, http.StatusOK, spec.TreeResponse{Nodes: allNodes, Progress: allProgress})
   }
   ```

6. In `internal/cli/server.go`, register the handler:
   ```go
   "GetSpecTree": h.GetSpecTree,
   ```

7. Update `CLAUDE.md` API Routes section with the new endpoint under a `### Specs` heading.

## Tests

- `TestSerializeTree`: Build a tree from test fixtures, serialize, verify all nodes are present with correct paths, children, and is_leaf flags.
- `TestSerializeTreeProgress`: Verify progress map contains correct done/total counts for non-leaf nodes.
- `TestGetSpecTreeHandler`: HTTP test: create a temp workspace with a few spec files, call `GET /api/specs/tree`, verify JSON response structure.
- `TestGetSpecTreeEmptyWorkspace`: Workspace with no `specs/` directory returns empty nodes list (not an error).
- `TestGetSpecTreeMultiWorkspace`: Two workspaces each with `specs/`, verify both trees appear in response with workspace-prefixed paths.
- `TestDateMarshalJSON`: Verify Date marshals as `"2026-03-30"` string.

## Boundaries

- Do NOT add SSE streaming for spec tree changes — polling is sufficient for now.
- Do NOT add the spec body to the API response — it's too large. The focused view fetches individual files via `ExplorerReadFile`.
- Do NOT modify the existing `ExplorerTree` endpoint.
- Do NOT add frontend rendering — that's the `spec-tree-renderer` task.
