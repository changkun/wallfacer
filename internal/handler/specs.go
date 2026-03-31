package handler

import (
	"maps"
	"net/http"
	"path/filepath"

	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/spec"
)

// GetSpecTree returns the full spec tree with metadata and progress for
// all workspaces. Each workspace's specs/ directory is scanned and the
// results are merged into a single response.
func (h *Handler) GetSpecTree(w http.ResponseWriter, _ *http.Request) {
	workspaces := h.currentWorkspaces()

	var allNodes []spec.NodeResponse
	allProgress := make(map[string]spec.Progress)

	for _, ws := range workspaces {
		specsDir := filepath.Join(ws, "specs")
		tree, err := spec.BuildTree(specsDir)
		if err != nil {
			continue // workspace has no specs/ — skip silently
		}
		resp := spec.SerializeTree(tree)
		allNodes = append(allNodes, resp.Nodes...)
		maps.Copy(allProgress, resp.Progress)
	}

	httpjson.Write(w, http.StatusOK, spec.TreeResponse{
		Nodes:    allNodes,
		Progress: allProgress,
	})
}
