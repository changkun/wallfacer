package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/gitutil"
	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

const defaultMaxConcurrentTasks = 5

// defaultMaxTestConcurrentTasks is used when WALLFACER_MAX_TEST_PARALLEL is not set.
const defaultMaxTestConcurrentTasks = 2

// maxConcurrentTasks reads the configured parallel task limit from the env file,
// falling back to defaultMaxConcurrentTasks.
func (h *Handler) maxConcurrentTasks() int {
	cfg, err := envconfig.Parse(h.envFile)
	if err != nil || cfg.MaxParallelTasks <= 0 {
		return defaultMaxConcurrentTasks
	}
	return cfg.MaxParallelTasks
}

// maxTestConcurrentTasks reads the configured parallel test-run limit from the
// env file, falling back to defaultMaxTestConcurrentTasks.
func (h *Handler) maxTestConcurrentTasks() int {
	cfg, err := envconfig.Parse(h.envFile)
	if err != nil || cfg.MaxTestParallelTasks <= 0 {
		return defaultMaxTestConcurrentTasks
	}
	return cfg.MaxTestParallelTasks
}

func (h *Handler) countRegularInProgress(_ context.Context) (int, error) {
	return h.store.CountRegularInProgress(), nil
}

func (h *Handler) waitingTaskUsesVerificationCapacity(t *store.Task) bool {
	if t == nil {
		return false
	}
	if h.AutotestEnabled() && t.LastTestResult == "" {
		return true
	}
	if !h.AutosubmitEnabled() {
		return false
	}
	if t.IsTestRun {
		return false
	}
	if t.LastTestResult == "pass" {
		return true
	}
	return t.StopReason != nil && *t.StopReason == "end_turn" && t.LastTestResult == "" && !h.AutotestEnabled()
}

func countRegularInProgress(tasks []store.Task) int {
	count := 0
	for i := range tasks {
		if tasks[i].Status == store.TaskStatusInProgress && !tasks[i].IsTestRun {
			count++
		}
	}
	return count
}

// checkConcurrencyAndUpdateStatus acquires promoteMu, enforces the regular
// in-progress concurrency limit, and calls store.UpdateTaskStatus. It writes
// the appropriate HTTP error response and returns false on any failure;
// on success it returns true with the mutex already released.
func (h *Handler) checkConcurrencyAndUpdateStatus(ctx context.Context, w http.ResponseWriter, id uuid.UUID, oldStatus, newStatus store.TaskStatus) bool {
	promoteMu.Lock()
	defer promoteMu.Unlock()

	regularInProgress, err := h.countRegularInProgress(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	if regularInProgress >= h.maxConcurrentTasks() {
		http.Error(w, fmt.Sprintf("max concurrent tasks (%d) reached", h.maxConcurrentTasks()), http.StatusConflict)
		return false
	}
	if err := h.store.UpdateTaskStatus(ctx, id, newStatus); err != nil {
		if errors.Is(err, store.ErrInvalidTransition) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return false
	}
	return true
}

// promoteMu serialises auto-promotion so two simultaneous state changes
// cannot both promote a task, exceeding the concurrency limit.
var promoteMu sync.Mutex

// StartAutoPromoter subscribes to store change notifications and automatically
// promotes backlog tasks to in_progress when there are fewer than
// maxConcurrentTasks running. A supplementary 60-second ticker fires
// periodically so that scheduled tasks are promoted even when no other
// state change occurs.
func (h *Handler) StartAutoPromoter(ctx context.Context) {
	subID, ch := h.store.Subscribe()
	ticker := time.NewTicker(60 * time.Second)
	go func() {
		defer h.store.Unsubscribe(subID)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				h.tryAutoPromote(ctx)
			case <-ticker.C:
				h.tryAutoPromote(ctx)
			}
		}
	}()
}

// maxHandlerAutoRetries is the maximum total number of automatic retries for a
// single task, mirroring the runner's maxTotalAutoRetries cap.
const maxHandlerAutoRetries = 3

// retryableCategories lists FailureCategory values that represent transient
// infrastructure errors that are safe to retry automatically.
var retryableCategories = map[store.FailureCategory]bool{
	store.FailureCategoryContainerCrash: true,
	store.FailureCategoryWorktree:       true,
	store.FailureCategorySyncError:      true,
}

// StartAutoRetrier subscribes to store change notifications and automatically
// retries tasks that failed with a transient infrastructure error category.
// It also runs a recovery scan on startup to pick up any failed tasks that
// may have been missed while the server was down.
func (h *Handler) StartAutoRetrier(ctx context.Context) {
	go func() {
		subID, ch := h.store.Subscribe()
		defer h.store.Unsubscribe(subID)

		// Recovery scan: retry any eligible failed tasks that predate startup.
		failed, _ := h.store.ListTasksByStatus(ctx, store.TaskStatusFailed)
		for _, t := range failed {
			h.tryAutoRetry(ctx, t)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case delta, ok := <-ch:
				if !ok {
					return
				}
				if delta.Deleted || delta.Task == nil {
					continue
				}
				if delta.Task.Status == store.TaskStatusFailed {
					h.tryAutoRetry(ctx, *delta.Task)
				}
			}
		}
	}()
}

// taskReachable reports whether target is reachable from start by following
// DependsOn edges (i.e., target is a transitive dependency of start).
// Used to detect cycles before accepting a new dependency edge.
func taskReachable(taskList []store.Task, start, target uuid.UUID) bool {
	adj := make(map[uuid.UUID][]uuid.UUID, len(taskList))
	for _, t := range taskList {
		for _, s := range t.DependsOn {
			if id, err := uuid.Parse(s); err == nil {
				adj[t.ID] = append(adj[t.ID], id)
			}
		}
	}
	return taskReachableInAdj(adj, start, target)
}

// taskReachableInAdj reports whether target is reachable from start by following
// edges in the provided adjacency map (task → its dependencies).
// Used by BatchCreateTasks for full-graph cycle detection.
func taskReachableInAdj(adj map[uuid.UUID][]uuid.UUID, start, target uuid.UUID) bool {
	visited := make(map[uuid.UUID]bool)
	var dfs func(uuid.UUID) bool
	dfs = func(cur uuid.UUID) bool {
		if cur == target {
			return true
		}
		if visited[cur] {
			return false
		}
		visited[cur] = true
		for _, dep := range adj[cur] {
			if dfs(dep) {
				return true
			}
		}
		return false
	}
	return dfs(start)
}

// tryAutoPromote checks if there is capacity to run more tasks and promotes
// the highest-priority (lowest position) backlog task if so.
// When autopilot is disabled, no promotion happens.
//
// Concurrency design: two-phase protocol via runTwoPhase.
//
// Phase 1 (no lock): call store.ListTasksByStatus, compute the regular in-progress
// count, and find the best backlog candidate. AreDependenciesSatisfied may do
// disk I/O here; we must not hold promoteMu during these potentially slow
// operations so that a concurrent tryAutoPromote call (or tryAutoTest) can
// proceed in parallel.
//
// Phase 2 (under promoteMu): re-count to pick up any state changes that
// happened during Phase 1, re-check capacity, then promote.
func (h *Handler) tryAutoPromote(ctx context.Context) {
	if !h.AutopilotEnabled() {
		return
	}

	type autoResumeCandidate struct {
		task     store.Task
		feedback string
	}

	var resumeCandidate *autoResumeCandidate

	runTwoPhase(ctx, &promoteMu, TwoPhaseWatcherConfig{
		Name: "auto-promote",
		Phase1: func(ctx context.Context) (*store.Task, error) {
			// Phase 1 (no lock): build candidate without holding promoteMu.
			waitingTasks, err := h.store.ListTasksByStatus(ctx, store.TaskStatusWaiting)
			if err == nil {
				for i := range waitingTasks {
					t := &waitingTasks[i]
					if t.IsTestRun || t.PendingTestFeedback == "" || t.LastTestResult != "fail" {
						continue
					}
					if t.SessionID == nil || *t.SessionID == "" {
						continue
					}
					if resumeCandidate == nil || t.Position < resumeCandidate.task.Position {
						cp := *t
						resumeCandidate = &autoResumeCandidate{
							task:     cp,
							feedback: t.PendingTestFeedback,
						}
					}
				}
			}
			if resumeCandidate != nil {
				return &resumeCandidate.task, nil
			}

			regularInProgress := h.store.CountRegularInProgress()
			if regularInProgress >= h.maxConcurrentTasks() {
				h.incAutopilotAction("auto_promoter", "skipped_capacity")
				return nil, nil
			}

			backlogTasks, err := h.store.ListTasksByStatus(ctx, store.TaskStatusBacklog)
			if err != nil {
				return nil, nil
			}

			var bestBacklog *store.Task
			for i := range backlogTasks {
				t := &backlogTasks[i]
				if t.Kind == store.TaskKindIdeaAgent {
					continue
				}
				// Skip tasks that have a future scheduled start time.
				if t.ScheduledAt != nil && time.Now().Before(*t.ScheduledAt) {
					h.incAutopilotAction("auto_promoter", "skipped_scheduled")
					continue
				}
				satisfied, err := h.store.AreDependenciesSatisfied(ctx, t.ID)
				if err != nil || !satisfied {
					h.incAutopilotAction("auto_promoter", "skipped_dependency")
					continue // skip: dependencies not yet done
				}
				if bestBacklog == nil || t.Position < bestBacklog.Position {
					cp := *t
					bestBacklog = &cp
				}
			}
			return bestBacklog, nil
		},
		AfterPhase1: h.testPhase1Done,
		Phase2: func(ctx context.Context, candidate *store.Task) (bool, error) {
			if resumeCandidate != nil && candidate != nil && candidate.ID == resumeCandidate.task.ID {
				freshTask, err := h.store.GetTask(ctx, candidate.ID)
				if err != nil || freshTask == nil {
					return false, nil
				}
				if freshTask.Status != store.TaskStatusWaiting || freshTask.IsTestRun || freshTask.LastTestResult != "fail" || freshTask.PendingTestFeedback == "" {
					return false, nil
				}
				if freshTask.SessionID == nil || *freshTask.SessionID == "" {
					return false, nil
				}

				logger.Handler.Info("auto-promote: resuming waiting task from failed test feedback",
					"task", freshTask.ID)
				if err := h.resumeWaitingTaskWithFeedbackLocked(ctx, freshTask, freshTask.PendingTestFeedback, store.TriggerFeedback, "Autopilot: resuming task with failed test feedback."); err != nil {
					logger.Handler.Error("auto-promote resume failed test feedback", "task", freshTask.ID, "error", err)
					h.pauseAllAutomation(&freshTask.ID, "auto-promote", err.Error())
					return false, nil
				}
				h.incAutopilotAction("auto_promoter", "resumed_failed_test")
				return true, nil
			}

			// Phase 2 (under promoteMu): re-verify capacity with a fresh count and promote.
			// Re-read in-progress count; state may have changed during Phase 1 I/O.
			freshInProgress := h.store.CountRegularInProgress()
			if freshInProgress >= h.maxConcurrentTasks() {
				h.incAutopilotAction("auto_promoter", "skipped_capacity")
				return false, nil
			}

			// Abort promotion when the container runtime is known-unavailable.
			// Without this guard, slot openings caused by failures would trigger
			// back-to-back promotions that all immediately fail, cascading across
			// every backlog task.
			if !h.runner.ContainerCircuitAllow() {
				logger.Handler.Warn("auto-promote skipped: container circuit breaker open")
				return false, nil
			}

			logger.Handler.Info("auto-promoting backlog task",
				"task", candidate.ID, "position", candidate.Position,
				"in_progress", freshInProgress)

			if err := h.store.UpdateTaskStatus(ctx, candidate.ID, store.TaskStatusInProgress); err != nil {
				logger.Handler.Error("auto-promote status update", "task", candidate.ID, "error", err)
				h.pauseAllAutomation(&candidate.ID, "auto-promote", err.Error())
				return false, nil
			}
			h.incAutopilotAction("auto_promoter", "promoted")
			h.store.InsertEvent(ctx, candidate.ID, store.EventTypeStateChange, map[string]string{
				"from":    string(store.TaskStatusBacklog),
				"to":      string(store.TaskStatusInProgress),
				"trigger": store.TriggerAutoPromote,
			})

			sessionID := ""
			if !candidate.FreshStart && candidate.SessionID != nil {
				sessionID = *candidate.SessionID
			}
			h.runner.RunBackground(candidate.ID, candidate.Prompt, sessionID, false)
			return true, nil
		},
	})
}

// tryAutoRetry checks whether a newly-failed task should be automatically
// reset to backlog for a retry. Only transient infrastructure failure
// categories (container_crash, worktree_setup, sync_error) are retried.
// Agent errors, budget overruns, timeouts, and unknown failures require
// human review.
//
// It respects the container circuit breaker: if the circuit is open,
// container_crash retries are suppressed to avoid cascading restarts.
func (h *Handler) tryAutoRetry(ctx context.Context, task store.Task) {
	if task.Status != store.TaskStatusFailed {
		return
	}
	if !retryableCategories[task.FailureCategory] {
		return
	}
	if task.AutoRetryBudget[task.FailureCategory] <= 0 || task.AutoRetryCount >= maxHandlerAutoRetries {
		logger.Handler.Info("auto-retry suppressed: max retries reached",
			"task", task.ID, "auto_retry_count", task.AutoRetryCount,
			"category", task.FailureCategory)
		h.incAutopilotAction("auto_retrier", "suppressed_budget")
		return
	}
	// For container-crash failures, honour the circuit breaker.
	if task.FailureCategory == store.FailureCategoryContainerCrash && !h.runner.ContainerCircuitAllow() {
		logger.Handler.Warn("auto-retry suppressed: container circuit breaker open",
			"task", task.ID)
		h.incAutopilotAction("auto_retrier", "suppressed_circuit")
		return
	}
	logger.Handler.Info("auto-retrying failed task",
		"task", task.ID, "category", task.FailureCategory,
		"retry_attempt", task.AutoRetryCount+1)
	if err := h.store.ResetTaskForRetry(ctx, task.ID, task.Prompt, false); err != nil {
		logger.Handler.Error("auto-retry reset failed", "task", task.ID, "error", err)
		h.pauseAllAutomation(&task.ID, "auto-retry", err.Error())
		return
	}
	h.incAutopilotAction("auto_retrier", "retried")
	h.store.InsertEvent(ctx, task.ID, store.EventTypeStateChange, map[string]string{
		"from":             string(store.TaskStatusFailed),
		"to":               string(store.TaskStatusBacklog),
		"trigger":          store.TriggerAutoRetry,
		"failure_category": string(task.FailureCategory),
	})
}

// waitingSyncInterval is how often the watcher polls for waiting tasks that
// have fallen behind the default branch.
const waitingSyncInterval = 30 * time.Second

// StartWaitingSyncWatcher starts a background goroutine that periodically
// checks all waiting tasks and automatically syncs any whose worktrees have
// fallen behind the default branch.
func (h *Handler) StartWaitingSyncWatcher(ctx context.Context) {
	ticker := time.NewTicker(waitingSyncInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.checkAndSyncWaitingTasks(ctx)
			}
		}
	}()
}

// checkAndSyncWaitingTasks inspects every waiting task that has worktrees. If
// any worktree is behind the default branch it automatically transitions the
// task to in_progress and triggers SyncWorktrees, exactly as if the user had
// clicked the "Sync" button.
func (h *Handler) checkAndSyncWaitingTasks(ctx context.Context) {
	tasks, err := h.store.ListTasksByStatus(ctx, store.TaskStatusWaiting)
	if err != nil {
		return
	}
	maxTasks := h.maxConcurrentTasks()
	maxVerificationTasks := h.maxTestConcurrentTasks()

	for i := range tasks {
		t := &tasks[i]
		if len(t.WorktreePaths) == 0 {
			continue
		}

		behind := false
		for repoPath, worktreePath := range t.WorktreePaths {
			if _, err := os.Stat(worktreePath); err != nil {
				// Worktree directory no longer exists on disk; skip silently.
				continue
			}
			n, err := gitutil.CommitsBehind(repoPath, worktreePath)
			if err != nil {
				logger.Handler.Warn("auto-sync: check commits behind",
					"task", t.ID, "repo", repoPath, "error", err)
				continue
			}
			if n > 0 {
				behind = true
				break
			}
		}

		if !behind {
			continue
		}

		logger.Handler.Info("auto-sync: waiting task behind default branch, syncing",
			"task", t.ID)

		promoteMu.Lock()
		regularInProgress := h.store.CountRegularInProgress()
		syncCapacity := maxTasks
		if h.waitingTaskUsesVerificationCapacity(t) {
			syncCapacity += maxVerificationTasks
		}

		if regularInProgress >= syncCapacity {
			promoteMu.Unlock()
			logger.Handler.Info("auto-sync: in-progress limit reached, deferring sync",
				"task", t.ID, "count", regularInProgress, "max", syncCapacity)
			h.incAutopilotAction("sync_watcher", "skipped_capacity")
			continue
		}

		if err := h.store.UpdateTaskStatus(ctx, t.ID, store.TaskStatusInProgress); err != nil {
			promoteMu.Unlock()
			logger.Handler.Error("auto-sync: update task status", "task", t.ID, "error", err)
			h.pauseAllAutomation(&t.ID, "auto-sync", err.Error())
			continue
		}
		regularInProgress++
		h.incAutopilotAction("sync_watcher", "synced")
		h.store.InsertEvent(ctx, t.ID, store.EventTypeStateChange, map[string]string{
			"from":    string(store.TaskStatusWaiting),
			"to":      string(store.TaskStatusInProgress),
			"trigger": store.TriggerSync,
		})
		h.store.InsertEvent(ctx, t.ID, store.EventTypeSystem, map[string]string{
			"result": "Auto-syncing: worktree is behind the default branch.",
		})

		sessionID := ""
		if t.SessionID != nil {
			sessionID = *t.SessionID
		}
		h.diffCache.invalidate(t.ID)
		taskID := t.ID
		promoteMu.Unlock()
		h.runner.SyncWorktreesBackground(taskID, sessionID, store.TaskStatusWaiting, func() {
			h.diffCache.invalidate(taskID)
		})
	}
}

// autoTestInterval is how often the auto-tester polls for eligible waiting tasks
// in addition to reacting to store change notifications.
const autoTestInterval = 30 * time.Second

// StartAutoTester subscribes to store change notifications and automatically
// triggers the test agent for waiting tasks that are untested and not behind
// the default branch tip.
func (h *Handler) StartAutoTester(ctx context.Context) {
	subID, ch := h.store.Subscribe()
	ticker := time.NewTicker(autoTestInterval)
	go func() {
		defer h.store.Unsubscribe(subID)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				h.tryAutoTest(ctx)
			case <-ticker.C:
				h.tryAutoTest(ctx)
			}
		}
	}()
}

// autoTestCandidate holds an eligible waiting task and its pre-built test prompt.
type autoTestCandidate struct {
	task       store.Task
	testPrompt string
}

// tryAutoTest scans all waiting tasks and triggers the test agent for any
// that are untested (LastTestResult == "") and whose worktrees are not behind
// the default branch. Does nothing when auto-test is disabled.
//
// Concurrency limit: test runs have their own independent limit controlled by
// maxTestConcurrentTasks (WALLFACER_MAX_TEST_PARALLEL). Only IsTestRun
// in-progress tasks count against this limit; regular tasks are unaffected.
// The promoteMu mutex is still shared with tryAutoPromote to prevent races on
// the same task.
//
// Uses the two-phase protocol via runTwoPhase:
//
// Phase 1 (no lock): scan waiting tasks and build the full candidates list,
// performing git I/O (CommitsBehind) without holding promoteMu.
//
// Phase 2 (under promoteMu): re-read a fresh task snapshot, enforce the test
// concurrency limit, then trigger each still-eligible candidate.
func (h *Handler) tryAutoTest(ctx context.Context) {
	if !h.AutotestEnabled() {
		return
	}

	// candidates is populated by Phase1 and consumed by Phase2 via closure.
	var candidates []autoTestCandidate

	runTwoPhase(ctx, &promoteMu, TwoPhaseWatcherConfig{
		Name: "auto-test",
		Phase1: func(ctx context.Context) (*store.Task, error) {
			// Phase 1 (no lock): build the list of eligible candidates.
			// Git I/O (CommitsBehind) happens here so we don't hold promoteMu
			// during potentially slow filesystem operations.
			waitingTasks, err := h.store.ListTasksByStatus(ctx, store.TaskStatusWaiting)
			if err != nil {
				return nil, nil
			}

			for i := range waitingTasks {
				t := &waitingTasks[i]
				// Skip tasks that already have a test result or are currently being tested.
				if t.LastTestResult != "" || t.IsTestRun {
					continue
				}

				// Skip tasks with no worktrees (nothing to test yet).
				if len(t.WorktreePaths) == 0 {
					continue
				}

				// Only trigger if the worktree is up to date with the default branch.
				behind := false
				for repoPath, worktreePath := range t.WorktreePaths {
					n, err := gitutil.CommitsBehind(repoPath, worktreePath)
					if err != nil {
						logger.Handler.Warn("auto-test: check commits behind",
							"task", t.ID, "repo", repoPath, "error", err)
						behind = true // treat errors conservatively
						break
					}
					if n > 0 {
						behind = true
						break
					}
				}
				if behind {
					continue
				}

				implResult := ""
				if t.Result != nil {
					implResult = *t.Result
				}
				diff := generateWorktreeDiff(t.WorktreePaths)
				testPrompt := buildTestPrompt(t.Prompt, "", implResult, diff)
				candidates = append(candidates, autoTestCandidate{task: *t, testPrompt: testPrompt})
			}

			if len(candidates) == 0 {
				return nil, nil
			}
			// Return first candidate as signal that there is at least one eligible task.
			first := &candidates[0].task
			return first, nil
		},
		Phase2: func(ctx context.Context, _ *store.Task) (bool, error) {
			// Phase 2 (under promoteMu): enforce the concurrency limit and trigger.
			// Sharing promoteMu with tryAutoPromote prevents the two from racing to
			// exceed maxConcurrentTasks simultaneously.

			// Re-read for a fresh snapshot; state may have changed during the git checks above.
			freshWaiting, err := h.store.ListTasksByStatus(ctx, store.TaskStatusWaiting)
			if err != nil {
				return false, nil
			}
			freshByID := make(map[uuid.UUID]store.Task, len(freshWaiting))
			for _, t := range freshWaiting {
				freshByID[t.ID] = t
			}
			freshInProgress, err := h.store.ListTasksByStatus(ctx, store.TaskStatusInProgress)
			if err != nil {
				return false, nil
			}
			testInProgress := 0
			for _, t := range freshInProgress {
				if t.IsTestRun {
					testInProgress++
				}
			}

			maxTestTasks := h.maxTestConcurrentTasks()
			triggered := false

			for _, c := range candidates {
				if testInProgress >= maxTestTasks {
					logger.Handler.Info("auto-test: test concurrency limit reached, deferring remaining tests",
						"limit", maxTestTasks)
					h.incAutopilotAction("auto_tester", "skipped_capacity")
					break
				}

				// Re-verify eligibility using the fresh snapshot.
				ft, ok := freshByID[c.task.ID]
				if !ok || ft.Status != store.TaskStatusWaiting || ft.LastTestResult != "" || ft.IsTestRun {
					continue
				}

				logger.Handler.Info("auto-test: triggering test agent for waiting task", "task", c.task.ID)

				if err := h.store.UpdateTaskTestRun(ctx, c.task.ID, true, ""); err != nil {
					logger.Handler.Error("auto-test: update test run flag", "task", c.task.ID, "error", err)
					h.pauseAllAutomation(&c.task.ID, "auto-test", err.Error())
					continue
				}
				if err := h.store.UpdateTaskStatus(ctx, c.task.ID, store.TaskStatusInProgress); err != nil {
					logger.Handler.Error("auto-test: update task status", "task", c.task.ID, "error", err)
					h.pauseAllAutomation(&c.task.ID, "auto-test", err.Error())
					continue
				}
				h.store.InsertEvent(ctx, c.task.ID, store.EventTypeStateChange, map[string]string{
					"from":    string(store.TaskStatusWaiting),
					"to":      string(store.TaskStatusInProgress),
					"trigger": store.TriggerAutoTest,
				})
				h.store.InsertEvent(ctx, c.task.ID, store.EventTypeSystem, map[string]string{
					"result": "Auto-test: triggering test verification agent.",
				})

				h.runner.RunBackground(c.task.ID, c.testPrompt, "", false)
				testInProgress++
				triggered = true
				h.incAutopilotAction("auto_tester", "tested")
			}

			return triggered, nil
		},
	})
}

// autoSubmitInterval is how often the auto-submitter polls for eligible waiting tasks
// in addition to reacting to store change notifications.
const autoSubmitInterval = 30 * time.Second

// StartAutoSubmitter subscribes to store change notifications and automatically
// moves waiting tasks to done when they are verified (LastTestResult == "pass"),
// not behind the default branch tip, and have no unresolved worktree conflicts.
func (h *Handler) StartAutoSubmitter(ctx context.Context) {
	subID, ch := h.store.Subscribe()
	ticker := time.NewTicker(autoSubmitInterval)
	go func() {
		defer h.store.Unsubscribe(subID)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				h.tryAutoSubmit(ctx)
			case <-ticker.C:
				h.tryAutoSubmit(ctx)
			}
		}
	}()
}

// autoSubmitCandidate holds a waiting task that has passed all eligibility checks
// and is ready for auto-submission.
type autoSubmitCandidate struct {
	task              store.Task
	naturallyComplete bool
}

// tryAutoSubmit scans all waiting tasks and moves any that are verified
// (LastTestResult == "pass"), not behind the default branch, and free of
// worktree conflicts directly to done (via the commit pipeline if a session
// exists). Does nothing when auto-submit is disabled.
//
// Uses the two-phase protocol via runTwoPhase with a nil mutex (auto-submit
// transitions tasks to done/committing rather than in_progress, so it does
// not compete for the promoteMu capacity slot):
//
// Phase 1 (no lock): perform the slow git I/O (CommitsBehind, HasConflicts)
// for every eligible waiting task and collect the candidates.
//
// Phase 2 (no lock): execute the status transitions for all collected candidates.
func (h *Handler) tryAutoSubmit(ctx context.Context) {
	if !h.AutosubmitEnabled() {
		return
	}

	// candidates is populated by Phase1 and consumed by Phase2 via closure.
	var candidates []autoSubmitCandidate

	runTwoPhase(ctx, nil, TwoPhaseWatcherConfig{
		Name: "auto-submit",
		Phase1: func(ctx context.Context) (*store.Task, error) {
			tasks, err := h.store.ListTasks(ctx, false)
			if err != nil {
				return nil, nil
			}

			for i := range tasks {
				t := &tasks[i]
				if t.Status != store.TaskStatusWaiting {
					continue
				}
				// Determine eligibility:
				// (a) Passed verification ("pass").
				// (b) Naturally completed (stop_reason="end_turn") and not yet tested,
				//     but only when auto-test is off — otherwise let auto-test run first.
				// Tasks that failed testing are never auto-submitted.
				tested := t.LastTestResult == "pass"
				naturallyComplete := t.StopReason != nil && *t.StopReason == "end_turn" && t.LastTestResult == "" && !h.AutotestEnabled()
				if !tested && !naturallyComplete {
					continue
				}
				// Skip while the test agent is still running.
				if t.IsTestRun {
					continue
				}

				// Check that all worktrees are up to date and conflict-free.
				skip := false
				for repoPath, worktreePath := range t.WorktreePaths {
					n, err := gitutil.CommitsBehind(repoPath, worktreePath)
					if err != nil {
						logger.Handler.Warn("auto-submit: check commits behind",
							"task", t.ID, "repo", repoPath, "error", err)
						skip = true
						break
					}
					if n > 0 {
						skip = true
						break
					}
					hasConflict, err := gitutil.HasConflicts(worktreePath)
					if err != nil {
						logger.Handler.Warn("auto-submit: check conflicts",
							"task", t.ID, "worktree", worktreePath, "error", err)
						skip = true
						break
					}
					if hasConflict {
						h.incAutopilotAction("auto_submitter", "skipped_conflict")
						skip = true
						break
					}
				}
				if skip {
					continue
				}

				candidates = append(candidates, autoSubmitCandidate{task: *t, naturallyComplete: naturallyComplete})
			}

			if len(candidates) == 0 {
				return nil, nil
			}
			first := &candidates[0].task
			return first, nil
		},
		Phase2: func(ctx context.Context, _ *store.Task) (bool, error) {
			submitted := false
			for _, c := range candidates {
				t := c.task
				logger.Handler.Info("auto-submit: completing verified waiting task", "task", t.ID)
				autoSubmitMsg := "Auto-submit: task verified with passing tests, up to date, and no conflicts."
				if c.naturallyComplete {
					autoSubmitMsg = "Auto-submit: task naturally completed, up to date, and no conflicts."
				}
				h.store.InsertEvent(ctx, t.ID, store.EventTypeSystem, map[string]string{
					"result": autoSubmitMsg,
				})

				if t.SessionID != nil && *t.SessionID != "" {
					if err := h.store.UpdateTaskStatus(ctx, t.ID, store.TaskStatusCommitting); err != nil {
						logger.Handler.Error("auto-submit: update task status", "task", t.ID, "error", err)
						h.pauseAllAutomation(&t.ID, "auto-submit", err.Error())
						continue
					}
					h.store.InsertEvent(ctx, t.ID, store.EventTypeStateChange, map[string]string{
						"from":    string(store.TaskStatusWaiting),
						"to":      string(store.TaskStatusCommitting),
						"trigger": store.TriggerAutoSubmit,
					})
					h.runCommitTransition(t.ID, *t.SessionID, store.TriggerAutoSubmit, "auto-submit: commit failed: ")
				} else {
					// No session — move directly to done (bypasses state machine
					// since waiting→done is deliberately blocked to protect the commit pipeline).
					if err := h.store.ForceUpdateTaskStatus(ctx, t.ID, store.TaskStatusDone); err != nil {
						logger.Handler.Error("auto-submit: update task status to done", "task", t.ID, "error", err)
						h.pauseAllAutomation(&t.ID, "auto-submit", err.Error())
						continue
					}
					h.store.InsertEvent(ctx, t.ID, store.EventTypeStateChange, map[string]string{
						"from":    string(store.TaskStatusWaiting),
						"to":      string(store.TaskStatusDone),
						"trigger": store.TriggerAutoSubmit,
					})
				}
				submitted = true
				h.incAutopilotAction("auto_submitter", "submitted")
			}
			return submitted, nil
		},
	})
}
