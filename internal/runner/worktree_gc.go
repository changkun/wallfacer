package runner

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

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
			// Task not found in store => orphan.
			orphans = append(orphans, id)
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
				if err := runGit(wsPath, "worktree", "remove", "--force", subdirPath); err != nil {
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

	// Run `git worktree prune` on all workspaces to clean up any stale
	// internal references that git worktree remove may have missed.
	for _, ws := range r.Workspaces() {
		gitPrune(ws)
	}

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
