package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"changkun.de/x/wallfacer/internal/gitutil"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

type workspaceMutationBlockingTask struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type workspaceMutationConflictResponse struct {
	Error         string                          `json:"error"`
	Workspace     string                          `json:"workspace"`
	BlockingTasks []workspaceMutationBlockingTask `json:"blocking_tasks"`
}

// collectWorkspaceStatuses fetches git status for all workspaces concurrently,
// capping parallelism at 4 to avoid overwhelming the system with git subprocesses.
func collectWorkspaceStatuses(workspaces []string) []gitutil.WorkspaceGitStatus {
	results := make([]gitutil.WorkspaceGitStatus, len(workspaces))
	sem := make(chan struct{}, 4) // cap concurrency at 4 git processes
	var wg sync.WaitGroup
	for i, ws := range workspaces {
		wg.Add(1)
		go func(idx int, path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = gitutil.WorkspaceStatus(path)
		}(i, ws)
	}
	wg.Wait()
	return results
}

func workspaceMutationGuardStatuses() []store.TaskStatus {
	return []store.TaskStatus{
		store.TaskStatusInProgress,
		store.TaskStatusWaiting,
		store.TaskStatusCommitting,
		store.TaskStatusFailed,
	}
}

func taskBlocksWorkspaceMutation(task store.Task, workspace string) bool {
	worktreePath, ok := task.WorktreePaths[workspace]
	if !ok || worktreePath == "" {
		return false
	}
	if task.Status != store.TaskStatusFailed {
		return true
	}
	if _, err := os.Stat(worktreePath); err != nil {
		return false
	}
	return true
}

func workspaceMutationBlockingTasks(ctx context.Context, s *store.Store, workspace string) ([]workspaceMutationBlockingTask, error) {
	var blocking []workspaceMutationBlockingTask
	for _, status := range workspaceMutationGuardStatuses() {
		tasks, err := s.ListTasksByStatus(ctx, status)
		if err != nil {
			return nil, err
		}
		for _, task := range tasks {
			if !taskBlocksWorkspaceMutation(task, workspace) {
				continue
			}
			blocking = append(blocking, workspaceMutationBlockingTask{
				ID:     task.ID.String(),
				Title:  task.Title,
				Status: string(task.Status),
			})
		}
	}
	return blocking, nil
}

func (h *Handler) refuseWorkspaceMutationIfBlocked(w http.ResponseWriter, r *http.Request, workspace, action string) bool {
	s, ok := h.currentStore()
	if !ok || s == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no workspaces configured"})
		return true
	}
	blocking, err := workspaceMutationBlockingTasks(r.Context(), s, workspace)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return true
	}
	if len(blocking) == 0 {
		return false
	}
	writeJSON(w, http.StatusConflict, workspaceMutationConflictResponse{
		Error:         fmt.Sprintf("cannot %s workspace while tasks still depend on its local git state", action),
		Workspace:     workspace,
		BlockingTasks: blocking,
	})
	return true
}

// GitStatus returns git status for every configured workspace.
func (h *Handler) GitStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, collectWorkspaceStatuses(h.currentWorkspaces()))
}

// GitStatusStream streams git status for all workspaces as SSE (5-second poll).
func (h *Handler) GitStatusStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	collect := func() []gitutil.WorkspaceGitStatus {
		return collectWorkspaceStatuses(h.currentWorkspaces())
	}

	send := func(statuses []gitutil.WorkspaceGitStatus) bool {
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
			nextData, nextErr := json.Marshal(next)
			curData, curErr := json.Marshal(current)
			if nextErr != nil || curErr != nil {
				logger.Git.Warn("git status marshal error", "next_err", nextErr, "cur_err", curErr)
				if !send(next) {
					return
				}
				current = next
				continue
			}
			if string(nextData) != string(curData) {
				if !send(next) {
					return
				}
				current = next
			}
		}
	}
}

// requireGitRepo checks that workspace is a git repository and writes a
// 400 error if it is not. Returns true when the caller should proceed.
func requireGitRepo(w http.ResponseWriter, workspace string) bool {
	if !gitutil.IsGitRepo(workspace) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": filepath.Base(workspace) + " is not a git repository",
		})
		return false
	}
	return true
}

// GitPush runs `git push` for the requested workspace.
func (h *Handler) GitPush(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Workspace string `json:"workspace"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	if !h.isAllowedWorkspace(req.Workspace) {
		http.Error(w, "workspace not configured", http.StatusBadRequest)
		return
	}
	if !requireGitRepo(w, req.Workspace) {
		return
	}

	logger.Git.Info("push", "workspace", req.Workspace)
	out, err := cmdexec.Git(req.Workspace, "push").WithContext(r.Context()).Combined()
	if err != nil {
		logger.Git.Error("push failed", "workspace", req.Workspace, "error", err)
		http.Error(w, out, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"output": out})
}

// GitSyncWorkspace fetches from remote and rebases the current branch onto its upstream.
func (h *Handler) GitSyncWorkspace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Workspace string `json:"workspace"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	if !h.isAllowedWorkspace(req.Workspace) {
		http.Error(w, "workspace not configured", http.StatusBadRequest)
		return
	}
	if !requireGitRepo(w, req.Workspace) {
		return
	}
	if h.refuseWorkspaceMutationIfBlocked(w, r, req.Workspace, "sync") {
		return
	}

	logger.Git.Info("sync workspace", "workspace", req.Workspace)

	if out, err := cmdexec.Git(req.Workspace, "fetch").WithContext(r.Context()).Combined(); err != nil {
		logger.Git.Error("fetch failed", "workspace", req.Workspace, "error", err)
		http.Error(w, "fetch failed: "+out, http.StatusInternalServerError)
		return
	}

	out, err := cmdexec.Git(req.Workspace, "rebase", "@{u}").WithContext(r.Context()).Combined()
	if err != nil {
		if abortErr := cmdexec.Git(req.Workspace, "rebase", "--abort").Run(); abortErr != nil {
			logger.Git.Warn("rebase abort failed", "workspace", req.Workspace, "error", abortErr)
		}
		logger.Git.Error("sync rebase failed", "workspace", req.Workspace, "error", err)
		if gitutil.IsConflictOutput(out) {
			http.Error(w, "rebase conflict: resolve manually in "+req.Workspace, http.StatusConflict)
			return
		}
		http.Error(w, "rebase failed: "+out, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"output": out})
}

// GitRebaseOnMain fetches the remote default branch and rebases the current branch onto it.
func (h *Handler) GitRebaseOnMain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Workspace string `json:"workspace"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	if !h.isAllowedWorkspace(req.Workspace) {
		http.Error(w, "workspace not configured", http.StatusBadRequest)
		return
	}
	if !requireGitRepo(w, req.Workspace) {
		return
	}
	if h.refuseWorkspaceMutationIfBlocked(w, r, req.Workspace, "rebase") {
		return
	}

	mainBranch := gitutil.RemoteDefaultBranch(req.Workspace)
	logger.Git.Info("rebase-on-main", "workspace", req.Workspace, "main", mainBranch)

	// Fetch the remote default branch.
	if out, err := cmdexec.Git(req.Workspace, "fetch", "origin", mainBranch).WithContext(r.Context()).Combined(); err != nil {
		logger.Git.Error("fetch failed", "workspace", req.Workspace, "error", err)
		http.Error(w, "fetch failed: "+out, http.StatusInternalServerError)
		return
	}

	// Rebase onto origin/<main>.
	out, err := cmdexec.Git(req.Workspace, "rebase", "origin/"+mainBranch).WithContext(r.Context()).Combined()
	if err != nil {
		if abortErr := cmdexec.Git(req.Workspace, "rebase", "--abort").Run(); abortErr != nil {
			logger.Git.Warn("rebase abort failed", "workspace", req.Workspace, "error", abortErr)
		}
		logger.Git.Error("rebase-on-main failed", "workspace", req.Workspace, "error", err)
		if gitutil.IsConflictOutput(out) {
			http.Error(w, "rebase conflict: resolve manually in "+req.Workspace, http.StatusConflict)
			return
		}
		http.Error(w, "rebase failed: "+out, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"output": out})
}

// TaskDiff returns the git diff for a task's worktrees versus the default branch.
// Responses are cached: terminal tasks (done/cancelled/archived) are cached
// indefinitely; active tasks are cached for diffCacheTTL (10 s). ETag and
// Cache-Control headers are set so browsers can issue conditional requests.
func (h *Handler) TaskDiff(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if len(task.WorktreePaths) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"diff": "", "behind_counts": map[string]int{}})
		return
	}

	// Serve from cache when available.
	if entry, ok := h.diffCache.get(id); ok {
		cacheControl := "no-cache"
		if entry.immutable {
			cacheControl = "immutable"
		}
		w.Header().Set("ETag", `"`+entry.etag+`"`)
		w.Header().Set("Cache-Control", cacheControl)
		if r.Header.Get("If-None-Match") == `"`+entry.etag+`"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(entry.payload); err != nil {
			logger.Handler.Debug("diff response write failed", "task", id, "error", err)
		}
		return
	}

	var combined strings.Builder
	behindCounts := make(map[string]int)

	for repoPath, worktreePath := range task.WorktreePaths {
		// Skip non-git workspaces silently — no diff to compute.
		if !gitutil.IsGitRepo(repoPath) {
			continue
		}
		// If the worktree directory no longer exists, fall back to stored commit hashes.
		if _, statErr := os.Stat(worktreePath); statErr != nil {
			commitHash := task.CommitHashes[repoPath]
			var out string
			if commitHash != "" {
				if baseHash := task.BaseCommitHashes[repoPath]; baseHash != "" {
					var gitErr error
					out, gitErr = cmdexec.Git(repoPath, "diff", baseHash, commitHash).WithContext(r.Context()).Output()
					if gitErr != nil {
						logger.Git.Debug("git diff base..commit failed", "repo", repoPath, "error", gitErr)
					}
				} else {
					var gitErr error
					out, gitErr = cmdexec.Git(repoPath, "show", commitHash).WithContext(r.Context()).Output()
					if gitErr != nil {
						logger.Git.Debug("git show commit failed", "repo", repoPath, "error", gitErr)
					}
				}
			} else if task.BranchName != "" {
				if defBranch, err := gitutil.DefaultBranch(repoPath); err == nil {
					// Use merge-base so we only see changes introduced on the task
					// branch, not the inverse of commits that advanced main.
					if base, mbErr := gitutil.MergeBase(repoPath, defBranch, task.BranchName); mbErr == nil {
						var gitErr error
						out, gitErr = cmdexec.Git(repoPath, "diff", base, task.BranchName).WithContext(r.Context()).Output()
						if gitErr != nil {
							logger.Git.Debug("git diff merge-base..branch failed", "repo", repoPath, "error", gitErr)
						}
					} else {
						var gitErr error
						out, gitErr = cmdexec.Git(repoPath, "diff", defBranch+".."+task.BranchName).WithContext(r.Context()).Output()
						if gitErr != nil {
							logger.Git.Debug("git diff default..branch failed", "repo", repoPath, "error", gitErr)
						}
					}
				}
			}
			if len(out) > 0 {
				if len(task.WorktreePaths) > 1 {
					fmt.Fprintf(&combined, "=== %s ===\n", filepath.Base(repoPath))
				}
				combined.WriteString(out)
			}
			continue
		}

		defBranch, err := gitutil.DefaultBranch(repoPath)
		if err != nil {
			continue
		}
		// Use merge-base to diff only this task's changes since it diverged,
		// ignoring any commits that advanced the default branch from other tasks.
		// Fall back to diffing against the default branch tip if merge-base fails.
		base, err := gitutil.MergeBase(worktreePath, "HEAD", defBranch)
		if err != nil {
			base = defBranch
		}
		out, diffErr := cmdexec.Git(worktreePath, "diff", base).WithContext(r.Context()).Output()
		if diffErr != nil {
			logger.Git.Debug("git diff base failed", "worktree", worktreePath, "error", diffErr)
		}

		// Include untracked files via --no-index diffs.
		if untrackedRaw, err := cmdexec.Git(worktreePath,
			"ls-files", "--others", "--exclude-standard").WithContext(r.Context()).Output(); err == nil {
			for _, file := range strings.Split(untrackedRaw, "\n") {
				if file == "" {
					continue
				}
				fd, _ := cmdexec.Git(worktreePath,
					"diff", "--no-index", "/dev/null", file).WithContext(r.Context()).Output()
				out += fd
			}
		}

		if len(out) > 0 {
			if len(task.WorktreePaths) > 1 {
				fmt.Fprintf(&combined, "=== %s ===\n", filepath.Base(repoPath))
			}
			combined.WriteString(out)
		}
		if n, err := gitutil.CommitsBehind(repoPath, worktreePath); err == nil && n > 0 {
			behindCounts[filepath.Base(repoPath)] = n
		}
	}

	// Serialize, cache, and write the response.
	payload, err := json.Marshal(map[string]any{
		"diff":          combined.String(),
		"behind_counts": behindCounts,
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	etag := diffETag(payload)
	immutable := (task.Status == store.TaskStatusDone || task.Status == store.TaskStatusCancelled) || task.Archived
	// Don't cache diff results for in_progress tasks: their worktrees are
	// actively being modified (sync, execution) so the computed diff/behind
	// counts are ephemeral and would become stale when the operation finishes.
	if task.Status != store.TaskStatusInProgress {
		entry := diffCacheEntry{
			payload:   payload,
			etag:      etag,
			immutable: immutable,
		}
		h.diffCache.set(id, entry)
	}

	// Terminal tasks are immutable — browsers can cache forever. Active
	// tasks use no-cache so the browser always revalidates via ETag; the
	// server's in-memory diffCache handles repeat-request efficiency.
	// Using max-age for active tasks would let the browser serve stale
	// behind_counts after sync completes.
	cacheControl := "no-cache"
	if immutable {
		cacheControl = "immutable"
	}
	w.Header().Set("ETag", `"`+etag+`"`)
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(payload); err != nil {
		logger.Handler.Debug("diff response write failed", "task", id, "error", err)
	}
}

// GitBranches returns the list of local branches for a workspace.
func (h *Handler) GitBranches(w http.ResponseWriter, r *http.Request) {
	ws := r.URL.Query().Get("workspace")
	if ws == "" {
		http.Error(w, "workspace query param required", http.StatusBadRequest)
		return
	}
	if !h.isAllowedWorkspace(ws) {
		http.Error(w, "workspace not configured", http.StatusBadRequest)
		return
	}
	if !requireGitRepo(w, ws) {
		return
	}

	out, err := cmdexec.Git(ws, "branch", "--list", "--format=%(refname:short)").WithContext(r.Context()).Output()
	if err != nil {
		http.Error(w, "failed to list branches", http.StatusInternalServerError)
		return
	}

	var branches []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			branches = append(branches, line)
		}
	}

	current := ""
	if curOut, err := cmdexec.Git(ws, "branch", "--show-current").WithContext(r.Context()).Output(); err == nil {
		current = curOut
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"branches": branches,
		"current":  current,
	})
}

// GitCheckout switches the active branch for a workspace.
func (h *Handler) GitCheckout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Workspace string `json:"workspace"`
		Branch    string `json:"branch"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	if !h.isAllowedWorkspace(req.Workspace) {
		http.Error(w, "workspace not configured", http.StatusBadRequest)
		return
	}
	if !requireGitRepo(w, req.Workspace) {
		return
	}

	// Validate branch name: must not contain "..", spaces, or control characters.
	if req.Branch == "" || strings.Contains(req.Branch, "..") || strings.ContainsAny(req.Branch, " \t\n\r") {
		http.Error(w, "invalid branch name", http.StatusBadRequest)
		return
	}
	if h.refuseWorkspaceMutationIfBlocked(w, r, req.Workspace, "switch branches for") {
		return
	}

	logger.Git.Info("checkout", "workspace", req.Workspace, "branch", req.Branch)
	out, err := cmdexec.Git(req.Workspace, "checkout", req.Branch).WithContext(r.Context()).Combined()
	if err != nil {
		logger.Git.Error("checkout failed", "workspace", req.Workspace, "branch", req.Branch, "error", err)
		http.Error(w, out, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"branch": req.Branch})
}

// GitCreateBranch creates a new branch in the workspace and checks it out.
func (h *Handler) GitCreateBranch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Workspace string `json:"workspace"`
		Branch    string `json:"branch"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	if !h.isAllowedWorkspace(req.Workspace) {
		http.Error(w, "workspace not configured", http.StatusBadRequest)
		return
	}
	if !requireGitRepo(w, req.Workspace) {
		return
	}

	// Validate branch name: must not contain "..", spaces, or control characters.
	if req.Branch == "" || strings.Contains(req.Branch, "..") || strings.ContainsAny(req.Branch, " \t\n\r") {
		http.Error(w, "invalid branch name", http.StatusBadRequest)
		return
	}
	if h.refuseWorkspaceMutationIfBlocked(w, r, req.Workspace, "create branches for") {
		return
	}

	logger.Git.Info("create-branch", "workspace", req.Workspace, "branch", req.Branch)
	out, err := cmdexec.Git(req.Workspace, "checkout", "-b", req.Branch).WithContext(r.Context()).Combined()
	if err != nil {
		logger.Git.Error("create-branch failed", "workspace", req.Workspace, "branch", req.Branch, "error", err)
		http.Error(w, out, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"branch": req.Branch})
}

// OpenFolder opens a workspace directory in the OS file manager (Finder on macOS, xdg-open on Linux).
func (h *Handler) OpenFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	if !h.isAllowedWorkspace(req.Path) {
		http.Error(w, "workspace not configured", http.StatusBadRequest)
		return
	}

	var cmd *cmdexec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = cmdexec.New("open", req.Path).WithContext(r.Context())
	case "windows":
		cmd = cmdexec.New("explorer", req.Path).WithContext(r.Context())
	default:
		cmd = cmdexec.New("xdg-open", req.Path).WithContext(r.Context())
	}

	if err := cmd.Run(); err != nil {
		http.Error(w, "failed to open folder: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// isAllowedWorkspace checks that the workspace path is one the server was started with.
func (h *Handler) isAllowedWorkspace(ws string) bool {
	for _, configured := range h.currentWorkspaces() {
		if configured == ws {
			return true
		}
	}
	return false
}
