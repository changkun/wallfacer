package handler

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// explorerEntry is a single directory or file entry returned by ExplorerTree.
type explorerEntry struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	Size     int64     `json:"size,omitempty"`
	Modified time.Time `json:"modified"`
}

// isWithinWorkspace resolves symlinks and cleans both paths, then verifies
// that requestedPath is equal to or a child of workspace. Returns the
// cleaned, resolved path or an error if the path escapes the workspace.
func isWithinWorkspace(requestedPath, workspace string) (string, error) {
	resolvedWS, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", errors.New("workspace path not accessible")
	}
	resolvedWS = filepath.Clean(resolvedWS)

	resolvedReq, err := filepath.EvalSymlinks(requestedPath)
	if err != nil {
		return "", errors.New("requested path not accessible")
	}
	resolvedReq = filepath.Clean(resolvedReq)

	if resolvedReq != resolvedWS && !strings.HasPrefix(resolvedReq, resolvedWS+string(filepath.Separator)) {
		return "", errors.New("path is outside workspace")
	}
	return resolvedReq, nil
}

// ExplorerTree lists one level of a workspace directory.
func (h *Handler) ExplorerTree(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	workspace := r.URL.Query().Get("workspace")

	if path == "" || workspace == "" {
		http.Error(w, "path and workspace query params required", http.StatusBadRequest)
		return
	}

	if !h.isAllowedWorkspace(workspace) {
		http.Error(w, "workspace not configured", http.StatusBadRequest)
		return
	}

	resolved, err := isWithinWorkspace(path, workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "directory not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to read directory", http.StatusInternalServerError)
		return
	}

	result := make([]explorerEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue // skip entries we can't stat
		}
		typ := "file"
		if e.IsDir() {
			typ = "dir"
		}
		entry := explorerEntry{
			Name:     e.Name(),
			Type:     typ,
			Modified: info.ModTime().UTC().Truncate(time.Second),
		}
		if typ == "file" {
			entry.Size = info.Size()
		}
		result = append(result, entry)
	}

	// Sort: directories first, then files; case-insensitive alphabetical within each group.
	slices.SortFunc(result, func(a, b explorerEntry) int {
		if a.Type != b.Type {
			if a.Type == "dir" {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})

	writeJSON(w, http.StatusOK, result)
}
