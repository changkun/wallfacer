package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/pkg/gitutil"

	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/store"
)

// classifyFailure returns the machine-readable FailureCategory for a task
// failure given the available error context. It is a pure function with no
// side effects, intended to be called immediately before a TaskStatusFailed
// transition so the category can be persisted alongside the status update.
//
// Priority order:
//  1. Context deadline exceeded → timeout
//  2. Result text contains "budget exceeded" → budget_exceeded
//  3. isError flag set by agent → agent_error
//  4. err message contains "empty output" or "exit status" → container_crash
//  5. Default → unknown
//
// The worktree_setup, sync_error, and model_credential_missing categories are
// not returned by this function — they are set directly at their respective
// call sites where the cause is unambiguous.
func classifyFailure(err error, isError bool, result string) store.FailureCategory {
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		return store.FailureCategoryTimeout
	}
	if strings.Contains(result, "budget exceeded") {
		return store.FailureCategoryBudget
	}
	if isError {
		return store.FailureCategoryAgentError
	}
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "empty output") || strings.Contains(msg, "exit status") {
			return store.FailureCategoryContainerCrash
		}
	}
	return store.FailureCategoryUnknown
}

// Run is the main task execution loop. It sets up worktrees, runs the agent
// in a container, handles auto-continue turns, and transitions the task to the
// appropriate terminal state (done/waiting/failed).
func (r *Runner) Run(taskID uuid.UUID, prompt, sessionID string, resumedFromWaiting bool) {
	bgCtx := r.shutdownCtx

	// Close the feedback_waiting span opened when the task entered waiting.
	if resumedFromWaiting {
		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "feedback_waiting", Label: "feedback_waiting"})

	}

	// Guard: if this goroutine returns without explicitly setting the task
	// status (panic, early error), move to "failed" so the task doesn't
	// stay stuck in "in_progress" forever. Every exit path in the turn loop
	// must set statusSet=true before returning; the defer only fires when
	// an unexpected code path (e.g. panic recovery) skips the explicit transition.
	statusSet := false
	defer func() {
		if p := recover(); p != nil {
			logger.Runner.Error("run panic", "task", taskID, "panic", p)
		}
		if !statusSet {
			category := store.FailureCategoryUnknown
			_ = r.taskStore(taskID).SetTaskFailureCategory(bgCtx, taskID, category)

			_ = r.taskStore(taskID).UpdateTaskStatus(bgCtx, taskID, store.TaskStatusFailed)

			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeStateChange,

				store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusFailed, store.TriggerSystem, nil))
		}
	}()
	// The per-task oversight mutex is intentionally NOT deleted here. Background
	// oversight generations (brainstorm/budget/end_turn) and the periodic worker
	// can outlive Run; deleting the entry would let a later oversightLock hand out
	// a fresh mutex, breaking serialization and racing two writers on
	// oversight.json. The entry is a single small *sync.Mutex per distinct task
	// ID, so retaining it for the server's lifetime is negligible.

	task, err := r.taskStore(taskID).GetTask(bgCtx, taskID)
	if err != nil {
		logger.Runner.Error("get task", "task", taskID, "error", err)
		return // defer moves to "failed"
	}

	// Record the execution environment for reproducibility auditing.
	execEnv := r.captureExecutionEnvironment(*task)
	if err := r.taskStore(taskID).UpdateTaskEnvironment(bgCtx, taskID, execEnv); err != nil {
		slog.Warn("failed to record execution environment", "task", taskID, "err", err)
		// non-fatal: continue execution
	}

	// Every task runs the built-in pipeline. A record written while fleets
	// existed may still name one in FlowID; the field is not read for
	// dispatch, and the task's timeline says once that the fleet is gone.
	r.noteRemovedFleet(bgCtx, task)
	r.noteSeveralWorktrees(bgCtx, task)

	// Extract per-task model override (empty string means use global default).
	modelOverride := task.EffectiveModel()

	// Apply per-task total timeout across all turns.
	timeout := time.Duration(task.Timeout) * time.Minute
	if timeout <= 0 {
		timeout = constants.DefaultTaskTimeout
	}
	ctx, cancel := context.WithTimeout(bgCtx, timeout)
	defer cancel()

	// Launch periodic oversight generation while the turn-loop executes.
	// The goroutine exits when Run returns (oversightCancel is deferred).
	oversightCtx, oversightCancel := context.WithCancel(ctx)
	defer oversightCancel()
	go r.periodicOversightWorker(oversightCtx, taskID)

	// Set up worktrees only if not already present.
	worktreePaths := task.WorktreePaths
	var branchName string
	needSetup := len(worktreePaths) == 0
	if !needSetup {
		// Verify stored paths still exist on disk and are valid git repos.
		// A directory can exist but have a broken .git link (e.g. if the
		// container deleted the .git file), so check both.
		for _, wt := range worktreePaths {
			if _, statErr := os.Stat(wt); statErr != nil {
				logger.Runner.Warn("stored worktree path missing, will recreate",
					"task", taskID, "path", wt)
				needSetup = true
				break
			}
			if !gitutil.IsGitRepo(wt) {
				logger.Runner.Warn("stored worktree path is not a valid git repo, will recreate",
					"task", taskID, "path", wt)
				needSetup = true
				break
			}
		}
	}
	if needSetup {
		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "worktree_setup", Label: "worktree_setup"})

		// Use ensureTaskWorktrees with the stored paths and branch name so
		// that existing branches are reattached (preserving committed changes)
		// rather than creating fresh worktrees from HEAD. When the task has
		// no stored paths (first run), this falls back to setupWorktrees
		// behavior which uses r.Workspaces().
		if len(task.WorktreePaths) > 0 {
			worktreePaths, branchName, err = r.ensureTaskWorktrees(taskID, task.WorktreePaths, task.BranchName)
		} else {
			worktreePaths, branchName, err = r.setupWorktrees(taskID)
		}
		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "worktree_setup", Label: "worktree_setup"})

		if err != nil {
			logger.Runner.Error("setup worktrees", "task", taskID, "error", err)
			statusSet = true
			_ = r.taskStore(taskID).UpdateTaskStatus(bgCtx, taskID, store.TaskStatusFailed)

			_ = r.taskStore(taskID).SetTaskFailureCategory(bgCtx, taskID, store.FailureCategoryWorktree)

			_ = r.taskStore(taskID).UpdateTaskResult(bgCtx, taskID, err.Error(), sessionID, "", task.Turns)

			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]string{"error": err.Error()})

			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeStateChange,

				store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusFailed, store.TriggerSystem, nil))
			return
		}
		if err := r.taskStore(taskID).UpdateTaskWorktrees(bgCtx, taskID, worktreePaths, branchName); err != nil {
			logger.Runner.Error("save worktree paths", "task", taskID, "error", err)
		}
	}

	turns := task.Turns

	// NOTE: The agent's -p --resume mode reports per-invocation totals for both
	// cost (total_cost_usd) and usage tokens — they are NOT session-cumulative.
	// Each container invocation's values represent only that invocation's
	// consumption, so we accumulate them directly without delta subtraction.

	// Prepare board context.
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "board_context", Label: "board_context"})

	boardJSON, boardErr := r.generateBoardContext(taskID)
	if boardErr != nil {
		logger.Runner.Warn("board context failed", "task", taskID, "error", boardErr)
	}
	var boardDir string
	if boardJSON != nil {
		boardDir, boardErr = writeBoardDir(boardJSON, r.tmpDir)
		if boardErr != nil {
			logger.Runner.Warn("board context write failed", "task", taskID, "error", boardErr)
		}
	}
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "board_context", Label: "board_context"})

	defer func() {
		if boardDir != "" {
			_ = os.RemoveAll(boardDir)

		}
	}()

	for {
		turns++
		logger.Runner.Info("turn", "task", taskID, "turn", turns, "session", sessionID, "timeout", timeout)

		// Refresh board.json before each turn so it reflects the latest state.
		if boardDir != "" {
			boardRefreshLabel := fmt.Sprintf("board_context_%d", turns)
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "board_context", Label: boardRefreshLabel})

			if data, err := r.generateBoardContext(taskID); err == nil {
				_ = os.WriteFile(filepath.Join(boardDir, "board.json"), data, 0644)
			}
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "board_context", Label: boardRefreshLabel})

		}

		turnLabel := fmt.Sprintf("implementation_%d", turns)
		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "agent_turn", Label: turnLabel})

		output, rawStdout, rawStderr, err := r.runContainer(ctx, taskID, prompt, sessionID, worktreePaths, boardDir, modelOverride, activityImplementation)
		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "agent_turn", Label: turnLabel})

		if saveErr := r.taskStore(taskID).SaveTurnOutput(taskID, turns, rawStdout, rawStderr); saveErr != nil {
			logger.Runner.Error("save turn output", "task", taskID, "turn", turns, "error", saveErr)
		}
		if captureErr := r.captureTaskCommits(bgCtx, taskID, turns, worktreePaths); captureErr != nil {
			r.failCommitHistory(bgCtx, taskID, captureErr)
			statusSet = true
			return
		}

		if len(rawStderr) > 0 {
			stderrFile := fmt.Sprintf("turn-%04d.stderr.txt", turns)
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

				"stderr_file": stderrFile,
				"turn":        fmt.Sprintf("%d", turns),
			})
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
			if sessionID != "" && errors.Is(err, errEmptyOutput) {
				logger.Runner.Warn("resume produced empty output, retrying without session",
					"task", taskID, "session", sessionID)
				_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

					"result": "Session resume failed (empty output). Retrying with fresh session...",
				})
				sessionID = ""
				prompt = task.Prompt
				continue
			}

			logger.Runner.Error("container error", "task", taskID, "error", err)
			// Don't overwrite a cancelled status.
			if cur, _ := r.taskStore(taskID).GetTask(bgCtx, taskID); cur != nil && cur.Status == store.TaskStatusCancelled {
				statusSet = true
				return
			}
			category := classifyFailure(err, false, "")
			_ = r.taskStore(taskID).SetTaskFailureCategory(bgCtx, taskID, category)

			_ = r.taskStore(taskID).UpdateTaskResult(bgCtx, taskID, err.Error(), sessionID, "", turns)

			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]string{"error": err.Error()})

			statusSet = true
			_ = r.taskStore(taskID).UpdateTaskStatus(bgCtx, taskID, store.TaskStatusFailed)

			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeStateChange,

				store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusFailed, store.TriggerSystem, nil))
			return
		}

		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeOutput, map[string]string{

			"result":      output.Result,
			"stop_reason": output.StopReason,
			"session_id":  output.SessionID,
		})

		// Supersede the best-effort requested model captured at task start
		// (often empty when the harness self-selects) with the model the
		// harness actually reported. The init line carries it on the first
		// turn; read-modify-write execEnv so the other provenance fields are
		// preserved, and only rewrite when the model changed.
		if output.ObservedModel != "" && output.ObservedModel != execEnv.ModelName {
			execEnv.ModelName = output.ObservedModel
			if err := r.taskStore(taskID).UpdateTaskEnvironment(bgCtx, taskID, execEnv); err != nil {
				slog.Warn("failed to update execution environment with observed model", "task", taskID, "err", err)
			}
		}

		if output.SessionID != "" {
			sessionID = output.SessionID
		}
		_ = r.taskStore(taskID).UpdateTaskResult(bgCtx, taskID, output.Result, sessionID, output.StopReason, turns)

		// Accumulate per-invocation cost and token values directly.
		subAgent := store.SandboxActivityImplementation
		_ = r.taskStore(taskID).AccumulateSubAgentUsage(bgCtx, taskID, subAgent, store.TaskUsage{

			InputTokens:          output.Usage.InputTokens,
			OutputTokens:         output.Usage.OutputTokens,
			CacheReadInputTokens: output.Usage.CacheReadInputTokens,
			CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
			CostUSD:              output.TotalCostUSD,
		})
		if err := r.taskStore(taskID).AppendTurnUsage(task.ID, store.TurnUsageRecord{
			Turn:                 turns,
			Timestamp:            time.Now().UTC(),
			InputTokens:          output.Usage.InputTokens,
			OutputTokens:         output.Usage.OutputTokens,
			CacheReadInputTokens: output.Usage.CacheReadInputTokens,
			CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
			CostUSD:              output.TotalCostUSD,
			StopReason:           output.StopReason,
			Sandbox:              output.ActualSandbox,
			SubAgent:             subAgent,
		}); err != nil {
			logger.Runner.Warn("append turn usage", "task", task.ID, "error", err)
		}

		// Budget guardrail: pause the task when accumulated spend exceeds user-set limits.
		if currentTask, gErr := r.taskStore(taskID).GetTask(bgCtx, taskID); gErr == nil {
			u := currentTask.Usage
			totalInputTokens := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationTokens
			budgetExceeded := (currentTask.MaxCostUSD > 0 && u.CostUSD >= currentTask.MaxCostUSD) ||
				(currentTask.MaxInputTokens > 0 && totalInputTokens >= currentTask.MaxInputTokens)
			if budgetExceeded {
				var reason string
				if currentTask.MaxCostUSD > 0 && u.CostUSD >= currentTask.MaxCostUSD {
					reason = fmt.Sprintf("cost budget exceeded: $%.4f of $%.4f", u.CostUSD, currentTask.MaxCostUSD)
				} else {
					reason = fmt.Sprintf("token budget exceeded: %d of %d input tokens", totalInputTokens, currentTask.MaxInputTokens)
				}
				statusSet = true
				_ = r.taskStore(taskID).UpdateTaskStatus(bgCtx, taskID, store.TaskStatusWaiting)

				_ = r.taskStore(taskID).SetTaskFailureCategory(bgCtx, taskID, store.FailureCategoryBudget)

				_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeStateChange,

					store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusWaiting, store.TriggerSystem, nil))
				_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]any{

					"message":         reason,
					"budget_exceeded": true,
				})
				r.GenerateOversightBackground(taskID)
				_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "feedback_waiting", Label: "feedback_waiting"})

				return
			}
		}

		if output.IsError {
			// If the error is a stale session ("No conversation found"),
			// drop the session and retry with the original prompt instead
			// of failing permanently.
			combinedErr := output.Result + " " + string(rawStdout)
			if sessionID != "" && strings.Contains(combinedErr, "No conversation found") {
				logger.Runner.Warn("session not found, retrying without session",
					"task", taskID, "session", sessionID)
				_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

					"result": "Session expired or lost. Retrying with fresh session...",
				})
				sessionID = ""
				prompt = task.Prompt
				continue
			}
			category := classifyFailure(nil, true, output.Result)
			_ = r.taskStore(taskID).SetTaskFailureCategory(bgCtx, taskID, category)

			statusSet = true
			_ = r.taskStore(taskID).UpdateTaskStatus(bgCtx, taskID, store.TaskStatusFailed)

			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeStateChange,

				store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusFailed, store.TriggerSystem, nil))
			return
		}

		// Route the agent's stop_reason to the appropriate state transition.
		// "end_turn" means the agent finished normally; "max_tokens"/"pause_turn"
		// mean it hit a limit and should auto-continue; empty/unknown means it
		// needs user feedback.
		switch output.StopReason {
		case "end_turn":
			statusSet = true
			// Move to waiting for human review; Done runs the commit pipeline.
			r.GenerateOversightBackground(taskID)
			_ = r.taskStore(taskID).UpdateTaskStatus(bgCtx, taskID, store.TaskStatusWaiting)

			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeStateChange,

				store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusWaiting, store.TriggerSystem, nil))
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

				"result": "Task complete — awaiting review.",
			})
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "feedback_waiting", Label: "feedback_waiting"})

			return

		case "max_tokens", "pause_turn":
			logger.Runner.Info("auto-continuing", "task", taskID, "stop_reason", output.StopReason)
			prompt = ""
			continue

		default:
			// Empty or unknown stop_reason — waiting for user feedback.
			if cur, _ := r.taskStore(taskID).GetTask(bgCtx, taskID); cur != nil && cur.Status == store.TaskStatusCancelled {
				statusSet = true
				return
			}
			statusSet = true
			r.GenerateOversight(taskID)
			_ = r.taskStore(taskID).UpdateTaskStatus(bgCtx, taskID, store.TaskStatusWaiting)

			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeStateChange,

				store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusWaiting, store.TriggerSystem, nil))
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "feedback_waiting", Label: "feedback_waiting"})

			return
		}
	}
}

// SyncWorktrees rebases all task worktrees onto the latest default branch
// without merging. On success the task is restored to prevStatus. If
// conflicts cannot be automatically resolved after retries, the task remains
// in_progress and Run() is invoked so the agent can resolve them
// interactively; the task returns to prevStatus only after the agent finishes.
func (r *Runner) SyncWorktrees(taskID uuid.UUID, sessionID string, prevStatus store.TaskStatus) {
	bgCtx := r.shutdownCtx

	statusSet := false
	defer func() {
		if p := recover(); p != nil {
			logger.Runner.Error("sync panic", "task", taskID, "panic", p)
		}
		if !statusSet {
			// Use ForceUpdateTaskStatus because this recovery path may need
			// transitions not in the normal state machine (e.g. failed → waiting).
			restoreStatus := prevStatus
			if restoreStatus == store.TaskStatusFailed {
				restoreStatus = store.TaskStatusWaiting
			}
			_ = r.taskStore(taskID).ForceUpdateTaskStatus(bgCtx, taskID, restoreStatus)

			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeStateChange,

				store.NewStateChangeData(store.TaskStatusInProgress, restoreStatus, store.TriggerSystem, nil))
		}
	}()

	task, err := r.taskStore(taskID).GetTask(bgCtx, taskID)
	if err != nil {
		logger.Runner.Error("sync: get task", "task", taskID, "error", err)
		return
	}

	timeout := time.Duration(task.Timeout) * time.Minute
	if timeout <= 0 {
		timeout = constants.DefaultTaskTimeout
	}
	ctx, cancel := context.WithTimeout(bgCtx, timeout)
	defer cancel()

	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

		"result": "Syncing worktrees with latest changes on default branch...",
	})

	for repoPath, worktreePath := range task.WorktreePaths {
		if _, statErr := os.Stat(worktreePath); statErr != nil {
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

				"result": fmt.Sprintf("Skipping %s — worktree no longer exists on disk.", filepath.Base(repoPath)),
			})
			continue
		}
		if !gitutil.IsGitRepo(repoPath) {
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

				"result": fmt.Sprintf("Skipping %s — not a git repository, cannot sync.", filepath.Base(repoPath)),
			})
			continue
		}

		// Fetch from remote so CommitsBehind operates on up-to-date refs.
		if fetchErr := gitutil.FetchOrigin(repoPath); fetchErr != nil {
			logger.Runner.Warn("sync: git fetch failed, continuing with local refs",
				"task", taskID, "repo", repoPath, "error", fetchErr)
		}

		defBranch, err := gitutil.DefaultBranch(repoPath)
		if err != nil {
			statusSet = true
			r.failSync(bgCtx, taskID, sessionID, task.Turns,
				fmt.Sprintf("defaultBranch for %s: %v", filepath.Base(repoPath), err))
			return
		}

		n, behindErr := gitutil.CommitsBehind(repoPath, worktreePath)
		if behindErr != nil {
			logger.Runner.Warn("CommitsBehind failed, skipping rebase", "task", taskID, "repo", filepath.Base(repoPath), "error", behindErr)
		}
		if n == 0 {
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

				"result": fmt.Sprintf("%s is already up to date with %s.", filepath.Base(repoPath), defBranch),
			})
			continue
		}

		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

			"result": fmt.Sprintf("Rebasing %s onto %s (%d new commit(s))...", filepath.Base(repoPath), defBranch, n),
		})

		stashed := gitutil.StashIfDirty(worktreePath)

		var rebaseErr error
		conflictDetected := false
		for attempt := 1; attempt <= constants.MaxRebaseRetries; attempt++ {
			rebaseErr = gitutil.RebaseOntoDefault(repoPath, worktreePath)
			if rebaseErr == nil {
				break
			}
			if !isConflictError(rebaseErr) {
				// Non-conflict git error (e.g. invalid ref, detached HEAD):
				// bail out immediately without retrying.
				break
			}
			conflictDetected = true
			if attempt == constants.MaxRebaseRetries {
				break
			}
			logger.Runner.Warn("sync rebase conflict, invoking resolver",
				"task", taskID, "repo", repoPath, "attempt", attempt)
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

				"result": fmt.Sprintf("Conflict in %s — running resolver (attempt %d/%d)...",
					filepath.Base(repoPath), attempt, constants.MaxRebaseRetries),
			})
			if resolveErr := r.resolveConflicts(ctx, taskID, repoPath, worktreePath, sessionID, defBranch, ConflictResolverTriggerSync, attempt, constants.MaxRebaseRetries); resolveErr != nil {
				rebaseErr = fmt.Errorf("conflict resolution failed: %w", resolveErr)
				break
			}
		}

		var stashPopErr error
		if stashed {
			if rebaseErr == nil {
				// Rebase succeeded — try to restore stashed changes on top.
				stashPopErr = gitutil.StashPop(worktreePath)
			} else {
				// Rebase failed/aborted — try to restore stashed changes.
				// The branch may have been modified by the conflict resolver,
				// so the pop can fail even though the rebase was aborted.
				stashPopErr = gitutil.StashPop(worktreePath)
				if stashPopErr != nil {
					logger.Runner.Error("sync: stash pop failed after aborted rebase",
						"task", taskID, "repo", repoPath, "error", stashPopErr)
				}
			}
		}

		if rebaseErr != nil {
			statusSet = true
			if !conflictDetected {
				// Non-conflict git error: fail the task so the user can see
				// what went wrong (e.g. invalid ref, detached HEAD).
				msg := fmt.Sprintf("rebase in %s: %v", filepath.Base(worktreePath), rebaseErr)
				if stashPopErr != nil {
					msg += "; uncommitted changes are saved in git stash"
				}
				r.failSync(bgCtx, taskID, sessionID, task.Turns, msg)
				return
			}
			// Conflict (or failed conflict resolution): keep the task
			// in_progress and hand off to the agent so it can resolve
			// interactively. The rebase was aborted by RebaseOntoDefault, so
			// the worktree is clean on the task branch.
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

				"result": fmt.Sprintf(
					"Sync conflict in %s could not be automatically resolved — "+
						"handing off to agent for interactive resolution.",
					filepath.Base(repoPath),
				),
			})
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]any{

				"phase":        "conflict_resolver",
				"status":       "handoff",
				"trigger":      string(ConflictResolverTriggerSync),
				"repo":         filepath.Base(repoPath),
				"attempt":      constants.MaxRebaseRetries,
				"max_attempts": constants.MaxRebaseRetries,
				"result":       fmt.Sprintf("Automatic conflict resolver exhausted retries for %s. Handing off to the main agent for interactive resolution.", filepath.Base(repoPath)),
			})
			conflictPrompt := fmt.Sprintf(
				"Syncing your worktree with the latest %s branch failed due to conflicting "+
					"changes in %s. The rebase was aborted and the worktree is back to its "+
					"pre-sync state.\n\n"+
					"Please incorporate the upstream changes:\n"+
					"1. Run `git log HEAD..%s` to see what changed upstream\n"+
					"2. Run `git diff HEAD..%s -- .` to inspect the upstream diff\n"+
					"3. Update your code to be compatible with those upstream changes\n"+
					"4. Commit the updated changes\n\n"+
					"Once your changes are committed and compatible, the sync will be retried.",
				defBranch, filepath.Base(worktreePath), defBranch, defBranch,
			)
			if stashPopErr != nil {
				conflictPrompt += fmt.Sprintf(
					"\n\nIMPORTANT: You had uncommitted changes before the sync that could not " +
						"be automatically restored. They are saved in the git stash.\n" +
						"After resolving the conflict above:\n" +
						"1. Run `git stash show -p` to see what was stashed\n" +
						"2. Run `git stash pop` to restore the uncommitted changes\n" +
						"3. Resolve any conflicts from the stash pop\n" +
						"Do NOT discard the stash — it contains your work in progress.",
				)
			}
			r.Run(taskID, conflictPrompt, sessionID, false)
			return
		}

		// Rebase succeeded but stash pop failed: the uncommitted changes
		// conflict with the rebased state. The stash entry is preserved.
		// Hand off to the agent to integrate those changes manually.
		if stashPopErr != nil {
			statusSet = true
			_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

				"result": fmt.Sprintf(
					"Rebase of %s succeeded, but restoring uncommitted changes failed "+
						"(they conflict with the rebased code). The changes are saved in "+
						"`git stash list`. Handing off to agent to re-apply them.",
					filepath.Base(repoPath)),
			})
			stashPrompt := fmt.Sprintf(
				"The worktree %s was successfully rebased onto %s, but your "+
					"uncommitted changes could not be automatically restored because "+
					"they conflict with the rebased code.\n\n"+
					"Your uncommitted work is saved in the git stash. Please:\n"+
					"1. Run `git stash show -p` to see what was stashed\n"+
					"2. Run `git stash pop` to attempt restoring (may produce conflicts)\n"+
					"3. Resolve any conflicts, ensuring your changes work with the updated code\n"+
					"4. Commit the result\n\n"+
					"Do NOT discard the stash — it contains your work in progress.",
				filepath.Base(worktreePath), defBranch,
			)
			r.Run(taskID, stashPrompt, sessionID, false)
			return
		}

		_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

			"result": fmt.Sprintf("Successfully synced %s with %s.", filepath.Base(repoPath), defBranch),
		})
	}

	statusSet = true
	// After a successful sync, restore to prevStatus — except when the task
	// was failed: putting it back to failed would be nonsensical (and can
	// cause retry loops). Restore failed tasks to waiting instead.
	// Use ForceUpdateTaskStatus because this may need transitions not in the
	// normal state machine (e.g. the task was force-moved to in_progress for sync).
	restoreStatus := prevStatus
	if restoreStatus == store.TaskStatusFailed {
		restoreStatus = store.TaskStatusWaiting
	}
	_ = r.taskStore(taskID).ForceUpdateTaskStatus(bgCtx, taskID, restoreStatus)

	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeStateChange,

		store.NewStateChangeData(store.TaskStatusInProgress, restoreStatus, store.TriggerSystem, nil))
	_ = r.taskStore(taskID).InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{

		"result": "Sync complete. Worktrees are up to date with the default branch.",
	})
	logger.Runner.Info("sync completed", "task", taskID)
}

// failSync transitions a task to "failed" after a sync error.
func (r *Runner) failSync(ctx context.Context, taskID uuid.UUID, sessionID string, turns int, msg string) {
	logger.Runner.Error("sync failed", "task", taskID, "error", msg)
	_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeError, map[string]string{"error": msg})

	_ = r.taskStore(taskID).UpdateTaskStatus(ctx, taskID, store.TaskStatusFailed)

	_ = r.taskStore(taskID).SetTaskFailureCategory(ctx, taskID, store.FailureCategorySyncError)

	_ = r.taskStore(taskID).InsertEvent(ctx, taskID, store.EventTypeStateChange,

		store.NewStateChangeData(store.TaskStatusInProgress, store.TaskStatusFailed, store.TriggerSystem, nil))
	_ = r.taskStore(taskID).UpdateTaskResult(ctx, taskID, "Sync failed: "+msg, sessionID, "sync_failed", turns)

}
