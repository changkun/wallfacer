package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrConflict is returned by rebaseOntoDefault when a merge conflict is detected.
var ErrConflict = errors.New("rebase conflict")

// isGitRepo reports whether path is inside a git repository.
func isGitRepo(path string) bool {
	return exec.Command("git", "-C", path, "rev-parse", "--git-dir").Run() == nil
}

// defaultBranch returns the default branch name for a repo (tries origin/HEAD,
// falls back to the current local HEAD branch, then "main").
func defaultBranch(repoPath string) (string, error) {
	// Try symbolic ref for origin/HEAD first (most reliable for cloned repos).
	out, err := exec.Command("git", "-C", repoPath, "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output()
	if err == nil {
		// output is e.g. "origin/main" — strip the "origin/" prefix.
		branch := strings.TrimSpace(strings.TrimPrefix(string(out), "origin/"))
		if branch != "" && branch != string(out) {
			return branch, nil
		}
	}
	// Fall back to current HEAD branch name.
	out, err = exec.Command("git", "-C", repoPath, "branch", "--show-current").Output()
	if err != nil {
		return "main", nil
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" {
		return "main", nil // detached HEAD
	}
	return branch, nil
}

// createWorktree creates a new branch and checks it out as a worktree at worktreePath.
func createWorktree(repoPath, worktreePath, branchName string) error {
	out, err := exec.Command(
		"git", "-C", repoPath,
		"worktree", "add", "-b", branchName, worktreePath, "HEAD",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add in %s: %w\n%s", repoPath, err, out)
	}
	return nil
}

// removeWorktree removes a worktree and deletes the associated branch.
func removeWorktree(repoPath, worktreePath, branchName string) error {
	out, err := exec.Command(
		"git", "-C", repoPath,
		"worktree", "remove", "--force", worktreePath,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove %s: %w\n%s", worktreePath, err, out)
	}
	// Delete the branch (best-effort).
	exec.Command("git", "-C", repoPath, "branch", "-D", branchName).Run()
	return nil
}

// rebaseOntoDefault rebases the task branch (currently checked out in worktreePath)
// onto the default branch of repoPath. On conflict it aborts the rebase and returns
// ErrConflict so the caller can invoke conflict resolution and retry.
func rebaseOntoDefault(repoPath, worktreePath string) error {
	defBranch, err := defaultBranch(repoPath)
	if err != nil {
		return err
	}
	out, err := exec.Command("git", "-C", worktreePath, "rebase", defBranch).CombinedOutput()
	if err != nil {
		// Abort so the repo is not stuck mid-rebase.
		exec.Command("git", "-C", worktreePath, "rebase", "--abort").Run()
		if isConflictOutput(string(out)) {
			return fmt.Errorf("%w in %s", ErrConflict, worktreePath)
		}
		return fmt.Errorf("git rebase in %s: %w\n%s", worktreePath, err, out)
	}
	return nil
}

// ffMerge fast-forward merges branchName into the default branch of repoPath.
func ffMerge(repoPath, branchName string) error {
	defBranch, err := defaultBranch(repoPath)
	if err != nil {
		return err
	}
	if out, err := exec.Command("git", "-C", repoPath, "checkout", defBranch).CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout %s in %s: %w\n%s", defBranch, repoPath, err, out)
	}
	out, err := exec.Command("git", "-C", repoPath, "merge", "--ff-only", branchName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git merge --ff-only %s in %s: %w\n%s", branchName, repoPath, err, out)
	}
	return nil
}

// getCommitHash returns the current HEAD commit hash in repoPath.
func getCommitHash(repoPath string) (string, error) {
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD in %s: %w", repoPath, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// hasCommitsAheadOf reports whether worktreePath has commits not yet in baseBranch.
func hasCommitsAheadOf(worktreePath, baseBranch string) (bool, error) {
	out, err := exec.Command(
		"git", "-C", worktreePath,
		"rev-list", "--count", baseBranch+"..HEAD",
	).Output()
	if err != nil {
		return false, fmt.Errorf("git rev-list in %s: %w", worktreePath, err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n > 0, nil
}

// isConflictOutput reports whether git output text indicates a merge conflict.
func isConflictOutput(s string) bool {
	return strings.Contains(s, "CONFLICT") ||
		strings.Contains(s, "Merge conflict") ||
		strings.Contains(s, "conflict")
}

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
