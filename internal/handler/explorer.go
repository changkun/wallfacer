package handler

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
)

// maxFileWriteSize is the maximum content size accepted by ExplorerWriteFile.
// Set to 2 MB to prevent accidental or malicious large writes through the API.
const maxFileWriteSize = 2 << 20

// explorerEntry is a single directory or file entry returned by ExplorerTree.
type explorerEntry struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	Size     int64     `json:"size,omitempty"`
	Modified time.Time `json:"modified"`
}

// isWithinWorkspace resolves symlinks and cleans both paths, then verifies
// that requestedPath is equal to or a child of workspace. This is the primary
// path-traversal guard for the file explorer: all read/write/tree operations
// pass through this function to ensure user input cannot escape the workspace.
// Returns the cleaned, resolved path or an error if the path escapes the workspace.
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

	httpjson.Write(w, http.StatusOK, result)
}

// isBinaryContent reports whether data contains a null byte, indicating
// binary content.
func isBinaryContent(data []byte) bool {
	return slices.Contains(data, 0)
}

// ExplorerReadFile returns file contents for preview, with binary detection
// and size limits.
func (h *Handler) ExplorerReadFile(w http.ResponseWriter, r *http.Request) {
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
		// isWithinWorkspace fails for non-existent paths because
		// EvalSymlinks requires the target to exist. Distinguish a
		// genuinely missing file from a path-escape attempt by
		// cleaning the raw path and checking containment manually.
		cleaned := filepath.Clean(path)
		wsClean := filepath.Clean(workspace)
		if cleaned == wsClean || strings.HasPrefix(cleaned, wsClean+string(filepath.Separator)) {
			// Path is within workspace but doesn't exist.
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	info, err := os.Stat(resolved)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to stat file", http.StatusInternalServerError)
		return
	}

	if info.IsDir() {
		http.Error(w, "path is a directory", http.StatusBadRequest)
		return
	}

	size := info.Size()
	w.Header().Set("X-File-Size", strconv.FormatInt(size, 10))

	if size > constants.ExplorerMaxFileSize {
		httpjson.Write(w, http.StatusRequestEntityTooLarge, map[string]any{
			"error": "file too large",
			"size":  size,
			"max":   constants.ExplorerMaxFileSize,
		})
		return
	}

	f, err := os.Open(resolved)
	if err != nil {
		http.Error(w, "failed to open file", http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	// Read first 8192 bytes to detect binary content.
	head := make([]byte, 8192)
	n, err := f.Read(head)
	if err != nil && err != io.EOF {
		http.Error(w, "failed to read file", http.StatusInternalServerError)
		return
	}
	head = head[:n]

	if isBinaryContent(head) {
		w.Header().Set("X-File-Binary", "true")
		httpjson.Write(w, http.StatusOK, map[string]any{
			"binary": true,
			"size":   size,
		})
		return
	}

	// Text file: write the head we already read, then copy the rest.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(head)
	_, _ = io.Copy(w, f)
}

// isGitPath reports whether p refers to a path inside a .git directory.
func isGitPath(p string) bool {
	return strings.Contains(p, "/.git/") || strings.HasSuffix(p, "/.git") ||
		strings.Contains(p, "\\.git\\") || strings.HasSuffix(p, "\\.git")
}

// ExplorerWriteFile writes content to a file in a workspace using atomic
// temp-file + rename. It rejects paths inside .git directories, content
// exceeding 2 MB, and paths whose parent directory does not exist.
func (h *Handler) ExplorerWriteFile(w http.ResponseWriter, r *http.Request) {
	req, ok := httpjson.DecodeBody[struct {
		Path      string `json:"path"`
		Workspace string `json:"workspace"`
		Content   string `json:"content"`
	}](w, r)
	if !ok {
		return
	}

	if req.Path == "" || req.Workspace == "" {
		http.Error(w, "path and workspace are required", http.StatusBadRequest)
		return
	}

	if !h.isAllowedWorkspace(req.Workspace) {
		http.Error(w, "workspace not configured", http.StatusBadRequest)
		return
	}

	if len(req.Content) > maxFileWriteSize {
		httpjson.Write(w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": "content exceeds " + strconv.Itoa(maxFileWriteSize) + " byte limit",
		})
		return
	}

	if isGitPath(req.Path) {
		http.Error(w, "writing to .git directories is not allowed", http.StatusBadRequest)
		return
	}

	resolved, err := isWithinWorkspace(req.Path, req.Workspace)
	if err != nil {
		// isWithinWorkspace fails for non-existent paths because
		// EvalSymlinks requires the target to exist. For writes the
		// target file may not exist yet; do a manual containment check.
		cleaned := filepath.Clean(req.Path)
		wsClean := filepath.Clean(req.Workspace)
		if cleaned == wsClean || strings.HasPrefix(cleaned, wsClean+string(filepath.Separator)) {
			resolved = cleaned
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Verify parent directory exists — do not create missing directories.
	dir := filepath.Dir(resolved)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "parent directory does not exist", http.StatusBadRequest)
			return
		}
		http.Error(w, "failed to stat parent directory", http.StatusInternalServerError)
		return
	}

	// Atomic write: temp file in the same directory, then rename.
	tmp, err := os.CreateTemp(dir, ".wallfacer-write-*")
	if err != nil {
		http.Error(w, "failed to create temp file", http.StatusInternalServerError)
		return
	}
	tmpName := tmp.Name()

	data := []byte(req.Content)
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		http.Error(w, "failed to write file", http.StatusInternalServerError)
		return
	}

	if err := os.Rename(tmpName, resolved); err != nil {
		_ = os.Remove(tmpName)
		http.Error(w, "failed to rename temp file", http.StatusInternalServerError)
		return
	}

	httpjson.Write(w, http.StatusOK, map[string]any{
		"status": "ok",
		"size":   len(data),
	})
}
