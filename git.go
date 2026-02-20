package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// WorkspaceGitStatus holds the git state for a single workspace directory.
type WorkspaceGitStatus struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	IsGitRepo  bool   `json:"is_git_repo"`
	Branch     string `json:"branch,omitempty"`
	HasRemote  bool   `json:"has_remote"`
	AheadCount int    `json:"ahead_count"`
}

// GitStatus returns git status for every configured workspace.
func (h *Handler) GitStatus(w http.ResponseWriter, r *http.Request) {
	workspaces := h.runner.Workspaces()
	statuses := make([]WorkspaceGitStatus, 0, len(workspaces))
	for _, ws := range workspaces {
		statuses = append(statuses, workspaceGitStatus(ws))
	}
	writeJSON(w, http.StatusOK, statuses)
}

// GitStatusStream streams git status for all workspaces as SSE, pushing an
// update whenever the status changes (checked every 5 seconds).
func (h *Handler) GitStatusStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	collect := func() []WorkspaceGitStatus {
		workspaces := h.runner.Workspaces()
		statuses := make([]WorkspaceGitStatus, 0, len(workspaces))
		for _, ws := range workspaces {
			statuses = append(statuses, workspaceGitStatus(ws))
		}
		return statuses
	}

	send := func(statuses []WorkspaceGitStatus) bool {
		data, err := json.Marshal(statuses)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	current := collect()
	if !send(current) {
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			next := collect()
			nextData, _ := json.Marshal(next)
			curData, _ := json.Marshal(current)
			if string(nextData) != string(curData) {
				if !send(next) {
					return
				}
				current = next
			}
		}
	}
}

// GitPush runs `git push` locally for the requested workspace.
func (h *Handler) GitPush(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Workspace string `json:"workspace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate that the workspace is one the server was started with.
	allowed := false
	for _, ws := range h.runner.Workspaces() {
		if ws == req.Workspace {
			allowed = true
			break
		}
	}
	if !allowed {
		http.Error(w, "workspace not configured", http.StatusBadRequest)
		return
	}

	logGit.Info("push", "workspace", req.Workspace)
	out, err := exec.CommandContext(r.Context(), "git", "-C", req.Workspace, "push").CombinedOutput()
	if err != nil {
		logGit.Error("push failed", "workspace", req.Workspace, "error", err)
		http.Error(w, string(out), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"output": string(out)})
}

// workspaceGitStatus inspects a directory and returns its git status.
func workspaceGitStatus(path string) WorkspaceGitStatus {
	s := WorkspaceGitStatus{
		Path: path,
		Name: filepath.Base(path),
	}

	// Is it a git repo?
	if err := exec.Command("git", "-C", path, "rev-parse", "--git-dir").Run(); err != nil {
		return s
	}
	s.IsGitRepo = true

	// Current branch.
	if out, err := exec.Command("git", "-C", path, "branch", "--show-current").Output(); err == nil {
		s.Branch = strings.TrimSpace(string(out))
	}

	// Does it have a remote tracking branch?
	if err := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "@{u}").Run(); err != nil {
		// No upstream configured — nothing to push to.
		return s
	}
	s.HasRemote = true

	// How many local commits are ahead of the remote?
	if out, err := exec.Command("git", "-C", path, "rev-list", "--count", "@{u}..HEAD").Output(); err == nil {
		n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
		s.AheadCount = n
	}

	return s
}
