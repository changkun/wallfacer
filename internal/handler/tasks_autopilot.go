package handler

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/gitutil"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/statemachine"
	"changkun.de/x/wallfacer/internal/pkg/watcher"
	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// maxConcurrentTasks returns the configured parallel task limit. The value is
// lazily cached and only re-parsed after UpdateEnvConfig calls Invalidate.
func (h *Handler) maxConcurrentTasks() int {
	return h.cachedMaxParallel.Get()
}

// maxTestConcurrentTasks returns the configured parallel test-run limit.
func (h *Handler) maxTestConcurrentTasks() int {
	return h.cachedMaxTestParallel.Get()
}

// countRegularInProgress returns the number of non-test in-progress tasks
// in the currently viewed workspace group. The context parameter is unused
// but matches the calling convention for store query methods.
func (h *Handler) countRegularInProgress(_ context.Context) (int, error) {
	return h.countGlobalInProgress(), nil
}

// countRegularInProgress counts non-test in-progress tasks from a task slice.
// Used in Phase 1 of auto-promotion where a snapshot is already available.
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
		if errors.Is(err, statemachine.ErrInvalidTransition) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return false
	}
	return true
}

// promoteMu serialises auto-promotion so two simultaneous state changes
// cannot both promote a task, exceeding the concurrency limit. It is shared
// across tryAutoPromote, tryAutoTest, tryAutoSubmit, SubmitFeedback,
// ResumeTask, SyncTask, and checkAndSyncWaitingTasks — any code path that
// transitions a task to in_progress or committing.
var promoteMu sync.Mutex

// StartAutoPromoter subscribes to store change notifications and automatically
// promotes backlog tasks to in_progress when there are fewer than
// maxConcurrentTasks running. A supplementary 60-second ticker fires
// periodically so that scheduled tasks are promoted even when no other
// state change occurs. Additionally, ensureScheduledPromoteTrigger arms a
// precise one-shot timer for the soonest scheduled task so promotion happens
// within milliseconds of the due time rather than waiting up to 60 seconds.
func (h *Handler) StartAutoPromoter(ctx context.Context) {
	watcher.Start(ctx, watcher.Config{
		Wake:     h.newResubscribingWakeSource(),
		Interval: constants.AutoPromoteInterval,
		Action:   h.tryAutoPromote,
		Shutdown: func() {
			h.scheduledPromoteMu.Lock()
			if h.scheduledPromoteTimer != nil {
				h.scheduledPromoteTimer.Stop()
			}
			h.scheduledPromoteMu.Unlock()
		},
	})
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
	retryAll := func(ctx context.Context) {
		h.forCurrentStore(func(s *store.Store, _ []string) {
			failed, _ := s.ListTasksByStatus(ctx, store.TaskStatusFailed)
			for _, t := range failed {
				h.tryAutoRetry(ctx, s, t)
			}
		})
	}
	watcher.Start(ctx, watcher.Config{
		Wake:   h.newResubscribingWakeSource(),
		Init:   retryAll,
		Action: retryAll,
	})
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

// autoPromoteCandidate holds a task eligible for auto-promotion and whether it
// is a resume (waiting task with failed-test feedback) vs a fresh backlog promote.
// Populated by Phase 1 and consumed by Phase 2 of the two-phase protocol.
type autoPromoteCandidate struct {
	task     store.Task
	store    *store.Store // the store that owns this task
	isResume bool
	feedback string
}

// tryAutoPromote checks if there is capacity to run more tasks and promotes
// backlog tasks up to the concurrency limit in a single pass.
// When autopilot is disabled, no promotion happens.
//
// Concurrency design: two-phase protocol via runTwoPhase.
//
// Phase 1 (no lock): call store.ListTasksByStatus, compute the regular in-progress
// count, and collect all eligible candidates. AreDependenciesSatisfied may do
// disk I/O here; we must not hold promoteMu during these potentially slow
// operations so that a concurrent tryAutoPromote call (or tryAutoTest) can
// proceed in parallel.
//
// Phase 2 (under promoteMu): re-count to pick up any state changes that
// happened during Phase 1, re-check capacity, then promote all candidates
// that still fit within the concurrency limit.
func (h *Handler) tryAutoPromote(ctx context.Context) {
	if !h.AutopilotEnabled() {
		return
	}
	if h.breakers["auto-promote"].isOpen() {
		return
	}

	// candidates is populated by Phase1 and consumed by Phase2 via closure.
	var candidates []autoPromoteCandidate

	runTwoPhase(ctx, &promoteMu, TwoPhaseWatcherConfig{
		Name: "auto-promote",
		OnPhase1Error: func(err error) {
			h.breakers["auto-promote"].recordFailure(nil, err.Error())
		},
		Phase1: func(ctx context.Context) (*store.Task, error) {
			// Phase 1 (no lock): build candidate list without holding promoteMu.
			// Scan ALL active stores for eligible tasks.

			// Check for auto-resume candidates first (waiting tasks with failed test feedback).
			// Automation is scoped to the currently viewed workspace group.
			h.forCurrentStore(func(s *store.Store, _ []string) {
				waitingTasks, err := s.ListTasksByStatus(ctx, store.TaskStatusWaiting)
				if err != nil {
					return
				}
				for i := range waitingTasks {
					t := &waitingTasks[i]
					if t.IsTestRun || t.PendingTestFeedback == "" || t.LastTestResult != "fail" {
						continue
					}
					if t.SessionID == nil || *t.SessionID == "" {
						continue
					}
					if t.TestFailCount >= constants.MaxTestFailRetries {
						logger.Handler.Info("auto-promote: skipping task — test fail cap reached",
							"task", t.ID, "test_fail_count", t.TestFailCount, "max", constants.MaxTestFailRetries)
						continue
					}
					candidates = append(candidates, autoPromoteCandidate{
						task:     *t,
						store:    s,
						isResume: true,
						feedback: t.PendingTestFeedback,
					})
				}
			})

			// Check available capacity for backlog promotion (global count).
			regularInProgress := h.countGlobalInProgress()
			availableSlots := h.maxConcurrentTasks() - regularInProgress
			if availableSlots <= 0 && len(candidates) == 0 {
				h.incAutopilotAction("auto_promoter", "skipped_capacity")
				return nil, nil
			}

			if availableSlots > 0 {
				type cpCandidate struct {
					task  store.Task
					store *store.Store
					score int
				}
				var cpCandidates []cpCandidate
				var nextScheduled *time.Time
				h.forCurrentStore(func(s *store.Store, _ []string) {
					backlogTasks, err := s.ListTasksByStatus(ctx, store.TaskStatusBacklog)
					if err != nil {
						return
					}
					for i := range backlogTasks {
						t := &backlogTasks[i]
						// Skip task-kinds that the auto-promoter must not touch:
						// idea-agent runs are scheduled by their own watcher, and
						// routine cards are schedule templates driven by the
						// routine engine rather than the board lifecycle.
						if t.IsIdeaAgent() || t.IsRoutine() {
							continue
						}
						if t.ScheduledAt != nil && time.Now().Before(*t.ScheduledAt) {
							h.incAutopilotAction("auto_promoter", "skipped_scheduled")
							if nextScheduled == nil || t.ScheduledAt.Before(*nextScheduled) {
								nextScheduled = t.ScheduledAt
							}
							continue
						}
						satisfied, err := s.AreDependenciesSatisfied(ctx, t.ID)
						if err != nil || !satisfied {
							h.incAutopilotAction("auto_promoter", "skipped_dependency")
							continue
						}
						cpCandidates = append(cpCandidates, cpCandidate{task: *t, store: s, score: s.CriticalPathScore(t.ID)})
					}
				})
				// Arm a precise timer for the soonest scheduled task so it is
				// promoted within milliseconds of its due time rather than waiting
				// for the next 60-second ticker tick.
				if nextScheduled != nil {
					h.ensureScheduledPromoteTrigger(ctx, *nextScheduled)
				}
				if len(cpCandidates) > 0 {
					slices.SortFunc(cpCandidates, func(a, b cpCandidate) int {
						if c := cmp.Compare(b.score, a.score); c != 0 {
							return c
						}
						if c := cmp.Compare(a.task.Position, b.task.Position); c != 0 {
							return c
						}
						return a.task.CreatedAt.Compare(b.task.CreatedAt)
					})
					for _, cp := range cpCandidates {
						if availableSlots <= 0 {
							break
						}
						candidates = append(candidates, autoPromoteCandidate{task: cp.task, store: cp.store})
						availableSlots--
					}
				}
			}

			if len(candidates) == 0 {
				return nil, nil
			}
			// Return first candidate as signal that there is at least one eligible task.
			return &candidates[0].task, nil
		},
		AfterPhase1: h.testPhase1Done,
		OnPhase2Miss: func(_ *store.Task) {
			h.incAutopilotPhase2Miss("auto_promoter")
		},
		Phase2: func(ctx context.Context, _ *store.Task) (bool, error) {
			// Phase 2 (under promoteMu): process all collected candidates.
			promoted := false

			for _, c := range candidates {
				if c.isResume {
					// Auto-resume: re-verify eligibility with fresh state.
					freshTask, err := c.store.GetTask(ctx, c.task.ID)
					if err != nil || freshTask == nil {
						continue
					}
					if freshTask.Status != store.TaskStatusWaiting || freshTask.IsTestRun || freshTask.LastTestResult != "fail" || freshTask.PendingTestFeedback == "" {
						continue
					}
					if freshTask.SessionID == nil || *freshTask.SessionID == "" {
						continue
					}
					if freshTask.TestFailCount >= constants.MaxTestFailRetries {
						logger.Handler.Info("auto-promote: test fail cap reached, stopping auto-resume",
							"task", freshTask.ID, "test_fail_count", freshTask.TestFailCount)
						h.insertEventOrLog(ctx, freshTask.ID, store.EventTypeSystem, map[string]string{
							"result": fmt.Sprintf("Auto-resume halted: %d consecutive test failures (cap: %d). Manual feedback required to continue.", freshTask.TestFailCount, constants.MaxTestFailRetries),
						})
						continue
					}

					logger.Handler.Info("auto-promote: resuming waiting task from failed test feedback",
						"task", freshTask.ID)
					if err := h.resumeWaitingTaskWithFeedbackLocked(ctx, freshTask, freshTask.PendingTestFeedback, store.TriggerFeedback, "Autopilot: resuming task with failed test feedback."); err != nil {
						logger.Handler.Error("auto-promote resume failed test feedback", "task", freshTask.ID, "error", err)
						h.breakers["auto-promote"].recordFailure(&freshTask.ID, err.Error())
						continue
					}
					h.incAutopilotAction("auto_promoter", "resumed_failed_test")
					h.breakers["auto-promote"].recordSuccess()
					promoted = true
					continue
				}

				// Backlog promotion: re-verify capacity with a fresh count (global).
				// Re-read in-progress count each iteration; prior iterations may
				// have promoted tasks, increasing the count.
				freshInProgress := h.countGlobalInProgress()
				if freshInProgress >= h.maxConcurrentTasks() {
					h.incAutopilotAction("auto_promoter", "skipped_capacity")
					break
				}

				// Abort promotion when the container runtime is known-unavailable.
				// Without this guard, slot openings caused by failures would trigger
				// back-to-back promotions that all immediately fail, cascading across
				// every backlog task.
				if !h.runner.ContainerCircuitAllow() {
					logger.Handler.Warn("auto-promote skipped: container circuit breaker open")
					break
				}

				logger.Handler.Info("auto-promoting backlog task",
					"task", c.task.ID, "position", c.task.Position,
					"in_progress", freshInProgress)

				if err := c.store.UpdateTaskStatus(ctx, c.task.ID, store.TaskStatusInProgress); err != nil {
					logger.Handler.Error("auto-promote status update", "task", c.task.ID, "error", err)
					h.breakers["auto-promote"].recordFailure(&c.task.ID, err.Error())
					continue
				}
				h.incAutopilotAction("auto_promoter", "promoted")
				h.insertEventOrLog(ctx, c.task.ID, store.EventTypeStateChange,
					store.NewStateChangeData(store.TaskStatusBacklog, store.TaskStatusInProgress, store.TriggerAutoPromote, nil))

				sessionID := ""
				if !c.task.FreshStart && c.task.SessionID != nil {
					sessionID = *c.task.SessionID
				}
				h.runner.RunBackground(c.task.ID, c.task.Prompt, sessionID, false)
				h.breakers["auto-promote"].recordSuccess()
				promoted = true
			}

			return promoted, nil
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
func (h *Handler) tryAutoRetry(ctx context.Context, s *store.Store, task store.Task) {
	if task.Status != store.TaskStatusFailed {
		return
	}
	if !retryableCategories[task.FailureCategory] {
		return
	}
	if h.breakers["auto-retry"].isOpen() {
		return
	}
	if task.AutoRetryBudget[task.FailureCategory] <= 0 {
		logger.Handler.Info("auto-retry suppressed: category budget exhausted",
			"task", task.ID, "auto_retry_count", task.AutoRetryCount,
			"category", task.FailureCategory)
		h.incAutopilotAction("auto_retrier", "suppressed_budget")
		return
	}
	if task.AutoRetryCount >= constants.MaxAutoRetries {
		logger.Handler.Info("auto-retry suppressed: global retry cap reached",
			"task", task.ID, "auto_retry_count", task.AutoRetryCount,
			"max", constants.MaxAutoRetries, "category", task.FailureCategory)
		h.incAutopilotAction("auto_retrier", "suppressed_max_count")
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
	if err := s.ResetTaskForRetry(ctx, task.ID, task.Prompt, false); err != nil {
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

// StartWaitingSyncWatcher starts a background goroutine that periodically
// checks all waiting tasks and automatically syncs any whose worktrees have
// fallen behind the default branch.
func (h *Handler) StartWaitingSyncWatcher(ctx context.Context) {
	watcher.Start(ctx, watcher.Config{
		Interval: constants.WaitingSyncInterval,
		Action:   h.checkAndSyncWaitingTasks,
	})
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

	type syncCandidate struct {
		task  store.Task
		store *store.Store
	}
	var syncCandidates []syncCandidate

	h.forCurrentStore(func(s *store.Store, _ []string) {
		tasks, err := s.ListTasksByStatus(ctx, store.TaskStatusWaiting)
		if err != nil {
			return
		}
		for i := range tasks {
			t := &tasks[i]
			if len(t.WorktreePaths) == 0 {
				continue
			}

			behind := false
			fetchFailed := false
			for repoPath, worktreePath := range t.WorktreePaths {
				if !gitutil.IsGitRepo(repoPath) || !gitutil.HasOriginRemote(repoPath) {
					continue
				}
				if _, err := os.Stat(worktreePath); err != nil {
					continue
				}
				if !gitutil.IsGitRepo(worktreePath) {
					continue
				}
				if fetchErr := gitutil.FetchOrigin(repoPath); fetchErr != nil {
					logger.Handler.Warn("auto-sync: git fetch failed; skipping CommitsBehind for this task",
						"task", t.ID, "repo", repoPath, "error", fetchErr)
					if recErr := s.RecordFetchFailure(ctx, t.ID, fetchErr.Error()); recErr != nil {
						logger.Handler.Warn("auto-sync: record fetch failure",
							"task", t.ID, "error", recErr)
					}
					fetchFailed = true
					break
				}
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
			if fetchFailed {
				continue
			}
			if err := s.ClearFetchFailure(ctx, t.ID); err != nil {
				logger.Handler.Warn("auto-sync: clear fetch failure", "task", t.ID, "error", err)
			}
			if behind {
				syncCandidates = append(syncCandidates, syncCandidate{task: *t, store: s})
			}
		}
	})

	for _, sc := range syncCandidates {
		t := &sc.task
		logger.Handler.Info("auto-sync: waiting task behind default branch, syncing",
			"task", t.ID)

		promoteMu.Lock()
		freshTask, freshErr := sc.store.GetTask(ctx, t.ID)
		if freshErr != nil || freshTask == nil || freshTask.Status != store.TaskStatusWaiting {
			promoteMu.Unlock()
			continue
		}
		if err := sc.store.UpdateTaskStatus(ctx, t.ID, store.TaskStatusInProgress); err != nil {
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

// StartAutoTester subscribes to store change notifications and automatically
// triggers the test agent for waiting tasks that are untested and not behind
// the default branch tip.
func (h *Handler) StartAutoTester(ctx context.Context) {
	watcher.Start(ctx, watcher.Config{
		Wake:        h.newResubscribingWakeSource(),
		Interval:    constants.AutoTestInterval,
		SettleDelay: constants.WatcherSettleDelay,
		Action:      h.tryAutoTest,
	})
}

// autoTestCandidate holds an eligible waiting task and its pre-built test prompt.
type autoTestCandidate struct {
	task       store.Task
	store      *store.Store
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
			// Phase 1 (no lock): build the list of eligible candidates scoped
			// to the currently viewed workspace group. Git I/O (CommitsBehind)
			// happens here so we don't hold promoteMu during potentially slow
			// filesystem operations.
			h.forCurrentStore(func(s *store.Store, _ []string) {
				waitingTasks, err := s.ListTasksByStatus(ctx, store.TaskStatusWaiting)
				if err != nil {
					return
				}

				for i := range waitingTasks {
					t := &waitingTasks[i]
					if t.LastTestResult != "" || t.IsTestRun {
						continue
					}
					if len(t.WorktreePaths) == 0 {
						continue
					}
					if len(missingTaskWorktrees(t)) > 0 {
						continue
					}

					behind := false
					for repoPath, worktreePath := range t.WorktreePaths {
						if !gitutil.IsGitRepo(repoPath) || !gitutil.HasOriginRemote(repoPath) {
							continue
						}
						n, err := h.commitsBehindCache.cachedCommitsBehind(repoPath, worktreePath)
						if err != nil {
							logger.Handler.Warn("auto-test: check commits behind",
								"task", t.ID, "repo", repoPath, "error", err)
							behind = true
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
					candidates = append(candidates, autoTestCandidate{task: *t, store: s, testPrompt: testPrompt})
				}
			})

			if len(candidates) == 0 {
				return nil, nil
			}
			first := &candidates[0].task
			return first, nil
		},
		Phase2: func(ctx context.Context, _ *store.Task) (bool, error) {
			// Phase 2 (under promoteMu): enforce the concurrency limit and trigger.
			// Sharing promoteMu with tryAutoPromote prevents the two from racing to
			// exceed maxConcurrentTasks simultaneously.

			// Count test runners across ALL active stores.
			testInProgress := h.countGlobalTestsInProgress(ctx)

			maxTestTasks := h.maxTestConcurrentTasks()
			triggered := false

			for _, c := range candidates {
				if testInProgress >= maxTestTasks {
					logger.Handler.Info("auto-test: test concurrency limit reached, deferring remaining tests",
						"limit", maxTestTasks)
					h.incAutopilotAction("auto_tester", "skipped_capacity")
					break
				}

				// Re-verify eligibility using fresh state from the task's store.
				ft, err := c.store.GetTask(ctx, c.task.ID)
				if err != nil || ft == nil || ft.Status != store.TaskStatusWaiting || ft.LastTestResult != "" || ft.IsTestRun {
					continue
				}
				if len(ft.WorktreePaths) == 0 || len(missingTaskWorktrees(ft)) > 0 {
					continue
				}

				logger.Handler.Info("auto-test: triggering test agent for waiting task", "task", c.task.ID)

				if err := c.store.UpdateTaskTestRun(ctx, c.task.ID, true, ""); err != nil {
					logger.Handler.Error("auto-test: update test run flag", "task", c.task.ID, "error", err)
					h.breakers["auto-test"].recordFailure(&c.task.ID, err.Error())
					continue
				}
				if err := c.store.UpdateTaskStatus(ctx, c.task.ID, store.TaskStatusInProgress); err != nil {
					logger.Handler.Error("auto-test: update task status", "task", c.task.ID, "error", err)
					h.breakers["auto-test"].recordFailure(&c.task.ID, err.Error())
					if rbErr := c.store.UpdateTaskTestRun(ctx, c.task.ID, false, ""); rbErr != nil {
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

// StartAutoSubmitter subscribes to store change notifications and automatically
// moves waiting tasks to done when they are verified (LastTestResult == "pass"),
// not behind the default branch tip, and have no unresolved worktree conflicts.
func (h *Handler) StartAutoSubmitter(ctx context.Context) {
	watcher.Start(ctx, watcher.Config{
		Wake:        h.newResubscribingWakeSource(),
		Interval:    constants.AutoSubmitInterval,
		SettleDelay: constants.WatcherSettleDelay,
		Action:      h.tryAutoSubmit,
	})
}

// autoSubmitCandidate holds a waiting task that has passed all eligibility checks
// and is ready for auto-submission.
type autoSubmitCandidate struct {
	task              store.Task
	store             *store.Store
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
			// Scan the currently viewed workspace group's store for
			// auto-submit candidates. Other groups are left alone so their
			// in-flight tasks finish without triggering new automation.
			var allTasks []struct {
				task  store.Task
				store *store.Store
			}
			h.forCurrentStore(func(s *store.Store, _ []string) {
				tasks, err := s.ListTasks(ctx, false)
				if err != nil {
					return
				}
				for i := range tasks {
					allTasks = append(allTasks, struct {
						task  store.Task
						store *store.Store
					}{task: tasks[i], store: s})
				}
			})

			for i := range allTasks {
				t := &allTasks[i].task
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
				hasRemoteRepo := false
				for repoPath, worktreePath := range t.WorktreePaths {
					if !gitutil.IsGitRepo(repoPath) || !gitutil.HasOriginRemote(repoPath) {
						// Non-git workspace or local-only repo: no remote to be behind, no conflicts.
						continue
					}
					hasRemoteRepo = true
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

				// Only apply the stale-fetch guard when the task has repos
				// with remotes. Local-only and non-git repos can never be
				// behind and don't need fetch protection.
				if hasRemoteRepo && t.LastFetchErrorAt != nil && time.Since(*t.LastFetchErrorAt) < constants.FetchErrorGracePeriod {
					h.incAutopilotAction("auto_submitter", "skipped_stale_fetch")
					continue
				}

				candidates = append(candidates, autoSubmitCandidate{task: *t, store: allTasks[i].store, naturallyComplete: naturallyComplete})
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
					if err := c.store.UpdateTaskStatus(ctx, t.ID, store.TaskStatusCommitting); err != nil {
						logger.Handler.Error("auto-submit: update task status", "task", t.ID, "error", err)
						h.breakers["auto-submit"].recordFailure(&t.ID, err.Error())
						continue
					}
					h.insertEventOrLog(ctx, t.ID, store.EventTypeStateChange,
						store.NewStateChangeData(store.TaskStatusWaiting, store.TaskStatusCommitting, store.TriggerAutoSubmit, nil))
					h.runCommitTransition(t.ID, *t.SessionID, store.TriggerAutoSubmit, "auto-submit: commit failed: ")
				} else {
					// No session — skip commit pipeline. Stop the worker directly.
					h.runner.StopTaskWorker(t.ID)
					if err := c.store.ForceUpdateTaskStatus(ctx, t.ID, store.TaskStatusDone); err != nil {
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

// StartAutoRefiner subscribes to store change notifications and automatically
// triggers the refinement agent for backlog tasks that have not yet been refined.
func (h *Handler) StartAutoRefiner(ctx context.Context) {
	watcher.Start(ctx, watcher.Config{
		Wake:     h.newResubscribingWakeSource(),
		Interval: constants.AutoRefineInterval,
		Action:   h.tryAutoRefine,
	})
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

	// Scan the currently viewed workspace group's store for backlog tasks
	// needing refinement. Only trigger one per poll to avoid overwhelming
	// the system.
	refined := false
	h.forCurrentStore(func(s *store.Store, _ []string) {
		if refined {
			return
		}
		backlogTasks, err := s.ListTasksByStatus(ctx, store.TaskStatusBacklog)
		if err != nil {
			return
		}

		for i := range backlogTasks {
			t := &backlogTasks[i]
			// Auto-refine targets human-authored prompts only. Idea-agent
			// tasks already have an agent-authored prompt; routine cards are
			// templates, not executable work.
			if t.IsIdeaAgent() || t.IsRoutine() {
				continue
			}
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
			if err := s.StartRefinementJobIfIdle(ctx, t.ID, job); err != nil {
				continue
			}

			h.insertEventOrLog(ctx, t.ID, store.EventTypeSystem, map[string]string{
				"result": "Auto-refine: triggering refinement agent for backlog task.",
			})
			h.runner.RunRefinementBackground(t.ID, "")
			h.incAutopilotAction("auto_refiner", "refined")
			h.breakers["auto-refine"].recordSuccess()
			refined = true
			return
		}
	})
}
