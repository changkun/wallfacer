package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"changkun.de/x/wallfacer/internal/envconfig"
	"changkun.de/x/wallfacer/internal/gitutil"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

const defaultMaxConcurrentTasks = 5

// maxTestFailRetries is the maximum number of consecutive test failures before
// the auto-resume cycle is halted. After this many failures without a passing
// test or manual feedback, the task stays in waiting until the user intervenes.
const maxTestFailRetries = 3

// defaultMaxTestConcurrentTasks is used when WALLFACER_MAX_TEST_PARALLEL is not set.
const defaultMaxTestConcurrentTasks = 2

// maxConcurrentTasks returns the configured parallel task limit. The value is
// cached in an atomic field so the env file is only parsed on the first call
// and after UpdateEnvConfig invalidates the cache (by resetting the field to 0).
func (h *Handler) maxConcurrentTasks() int {
	if v := h.cachedMaxParallel.Load(); v > 0 {
		return int(v)
	}
	cfg, err := envconfig.Parse(h.envFile)
	if err != nil || cfg.MaxParallelTasks <= 0 {
		h.cachedMaxParallel.Store(int32(defaultMaxConcurrentTasks))
		return defaultMaxConcurrentTasks
	}
	h.cachedMaxParallel.Store(int32(cfg.MaxParallelTasks))
	return cfg.MaxParallelTasks
}

// maxTestConcurrentTasks returns the configured parallel test-run limit. The
// value is cached in an atomic field so the env file is only parsed on the
// first call and after UpdateEnvConfig invalidates the cache (by resetting the
// field to 0).
func (h *Handler) maxTestConcurrentTasks() int {
	if v := h.cachedMaxTestParallel.Load(); v > 0 {
		return int(v)
	}
	cfg, err := envconfig.Parse(h.envFile)
	if err != nil || cfg.MaxTestParallelTasks <= 0 {
		h.cachedMaxTestParallel.Store(int32(defaultMaxTestConcurrentTasks))
		return defaultMaxTestConcurrentTasks
	}
	h.cachedMaxTestParallel.Store(int32(cfg.MaxTestParallelTasks))
	return cfg.MaxTestParallelTasks
}

func (h *Handler) countRegularInProgress(_ context.Context) (int, error) {
	return h.store.CountRegularInProgress(), nil
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
func (h *Handler) checkConcurrencyAndUpdateStatus(ctx context.Context, w http.ResponseWriter, id uuid.UUID, newStatus store.TaskStatus) bool {
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
// state change occurs. Additionally, ensureScheduledPromoteTrigger arms a
// precise one-shot timer for the soonest scheduled task so promotion happens
// within milliseconds of the due time rather than waiting up to 60 seconds.
func (h *Handler) StartAutoPromoter(ctx context.Context) {
	subID, ch := h.store.SubscribeWake()
	ticker := time.NewTicker(60 * time.Second)
	go func() {
		defer h.store.UnsubscribeWake(subID)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				h.scheduledPromoteMu.Lock()
				if h.scheduledPromoteTimer != nil {
					h.scheduledPromoteTimer.Stop()
				}
				h.scheduledPromoteMu.Unlock()
				return
			case <-ch:
				h.tryAutoPromote(ctx)
			case <-ticker.C:
				h.tryAutoPromote(ctx)
			}
		}
	}()
}

// ensureScheduledPromoteTrigger arms (or re-arms) a one-shot timer so that
// tryAutoPromote is called at precisely the moment the soonest scheduled task
// becomes due. If due is already in the past the timer is not set (the current
// tryAutoPromote call handles it). Any existing timer is replaced so that we
// always fire at the earliest due time.
func (h *Handler) ensureScheduledPromoteTrigger(ctx context.Context, due time.Time) {
	delay := time.Until(due)
	if delay <= 0 {
		return // already due; current tryAutoPromote call handles it
	}
	h.scheduledPromoteMu.Lock()
	defer h.scheduledPromoteMu.Unlock()
	if h.scheduledPromoteTimer != nil {
		if !h.scheduledPromoteTimer.Stop() {
			select {
			case <-h.scheduledPromoteTimer.C:
			default:
			}
		}
	}
	h.scheduledPromoteTimer = time.AfterFunc(delay, func() {
		h.tryAutoPromote(ctx)
	})
}

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
		subID, ch := h.store.SubscribeWake()
		defer h.store.UnsubscribeWake(subID)

		// Recovery scan: retry any eligible failed tasks that predate startup.
		failed, _ := h.store.ListTasksByStatus(ctx, store.TaskStatusFailed)
		for _, t := range failed {
			h.tryAutoRetry(ctx, t)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				failed, _ := h.store.ListTasksByStatus(ctx, store.TaskStatusFailed)
				for _, t := range failed {
					h.tryAutoRetry(ctx, t)
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
	if h.breakers["auto-promote"].isOpen() {
		return
	}

	type autoResumeCandidate struct {
		task     store.Task
		feedback string
	}

	var resumeCandidate *autoResumeCandidate

	runTwoPhase(ctx, &promoteMu, TwoPhaseWatcherConfig{
		Name: "auto-promote",
		OnPhase1Error: func(err error) {
			h.breakers["auto-promote"].recordFailure(nil, err.Error())
		},
		Phase1: func(ctx context.Context) (*store.Task, error) {
			// Phase 1 (no lock): build candidate without holding promoteMu.
			waitingTasks, err := h.store.ListTasksByStatus(ctx, store.TaskStatusWaiting)
			if err != nil {
				return nil, err
			}
			for i := range waitingTasks {
				t := &waitingTasks[i]
				if t.IsTestRun || t.PendingTestFeedback == "" || t.LastTestResult != "fail" {
					continue
				}
				if t.SessionID == nil || *t.SessionID == "" {
					continue
				}
				// Cap: skip tasks that have failed too many consecutive tests.
				if t.TestFailCount >= maxTestFailRetries {
					logger.Handler.Info("auto-promote: skipping task — test fail cap reached",
						"task", t.ID, "test_fail_count", t.TestFailCount, "max", maxTestFailRetries)
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
				return nil, err
			}

			type cpCandidate struct {
				task  store.Task
				score int
			}
			var cpCandidates []cpCandidate
			var nextScheduled *time.Time
			for i := range backlogTasks {
				t := &backlogTasks[i]
				if t.Kind == store.TaskKindIdeaAgent {
					continue
				}
				if t.ScheduledAt != nil && time.Now().Before(*t.ScheduledAt) {
					h.incAutopilotAction("auto_promoter", "skipped_scheduled")
					if nextScheduled == nil || t.ScheduledAt.Before(*nextScheduled) {
						nextScheduled = t.ScheduledAt
					}
					continue
				}
				satisfied, err := h.store.AreDependenciesSatisfied(ctx, t.ID)
				if err != nil || !satisfied {
					h.incAutopilotAction("auto_promoter", "skipped_dependency")
					continue
				}
				cpCandidates = append(cpCandidates, cpCandidate{task: *t, score: h.store.CriticalPathScore(t.ID)})
			}
			// Arm a precise timer for the soonest scheduled task so it is
			// promoted within milliseconds of its due time rather than waiting
			// for the next 60-second ticker tick.
			if nextScheduled != nil {
				h.ensureScheduledPromoteTrigger(ctx, *nextScheduled)
			}
			if len(cpCandidates) == 0 {
				return nil, nil
			}
			sort.Slice(cpCandidates, func(i, j int) bool {
				if cpCandidates[i].score != cpCandidates[j].score {
					return cpCandidates[i].score > cpCandidates[j].score
				}
				if cpCandidates[i].task.Position != cpCandidates[j].task.Position {
					return cpCandidates[i].task.Position < cpCandidates[j].task.Position
				}
				return cpCandidates[i].task.CreatedAt.Before(cpCandidates[j].task.CreatedAt)
			})
			best := cpCandidates[0].task
			return &best, nil
		},
		AfterPhase1: h.testPhase1Done,
		OnPhase2Miss: func(_ *store.Task) {
			h.incAutopilotPhase2Miss("auto_promoter")
		},
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
				if freshTask.TestFailCount >= maxTestFailRetries {
					logger.Handler.Info("auto-promote: test fail cap reached, stopping auto-resume",
						"task", freshTask.ID, "test_fail_count", freshTask.TestFailCount)
					h.insertEventOrLog(ctx, freshTask.ID, store.EventTypeSystem, map[string]string{
						"result": fmt.Sprintf("Auto-resume halted: %d consecutive test failures (cap: %d). Manual feedback required to continue.", freshTask.TestFailCount, maxTestFailRetries),
					})
					return false, nil
				}

				logger.Handler.Info("auto-promote: resuming waiting task from failed test feedback",
					"task", freshTask.ID)
				if err := h.resumeWaitingTaskWithFeedbackLocked(ctx, freshTask, freshTask.PendingTestFeedback, store.TriggerFeedback, "Autopilot: resuming task with failed test feedback."); err != nil {
					logger.Handler.Error("auto-promote resume failed test feedback", "task", freshTask.ID, "error", err)
					h.breakers["auto-promote"].recordFailure(&freshTask.ID, err.Error())
					return false, nil
				}
				h.incAutopilotAction("auto_promoter", "resumed_failed_test")
				h.breakers["auto-promote"].recordSuccess()
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
				h.breakers["auto-promote"].recordFailure(&candidate.ID, err.Error())
				return false, nil
			}
			h.incAutopilotAction("auto_promoter", "promoted")
			h.insertEventOrLog(ctx, candidate.ID, store.EventTypeStateChange,
				store.NewStateChangeData(store.TaskStatusBacklog, store.TaskStatusInProgress, store.TriggerAutoPromote, nil))

			sessionID := ""
			if !candidate.FreshStart && candidate.SessionID != nil {
				sessionID = *candidate.SessionID
			}
			h.runner.RunBackground(candidate.ID, candidate.Prompt, sessionID, false)
			h.breakers["auto-promote"].recordSuccess()
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
	if h.breakers["auto-retry"].isOpen() {
		return
	}
	if !store.IsAutoRetryEligible(task, task.FailureCategory) {
		logger.Handler.Info("auto-retry suppressed: max retries reached",
			"task", task.ID, "auto_retry_count", task.AutoRetryCount,
			"max", store.MaxAutoRetries, "category", task.FailureCategory)
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
		h.breakers["auto-retry"].recordFailure(&task.ID, err.Error())
		return
	}
	h.incAutopilotAction("auto_retrier", "retried")
	h.breakers["auto-retry"].recordSuccess()
	h.insertEventOrLog(ctx, task.ID, store.EventTypeStateChange,
		store.NewStateChangeData(store.TaskStatusFailed, store.TaskStatusBacklog, store.TriggerAutoRetry, map[string]string{
			"failure_category": string(task.FailureCategory),
		}))
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
//
// Sync operations are lightweight host-side git rebases — they do not launch
// containers and therefore bypass the regular task capacity check. This ensures
// waiting tasks stay up to date even when the board is running at full capacity.
//
// Shared cache design: this watcher, tryAutoTest, and tryAutoSubmit all call
// CommitsBehind for every waiting task on 30-second tickers. h.commitsBehindCache
// deduplicates those calls (TTL=20s) so all three watchers share a single
// post-fetch result per (repoPath, worktreePath) within each polling window.
// checkAndSyncWaitingTasks always invalidates the cache entry before calling
// CommitsBehind so the result reflects the just-fetched remote refs.
func (h *Handler) checkAndSyncWaitingTasks(ctx context.Context) {
	if !h.AutosyncEnabled() {
		return
	}
	if h.breakers["auto-sync"].isOpen() {
		return
	}

	tasks, err := h.store.ListTasksByStatus(ctx, store.TaskStatusWaiting)
	if err != nil {
		return
	}

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
			if !gitutil.IsGitRepo(worktreePath) {
				// Directory exists but .git link is broken; skip silently.
				continue
			}
			// Fetch from remote so CommitsBehind operates on up-to-date refs.
			if fetchErr := gitutil.FetchOrigin(repoPath); fetchErr != nil {
				logger.Handler.Warn("auto-sync: git fetch failed, continuing with local refs",
					"task", t.ID, "repo", repoPath, "error", fetchErr)
			}
			// Invalidate any cached pre-fetch result so we always observe the
			// post-fetch remote ref state. The fresh result is then cached for
			// tryAutoTest and tryAutoSubmit to share within this polling window.
			h.commitsBehindCache.invalidate(repoPath, worktreePath)
			n, err := h.commitsBehindCache.cachedCommitsBehind(repoPath, worktreePath)
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
		if err := h.store.UpdateTaskStatus(ctx, t.ID, store.TaskStatusInProgress); err != nil {
			promoteMu.Unlock()
			logger.Handler.Error("auto-sync: update task status", "task", t.ID, "error", err)
			h.breakers["auto-sync"].recordFailure(&t.ID, err.Error())
			continue
		}
		h.incAutopilotAction("sync_watcher", "synced")
		h.insertEventOrLog(ctx, t.ID, store.EventTypeStateChange,
			store.NewStateChangeData(store.TaskStatusWaiting, store.TaskStatusInProgress, store.TriggerSync, nil))
		h.insertEventOrLog(ctx, t.ID, store.EventTypeSystem, map[string]string{
			"result": "Auto-syncing: worktree is behind the default branch.",
		})

		sessionID := ""
		if t.SessionID != nil {
			sessionID = *t.SessionID
		}
		h.diffCache.invalidate(t.ID)
		for repoPath, worktreePath := range t.WorktreePaths {
			h.commitsBehindCache.invalidate(repoPath, worktreePath)
		}
		taskID := t.ID
		worktreePaths := t.WorktreePaths
		promoteMu.Unlock()
		h.runner.SyncWorktreesBackground(taskID, sessionID, store.TaskStatusWaiting, func() {
			h.diffCache.invalidate(taskID)
			for repoPath, worktreePath := range worktreePaths {
				h.commitsBehindCache.invalidate(repoPath, worktreePath)
			}
		})
	}
	if !h.breakers["auto-sync"].isOpen() {
		h.breakers["auto-sync"].recordSuccess()
	}
}

// autoTestInterval is how often the auto-tester polls for eligible waiting tasks
// in addition to reacting to store change notifications.
const autoTestInterval = 30 * time.Second

// StartAutoTester subscribes to store change notifications and automatically
// triggers the test agent for waiting tasks that are untested and not behind
// the default branch tip.
func (h *Handler) StartAutoTester(ctx context.Context) {
	subID, ch := h.store.SubscribeWake()
	ticker := time.NewTicker(autoTestInterval)
	go func() {
		defer h.store.UnsubscribeWake(subID)
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
	if h.breakers["auto-test"].isOpen() {
		return
	}

	// candidates is populated by Phase1 and consumed by Phase2 via closure.
	var candidates []autoTestCandidate

	runTwoPhase(ctx, &promoteMu, TwoPhaseWatcherConfig{
		Name: "auto-test",
		OnPhase1Error: func(err error) {
			h.breakers["auto-test"].recordFailure(nil, err.Error())
		},
		OnPhase2Miss: func(_ *store.Task) {
			h.incAutopilotPhase2Miss("auto_tester")
		},
		Phase1: func(ctx context.Context) (*store.Task, error) {
			// Phase 1 (no lock): build the list of eligible candidates.
			// Git I/O (CommitsBehind) happens here so we don't hold promoteMu
			// during potentially slow filesystem operations.
			waitingTasks, err := h.store.ListTasksByStatus(ctx, store.TaskStatusWaiting)
			if err != nil {
				return nil, err
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
				if len(missingTaskWorktrees(t)) > 0 {
					continue
				}

				// Only trigger if the worktree is up to date with the default branch.
				behind := false
				for repoPath, worktreePath := range t.WorktreePaths {
					n, err := h.commitsBehindCache.cachedCommitsBehind(repoPath, worktreePath)
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
				if len(ft.WorktreePaths) == 0 || len(missingTaskWorktrees(&ft)) > 0 {
					continue
				}

				logger.Handler.Info("auto-test: triggering test agent for waiting task", "task", c.task.ID)

				if err := h.store.UpdateTaskTestRun(ctx, c.task.ID, true, ""); err != nil {
					logger.Handler.Error("auto-test: update test run flag", "task", c.task.ID, "error", err)
					h.breakers["auto-test"].recordFailure(&c.task.ID, err.Error())
					continue
				}
				if err := h.store.UpdateTaskStatus(ctx, c.task.ID, store.TaskStatusInProgress); err != nil {
					logger.Handler.Error("auto-test: update task status", "task", c.task.ID, "error", err)
					h.breakers["auto-test"].recordFailure(&c.task.ID, err.Error())
					// Roll back the IsTestRun flag so the task remains eligible for future test cycles.
					if rbErr := h.store.UpdateTaskTestRun(ctx, c.task.ID, false, ""); rbErr != nil {
						logger.Handler.Error("auto-test: rollback IsTestRun flag", "task", c.task.ID, "error", rbErr)
					}
					continue
				}
				h.closeFeedbackWaitingSpan(ctx, c.task.ID)
				h.insertEventOrLog(ctx, c.task.ID, store.EventTypeStateChange,
					store.NewStateChangeData(store.TaskStatusWaiting, store.TaskStatusInProgress, store.TriggerAutoTest, nil))
				h.insertEventOrLog(ctx, c.task.ID, store.EventTypeSystem, map[string]string{
					"result": "Auto-test: triggering test verification agent.",
				})

				h.runner.RunBackground(c.task.ID, c.testPrompt, "", false)
				testInProgress++
				triggered = true
				h.incAutopilotAction("auto_tester", "tested")
			}

			if !h.breakers["auto-test"].isOpen() {
				h.breakers["auto-test"].recordSuccess()
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
	subID, ch := h.store.SubscribeWake()
	ticker := time.NewTicker(autoSubmitInterval)
	go func() {
		defer h.store.UnsubscribeWake(subID)
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
// Uses the two-phase protocol via runTwoPhase with promoteMu to coordinate
// with SubmitFeedback and other operations that transition waiting tasks.
// Without this lock, auto-submit can race with feedback: auto-submit moves
// the task to committing and the commit pipeline cleans up worktrees while
// the user believes they are sending feedback to a still-waiting task.
//
// Phase 1 (no lock): perform the slow git I/O (CommitsBehind, HasConflicts)
// for every eligible waiting task and collect the candidates.
//
// Phase 2 (under promoteMu): execute the status transitions for all collected candidates.
func (h *Handler) tryAutoSubmit(ctx context.Context) {
	if !h.AutosubmitEnabled() {
		return
	}
	if h.breakers["auto-submit"].isOpen() {
		return
	}

	// candidates is populated by Phase1 and consumed by Phase2 via closure.
	var candidates []autoSubmitCandidate

	runTwoPhase(ctx, &promoteMu, TwoPhaseWatcherConfig{
		Name: "auto-submit",
		OnPhase1Error: func(err error) {
			h.breakers["auto-submit"].recordFailure(nil, err.Error())
		},
		OnPhase2Miss: func(_ *store.Task) {
			h.incAutopilotPhase2Miss("auto_submitter")
		},
		Phase1: func(ctx context.Context) (*store.Task, error) {
			tasks, err := h.store.ListTasks(ctx, false)
			if err != nil {
				return nil, err
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
				if len(t.WorktreePaths) == 0 || len(missingTaskWorktrees(t)) > 0 {
					continue
				}

				// Check that all worktrees are up to date and conflict-free.
				skip := false
				for repoPath, worktreePath := range t.WorktreePaths {
					if !gitutil.IsGitRepo(worktreePath) {
						skip = true
						break
					}
					n, err := h.commitsBehindCache.cachedCommitsBehind(repoPath, worktreePath)
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
				if len(t.WorktreePaths) == 0 || len(missingTaskWorktrees(&t)) > 0 {
					continue
				}
				logger.Handler.Info("auto-submit: completing verified waiting task", "task", t.ID)
				autoSubmitMsg := "Auto-submit: task verified with passing tests, up to date, and no conflicts."
				if c.naturallyComplete {
					autoSubmitMsg = "Auto-submit: task naturally completed, up to date, and no conflicts."
				}
				h.insertEventOrLog(ctx, t.ID, store.EventTypeSystem, map[string]string{
					"result": autoSubmitMsg,
				})
				h.closeFeedbackWaitingSpan(ctx, t.ID)

				if t.SessionID != nil && *t.SessionID != "" {
					if err := h.store.UpdateTaskStatus(ctx, t.ID, store.TaskStatusCommitting); err != nil {
						logger.Handler.Error("auto-submit: update task status", "task", t.ID, "error", err)
						h.breakers["auto-submit"].recordFailure(&t.ID, err.Error())
						continue
					}
					h.insertEventOrLog(ctx, t.ID, store.EventTypeStateChange,
						store.NewStateChangeData(store.TaskStatusWaiting, store.TaskStatusCommitting, store.TriggerAutoSubmit, nil))
					h.runCommitTransition(t.ID, *t.SessionID, store.TriggerAutoSubmit, "auto-submit: commit failed: ")
				} else {
					// No session — move directly to done (bypasses state machine
					// since waiting→done is deliberately blocked to protect the commit pipeline).
					if err := h.store.ForceUpdateTaskStatus(ctx, t.ID, store.TaskStatusDone); err != nil {
						logger.Handler.Error("auto-submit: update task status to done", "task", t.ID, "error", err)
						h.breakers["auto-submit"].recordFailure(&t.ID, err.Error())
						continue
					}
					h.insertEventOrLog(ctx, t.ID, store.EventTypeStateChange,
						store.NewStateChangeData(store.TaskStatusWaiting, store.TaskStatusDone, store.TriggerAutoSubmit, nil))
				}
				submitted = true
				h.incAutopilotAction("auto_submitter", "submitted")
			}
			if !h.breakers["auto-submit"].isOpen() {
				h.breakers["auto-submit"].recordSuccess()
			}
			return submitted, nil
		},
	})
}

// autoRefineInterval is how often the auto-refiner polls for backlog tasks.
const autoRefineInterval = 30 * time.Second

// StartAutoRefiner subscribes to store change notifications and automatically
// triggers the refinement agent for backlog tasks that have not yet been refined.
func (h *Handler) StartAutoRefiner(ctx context.Context) {
	subID, ch := h.store.SubscribeWake()
	ticker := time.NewTicker(autoRefineInterval)
	go func() {
		defer h.store.UnsubscribeWake(subID)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				h.tryAutoRefine(ctx)
			case <-ticker.C:
				h.tryAutoRefine(ctx)
			}
		}
	}()
}

// tryAutoRefine scans backlog tasks and triggers the refinement agent for any
// that have not yet been refined and do not have a refinement currently running.
// Does nothing when auto-refine is disabled.
func (h *Handler) tryAutoRefine(ctx context.Context) {
	if !h.AutorefineEnabled() {
		return
	}
	if h.breakers["auto-refine"].isOpen() {
		return
	}

	backlogTasks, err := h.store.ListTasksByStatus(ctx, store.TaskStatusBacklog)
	if err != nil {
		return
	}

	for i := range backlogTasks {
		t := &backlogTasks[i]
		// Skip idea-agent tasks — they are auto-generated stubs, not user tasks.
		if t.Kind == store.TaskKindIdeaAgent {
			continue
		}
		// Skip tasks that already have a completed or running refinement.
		if t.CurrentRefinement != nil {
			continue
		}
		if len(t.RefineSessions) > 0 {
			continue
		}

		logger.Handler.Info("auto-refine: triggering refinement for backlog task", "task", t.ID)

		job := &store.RefinementJob{
			ID:        uuid.New().String(),
			CreatedAt: time.Now(),
			Status:    store.RefinementJobStatusRunning,
			Source:    "auto",
		}
		if err := h.store.StartRefinementJobIfIdle(ctx, t.ID, job); err != nil {
			continue // already running or race — skip silently
		}

		h.insertEventOrLog(ctx, t.ID, store.EventTypeSystem, map[string]string{
			"result": "Auto-refine: triggering refinement agent for backlog task.",
		})
		h.runner.RunRefinementBackground(t.ID, "")
		h.incAutopilotAction("auto_refiner", "refined")
		h.breakers["auto-refine"].recordSuccess()

		// Only trigger one per poll to avoid overwhelming the system.
		return
	}
}
