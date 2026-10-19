package runner

import (
	"context"
	"fmt"
	"os"
	"strings"

	"latere.ai/x/pkg/gitutil"

	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/store"
)

// missingRecoveryWorktrees returns the repo paths whose worktree directories
// are either empty strings or missing from the filesystem. Used during startup
// recovery to detect tasks with broken worktree state.
func missingRecoveryWorktrees(t store.Task) []string {
	var missing []string
	for repoPath, worktreePath := range t.WorktreePaths {
		if worktreePath == "" {
			missing = append(missing, repoPath)
			continue
		}
		if _, err := os.Stat(worktreePath); err != nil {
			missing = append(missing, repoPath)
		}
	}
	return missing
}

// markTaskFailedForMissingWorktrees transitions a task to failed with a
// worktree_setup failure category when its worktree directories are missing.
func markTaskFailedForMissingWorktrees(ctx context.Context, s *store.Store, task store.Task, from store.TaskStatus, trigger store.Trigger) {
	message := fmt.Sprintf("task worktree missing for: %s", strings.Join(missingRecoveryWorktrees(task), ", "))
	logger.Recovery.Warn("task worktree missing during recovery", "task", task.ID, "from", from, "error", message)
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed); err != nil {
		logger.Recovery.Warn("markTaskFailed: ForceUpdateTaskStatus", "task", task.ID, "error", err)
	}
	if err := s.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryWorktree); err != nil {
		logger.Recovery.Warn("markTaskFailed: SetTaskFailureCategory", "task", task.ID, "error", err)
	}
	if err := s.InsertEvent(ctx, task.ID, store.EventTypeError, map[string]string{
		"error": message,
	}); err != nil {
		logger.Recovery.Warn("markTaskFailed: InsertEvent error", "task", task.ID, "error", err)
	}
	if err := s.InsertEvent(ctx, task.ID, store.EventTypeStateChange,
		store.NewStateChangeData(from, store.TaskStatusFailed, trigger, nil)); err != nil {
		logger.Recovery.Warn("markTaskFailed: InsertEvent stateChange", "task", task.ID, "error", err)
	}
}

// RecoverOrphanedTasks reconciles in_progress/committing tasks on startup. The
// server tracks no agent process across a restart, so a task found in
// progress has no run to follow.
//
//   - committing tasks: inspects each worktree's git branch to determine whether
//     a commit landed after the task's UpdatedAt timestamp. If so, the commit
//     pipeline completed before the crash and the task is promoted to done.
//     Otherwise it is moved to failed.
//   - in_progress tasks are moved to waiting so the user can inspect the partial
//     results and decide what to do next, or to failed when their worktrees
//     are missing.
//
// When r is set, orphaned worktree directories are removed afterwards.
func RecoverOrphanedTasks(ctx context.Context, s *store.Store, r *Runner) {
	tasks, err := s.ListTasks(ctx, true)
	if err != nil {
		logger.Recovery.Error("list tasks", "error", err)
		return
	}

	for _, t := range tasks {
		switch t.Status {
		case store.TaskStatusCommitting:
			// Check whether a commit landed after the last recorded state change.
			// If so, the commit pipeline completed just before the crash and the
			// task should be promoted to done rather than failed.
			//
			// ForceUpdateTaskStatus is used here because a crash may leave a task in an
			// unexpected state. Recovery must always complete regardless of the normal
			// allowed transitions.
			recovered := false
			for repoPath := range t.WorktreePaths {
				hash, _, commitTS, err := gitutil.BranchTipCommit(repoPath, t.BranchName)
				if err != nil {
					// Not a git repo or branch missing — skip.
					continue
				}
				if commitTS.After(t.UpdatedAt) {
					recovered = true
					logger.Recovery.Warn("task was committing at startup; commit found after UpdatedAt, auto-recovering to done",
						"task", t.ID, "repo", repoPath, "commit", hash, "recovered", true)
					// Only claim the recovery once it has actually persisted.
					// Emitting the state-change event regardless would leave the
					// task in committing while its trace says done, and this is
					// the one path a user cannot redrive from the UI.
					if err := s.ForceUpdateTaskStatus(ctx, t.ID, store.TaskStatusDone); err != nil {
						logger.Recovery.Error("auto-recovery to done did not persist; task stays in committing for the next startup",
							"task", t.ID, "error", err)
						break
					}
					_ = s.InsertEvent(ctx, t.ID, store.EventTypeSystem, map[string]string{

						"result": "server restarted after commit completed; auto-recovered to done",
					})
					_ = s.InsertEvent(ctx, t.ID, store.EventTypeStateChange,

						store.NewStateChangeData(store.TaskStatusCommitting, store.TaskStatusDone, store.TriggerRecovery, nil))
					break
				}
			}
			if !recovered {
				logger.Recovery.Warn("task was committing at startup, marking as failed",
					"task", t.ID, "recovered", false)
				if err := s.ForceUpdateTaskStatus(ctx, t.ID, store.TaskStatusFailed); err != nil {
					logger.Recovery.Error("marking an interrupted commit failed did not persist; task stays in committing for the next startup",
						"task", t.ID, "error", err)
				} else {
					_ = s.InsertEvent(ctx, t.ID, store.EventTypeError, map[string]string{

						"error": "server restarted during commit",
					})
					_ = s.InsertEvent(ctx, t.ID, store.EventTypeStateChange,

						store.NewStateChangeData(store.TaskStatusCommitting, store.TaskStatusFailed, store.TriggerRecovery, nil))
				}
			}

		case store.TaskStatusInProgress:
			if len(missingRecoveryWorktrees(t)) > 0 {
				markTaskFailedForMissingWorktrees(ctx, s, t, store.TaskStatusInProgress, store.TriggerRecovery)
				continue
			}
			// ForceUpdateTaskStatus is used here because a crash may leave a task in an
			// unexpected state. Recovery must always complete regardless of the normal
			// allowed transitions.
			logger.Recovery.Warn("task was running at startup, moving to waiting", "task", t.ID)
			_ = s.ForceUpdateTaskStatus(ctx, t.ID, store.TaskStatusWaiting)

			_ = s.InsertEvent(ctx, t.ID, store.EventTypeSystem, map[string]string{

				"result": "Server restarted while task was running. Please review the output and decide whether to continue or mark as done.",
			})
			_ = s.InsertEvent(ctx, t.ID, store.EventTypeStateChange,

				store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusWaiting, store.TriggerRecovery, nil))
		}
	}

	// Startup worktree GC: remove any orphaned worktree directories whose
	// tasks have reached a terminal state or no longer exist in the store.
	if r != nil {
		orphans, err := r.ScanOrphanedWorktrees(ctx)
		if err != nil {
			logger.Recovery.Warn("worktree GC scan failed", "error", err)
		} else if len(orphans) > 0 {
			removed := r.PruneOrphanedWorktrees(ctx, orphans)
			logger.Recovery.Info("startup worktree GC complete", "scanned", len(orphans), "removed", removed)
		}
	}
}
