package handler

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/coordinator"
	"latere.ai/x/wallfacer/internal/github"
	"latere.ai/x/wallfacer/internal/gitutil"
	"latere.ai/x/wallfacer/internal/pkg/httpjson"
	"latere.ai/x/wallfacer/internal/store"
)

// Task-scoped GitHub PR surface (spec: github-integration, task-centric redesign):
//
//	GET  /api/tasks/{id}/pr          -> the task branch's PR, or null
//	POST /api/tasks/{id}/pr          -> create (or return existing) PR for the branch
//	POST /api/tasks/{id}/pr/comment  -> comment on the task's PR
//
// Everything is derived from the task: the repo from its workspace git origin,
// the head from its branch, the base from the repo's default branch. There is
// no repo picker -- a task is a branch in a repo.

// taskRepoRef resolves the GitHub owner/name, base, and head branch for a task
// from its primary git worktree. ok is false when the task has no branch or no
// GitHub (github.com) origin.
func taskRepoRef(task *store.Task) (owner, name, base, head string, ok bool) {
	head = task.BranchName
	if head == "" {
		return "", "", "", "", false
	}
	for repoPath := range task.WorktreePaths {
		if !gitutil.IsGitRepo(repoPath) {
			continue
		}
		origin := gitutil.WorkspaceStatus(repoPath).RemoteURL
		// NormalizeRemoteURL yields "host/owner/repo"; the write API wants
		// owner/name on github.com.
		parts := strings.SplitN(coordinator.NormalizeRemoteURL(origin), "/", 3)
		if len(parts) != 3 || parts[0] != "github.com" {
			continue
		}
		owner, name = parts[1], parts[2]
		if b, err := gitutil.DefaultBranch(repoPath); err == nil && b != "" {
			base = b
		} else {
			base = "main"
		}
		return owner, name, base, head, true
	}
	return "", "", "", "", false
}

// taskForPR loads the task and resolves its repo ref, writing the appropriate
// error response when the task is missing or has no GitHub branch.
func (h *Handler) taskForPR(w http.ResponseWriter, r *http.Request, id uuid.UUID) (*store.Task, string, string, string, string, bool) {
	s, ok := h.requireStore(w)
	if !ok {
		return nil, "", "", "", "", false
	}
	task, err := s.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return nil, "", "", "", "", false
	}
	owner, name, base, head, ok := taskRepoRef(task)
	if !ok {
		http.Error(w, "task has no GitHub branch (needs a pushed branch on a github.com repo)", http.StatusBadRequest)
		return nil, "", "", "", "", false
	}
	return task, owner, name, base, head, true
}

// TaskPRStatus returns the open PR for the task's branch, or {pull_request:null}.
func (h *Handler) TaskPRStatus(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	_, owner, name, _, head, ok := h.taskForPR(w, r, id)
	if !ok {
		return
	}
	tok, ok := h.githubToken(w, r)
	if !ok {
		return
	}
	pr, err := github.PullForBranch(r.Context(), h.github.APIClient(), tok, owner, name, head)
	if err != nil {
		mapGitHubAPIError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"pull_request": pr})
}

// CreateTaskPR creates (or returns the existing) PR for the task's branch.
// Title/body default from the task; a request body may override them.
func (h *Handler) CreateTaskPR(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, owner, name, base, head, ok := h.taskForPR(w, r, id)
	if !ok {
		return
	}
	body, _ := httpjson.DecodeOptionalBody[struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Draft bool   `json:"draft"`
	}](w, r)
	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = prTitleForTask(task)
	}
	prBody := body.Body
	if strings.TrimSpace(prBody) == "" {
		prBody = task.CommitMessage
	}
	tok, ok := h.githubToken(w, r)
	if !ok {
		return
	}
	pr, err := github.CreatePull(r.Context(), h.github.APIClient(), tok, owner, name, github.CreatePullParams{
		Title: title, Body: prBody, Head: head, Base: base, Draft: body.Draft,
	})
	if err != nil {
		mapGitHubAPIError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, pr)
}

// TaskPRComment posts a comment to the task's PR.
func (h *Handler) TaskPRComment(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	_, owner, name, _, head, ok := h.taskForPR(w, r, id)
	if !ok {
		return
	}
	body, ok := httpjson.DecodeBody[struct {
		Body string `json:"body"`
	}](w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(body.Body) == "" {
		http.Error(w, "body is required", http.StatusBadRequest)
		return
	}
	tok, ok := h.githubToken(w, r)
	if !ok {
		return
	}
	pr, err := github.PullForBranch(r.Context(), h.github.APIClient(), tok, owner, name, head)
	if err != nil {
		mapGitHubAPIError(w, err)
		return
	}
	if pr == nil {
		http.Error(w, "no pull request for this task yet", http.StatusNotFound)
		return
	}
	cm, err := github.CreateComment(r.Context(), h.github.APIClient(), tok, owner, name, pr.Number, body.Body)
	if err != nil {
		mapGitHubAPIError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, cm)
}

// prTitleForTask derives a PR title from the task, preferring its title, then a
// trimmed prompt, then a branch-based fallback.
func prTitleForTask(task *store.Task) string {
	if t := strings.TrimSpace(task.Title); t != "" {
		return t
	}
	if p := strings.TrimSpace(task.Prompt); p != "" {
		if len(p) > 72 {
			p = p[:72]
		}
		return strings.SplitN(p, "\n", 2)[0]
	}
	return "Changes from " + task.BranchName
}
