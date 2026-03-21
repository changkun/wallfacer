package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/gitutil"
	"changkun.de/x/wallfacer/internal/store"
)

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
	writeJSON(w, http.StatusOK, map[string]any{
		"path":    path,
		"entries": resp,
	})
}

// UpdateWorkspaces switches the active workspace set.
func (h *Handler) UpdateWorkspaces(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Workspaces []string `json:"workspaces"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if s, ok := h.currentStore(); ok && s != nil {
		tasks, err := s.ListTasks(r.Context(), false)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, task := range tasks {
			if task.Status == store.TaskStatusInProgress || task.Status == store.TaskStatusCommitting {
				http.Error(w, "cannot switch workspaces while tasks are in progress or committing", http.StatusConflict)
				return
			}
		}
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
	writeJSON(w, http.StatusOK, h.buildConfigResponse(r.Context(), cfg))
}
