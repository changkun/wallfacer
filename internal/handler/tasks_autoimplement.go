package handler

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/pkg/gitutil"
	"latere.ai/x/pkg/statemachine"
	"latere.ai/x/pkg/watcher"

	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/review"
	"latere.ai/x/wallfacer/internal/store"
)

// maxConcurrentTasks returns the configured parallel task limit. Per-group
// overrides (Group.MaxParallel in workspace-groups.json) take precedence
// over the env-file default; a 0 override means "unlimited" for that group.
func (h *Handler) maxConcurrentTasks() int {
	if override, ok := h.currentGroupParallelLimit(); ok {
		return override
	}
	return h.cachedMaxParallel.Get()
}

// checkConcurrencyAndUpdateStatus acquires promoteMu, enforces the regular
// in-progress concurrency limit, and calls store.UpdateTaskStatus. It writes
// the appropriate HTTP error response and returns false on any failure;
// on success it returns true with the mutex already released.
func (h *Handler) checkConcurrencyAndUpdateStatus(ctx context.Context, w http.ResponseWriter, id uuid.UUID, newStatus store.TaskStatus) bool {
	promoteMu.Lock()
	defer promoteMu.Unlock()

	regularInProgress := h.countGlobalInProgress()
	if regularInProgress >= h.maxConcurrentTasks() {
		http.Error(w, fmt.Sprintf("max concurrent tasks (%d) reached", h.maxConcurrentTasks()), http.StatusConflict)
		return false
	}
	s, ok := h.requireStore(w)
	if !ok {
		return false
	}
	if err := s.UpdateTaskStatus(ctx, id, newStatus); err != nil {
		if errors.Is(err, statemachine.ErrInvalidTransition) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return false
	}
	return true
}

// promoteMu serializes auto-promotion so two simultaneous state changes
// cannot both promote a task, exceeding the concurrency limit. It is shared
// across tryAutoPromote, tryAutoSubmit, SubmitFeedback,
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

// autoPromoteCandidate holds a backlog task eligible for auto-promotion.
// Populated by Phase 1 and consumed by Phase 2 of the two-phase protocol.
type autoPromoteCandidate struct {
	task  store.Task
	store *store.Store // the store that owns this task
}

// tryAutoPromote checks if there is capacity to run more tasks and promotes
// backlog tasks up to the concurrency limit in a single pass.
// When autoimplement is disabled, no promotion happens.
//
// Concurrency design: two-phase protocol via runTwoPhase.
//
// Phase 1 (no lock): call store.ListTasksByStatus, compute the regular in-progress
// count, and collect all eligible candidates without holding promoteMu, so
// that a concurrent tryAutoPromote call can proceed in parallel.
//
// Phase 2 (under promoteMu): re-count to pick up any state changes that
// happened during Phase 1, re-check capacity, then promote all candidates
// that still fit within the concurrency limit.
func (h *Handler) tryAutoPromote(ctx context.Context) {
	if !h.AutoimplementEnabled() {
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
			// Automation is scoped to the currently viewed workspace group.

			// Check available capacity for backlog promotion (global count).
			regularInProgress := h.countGlobalInProgress()
			availableSlots := h.maxConcurrentTasks() - regularInProgress
			if availableSlots <= 0 {
				h.incAutoimplementAction("auto_promoter", "skipped_capacity")
				return nil, nil
			}

			if availableSlots > 0 {
				type backlogCandidate struct {
					task  store.Task
					store *store.Store
				}
				var backlog []backlogCandidate
				var nextScheduled *time.Time
				h.forCurrentStore(func(s *store.Store, _ []string) {
					backlogTasks, err := s.ListTasksByStatus(ctx, store.TaskStatusBacklog)
					if err != nil {
						return
					}
					for i := range backlogTasks {
						t := &backlogTasks[i]
						if t.ScheduledAt != nil && time.Now().Before(*t.ScheduledAt) {
							h.incAutoimplementAction("auto_promoter", "skipped_scheduled")
							if nextScheduled == nil || t.ScheduledAt.Before(*nextScheduled) {
								nextScheduled = t.ScheduledAt
							}
							continue
						}
						if locked, threadID := h.isTaskLockedByAgent(t.ID.String()); locked {
							h.incAutoimplementAction("auto_promoter", "skipped_locked")
							logger.Handler.Debug("auto-promote: skipping locked task",
								"task", t.ID, "thread", threadID)
							continue
						}
						backlog = append(backlog, backlogCandidate{task: *t, store: s})
					}
				})
				// Arm a precise timer for the soonest scheduled task so it is
				// promoted within milliseconds of its due time rather than waiting
				// for the next 60-second ticker tick.
				if nextScheduled != nil {
					h.ensureScheduledPromoteTrigger(ctx, *nextScheduled)
				}
				if len(backlog) > 0 {
					slices.SortFunc(backlog, func(a, b backlogCandidate) int {
						if c := cmp.Compare(a.task.Position, b.task.Position); c != 0 {
							return c
						}
						return a.task.CreatedAt.Compare(b.task.CreatedAt)
					})
					for _, bc := range backlog {
						if availableSlots <= 0 {
							break
						}
						candidates = append(candidates, autoPromoteCandidate{task: bc.task, store: bc.store})
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
			h.incAutoimplementPhase2Miss("auto_promoter")
		},
		Phase2: func(ctx context.Context, _ *store.Task) (bool, error) {
			// Phase 2 (under promoteMu): process all collected candidates.
			promoted := false

			for _, c := range candidates {
				// Backlog promotion: re-verify capacity with a fresh count (global).
				// Re-read in-progress count each iteration; prior iterations may
				// have promoted tasks, increasing the count.
				freshInProgress := h.countGlobalInProgress()
				if freshInProgress >= h.maxConcurrentTasks() {
					h.incAutoimplementAction("auto_promoter", "skipped_capacity")
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

				// Re-check under the Phase-2 lock: a plan turn may have started
				// between Phase 1 and Phase 2 and pinned this task.
				if locked, threadID := h.isTaskLockedByAgent(c.task.ID.String()); locked {
					h.incAutoimplementAction("auto_promoter", "skipped_locked")
					logger.Handler.Debug("auto-promote Phase 2: skipping locked task",
						"task", c.task.ID, "thread", threadID)
					continue
				}

				logger.Handler.Info("auto-promoting backlog task",
					"task", c.task.ID, "position", c.task.Position,
					"in_progress", freshInProgress)

				if err := c.store.UpdateTaskStatus(ctx, c.task.ID, store.TaskStatusInProgress); err != nil {
					logger.Handler.Error("auto-promote status update", "task", c.task.ID, "error", err)
					h.breakers["auto-promote"].recordFailure(&c.task.ID, err.Error())
					continue
				}
				h.incAutoimplementAction("auto_promoter", "promoted")
				h.insertEventOrLog(ctx, c.task.ID, store.EventTypeStateChange,
					store.NewStateChangeData(store.TaskStatusBacklog, store.TaskStatusInProgress, store.TriggerAutoPromote, nil))
				h.cascadeArchiveThreadsForTask(c.task.ID.String())

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
		h.incAutoimplementAction("auto_retrier", "suppressed_budget")
		return
	}
	if task.AutoRetryCount >= constants.MaxAutoRetries {
		logger.Handler.Info("auto-retry suppressed: global retry cap reached",
			"task", task.ID, "auto_retry_count", task.AutoRetryCount,
			"max", constants.MaxAutoRetries, "category", task.FailureCategory)
		h.incAutoimplementAction("auto_retrier", "suppressed_max_count")
		return
	}
	// For container-crash failures, honor the circuit breaker.
	if task.FailureCategory == store.FailureCategoryContainerCrash && !h.runner.ContainerCircuitAllow() {
		logger.Handler.Warn("auto-retry suppressed: container circuit breaker open",
			"task", task.ID)
		h.incAutoimplementAction("auto_retrier", "suppressed_circuit")
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
	h.incAutoimplementAction("auto_retrier", "retried")
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
// Shared cache design: this watcher and tryAutoSubmit both call
// CommitsBehind for every waiting task on 30-second tickers. h.commitsBehindCache
// deduplicates those calls (TTL=20s) so both watchers share a single
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
		h.incAutoimplementAction("sync_watcher", "synced")
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

// StartAutoSubmitter subscribes to store change notifications and automatically
// moves waiting tasks to done when they are complete (see tryAutoSubmit), not
// behind the default branch tip, and have no unresolved worktree conflicts.
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
	task  store.Task
	store *store.Store
}

// tryAutoSubmit scans all waiting tasks and moves any that are complete, not
// behind the default branch, and free of worktree conflicts directly to done
// (via the commit pipeline if a session exists). A task is complete when the
// review approved it (review enabled and the task reviewable) or, otherwise,
// when its agent ended its turn on its own (stop_reason "end_turn"). Does
// nothing when auto-submit is disabled.
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
			h.incAutoimplementPhase2Miss("auto_submitter")
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
				// Determine eligibility. When review gates the task (review
				// enabled + the task is reviewable), the gate is a clean review
				// verdict; otherwise the agent must have ended its turn on its own.
				var complete bool
				if h.reviewGatesSubmit(t) {
					complete = t.ReviewUnresolved != nil && *t.ReviewUnresolved == 0
				} else {
					complete = t.StopReason != nil && *t.StopReason == "end_turn"
				}
				if !complete {
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
						h.incAutoimplementAction("auto_submitter", "skipped_conflict")
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
					h.incAutoimplementAction("auto_submitter", "skipped_stale_fetch")
					continue
				}

				candidates = append(candidates, autoSubmitCandidate{task: *t, store: allTasks[i].store})
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
				if h.submitAutoSubmitCandidate(ctx, c) {
					submitted = true
				}
			}
			if !h.breakers["auto-submit"].isOpen() {
				h.breakers["auto-submit"].recordSuccess()
			}
			return submitted, nil
		},
	})
}

// submitAutoSubmitCandidate re-reads the candidate's fresh state and, if it is
// still an eligible waiting task with present worktrees, submits it (commit
// pipeline when a session exists, otherwise direct to done). It returns true
// only when it actually submitted.
//
// The re-fetch is essential: Phase 1 runs without promoteMu while doing slow
// git I/O, so a candidate may have left waiting in the meantime (user resume,
// feedback). Acting on the stale Phase-1 snapshot could force a
// no-longer-waiting task to done via ForceUpdateTaskStatus, which bypasses the
// state machine. This mirrors the auto-promote watcher, which also re-reads
// fresh state before acting.
func (h *Handler) submitAutoSubmitCandidate(ctx context.Context, c autoSubmitCandidate) bool {
	ft, err := c.store.GetTask(ctx, c.task.ID)
	if err != nil || ft == nil || ft.Status != store.TaskStatusWaiting {
		return false
	}
	t := *ft
	if len(t.WorktreePaths) == 0 || len(missingTaskWorktrees(&t)) > 0 {
		return false
	}
	logger.Handler.Info("auto-submit: completing waiting task", "task", t.ID)
	h.insertEventOrLog(ctx, t.ID, store.EventTypeSystem, map[string]string{
		"result": "Auto-submit: task complete, up to date, and no conflicts.",
	})
	h.closeFeedbackWaitingSpan(ctx, t.ID)

	if t.SessionID != nil && *t.SessionID != "" {
		if err := c.store.UpdateTaskStatus(ctx, t.ID, store.TaskStatusCommitting); err != nil {
			logger.Handler.Error("auto-submit: update task status", "task", t.ID, "error", err)
			h.breakers["auto-submit"].recordFailure(&t.ID, err.Error())
			return false
		}
		h.insertEventOrLog(ctx, t.ID, store.EventTypeStateChange,
			store.NewStateChangeData(store.TaskStatusWaiting, store.TaskStatusCommitting, store.TriggerAutoSubmit, nil))
		h.runCommitTransition(c.store, t.ID, *t.SessionID, store.TriggerAutoSubmit, "auto-submit: commit failed: ")
	} else {
		// No session: skip the commit pipeline.
		if err := c.store.ForceUpdateTaskStatus(ctx, t.ID, store.TaskStatusDone); err != nil {
			logger.Handler.Error("auto-submit: update task status to done", "task", t.ID, "error", err)
			h.breakers["auto-submit"].recordFailure(&t.ID, err.Error())
			return false
		}
		h.insertEventOrLog(ctx, t.ID, store.EventTypeStateChange,
			store.NewStateChangeData(store.TaskStatusWaiting, store.TaskStatusDone, store.TriggerAutoSubmit, nil))
	}
	h.incAutoimplementAction("auto_submitter", "submitted")
	return true
}

// StartAutoReview subscribes to store change notifications and fires tryAutoReview
// each tick so review verification runs automatically for waiting tasks when the
// toggle is enabled.
func (h *Handler) StartAutoReview(ctx context.Context) {
	watcher.Start(ctx, watcher.Config{
		Wake:        h.newResubscribingWakeSource(),
		Interval:    constants.AutoReviewInterval,
		SettleDelay: constants.WatcherSettleDelay,
		Action:      h.tryAutoReview,
	})
}

// maxConcurrentReview caps how many review verification runs may execute at once
// across all tasks. Review runs are multi-round, multi-agent, and untracked by
// the regular task concurrency limits, so they get their own conservative cap.
const maxConcurrentReview = 2

// beginReview reserves an in-flight slot for an review run on the given task. It
// returns false when the task already has a run in flight or the global
// maxConcurrentReview cap is reached. The dedup is essential: ReviewUnresolved is
// only written when a run finishes, so without this guard a multi-minute run
// would be re-launched on every watcher tick. Callers that get true MUST call
// endReview when the run completes.
func (h *Handler) beginReview(id uuid.UUID) bool {
	h.reviewMu.Lock()
	defer h.reviewMu.Unlock()
	if _, running := h.reviewInFlight[id]; running {
		return false
	}
	if len(h.reviewInFlight) >= maxConcurrentReview {
		return false
	}
	h.reviewInFlight[id] = struct{}{}
	return true
}

// endReview releases the in-flight slot reserved by beginReview.
func (h *Handler) endReview(id uuid.UUID) {
	h.reviewMu.Lock()
	delete(h.reviewInFlight, id)
	h.reviewMu.Unlock()
}

// reviewable reports whether a task has what a review reads: a worktree whose
// diff against the default branch is the change under review. The reviewer
// needs no agent session, so tasks on every harness qualify.
func reviewable(t *store.Task) bool {
	return len(t.WorktreePaths) > 0
}

// reviewGatesSubmit reports whether auto-submit waits on the review verdict
// for this task: review is enabled and the task is reviewable.
func (h *Handler) reviewGatesSubmit(t *store.Task) bool {
	return h.ReviewEnabled() && reviewable(t)
}

// isReviewRunning reports whether a review round is currently executing for
// the task. This is the authoritative "running" signal (the real-time
// in-flight set), independent of on-disk artifacts.
func (h *Handler) isReviewRunning(id uuid.UUID) bool {
	h.reviewMu.Lock()
	defer h.reviewMu.Unlock()
	_, ok := h.reviewInFlight[id]
	return ok
}

// reviewCandidate pairs a waiting task with the store that owns it, captured at
// scan time. The owning store is threaded through to the completion write and
// the feedback delivery so a workspace-group switch during the run cannot
// redirect them to a different store (which would silently drop them).
type reviewCandidate struct {
	task  store.Task
	store *store.Store
}

// tryAutoReview scans the waiting tasks that are reviewable and have no review
// verdict yet (ReviewUnresolved == nil), then fires a goroutine for each that
// is not already running and fits within maxConcurrentReview. Each goroutine
// runs one review round. Does nothing when review is disabled or its circuit
// breaker is open.
func (h *Handler) tryAutoReview(ctx context.Context) {
	if !h.ReviewEnabled() {
		return
	}
	if h.breakers["auto-review"].isOpen() {
		return
	}

	var candidates []reviewCandidate
	h.forCurrentStore(func(s *store.Store, _ []string) {
		for _, t := range s.ListWaitingTasksForReview(ctx) {
			candidates = append(candidates, reviewCandidate{task: t, store: s})
		}
	})

	for _, c := range candidates {
		if !h.beginReview(c.task.ID) {
			continue
		}
		c := c // capture loop variable
		go func() {
			defer h.endReview(c.task.ID)
			if err := h.runReview(ctx, c.store, c.task); err != nil {
				logger.Handler.Warn("review: round failed",
					"task", c.task.ID, "error", err)
			}
		}()
	}
}

// Review session bounds, used when the env file sets none.
//
// reviewMaxRounds is the number of reviewer runs per session: three gives the
// task two turns on requested changes, each re-reviewed, before the session
// ends with what is still open. reviewCostCap is the reviewer token budget per
// session (input plus output tokens of the reviewer runs; the task's own turns
// are held to the task's budget instead). It is checked between rounds, so a
// round already running finishes.
const (
	reviewMaxRounds = 3
	reviewCostCap   = 50000
)

// reviewTuning returns the round limit and the reviewer token budget of a
// review session, applying env overrides over the defaults. A missing or
// non-positive env value keeps the default.
func (h *Handler) reviewTuning() (rounds, costCap int) {
	rounds, costCap = reviewMaxRounds, reviewCostCap
	cfg, err := envconfig.Parse(h.envFile)
	if err != nil {
		return rounds, costCap
	}
	if cfg.ReviewMaxRounds > 0 {
		rounds = cfg.ReviewMaxRounds
	}
	if cfg.ReviewCostCap > 0 {
		costCap = cfg.ReviewCostCap
	}
	return rounds, costCap
}

// primaryWorktree returns the task's worktree path chosen deterministically
// (lexicographically smallest), so a multi-repo run picks the same cwd on
// every tick instead of a random map-iteration entry.
func primaryWorktree(worktreePaths map[string]string) string {
	if len(worktreePaths) == 0 {
		return ""
	}
	return slices.Min(slices.Collect(maps.Values(worktreePaths)))
}

// reviewStateDir returns the .review state directory for a task. It lives
// beside the worktree in the shared per-task dir
// (<worktreesDir>/<taskID>/.review), not inside it, so the review record is
// never staged by `git add -A`, never appears in generateWorktreeDiff, and is
// cleaned up with the task's worktree dir.
func reviewStateDir(worktreePath string) string {
	if worktreePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(worktreePath), ".review")
}

// runReview runs one review round for a task and acts on its outcome, writing
// to the given store (captured at scan time so a group switch mid-run cannot
// redirect the write):
//
//   - feedback: the findings go to the task through the feedback path, the
//     path a person's feedback takes, so it works on every harness; the next
//     round runs when the task waits again;
//   - finished: the verdict is persisted, if the task is still waiting;
//   - skipped: the reason goes on the timeline once, and the verdict stays
//     unset, so a task whose auto-submit waits on the review keeps waiting.
//
// A failed round leaves the verdict unset and opens the auto-review breaker,
// so the retry waits out the breaker's backoff.
func (h *Handler) runReview(ctx context.Context, s *store.Store, t store.Task) error {
	// A worktree that is gone or is not a git repository contributes nothing
	// to the diff, and an approval of the remaining (possibly empty) diff
	// would open the auto-submit gate unreviewed. Skip the round.
	if !reviewable(&t) || len(missingTaskWorktrees(&t)) > 0 {
		return nil
	}
	cwd := primaryWorktree(t.WorktreePaths)
	rounds, costCap := h.reviewTuning()

	res, err := h.verifier.Verify(ctx, review.Input{
		Task:          &t,
		Diff:          generateWorktreeDiff(t.WorktreePaths),
		StateDir:      reviewStateDir(cwd),
		MaxRounds:     rounds,
		CostCapTokens: costCap,
		// Surface the round on the timeline as it starts: a reviewer takes a
		// while, and without this a manual or auto trigger looks inert.
		OnStart: func(round, maxRounds int) {
			h.insertEventOrLogTo(ctx, s, t.ID, store.EventTypeSystem, map[string]string{
				"result": fmt.Sprintf("Review: round %d of at most %d started.", round, maxRounds),
			})
		},
	})
	if err != nil {
		h.breakers["auto-review"].recordFailure(&t.ID, err.Error())
		h.insertEventOrLogTo(ctx, s, t.ID, store.EventTypeSystem, map[string]string{
			"result": review.FailureMessage(err),
		})
		return err
	}
	h.breakers["auto-review"].recordSuccess()

	switch res.Outcome {
	case review.OutcomeSkipped:
		if res.SkipRecorded {
			h.insertEventOrLogTo(ctx, s, t.ID, store.EventTypeSystem, map[string]string{
				"result": res.Skip.Message,
			})
		}
		return nil
	case review.OutcomeFeedback:
		if err := h.sendReviewFeedback(ctx, s, t.ID, res); err != nil {
			h.breakers["auto-review"].recordFailure(&t.ID, err.Error())
			return err
		}
		return nil
	}

	// Only persist if the task is still waiting: a round that completes after
	// the task was resumed, submitted, or failed would otherwise stamp a stale
	// result onto a task whose worktree has already moved on.
	ft, err := s.GetTask(ctx, t.ID)
	if err != nil {
		return err
	}
	if ft == nil || ft.Status != store.TaskStatusWaiting {
		return nil
	}
	if err := s.UpdateTaskReview(ctx, t.ID, res.Unresolved, res.Headline, res.SessionDir); err != nil {
		return err
	}

	// Verdict gate. An approval lets autoimplement proceed (auto-submit checks
	// ReviewUnresolved == 0). Open findings at the end of a session are a hard
	// barrier: the task stays parked in waiting, and clearing it is a human
	// act: confirm the work, or resume with feedback, which clears the verdict
	// and starts a new review session when the task waits again.
	h.insertEventOrLogTo(ctx, s, t.ID, store.EventTypeSystem, map[string]string{
		"result": reviewFinishedMessage(res),
	})
	return nil
}

// reviewFinishedMessage is the timeline sentence for a finished session.
func reviewFinishedMessage(res *review.Result) string {
	switch {
	case res.Unresolved == 0:
		return "Review: approved, no open findings."
	case res.Termination == review.TerminationCostCap:
		return fmt.Sprintf("Review: the reviewer token budget is spent with %d open finding(s); the task is halted for review. Confirm the work, or resume it with feedback to review again.", res.Unresolved)
	default:
		return fmt.Sprintf("Review: %d open finding(s) after the last round; the task is halted for review. Confirm the work, or resume it with feedback to review again.", res.Unresolved)
	}
}

// sendReviewFeedback delivers a round's findings to the task through the
// feedback path. It holds promoteMu, the lock every waiting-task transition
// takes, and re-checks that the task is still waiting: one that moved on while
// the reviewer ran has a different diff now, so the session is ended as
// superseded instead and the next review starts afresh.
func (h *Handler) sendReviewFeedback(ctx context.Context, s *store.Store, taskID uuid.UUID, res *review.Result) error {
	promoteMu.Lock()
	defer promoteMu.Unlock()

	ft, err := s.GetTask(ctx, taskID)
	if err != nil || ft == nil || ft.Status != store.TaskStatusWaiting {
		if serr := review.Supersede(res.SessionDir, time.Now()); serr != nil {
			logger.Handler.Warn("review: end superseded session", "task", taskID, "error", serr)
		}
		return err
	}
	note := fmt.Sprintf("Review: round %d requested changes with %d finding(s); the findings were sent to the task as feedback.", res.Round, len(res.Findings))
	if err := h.resumeWaitingTaskWithFeedbackOnStoreLocked(ctx, s, ft, res.Feedback, store.TriggerAutoReview, note); err != nil {
		if serr := review.Supersede(res.SessionDir, time.Now()); serr != nil {
			logger.Handler.Warn("review: end superseded session", "task", taskID, "error", serr)
		}
		return err
	}
	return nil
}
