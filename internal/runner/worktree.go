package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"changkun.de/wallfacer/internal/gitutil"
	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// setupWorktrees creates an isolated working directory for each workspace.
// For git-backed workspaces a proper git worktree is created.
// For non-git workspaces a snapshot copy is created and tracked with a local
// git repo so that the same commit pipeline can be used for both cases.
// Returns (worktreePaths, branchName, error).
// Idempotent: if the worktree/snapshot directory already exists it is reused.
func (r *Runner) setupWorktrees(taskID uuid.UUID) (map[string]string, string, error) {
	return r.ensureTaskWorktrees(taskID, nil, "")
}

func (r *Runner) ensureTaskWorktrees(taskID uuid.UUID, existing map[string]string, branchName string) (map[string]string, string, error) {
	r.worktreeMu.Lock()
	defer r.worktreeMu.Unlock()

	if branchName == "" {
		branchName = "task/" + taskID.String()[:8]
	}
	worktreePaths := make(map[string]string)
	createdPaths := make(map[string]string)

	repos := r.Workspaces()
	if len(existing) > 0 {
		repos = make([]string, 0, len(existing))
		for repoPath := range existing {
			repos = append(repos, repoPath)
		}
	}

	for _, ws := range repos {
		basename := filepath.Base(ws)
		worktreePath := ""
		if existing != nil {
			worktreePath = existing[ws]
		}
		if worktreePath == "" {
			worktreePath = filepath.Join(r.worktreesDir, taskID.String(), basename)
		}

		// Idempotent: reuse existing worktree/snapshot (e.g. task resumed from waiting).
		if _, err := os.Stat(worktreePath); err == nil {
			worktreePaths[ws] = worktreePath
			continue
		}

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0755); err != nil {
			r.cleanupWorktrees(taskID, createdPaths, branchName)
			return nil, "", fmt.Errorf("mkdir worktree parent: %w", err)
		}

		if gitutil.IsGitRepo(ws) {
			if err := gitutil.CreateWorktree(ws, worktreePath, branchName); errors.Is(err, gitutil.ErrEmptyRepo) {
				// Empty repo (no commits) — fall back to snapshot so
				// the task can still run with a local git for tracking.
				logger.Runner.Warn("empty git repo, using snapshot instead", "workspace", ws)
				if err := setupNonGitSnapshot(ws, worktreePath); err != nil {
					r.cleanupWorktrees(taskID, createdPaths, branchName)
					return nil, "", fmt.Errorf("snapshot for empty repo %s: %w", ws, err)
				}
			} else if err != nil {
				r.cleanupWorktrees(taskID, createdPaths, branchName)
				return nil, "", fmt.Errorf("createWorktree for %s: %w", ws, err)
			}
		} else {
			if err := setupNonGitSnapshot(ws, worktreePath); err != nil {
				r.cleanupWorktrees(taskID, createdPaths, branchName)
				return nil, "", fmt.Errorf("snapshot for %s: %w", ws, err)
			}
		}

		worktreePaths[ws] = worktreePath
		createdPaths[ws] = worktreePath
	}

	return worktreePaths, branchName, nil
}

// EnsureTaskWorktrees recreates missing task worktrees when the task branch
// still exists (for example after a lost linked-worktree directory). Existing
// worktrees are reused unchanged.
func (r *Runner) EnsureTaskWorktrees(taskID uuid.UUID, existing map[string]string, branchName string) (map[string]string, string, error) {
	return r.ensureTaskWorktrees(taskID, existing, branchName)
}

// CleanupWorktrees is the exported variant of cleanupWorktrees for handler use.
func (r *Runner) CleanupWorktrees(taskID uuid.UUID, worktreePaths map[string]string, branchName string) {
	r.worktreeMu.Lock()
	defer r.worktreeMu.Unlock()
	r.cleanupWorktrees(taskID, worktreePaths, branchName)
}

// cleanupWorktrees removes all worktrees/snapshots for a task and the task's
// directory. Must be called with r.worktreeMu held (use CleanupWorktrees for
// the public API). Safe to call multiple times — errors are logged as warnings.
func (r *Runner) cleanupWorktrees(taskID uuid.UUID, worktreePaths map[string]string, branchName string) {
	bgCtx := context.Background()
	r.store.InsertEvent(bgCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "worktree_cleanup", Label: "worktree_cleanup"})
	for repoPath, wt := range worktreePaths {
		if !gitutil.IsGitRepo(repoPath) || !gitutil.HasCommits(repoPath) {
			// Non-git snapshots and empty-repo snapshots are cleaned by
			// os.RemoveAll below — they were never real git worktrees.
			continue
		}
		if err := gitutil.RemoveWorktree(repoPath, wt, branchName); err != nil {
			logger.Runner.Warn("remove worktree", "task", taskID, "repo", repoPath, "error", err)
		}
	}
	taskWorktreeDir := filepath.Join(r.worktreesDir, taskID.String())
	if err := os.RemoveAll(taskWorktreeDir); err != nil && !os.IsNotExist(err) {
		logger.Runner.Warn("remove worktree dir", "task", taskID, "error", err)
	}
	r.store.InsertEvent(bgCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "worktree_cleanup", Label: "worktree_cleanup"})
}

// PruneUnknownWorktrees scans worktreesDir for directories whose UUID does not
// match any known task, removes them, and runs `git worktree prune` on all
// git workspaces to clean up stale internal references.
func (r *Runner) PruneUnknownWorktrees() {
	r.worktreeMu.Lock()
	defer r.worktreeMu.Unlock()

	entries, err := os.ReadDir(r.worktreesDir)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Runner.Warn("read worktrees dir", "error", err)
		}
		return
	}

	ctx := r.shutdownCtx
	knownIDs := map[string]bool{}
	if r.store != nil {
		tasks, _ := r.store.ListTasks(ctx, true)
		knownIDs = make(map[string]bool, len(tasks))
		for _, t := range tasks {
			knownIDs[t.ID.String()] = true
		}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if knownIDs[entry.Name()] {
			continue
		}
		orphanDir := filepath.Join(r.worktreesDir, entry.Name())
		logger.Runner.Warn("pruning orphaned worktree dir", "dir", orphanDir)
		os.RemoveAll(orphanDir)
	}

	// Run `git worktree prune` on all workspaces to clean stale references.
	for _, ws := range r.Workspaces() {
		if gitutil.IsGitRepo(ws) {
			gitPrune(ws)
		}
	}
}

func gitPrune(repoPath string) {
	// best-effort; errors are silently ignored
	_ = runGit(repoPath, "worktree", "prune")
}
