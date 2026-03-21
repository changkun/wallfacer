package runner

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/gitutil"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

const containerPollInterval = 5 * time.Second

// ContainerLister can enumerate currently running containers.
type ContainerLister interface {
	ListContainers() ([]ContainerInfo, error)
}

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

func markTaskFailedForMissingWorktrees(ctx context.Context, s *store.Store, task store.Task, from store.TaskStatus, trigger store.Trigger) {
	message := fmt.Sprintf("task worktree missing for: %s", strings.Join(missingRecoveryWorktrees(task), ", "))
	logger.Recovery.Warn("task worktree missing during recovery", "task", task.ID, "from", from, "error", message)
	_ = s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusFailed)

	_ = s.SetTaskFailureCategory(ctx, task.ID, store.FailureCategoryWorktree)

	_ = s.InsertEvent(ctx, task.ID, store.EventTypeError, map[string]string{

		"error": message,
	})
	_ = s.InsertEvent(ctx, task.ID, store.EventTypeStateChange,

		store.NewStateChangeData(from, store.TaskStatusFailed, trigger, nil))
}

// RecoverOrphanedTasks reconciles in_progress/committing tasks on startup by
// checking which containers are still running.
//
//   - committing tasks: inspects each worktree's git branch to determine whether
//     a commit landed after the task's UpdatedAt timestamp. If so, the commit
//     pipeline completed before the crash and the task is promoted to done.
//     Otherwise it is moved to failed.
//   - in_progress tasks whose container is still running are left in_progress; a
//     background goroutine monitors the container and moves the task to waiting
//     once it stops.
//   - in_progress tasks whose container is already gone are moved to waiting so
//     the user can inspect the partial results and decide what to do next.
func RecoverOrphanedTasks(ctx context.Context, s *store.Store, lister ContainerLister) {
	tasks, err := s.ListTasks(ctx, true)
	if err != nil {
		logger.Recovery.Error("list tasks", "error", err)
		return
	}

	// Build a set of task IDs whose containers are currently running.
	runningContainers := map[string]bool{}
	if containers, listErr := lister.ListContainers(); listErr != nil {
		logger.Recovery.Warn("could not list containers during recovery; treating all in_progress tasks as stopped",
			"error", listErr)
	} else {
		for _, c := range containers {
			if c.State == "running" && c.TaskID != "" {
				runningContainers[c.TaskID] = true
			}
		}
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
					_ = s.ForceUpdateTaskStatus(ctx, t.ID, store.TaskStatusDone)

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
				_ = s.ForceUpdateTaskStatus(ctx, t.ID, store.TaskStatusFailed)

				_ = s.InsertEvent(ctx, t.ID, store.EventTypeError, map[string]string{

					"error": "server restarted during commit",
				})
				_ = s.InsertEvent(ctx, t.ID, store.EventTypeStateChange,

					store.NewStateChangeData(store.TaskStatusCommitting, store.TaskStatusFailed, store.TriggerRecovery, nil))
			}

		case store.TaskStatusInProgress:
			if runningContainers[t.ID.String()] {
				// Container is still active — leave the task in_progress and
				// monitor it; move to waiting once the container stops.
				logger.Recovery.Info("container still running after restart, monitoring",
					"task", t.ID)
				_ = s.InsertEvent(ctx, t.ID, store.EventTypeSystem, map[string]string{

					"result": "Server restarted while task was running. Container is still active — monitoring for completion.",
				})
				go monitorContainerUntilStopped(ctx, s, lister, t.ID)
			} else {
				if len(missingRecoveryWorktrees(t)) > 0 {
					markTaskFailedForMissingWorktrees(ctx, s, t, store.TaskStatusInProgress, store.TriggerRecovery)
					continue
				}
				// Container is gone — move to waiting so the user can review
				// partial results and decide whether to continue or finish.
				//
				// ForceUpdateTaskStatus is used here because a crash may leave a task in an
				// unexpected state. Recovery must always complete regardless of the normal
				// allowed transitions.
				logger.Recovery.Warn("task container gone after restart, moving to waiting",
					"task", t.ID)
				_ = s.ForceUpdateTaskStatus(ctx, t.ID, store.TaskStatusWaiting)

				_ = s.InsertEvent(ctx, t.ID, store.EventTypeSystem, map[string]string{

					"result": "Server restarted while task was running. Container is no longer active — please review the output and decide whether to continue or mark as done.",
				})
				_ = s.InsertEvent(ctx, t.ID, store.EventTypeStateChange,

					store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusWaiting, store.TriggerRecovery, nil))
			}
		}
	}

	// Startup worktree GC: remove any orphaned worktree directories whose
	// tasks have reached a terminal state or no longer exist in the store.
	if gcRunner, ok := lister.(*Runner); ok {
		orphans, err := gcRunner.ScanOrphanedWorktrees(ctx)
		if err != nil {
			logger.Recovery.Warn("worktree GC scan failed", "error", err)
		} else if len(orphans) > 0 {
			removed := gcRunner.PruneOrphanedWorktrees(ctx, orphans)
			logger.Recovery.Info("startup worktree GC complete", "scanned", len(orphans), "removed", removed)
		}
	}
}

// monitorContainerUntilStopped polls the container runtime until the container
// for taskID is no longer running, then transitions the task from in_progress
// to waiting so the user can decide what to do next.
//
// The goroutine exits when ctx is cancelled (e.g. on server shutdown) or after
// a 4-hour safety timeout to prevent indefinitely-leaked goroutines.
func monitorContainerUntilStopped(ctx context.Context, s *store.Store, lister ContainerLister, taskID uuid.UUID) {
	monitorContainerUntilStoppedWithConfig(ctx, s, lister, taskID, containerPollInterval, 4*time.Hour)
}

// monitorContainerUntilStoppedWithConfig is the testable core of
// monitorContainerUntilStopped. pollInterval controls how often the container
// runtime is queried; maxWait is the safety deadline after which the function
// gives up waiting and transitions the task to waiting.
func monitorContainerUntilStoppedWithConfig(ctx context.Context, s *store.Store, lister ContainerLister, taskID uuid.UUID, pollInterval, maxWait time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// storeCtx is intentionally decoupled from the monitor lifetime so that
	// store writes always complete even when ctx is cancelled or timed out.
	storeCtx := context.Background()

	transitionToWaiting := func() {
		cur, getErr := s.GetTask(storeCtx, taskID)
		if getErr != nil || cur == nil {
			return
		}
		if cur.Status != store.TaskStatusInProgress {
			// Task was already transitioned by another path (e.g. cancelled).
			return
		}
		if len(missingRecoveryWorktrees(*cur)) > 0 {
			markTaskFailedForMissingWorktrees(storeCtx, s, *cur, store.TaskStatusInProgress, store.TriggerRecovery)
			return
		}
		// ForceUpdateTaskStatus is used here because a crash may leave a task in an
		// unexpected state. Recovery must always complete regardless of the normal
		// allowed transitions.
		_ = s.ForceUpdateTaskStatus(storeCtx, taskID, store.TaskStatusWaiting)

		_ = s.InsertEvent(storeCtx, taskID, store.EventTypeSystem, map[string]string{

			"result": "Container has stopped. Please review the output and decide whether to continue or mark as done.",
		})
		_ = s.InsertEvent(storeCtx, taskID, store.EventTypeStateChange,

			store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusWaiting, store.TriggerRecovery, nil))
	}

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				logger.Recovery.Warn("monitor: container not seen stopping after safety timeout, giving up", "task", taskID)
				transitionToWaiting()
			}
			// If cancelled (server shutdown), exit silently.
			return

		case <-ticker.C:
			containers, err := lister.ListContainers()
			if err != nil {
				logger.Recovery.Warn("monitor: list containers error", "task", taskID, "error", err)
				continue
			}
			running := false
			for _, c := range containers {
				// Match by task ID (from label) so this works regardless of
				// the container name format (slug-based or legacy UUID-based).
				if c.TaskID == taskID.String() && c.State == "running" {
					running = true
					break
				}
			}
			if running {
				continue
			}

			// Container stopped — move the task to waiting if it is still in_progress.
			logger.Recovery.Info("monitored container stopped, moving task to waiting", "task", taskID)
			transitionToWaiting()
			return
		}
	}
}
