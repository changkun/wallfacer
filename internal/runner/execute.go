package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/gitutil"
	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

var (
	// verdictLabelPattern detects explicit labeled verdict lines such as:
	// "Result: PASS", "Verdict: FAILED", "Status - Pass", etc.
	verdictLabelPattern = regexp.MustCompile(`(?i)\b(?:RESULT|VERDICT|STATUS|OUTCOME|CONCLUSION|SUMMARY)\s*[:\-]?\s*(PASS|PASSED|PASSING|FAIL|FAILED|FAILURE|FAILS)\b`)
	// negatedPassPattern catches explicit negative-pass language near a verdict token.
	// This is treated as a failure to avoid false positives like "NO PASS".
	negatedPassPattern = regexp.MustCompile(`(?i)\b(?:NO|NOT)\s+PASS(?:ED|ING)?\b`)
)

// Run is the main task execution loop. It sets up worktrees, runs the agent
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
			r.store.UpdateTaskStatus(bgCtx, taskID, store.TaskStatusFailed)
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
				"from": string(store.TaskStatusInProgress),
				"to":   string(store.TaskStatusFailed),
			})
		}
	}()
	// Clean up the per-task oversight mutex entry when Run exits to avoid
	// unbounded growth in the oversightMu sync.Map for long-running servers.
	defer r.oversightMu.Delete(taskID.String())

	task, err := r.store.GetTask(bgCtx, taskID)
	if err != nil {
		logger.Runner.Error("get task", "task", taskID, "error", err)
		return // defer moves to "failed"
	}

	// Idea-tagged tasks store a short title in Prompt for card display and the
	// full implementation text in ExecutionPrompt. Use the latter for the sandbox.
	if task.ExecutionPrompt != "" {
		prompt = task.ExecutionPrompt
	}

	// Idea-agent tasks use a special execution path: run the brainstorm agent,
	// create backlog tasks from the results, then move directly to done.
	if task.Kind == store.TaskKindIdeaAgent {
		statusSet = true
		ideaTimeout := time.Duration(task.Timeout) * time.Minute
		if ideaTimeout <= 0 {
			ideaTimeout = defaultTaskTimeout
		}
		ideaCtx, ideaCancel := context.WithTimeout(bgCtx, ideaTimeout)
		defer ideaCancel()

		if runErr := r.runIdeationTask(ideaCtx, task); runErr != nil {
			// Don't overwrite a cancelled status.
			if cur, _ := r.store.GetTask(bgCtx, taskID); cur != nil && cur.Status == store.TaskStatusCancelled {
				return
			}
			r.store.UpdateTaskStatus(bgCtx, taskID, store.TaskStatusFailed)
			r.store.UpdateTaskResult(bgCtx, taskID, runErr.Error(), "", "", 0)
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]string{"error": runErr.Error()})
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
				"from": string(store.TaskStatusInProgress), "to": string(store.TaskStatusFailed),
			})
			return
		}
		r.store.ForceUpdateTaskStatus(bgCtx, taskID, store.TaskStatusDone)
		r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
			"from": string(store.TaskStatusInProgress), "to": string(store.TaskStatusDone),
		})
		r.GenerateOversightBackground(taskID)
		return
	}

	isTestRun := task.IsTestRun

	// Apply per-task total timeout across all turns.
	timeout := time.Duration(task.Timeout) * time.Minute
	if timeout <= 0 {
		timeout = defaultTaskTimeout
	}
	ctx, cancel := context.WithTimeout(bgCtx, timeout)
	defer cancel()

	// Launch periodic oversight generation while the turn-loop executes.
	// The goroutine exits when Run returns (oversightCancel is deferred).
	// Skip for test runs — those are short verification passes where the
	// implementation oversight is already finalised.
	if !isTestRun {
		oversightCtx, oversightCancel := context.WithCancel(ctx)
		defer oversightCancel()
		go r.periodicOversightWorker(oversightCtx, taskID)
	}

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
		r.store.InsertEvent(bgCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "worktree_setup", Label: "worktree_setup"})
		worktreePaths, branchName, err = r.setupWorktrees(taskID)
		r.store.InsertEvent(bgCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "worktree_setup", Label: "worktree_setup"})
		if err != nil {
			logger.Runner.Error("setup worktrees", "task", taskID, "error", err)
			statusSet = true
			r.store.UpdateTaskStatus(bgCtx, taskID, store.TaskStatusFailed)
			r.store.UpdateTaskResult(bgCtx, taskID, err.Error(), sessionID, "", task.Turns)
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]string{"error": err.Error()})
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
				"from": string(store.TaskStatusInProgress), "to": string(store.TaskStatusFailed),
			})
			return
		}
		if err := r.store.UpdateTaskWorktrees(bgCtx, taskID, worktreePaths, branchName); err != nil {
			logger.Runner.Error("save worktree paths", "task", taskID, "error", err)
		}
	}

	turns := task.Turns

	// testSessionID tracks the test agent's session across turns so that
	// multi-turn test runs (max_tokens/pause_turn) can resume their own
	// session rather than starting a fresh empty-prompt session.
	// It is kept separate from sessionID which holds the implementation session.
	var testSessionID string

	// The agent's -p --resume mode reports per-invocation totals for both
	// cost (total_cost_usd) and usage tokens — they are NOT session-cumulative.
	// Each container invocation's values represent only that invocation's
	// consumption, so we accumulate them directly without delta subtraction.

	// Prepare board context (board.json manifest of all tasks).
	boardDir, boardErr := r.prepareBoardContext(taskID, task.MountWorktrees)
	if boardErr != nil {
		logger.Runner.Warn("board context failed", "task", taskID, "error", boardErr)
	}
	defer func() {
		if boardDir != "" {
			os.RemoveAll(boardDir)
		}
	}()

	// Build sibling worktree mounts if opted in.
	var siblingMounts map[string]map[string]string
	if task.MountWorktrees {
		siblingMounts = r.buildSiblingMounts(taskID)
	}

	for {
		turns++
		logger.Runner.Info("turn", "task", taskID, "turn", turns, "session", sessionID, "timeout", timeout)

		// Refresh board.json before each turn so it reflects latest state.
		if boardDir != "" {
			if data, err := r.generateBoardContext(taskID, task.MountWorktrees); err == nil {
				os.WriteFile(filepath.Join(boardDir, "board.json"), data, 0644)
			}
		}

		runActivity := activityImplementation
		if isTestRun {
			runActivity = activityTesting
		}
		turnLabel := fmt.Sprintf("implementation_%d", turns)
		if isTestRun {
			turnLabel = fmt.Sprintf("test_%d", turns)
		}
		r.store.InsertEvent(bgCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "agent_turn", Label: turnLabel})
		output, rawStdout, rawStderr, err := r.runContainer(ctx, taskID, prompt, sessionID, worktreePaths, boardDir, siblingMounts, "", runActivity)
		r.store.InsertEvent(bgCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "agent_turn", Label: turnLabel})
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
				if task.ExecutionPrompt != "" {
					prompt = task.ExecutionPrompt
				} else {
					prompt = task.Prompt
				}
				continue
			}

			logger.Runner.Error("container error", "task", taskID, "error", err)
			// Don't overwrite a cancelled status.
			if cur, _ := r.store.GetTask(bgCtx, taskID); cur != nil && cur.Status == store.TaskStatusCancelled {
				statusSet = true
				return
			}
			statusSet = true
			r.store.UpdateTaskStatus(bgCtx, taskID, store.TaskStatusFailed)
			r.store.UpdateTaskResult(bgCtx, taskID, err.Error(), sessionID, "", turns)
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]string{"error": err.Error()})
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
				"from": string(store.TaskStatusInProgress), "to": string(store.TaskStatusFailed),
			})
			return
		}

		r.store.InsertEvent(bgCtx, taskID, store.EventTypeOutput, map[string]string{
			"result":      output.Result,
			"stop_reason": output.StopReason,
			"session_id":  output.SessionID,
		})

		if isTestRun {
			// During a test run, preserve the implementation agent's result and
			// session ID — only track the turn count so progress is visible.
			// Also capture the test agent's session ID for multi-turn continuation.
			if output.SessionID != "" {
				testSessionID = output.SessionID
			}
			r.store.UpdateTaskTurns(bgCtx, taskID, turns)
		} else {
			if output.SessionID != "" {
				sessionID = output.SessionID
			}
			r.store.UpdateTaskResult(bgCtx, taskID, output.Result, sessionID, output.StopReason, turns)
		}

		// Accumulate per-invocation cost and token values directly.
		// Attribute to "test" sub-agent when in test mode, "implementation" otherwise.
		subAgent := "implementation"
		if isTestRun {
			subAgent = "test"
		}
		r.store.AccumulateSubAgentUsage(bgCtx, taskID, subAgent, store.TaskUsage{
			InputTokens:          output.Usage.InputTokens,
			OutputTokens:         output.Usage.OutputTokens,
			CacheReadInputTokens: output.Usage.CacheReadInputTokens,
			CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
			CostUSD:              output.TotalCostUSD,
		})
		if err := r.store.AppendTurnUsage(task.ID, store.TurnUsageRecord{
			Turn:                 turns,
			Timestamp:            time.Now().UTC(),
			InputTokens:          output.Usage.InputTokens,
			OutputTokens:         output.Usage.OutputTokens,
			CacheReadInputTokens: output.Usage.CacheReadInputTokens,
			CacheCreationTokens:  output.Usage.CacheCreationInputTokens,
			CostUSD:              output.TotalCostUSD,
			StopReason:           output.StopReason,
			Sandbox:              task.Sandbox,
			SubAgent:             subAgent,
		}); err != nil {
			logger.Runner.Warn("append turn usage", "task", task.ID, "error", err)
		}

		if output.IsError {
			statusSet = true
			r.store.UpdateTaskStatus(bgCtx, taskID, store.TaskStatusFailed)
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
				"from": string(store.TaskStatusInProgress), "to": string(store.TaskStatusFailed),
			})
			return
		}

		switch output.StopReason {
		case "end_turn":
			statusSet = true
			if isTestRun {
				// Test verification complete: don't commit, return to waiting with verdict.
				verdict := parseTestVerdict(output.Result)
				if verdict == "" {
					// Test ran but no clear verdict detected; use "unknown" so the
					// UI can distinguish "never tested" from "tested but ambiguous".
					verdict = "unknown"
				}
				r.store.UpdateTaskTestRun(bgCtx, taskID, false, verdict)
				r.GenerateTestOversight(taskID, task.TestRunStartTurn)
				r.store.UpdateTaskStatus(bgCtx, taskID, store.TaskStatusWaiting)
				r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
					"from": string(store.TaskStatusInProgress), "to": string(store.TaskStatusWaiting),
				})
				r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
					"result": "Test verification complete: " + strings.ToUpper(verdict),
				})
			} else {
				r.store.UpdateTaskStatus(bgCtx, taskID, store.TaskStatusCommitting)
				r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
					"from": string(store.TaskStatusInProgress), "to": string(store.TaskStatusCommitting),
				})
				r.store.InsertEvent(bgCtx, taskID, store.EventTypeSpanStart, store.SpanData{Phase: "commit", Label: "commit"})
				commitErr := r.commit(ctx, taskID, sessionID, turns, worktreePaths, branchName)
				r.store.InsertEvent(bgCtx, taskID, store.EventTypeSpanEnd, store.SpanData{Phase: "commit", Label: "commit"})
				if commitErr != nil {
					r.store.UpdateTaskStatus(bgCtx, taskID, store.TaskStatusFailed)
					r.store.InsertEvent(bgCtx, taskID, store.EventTypeError, map[string]string{
						"error": "commit failed: " + commitErr.Error(),
					})
					r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
						"from": string(store.TaskStatusCommitting), "to": string(store.TaskStatusFailed),
					})
				} else {
					r.store.UpdateTaskStatus(bgCtx, taskID, store.TaskStatusDone)
					r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
						"from": string(store.TaskStatusCommitting), "to": string(store.TaskStatusDone),
					})
					r.GenerateOversightBackground(taskID)
				}
			}
			return

		case "max_tokens", "pause_turn":
			logger.Runner.Info("auto-continuing", "task", taskID, "stop_reason", output.StopReason)
			prompt = ""
			// For test runs, resume the test agent's own session rather than
			// the implementation session (which must be preserved untouched).
			if isTestRun && testSessionID != "" {
				sessionID = testSessionID
			}
			continue

		default:
			// Empty or unknown stop_reason — waiting for user feedback.
			if cur, _ := r.store.GetTask(bgCtx, taskID); cur != nil && cur.Status == store.TaskStatusCancelled {
				statusSet = true
				return
			}
			statusSet = true
			if isTestRun {
				// Test run ended without an explicit stop_reason. Record
				// "unknown" so the UI shows "no verdict" instead of "unverified".
				verdict := parseTestVerdict(output.Result)
				if verdict == "" {
					verdict = "unknown"
				}
				r.store.UpdateTaskTestRun(bgCtx, taskID, false, verdict)
				r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
					"result": "Test verification complete: " + strings.ToUpper(verdict),
				})
				r.GenerateTestOversight(taskID, task.TestRunStartTurn)
			} else {
				r.GenerateOversight(taskID)
			}
			r.store.UpdateTaskStatus(bgCtx, taskID, store.TaskStatusWaiting)
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
				"from": string(store.TaskStatusInProgress), "to": string(store.TaskStatusWaiting),
			})
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
	bgCtx := context.Background()

	statusSet := false
	defer func() {
		if p := recover(); p != nil {
			logger.Runner.Error("sync panic", "task", taskID, "panic", p)
		}
		if !statusSet {
			r.store.UpdateTaskStatus(bgCtx, taskID, prevStatus)
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
				"from": string(store.TaskStatusInProgress),
				"to":   string(prevStatus),
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
		conflictDetected := false
		for attempt := 1; attempt <= maxRebaseRetries; attempt++ {
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
			if attempt == maxRebaseRetries {
				break
			}
			logger.Runner.Warn("sync rebase conflict, invoking resolver",
				"task", taskID, "repo", repoPath, "attempt", attempt)
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
				"result": fmt.Sprintf("Conflict in %s — running resolver (attempt %d/%d)...",
					filepath.Base(repoPath), attempt, maxRebaseRetries),
			})
			if resolveErr := r.resolveConflicts(ctx, taskID, repoPath, worktreePath, sessionID, defBranch); resolveErr != nil {
				rebaseErr = fmt.Errorf("conflict resolution failed: %w", resolveErr)
				break
			}
		}

		if stashed {
			gitutil.StashPop(worktreePath)
		}

		if rebaseErr != nil {
			statusSet = true
			if !conflictDetected {
				// Non-conflict git error: fail the task so the user can see
				// what went wrong (e.g. invalid ref, detached HEAD).
				r.failSync(bgCtx, taskID, sessionID, task.Turns,
					fmt.Sprintf("rebase in %s: %v", filepath.Base(worktreePath), rebaseErr))
				return
			}
			// Conflict (or failed conflict resolution): keep the task
			// in_progress and hand off to the agent so it can resolve
			// interactively. The rebase was aborted by RebaseOntoDefault, so
			// the worktree is clean on the task branch.
			r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
				"result": fmt.Sprintf(
					"Sync conflict in %s could not be automatically resolved — "+
						"handing off to agent for interactive resolution.",
					filepath.Base(repoPath),
				),
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
			r.Run(taskID, conflictPrompt, sessionID, false)
			return
		}

		r.store.InsertEvent(bgCtx, taskID, store.EventTypeSystem, map[string]string{
			"result": fmt.Sprintf("Successfully synced %s with %s.", filepath.Base(repoPath), defBranch),
		})
	}

	statusSet = true
	r.store.UpdateTaskStatus(bgCtx, taskID, prevStatus)
	r.store.InsertEvent(bgCtx, taskID, store.EventTypeStateChange, map[string]string{
		"from": string(store.TaskStatusInProgress),
		"to":   string(prevStatus),
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
	r.store.UpdateTaskStatus(ctx, taskID, store.TaskStatusFailed)
	r.store.InsertEvent(ctx, taskID, store.EventTypeStateChange, map[string]string{
		"from": string(store.TaskStatusInProgress),
		"to":   string(store.TaskStatusFailed),
	})
	r.store.UpdateTaskResult(ctx, taskID, "Sync failed: "+msg, sessionID, "sync_failed", turns)
}

// parseTestVerdict extracts "pass" or "fail" from a test agent's result text.
// Returns "" if no clear verdict is found.
//
// Detection strategy (in priority order):
//  1. Explicit markdown bold markers (**PASS** or **FAIL**) anywhere in the text.
//  2. The last non-empty line ends with the verdict word, after stripping common
//     trailing punctuation (handles "PASS.", "Result: PASS", etc.).
func parseTestVerdict(result string) string {
	upper := strings.ToUpper(result)

	// Highest confidence: explicit markdown bold markers.
	if strings.Contains(upper, "**PASS**") {
		return "pass"
	}
	if strings.Contains(upper, "**FAIL**") {
		return "fail"
	}

	// Scan lines from the end, stripping trailing punctuation, and check
	// whether the line contains an explicit labeled verdict or ends with a
	// verdict word. Check a small tail window so trailing status text does not
	// hide a valid verdict.
	lines := strings.Split(upper, "\n")
	const maxTailLines = 6
	seen := 0
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimRight(strings.TrimSpace(lines[i]), ".*!?:;,-")
		if line == "" {
			continue
		}

		seen++
		if seen > maxTailLines {
			break
		}

		if verdict := parseTestVerdictFromLine(line); verdict != "" {
			return verdict
		}

		// Legacy compatibility with the older "ends-with PASS/FAIL" logic.
		if strings.HasSuffix(line, "PASS") {
			return "pass"
		}
		if strings.HasSuffix(line, "FAIL") {
			return "fail"
		}
	}

	return ""
}

func parseTestVerdictFromLine(line string) string {
	if m := verdictLabelPattern.FindStringSubmatch(line); m != nil {
		return verdictTokenToValue(m[1])
	}

	words := strings.FieldsFunc(line, func(r rune) bool {
		return (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	})
	if len(words) == 0 {
		return ""
	}

	// Check negation before default token matching.
	if negatedPassPattern.MatchString(line) {
		return "fail"
	}

	last := words[len(words)-1]
	return verdictTokenToValue(last)
}

func verdictTokenToValue(token string) string {
	switch strings.ToUpper(token) {
	case "PASS", "PASSED", "PASSING":
		return "pass"
	case "FAIL", "FAILS", "FAILED", "FAILURE", "FAILURES":
		return "fail"
	default:
		return ""
	}
}
