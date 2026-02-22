package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/gitutil"
	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

// Run is the main task execution loop. It sets up worktrees, runs Claude Code
// in a container, handles auto-continue turns, and transitions the task to the
// appropriate terminal state (done/waiting/failed).
func (r *Runner) Run(taskID uuid.UUID, prompt, sessionID string, resumedFromWaiting bool) {
	bgCtx := context.Background()

	// Guard: if this goroutine returns without explicitly setting the task
	// status (panic, early error), move to "failed" so the task doesn't
	// stay stuck in "in_progress" forever.
	statusSet := false
	defer func() {
		if p := recover(); p != nil {
			logger.Runner.Error("run panic", "task", taskID, "panic", p)
		}
		if !statusSet {
			r.store.UpdateTaskStatus(bgCtx, taskID, "failed")
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
				"from": "in_progress",
				"to":   "failed",
			})
		}
	}()

	task, err := r.store.GetTask(bgCtx, taskID)
	if err != nil {
		logger.Runner.Error("get task", "task", taskID, "error", err)
		return // defer moves to "failed"
	}

	// Apply per-task total timeout across all turns.
	timeout := time.Duration(task.Timeout) * time.Minute
	if timeout <= 0 {
		timeout = defaultTaskTimeout
	}
	ctx, cancel := context.WithTimeout(bgCtx, timeout)
	defer cancel()

	// Set up worktrees only if not already present.
	worktreePaths := task.WorktreePaths
	branchName := task.BranchName
	needSetup := len(worktreePaths) == 0
	if !needSetup {
		// Verify stored paths still exist on disk.
		for _, wt := range worktreePaths {
			if _, statErr := os.Stat(wt); statErr != nil {
				logger.Runner.Warn("stored worktree path missing, will recreate",
					"task", taskID, "path", wt)
				needSetup = true
				break
			}
		}
	}
	if needSetup {
		worktreePaths, branchName, err = r.setupWorktrees(taskID)
		if err != nil {
			logger.Runner.Error("setup worktrees", "task", taskID, "error", err)
			statusSet = true
			r.store.UpdateTaskStatus(bgCtx, taskID, "failed")
			r.store.UpdateTaskResult(bgCtx, taskID, err.Error(), sessionID, "", task.Turns)
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]string{"error": err.Error()})
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
				"from": "in_progress", "to": "failed",
			})
			return
		}
		if err := r.store.UpdateTaskWorktrees(bgCtx, taskID, worktreePaths, branchName); err != nil {
			logger.Runner.Error("save worktree paths", "task", taskID, "error", err)
		}
	}

	turns := task.Turns

	for {
		turns++
		logger.Runner.Info("turn", "task", taskID, "turn", turns, "session", sessionID, "timeout", timeout)

		output, rawStdout, rawStderr, err := r.runContainer(ctx, taskID, prompt, sessionID, worktreePaths)
		if saveErr := r.store.SaveTurnOutput(taskID, turns, rawStdout, rawStderr); saveErr != nil {
			logger.Runner.Error("save turn output", "task", taskID, "turn", turns, "error", saveErr)
		}
		if err != nil {
			// Try to salvage session_id from partial output so the task
			// can be resumed even when the container fails (e.g. timeout).
			if sessionID == "" && len(rawStdout) > 0 {
				if sid := extractSessionID(rawStdout); sid != "" {
					sessionID = sid
				}
			}

			// If resume produced empty output, drop the session and retry.
			if sessionID != "" && strings.Contains(err.Error(), "empty output from container") {
				logger.Runner.Warn("resume produced empty output, retrying without session",
					"task", taskID, "session", sessionID)
				r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
					"result": "Session resume failed (empty output). Retrying with fresh session...",
				})
				sessionID = ""
				prompt = task.Prompt
				continue
			}

			logger.Runner.Error("container error", "task", taskID, "error", err)
			// Don't overwrite a cancelled status.
			if cur, _ := r.store.GetTask(bgCtx, taskID); cur != nil && cur.Status == "cancelled" {
				statusSet = true
				return
			}
			statusSet = true
			r.store.UpdateTaskStatus(bgCtx, taskID, "failed")
			r.store.UpdateTaskResult(bgCtx, taskID, err.Error(), sessionID, "", turns)
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]string{"error": err.Error()})
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
				"from": "in_progress", "to": "failed",
			})
			return
		}

		r.store.InsertEvent(bgCtx, taskID, store.EventTypeOutput, map[string]string{
			"result":      output.Result,
			"stop_reason": output.StopReason,
			"session_id":  output.SessionID,
		})

		if output.SessionID != "" {
			sessionID = output.SessionID
		}
		r.store.UpdateTaskResult(bgCtx, taskID, output.Result, sessionID, output.StopReason, turns)
		r.store.AccumulateTaskUsage(bgCtx, taskID, store.TaskUsage{
			InputTokens:          output.Usage.InputTokens,
			OutputTokens:         output.Usage.OutputTokens,
			CacheReadInputTokens: output.Usage.CacheReadInputTokens,
			CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
			CostUSD:              output.TotalCostUSD,
		})

		if output.IsError {
			statusSet = true
			r.store.UpdateTaskStatus(bgCtx, taskID, "failed")
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
				"from": "in_progress", "to": "failed",
			})
			return
		}

		switch output.StopReason {
		case "end_turn":
			statusSet = true
			if err := r.commit(ctx, taskID, sessionID, turns, worktreePaths, branchName); err != nil {
				r.store.UpdateTaskStatus(bgCtx, taskID, "failed")
				r.store.InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]string{
					"error": "commit failed: " + err.Error(),
				})
				r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
					"from": "in_progress", "to": "failed",
				})
			} else {
				r.store.UpdateTaskStatus(bgCtx, taskID, "done")
				r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
					"from": "in_progress", "to": "done",
				})
			}
			return

		case "max_tokens", "pause_turn":
			logger.Runner.Info("auto-continuing", "task", taskID, "stop_reason", output.StopReason)
			prompt = ""
			continue

		default:
			// Empty or unknown stop_reason — waiting for user feedback.
			if cur, _ := r.store.GetTask(bgCtx, taskID); cur != nil && cur.Status == "cancelled" {
				statusSet = true
				return
			}
			statusSet = true
			r.store.UpdateTaskStatus(bgCtx, taskID, "waiting")
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
				"from": "in_progress", "to": "waiting",
			})
			return
		}
	}
}

// SyncWorktrees rebases all task worktrees onto the latest default branch
// without merging. On success the task is restored to prevStatus; on
// unrecoverable failure it is moved to "failed".
func (r *Runner) SyncWorktrees(taskID uuid.UUID, sessionID, prevStatus string) {
	bgCtx := context.Background()

	statusSet := false
	defer func() {
		if p := recover(); p != nil {
			logger.Runner.Error("sync panic", "task", taskID, "panic", p)
		}
		if !statusSet {
			r.store.UpdateTaskStatus(bgCtx, taskID, prevStatus)
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
				"from": "in_progress",
				"to":   prevStatus,
			})
		}
	}()

	task, err := r.store.GetTask(bgCtx, taskID)
	if err != nil {
		logger.Runner.Error("sync: get task", "task", taskID, "error", err)
		return
	}

	timeout := time.Duration(task.Timeout) * time.Minute
	if timeout <= 0 {
		timeout = defaultTaskTimeout
	}
	ctx, cancel := context.WithTimeout(bgCtx, timeout)
	defer cancel()

	r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
		"result": "Syncing worktrees with latest changes on default branch...",
	})

	for repoPath, worktreePath := range task.WorktreePaths {
		if !gitutil.IsGitRepo(repoPath) {
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
				"result": fmt.Sprintf("Skipping %s — not a git repository, cannot sync.", filepath.Base(repoPath)),
			})
			continue
		}

		defBranch, err := gitutil.DefaultBranch(repoPath)
		if err != nil {
			statusSet = true
			r.failSync(bgCtx, taskID, sessionID, task.Turns,
				fmt.Sprintf("defaultBranch for %s: %v", filepath.Base(repoPath), err))
			return
		}

		n, _ := gitutil.CommitsBehind(repoPath, worktreePath)
		if n == 0 {
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
				"result": fmt.Sprintf("%s is already up to date with %s.", filepath.Base(repoPath), defBranch),
			})
			continue
		}

		r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
			"result": fmt.Sprintf("Rebasing %s onto %s (%d new commit(s))...", filepath.Base(repoPath), defBranch, n),
		})

		stashed := gitutil.StashIfDirty(worktreePath)

		var rebaseErr error
		for attempt := 1; attempt <= maxRebaseRetries; attempt++ {
			rebaseErr = gitutil.RebaseOntoDefault(repoPath, worktreePath)
			if rebaseErr == nil {
				break
			}
			if attempt == maxRebaseRetries || !isConflictError(rebaseErr) {
				break
			}
			logger.Runner.Warn("sync rebase conflict, invoking resolver",
				"task", taskID, "repo", repoPath, "attempt", attempt)
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
				"result": fmt.Sprintf("Conflict in %s — running resolver (attempt %d/%d)...",
					filepath.Base(repoPath), attempt, maxRebaseRetries),
			})
			if resolveErr := r.resolveConflicts(ctx, taskID, repoPath, worktreePath, sessionID); resolveErr != nil {
				rebaseErr = fmt.Errorf("conflict resolution failed: %w", resolveErr)
				break
			}
		}

		if stashed {
			gitutil.StashPop(worktreePath)
		}

		if rebaseErr != nil {
			statusSet = true
			r.failSync(bgCtx, taskID, sessionID, task.Turns,
				fmt.Sprintf("sync failed for %s: %v", filepath.Base(repoPath), rebaseErr))
			return
		}

		r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
			"result": fmt.Sprintf("Successfully synced %s with %s.", filepath.Base(repoPath), defBranch),
		})
	}

	statusSet = true
	r.store.UpdateTaskStatus(bgCtx, taskID, prevStatus)
	r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
		"from": "in_progress",
		"to":   prevStatus,
	})
	r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
		"result": "Sync complete. Worktrees are up to date with the default branch.",
	})
	logger.Runner.Info("sync completed", "task", taskID)
}

// failSync transitions a task to "failed" after a sync error.
func (r *Runner) failSync(ctx context.Context, taskID uuid.UUID, sessionID string, turns int, msg string) {
	logger.Runner.Error("sync failed", "task", taskID, "error", msg)
	r.store.InsertEvent(ctx, taskID, store.EventTypeError, map[string]string{"error": msg})
	r.store.UpdateTaskStatus(ctx, taskID, "failed")
	r.store.InsertEvent(ctx, taskID, store.EventTypeStateChange, map[string]string{
		"from": "in_progress",
		"to":   "failed",
	})
	r.store.UpdateTaskResult(ctx, taskID, "Sync failed: "+msg, sessionID, "sync_failed", turns)
}

