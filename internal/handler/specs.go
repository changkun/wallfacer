package handler

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"path/filepath"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
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

// SpecTreeStream sends SSE notifications when the spec tree changes.
// The server polls the spec directories every 3 seconds and sends the
// full tree data only when it differs from the previous snapshot.
func (h *Handler) SpecTreeStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	collectTree := func() spec.TreeResponse {
		workspaces := h.currentWorkspaces()
		var allNodes []spec.NodeResponse
		allProgress := make(map[string]spec.Progress)
		for _, ws := range workspaces {
			specsDir := filepath.Join(ws, "specs")
			tree, err := spec.BuildTree(specsDir)
			if err != nil {
				continue
			}
			resp := spec.SerializeTree(tree)
			allNodes = append(allNodes, resp.Nodes...)
			maps.Copy(allProgress, resp.Progress)
		}
		return spec.TreeResponse{Nodes: allNodes, Progress: allProgress}
	}

	send := func(tree spec.TreeResponse) ([]byte, bool) {
		data, err := json.Marshal(tree)
		if err != nil {
			return nil, false
		}
		if _, err := fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data); err != nil {
			return nil, false
		}
		flusher.Flush()
		return data, true
	}

	current := collectTree()
	curData, ok := send(current)
	if !ok {
		return
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	keepalive := time.NewTicker(constants.SSEKeepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if _, err := fmt.Fprintf(w, "event: heartbeat\ndata: {}\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			next := collectTree()
			nextData, err := json.Marshal(next)
			if err != nil {
				continue
			}
			if string(nextData) != string(curData) {
				if _, err := fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", nextData); err != nil {
					return
				}
				flusher.Flush()
				curData = nextData
			}
		}
	}
}
