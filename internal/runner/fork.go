package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"changkun.de/wallfacer/internal/gitutil"
	"changkun.de/wallfacer/internal/logger"
	"github.com/google/uuid"
)

// Fork sets up a new task's worktrees by branching from the source task's
// current commit state. For done tasks the merge commit (CommitHashes) is
// used; for waiting/failed tasks the live HEAD of the source worktree is used.
//
// Fork acquires worktreeMu, so callers must not hold it.
// After Fork returns the new task's WorktreePaths and BranchName are persisted.
func (r *Runner) Fork(ctx context.Context, sourceID, newTaskID uuid.UUID) error {
	source, err := r.store.GetTask(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("get source task: %w", err)
	}
	if len(source.WorktreePaths) == 0 {
		return fmt.Errorf("source task has no worktrees")
	}

	shortID := newTaskID.String()[:8]
	branchName := "task/" + shortID
	worktreePaths := make(map[string]string)

	r.worktreeMu.Lock()
	defer r.worktreeMu.Unlock()

	for repoPath, sourceWorktreePath := range source.WorktreePaths {
		worktreeDir := filepath.Join(r.worktreesDir, newTaskID.String(), filepath.Base(repoPath))
		if err := os.MkdirAll(filepath.Dir(worktreeDir), 0755); err != nil {
			return fmt.Errorf("create worktree parent for %s: %w", repoPath, err)
		}

		if !gitutil.IsGitRepo(repoPath) {
			// Non-git workspace: copy the source worktree snapshot.
			if err := setupNonGitSnapshot(sourceWorktreePath, worktreeDir); err != nil {
				for createdRepo, createdPath := range worktreePaths {
					_ = createdRepo
					_ = createdPath
				}
				taskDir := filepath.Join(r.worktreesDir, newTaskID.String())
				os.RemoveAll(taskDir)
				return fmt.Errorf("fork non-git snapshot for %s: %w", repoPath, err)
			}
			worktreePaths[repoPath] = worktreeDir
			logger.Runner.Info("fork: copied non-git snapshot",
				"new_task", newTaskID,
				"repo", repoPath,
			)
			continue
		}

		// Determine the commit to branch from:
		//   1. For done tasks: CommitHashes holds the post-merge commit in the
		//      default branch — use that so the fork starts from merged code.
		//   2. Otherwise: resolve HEAD of the live source worktree.
		var baseCommit string
		if hash, ok := source.CommitHashes[repoPath]; ok && hash != "" {
			baseCommit = hash
		} else {
			baseCommit, err = gitutil.ResolveHead(sourceWorktreePath)
			if err != nil {
				return fmt.Errorf("resolve source worktree HEAD for %s: %w", repoPath, err)
			}
		}

		if err := gitutil.CreateWorktreeAt(repoPath, worktreeDir, branchName, baseCommit); err != nil {
			// Best-effort cleanup of successfully created worktrees before returning.
			for createdRepo, createdPath := range worktreePaths {
				gitutil.RemoveWorktree(createdRepo, createdPath, branchName)
			}
			taskDir := filepath.Join(r.worktreesDir, newTaskID.String())
			os.RemoveAll(taskDir)
			return fmt.Errorf("create forked worktree for %s: %w", repoPath, err)
		}

		worktreePaths[repoPath] = worktreeDir
		logger.Runner.Info("fork: created worktree",
			"new_task", newTaskID,
			"repo", repoPath,
			"branch", branchName,
			"base", baseCommit,
		)
	}

	return r.store.UpdateTaskWorktrees(ctx, newTaskID, worktreePaths, branchName)
}
