package runner

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"changkun.de/x/wallfacer/internal/gitutil"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// ScanMissingTaskWorktrees iterates over all tasks in in_progress, waiting,
// and committing states and returns those where at least one WorktreePaths
// entry is missing on disk. It does not remove anything.
func (r *Runner) ScanMissingTaskWorktrees(ctx context.Context) ([]store.Task, error) {
	s := r.currentStore()
	if s == nil {
		return nil, nil
	}

	activeStatuses := []store.TaskStatus{
		store.TaskStatusInProgress,
		store.TaskStatusWaiting,
		store.TaskStatusCommitting,
	}

	var missing []store.Task
	for _, status := range activeStatuses {
		tasks, err := s.ListTasksByStatus(ctx, status)
		if err != nil {
			return nil, err
		}
		for _, task := range tasks {
			for _, path := range task.WorktreePaths {
				if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
					missing = append(missing, task)
					break // one missing path is enough to flag the task
				} else if statErr == nil && !gitutil.IsGitRepo(path) {
					// Directory exists but .git link is broken (e.g. container
					// deleted the .git file). Remove the broken directory so
					// ensureTaskWorktrees can recreate it cleanly.
					logger.Runner.Warn("worktree health: directory exists but is not a valid git repo, removing",
						"task", task.ID, "path", path)
					_ = os.RemoveAll(path)

					missing = append(missing, task)
					break
				}
			}
		}
	}
	return missing, nil
}

// RestoreMissingTaskWorktrees attempts to recreate worktrees for each task in
// the slice by calling ensureTaskWorktrees. On success it appends a system
// event to the task's audit trail and increments the
// wallfacer_worktree_restorations_total counter. Returns the count of
// successfully restored tasks.
func (r *Runner) RestoreMissingTaskWorktrees(ctx context.Context, tasks []store.Task) int {
	s := r.currentStore()
	if s == nil {
		return 0
	}

	restored := 0
	for _, task := range tasks {
		if task.BranchName == "" {
			logger.Runner.Debug("worktree health: skipping task with empty branch name", "task", task.ID)
			continue
		}
		if _, _, err := r.ensureTaskWorktrees(task.ID, task.WorktreePaths, task.BranchName); err != nil {
			logger.Runner.Warn("worktree health: restore failed", "task", task.ID, "error", err)
			continue
		}
		if err := s.InsertEvent(ctx, task.ID, store.EventTypeSystem, map[string]string{
			"message": "worktree restored by health watcher",
		}); err != nil {
			logger.Runner.Warn("worktree health: insert event failed", "task", task.ID, "error", err)
		}
		if r.reg != nil {
			r.reg.Counter("wallfacer_worktree_restorations_total",
				"Total number of task worktrees restored by the health watcher.").Inc(nil)
		}
		restored++
	}
	return restored
}

const defaultWorktreeHealthInterval = 2 * time.Minute

// StartWorktreeHealthWatcher proactively scans active tasks for missing
// worktree directories and attempts to restore them. It runs an initial scan
// at startup and then repeats every 2 minutes. Errors are logged at Warn level
// and do not crash the loop. Keep this goroutine separate from StartWorktreeGC
// since the two have distinct concerns (GC removes orphans; health watcher
// restores missing paths for live tasks).
func (r *Runner) StartWorktreeHealthWatcher(ctx context.Context) {
	r.backgroundWg.Add("worktree-health")
	defer r.backgroundWg.Done("worktree-health")

	runScan := func() {
		missing, err := r.ScanMissingTaskWorktrees(ctx)
		if err != nil {
			logger.Runner.Warn("worktree health: scan failed", "error", err)
			return
		}
		if len(missing) > 0 {
			logger.Runner.Info("worktree health: missing worktrees detected", "count", len(missing))
			r.RestoreMissingTaskWorktrees(ctx, missing)
		}
	}

	// Run one scan immediately at startup before the first tick.
	runScan()

	ticker := time.NewTicker(defaultWorktreeHealthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runScan()
		}
	}
}

// ScanOrphanedWorktrees inspects r.worktreesDir and returns the task IDs
// whose worktree directories exist on disk but whose tasks are in a terminal
// or non-existent state (done, cancelled, archived, or unknown to the store).
// Tasks in backlog, in_progress, waiting, committing, or failed still need
// their worktrees and are skipped.
func (r *Runner) ScanOrphanedWorktrees(ctx context.Context) ([]uuid.UUID, error) {
	entries, err := os.ReadDir(r.worktreesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var orphans []uuid.UUID
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id, parseErr := uuid.Parse(entry.Name())
		if parseErr != nil {
			continue // skip non-UUID directories
		}

		task, getErr := r.store.GetTask(ctx, id)
		if getErr != nil {
			// Task not found in the current store — it may belong to a
			// different workspace scope. Leave it alone; the next prune
			// cycle after a workspace switch will clean it up if needed.
			continue
		}

		// Terminal states whose worktrees should have been cleaned up.
		if task.Status == store.TaskStatusDone ||
			task.Status == store.TaskStatusCancelled ||
			task.Archived {
			orphans = append(orphans, id)
		}
		// backlog, in_progress, waiting, committing, failed: skip.
	}

	return orphans, nil
}

// PruneOrphanedWorktrees removes the on-disk worktree directories for the
// given task IDs. It first attempts `git worktree remove --force` for each
// registered worktree subdirectory, then falls back to os.RemoveAll.
// Errors are logged as warnings; the function proceeds to the next ID.
// Returns count of successfully removed task directories.
func (r *Runner) PruneOrphanedWorktrees(ctx context.Context, orphans []uuid.UUID) int {
	r.worktreeMu.Lock()
	defer r.worktreeMu.Unlock()

	// Build basename → workspace path lookup so we can match worktree
	// subdirectories to their originating workspace.
	basenames := make(map[string]string)
	for _, ws := range r.Workspaces() {
		basenames[filepath.Base(ws)] = ws
	}

	removed := 0
	for _, id := range orphans {
		if ctx.Err() != nil {
			break
		}
		taskDir := filepath.Join(r.worktreesDir, id.String())

		subdirs, err := os.ReadDir(taskDir)
		if err != nil {
			if !os.IsNotExist(err) {
				logger.Runner.Warn("worktree GC: read task dir", "task", id, "error", err)
			}
			continue
		}

		for _, sub := range subdirs {
			if !sub.IsDir() {
				continue
			}
			subdirPath := filepath.Join(taskDir, sub.Name())
			if wsPath, ok := basenames[sub.Name()]; ok {
				// Best-effort: unregister the worktree from git's internal index.
				if err := runGitContext(ctx, wsPath, "worktree", "remove", "--force", subdirPath); err != nil {
					logger.Runner.Warn("worktree GC: git worktree remove", "task", id, "subdir", subdirPath, "error", err)
				}
			}
		}

		if err := os.RemoveAll(taskDir); err != nil && !os.IsNotExist(err) {
			logger.Runner.Warn("worktree GC: remove task dir", "task", id, "error", err)
			continue
		}
		logger.Runner.Info("worktree GC: removed orphaned worktree", "task", id)
		removed++
	}

	// NOTE: do NOT run a blanket `git worktree prune` here. The targeted
	// `git worktree remove --force` above already cleans each orphan's
	// tracking entry individually. A blanket prune can destroy entries
	// for active tasks whose worktree paths share a basename with the
	// just-removed orphan (e.g. both named "wallfacer"), breaking the
	// active task's .git link and causing the health watcher to recreate
	// the worktree from HEAD — losing all committed work.

	return removed
}

const defaultWorktreeGCInterval = 24 * time.Hour

// StartWorktreeGC runs ScanOrphanedWorktrees + PruneOrphanedWorktrees on a
// periodic interval. interval defaults to 24h; override with
// WALLFACER_WORKTREE_GC_INTERVAL (e.g. "6h", "30m").
func (r *Runner) StartWorktreeGC(ctx context.Context) {
	r.backgroundWg.Add("worktree-gc")
	defer r.backgroundWg.Done("worktree-gc")

	interval := defaultWorktreeGCInterval
	if v := os.Getenv("WALLFACER_WORKTREE_GC_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			orphans, err := r.ScanOrphanedWorktrees(ctx)
			if err != nil {
				logger.Runner.Warn("worktree GC: scan failed", "error", err)
				continue
			}
			if len(orphans) > 0 {
				removed := r.PruneOrphanedWorktrees(ctx, orphans)
				logger.Runner.Info("worktree GC: complete", "scanned", len(orphans), "removed", removed)
			}
		}
	}
}
