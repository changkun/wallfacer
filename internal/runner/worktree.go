package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"changkun.de/x/wallfacer/internal/gitutil"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/store"
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

// ensureTaskWorktrees creates or reuses worktrees for each workspace. When existing
// is non-nil, only those repos are processed (reattach mode). Otherwise all configured
// workspaces are used. For git repos a proper worktree is created; for non-git
// workspaces a snapshot copy with a local git repo is set up instead. The worktreeMu
// must be held by the caller (or this method acquires it internally).
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
		// The directory must also be a valid git repo; if the .git link inside the
		// worktree was deleted or corrupted (e.g. by a container), we tear the
		// directory down and recreate it below.
		if _, err := os.Stat(worktreePath); err == nil {
			if gitutil.IsGitRepo(worktreePath) {
				worktreePaths[ws] = worktreePath
				continue
			}
			logger.Runner.Warn("worktree directory exists but is not a valid git repo, removing",
				"workspace", ws, "path", worktreePath)
			_ = os.RemoveAll(worktreePath)

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
	bgCtx := r.shutdownCtx
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "worktree_cleanup", Label: "worktree_cleanup"})

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
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "worktree_cleanup", Label: "worktree_cleanup"})

}

// PruneUnknownWorktrees scans worktreesDir for directories whose task is in a
// terminal state (archived or deleted) and removes them. Directories whose
// task UUID is not found in the current store are left alone — they may belong
// to a different workspace scope and must not be destroyed.
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

	if r.store == nil {
		return
	}

	ctx := r.shutdownCtx

	// Build a map of task IDs to their pruning eligibility.
	// Only tasks that are archived or deleted (tombstoned) should have
	// their worktrees removed. Tasks not in the store are left alone
	// because they may belong to a different workspace scope.
	pruneIDs := map[string]bool{}
	tasks, _ := r.store.ListTasks(ctx, true)
	for _, t := range tasks {
		if t.Archived ||
			t.Status == store.TaskStatusDone ||
			t.Status == store.TaskStatusCancelled {
			pruneIDs[t.ID.String()] = true
		}
	}
	// Also include soft-deleted (tombstoned) tasks.
	deleted, _ := r.store.ListDeletedTasks(ctx)
	for _, t := range deleted {
		pruneIDs[t.ID.String()] = true
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !pruneIDs[entry.Name()] {
			continue
		}
		orphanDir := filepath.Join(r.worktreesDir, entry.Name())
		logger.Runner.Warn("pruning orphaned worktree dir", "dir", orphanDir)
		_ = os.RemoveAll(orphanDir)
	}

	// NOTE: do NOT run `git worktree prune` here. Pruning removes
	// .git/worktrees/<name>/ entries whose linked directories were just
	// deleted above (orphan removal). However, an active task's worktree
	// may share the same entry name (e.g. "wallfacer") if the entry was
	// reused after a previous task completed. Pruning that entry breaks
	// the active worktree's .git file (which references the now-deleted
	// .git/worktrees/<name>/), causing the health watcher to detect a
	// broken repo, delete the directory, and recreate the worktree from
	// HEAD — destroying all committed work on the task branch.
	//
	// Stale worktree entries are cleaned up by the periodic GC
	// (StartWorktreeGC) and by RemoveWorktree during normal task cleanup.
}
