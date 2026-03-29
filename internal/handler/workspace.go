package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/gitutil"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
)

// workspaceBrowseEntry describes a single directory entry returned by
// BrowseWorkspaces, including whether it is a git repository.
type workspaceBrowseEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsGitRepo bool   `json:"is_git_repo"`
}

// BrowseWorkspaces lists directories at a given path for workspace selection.
func (h *Handler) BrowseWorkspaces(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	includeHidden := r.URL.Query().Get("include_hidden") == "true"
	if path == "" {
		home, _ := os.UserHomeDir()
		path = home
	}
	if !filepath.IsAbs(path) {
		http.Error(w, "path must be an absolute clean directory", http.StatusBadRequest)
		return
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		http.Error(w, "path must be an existing directory", http.StatusBadRequest)
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := make([]workspaceBrowseEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !includeHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		child := filepath.Join(path, entry.Name())
		resp = append(resp, workspaceBrowseEntry{
			Name:      entry.Name(),
			Path:      child,
			IsGitRepo: gitutil.IsGitRepo(child),
		})
	}
	slices.SortFunc(resp, func(a, b workspaceBrowseEntry) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	httpjson.Write(w, http.StatusOK, map[string]any{
		"path":    path,
		"entries": resp,
	})
}

// MkdirWorkspace creates a new directory under an absolute host path.
func (h *Handler) MkdirWorkspace(w http.ResponseWriter, r *http.Request) {
	req, ok := httpjson.DecodeBody[struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}](w, r)
	if !ok {
		return
	}
	if !filepath.IsAbs(req.Path) {
		http.Error(w, "path must be absolute", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(req.Path)
	if err != nil || !info.IsDir() {
		http.Error(w, "path must be an existing directory", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Name == "." || req.Name == ".." ||
		strings.Contains(req.Name, "/") {
		http.Error(w, "invalid folder name", http.StatusBadRequest)
		return
	}
	target := filepath.Join(req.Path, req.Name)
	if _, err := os.Stat(target); err == nil {
		http.Error(w, "directory already exists", http.StatusConflict)
		return
	}
	if err := os.Mkdir(target, 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpjson.Write(w, http.StatusCreated, map[string]string{"path": target})
}

// RenameWorkspace renames a directory at an absolute host path.
func (h *Handler) RenameWorkspace(w http.ResponseWriter, r *http.Request) {
	req, ok := httpjson.DecodeBody[struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}](w, r)
	if !ok {
		return
	}
	if !filepath.IsAbs(req.Path) {
		http.Error(w, "path must be absolute", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(req.Path); err != nil {
		http.Error(w, "path does not exist", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Name == "." || req.Name == ".." ||
		strings.ContainsAny(req.Name, "/\\") {
		http.Error(w, "invalid folder name", http.StatusBadRequest)
		return
	}
	target := filepath.Join(filepath.Dir(req.Path), req.Name)
	if _, err := os.Stat(target); err == nil {
		http.Error(w, "target already exists", http.StatusConflict)
		return
	}
	if err := os.Rename(req.Path, target); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]string{"path": target})
}

// UpdateWorkspaces switches the active workspace set.
func (h *Handler) UpdateWorkspaces(w http.ResponseWriter, r *http.Request) {
	req, ok := httpjson.DecodeBody[struct {
		Workspaces []string `json:"workspaces"`
	}](w, r)
	if !ok {
		return
	}

	snap, err := h.workspace.Switch(req.Workspaces)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// h.store and h.workspaces are updated asynchronously by the workspace
	// subscription goroutine started in NewHandler; no direct assignment here.
	if snap.Store != nil {
		h.runner.PruneUnknownWorktrees()
	}
	var cfg *envconfig.Config
	if h.envFile != "" {
		if parsed, err := envconfig.Parse(h.envFile); err == nil {
			cfg = &parsed
		}
	}
	httpjson.Write(w, http.StatusOK, h.buildConfigResponse(r.Context(), cfg))
}
