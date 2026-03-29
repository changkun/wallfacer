package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/sandbox"
	"github.com/google/uuid"
)

// UpdateTaskStatus transitions the task identified by id to the given status.
func (s *Store) UpdateTaskStatus(_ context.Context, id uuid.UUID, status TaskStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if err := ValidateTransition(t.Status, status); err != nil {
		return err
	}
	s.removeFromStatusIndex(t.Status, id)
	t.Status = status
	s.addToStatusIndex(t.Status, id)
	if status == TaskStatusInProgress && t.StartedAt == nil {
		now := time.Now()
		t.StartedAt = &now
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	if status == TaskStatusDone {
		s.buildAndSaveSummary(*t)
	}
	// Search index not updated: status is not a search-indexed field
	// (title, prompt, tags, oversight).
	s.notify(t, false)
	if status == TaskStatusDone || status == TaskStatusFailed || status == TaskStatusCancelled {
		// Terminal state reached: compact event trace files in the background.
		// Capture the highest sequence number from in-memory state while we
		// still hold the store lock, so the goroutine only compacts events
		// that belong to the session that just finished. This bounded
		// compaction prevents a race where an immediate retry would have
		// new-session events bundled into the previous session's compact file.
		maxSeq := int64(s.nextSeq[id] - 1)
		s.compactWg.Add(1)
		go func(taskID uuid.UUID, maxSeq int64) {
			defer s.compactWg.Done()
			if err := s.compactTaskEvents(taskID, maxSeq); err != nil {
				logger.Store.Error("failed to compact task traces", "task", taskID, "error", err)
			}
		}(id, maxSeq)
	}
	return nil
}

// buildAndSaveSummary constructs a TaskSummary from the in-memory task and
// persists it to data/<uuid>/summary.json atomically. It is called while
// s.mu is held for writing so the file is present before any subscriber is
// notified of the done transition. GetOversight reads directly from disk and
// does not acquire s.mu, so it is safe to call here.
func (s *Store) buildAndSaveSummary(task Task) {
	oversight, _ := s.GetOversight(task.ID)
	phaseCount := 0
	if oversight != nil {
		phaseCount = len(oversight.Phases)
	}

	duration := task.UpdatedAt.Sub(task.CreatedAt).Seconds()
	execDuration := duration // fallback: same as wall-clock if no StartedAt
	if task.StartedAt != nil {
		execDuration = task.UpdatedAt.Sub(*task.StartedAt).Seconds()
	}

	summary := TaskSummary{
		TaskID:                   task.ID,
		Title:                    task.Title,
		Status:                   task.Status,
		CompletedAt:              task.UpdatedAt,
		CreatedAt:                task.CreatedAt,
		DurationSeconds:          duration,
		ExecutionDurationSeconds: execDuration,
		TotalTurns:               task.Turns,
		TotalCostUSD:             task.Usage.CostUSD,
		ByActivity:               task.UsageBreakdown,
		TestResult:               task.LastTestResult,
		PhaseCount:               phaseCount,
		FailureCategory:          task.FailureCategory,
	}

	if err := s.SaveSummary(task.ID, summary); err != nil {
		logger.Store.Warn("failed to save task summary", "task", task.ID, "error", err)
	}
}

// ForceUpdateTaskStatus sets a task's status field without validating the
// transition. Use this only for server recovery paths that must succeed
// regardless of current state, and for test fixtures that need arbitrary
// initial states. Prefer UpdateTaskStatus for all normal code paths.
//
// Like UpdateTaskStatus, it writes summary.json atomically before notifying
// subscribers when transitioning to TaskStatusDone.
func (s *Store) ForceUpdateTaskStatus(_ context.Context, id uuid.UUID, status TaskStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	s.removeFromStatusIndex(t.Status, id)
	t.Status = status
	s.addToStatusIndex(t.Status, id)
	if status == TaskStatusInProgress && t.StartedAt == nil {
		now := time.Now()
		t.StartedAt = &now
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	if status == TaskStatusDone {
		s.buildAndSaveSummary(*t)
	}
	// Search index not updated: status is not a search-indexed field
	// (title, prompt, tags, oversight).
	s.notify(t, false)
	return nil
}

// UpdateTaskTitle sets a task's display title.
func (s *Store) UpdateTaskTitle(_ context.Context, id uuid.UUID, title string) error {
	// Compute the lowercased title before acquiring the lock so that the
	// strings.ToLower call (potentially non-trivial for long titles) does not
	// extend the critical section.
	loweredTitle := strings.ToLower(title)
	return s.mutateTask(id, func(t *Task) error {
		t.Title = title
		if entry, ok := s.searchIndex[id]; ok {
			entry.title = loweredTitle
			s.searchIndex[id] = entry
		}
		return nil
	})
}

// UpdateTaskExecutionPrompt sets the full execution prompt used at runtime.
// When non-empty, the runner passes ExecutionPrompt to the sandbox instead of
// Prompt, so Prompt can be kept as a short human-readable card label.
func (s *Store) UpdateTaskExecutionPrompt(_ context.Context, id uuid.UUID, executionPrompt string) error {
	return s.mutateTask(id, func(t *Task) error {
		t.ExecutionPrompt = executionPrompt
		return nil
	})
}

// UpdateTaskTurns updates only the turn counter for a task, leaving all other
// fields (Result, SessionID, StopReason) unchanged. Used during test runs so
// that the implementation agent's output is not overwritten.
func (s *Store) UpdateTaskTurns(_ context.Context, id uuid.UUID, turns int) error {
	return s.mutateTask(id, func(t *Task) error {
		t.Turns = turns
		return nil
	})
}

// UpdateTaskResult stores the final output, session ID, stop reason, and turn count.
func (s *Store) UpdateTaskResult(_ context.Context, id uuid.UUID, result, sessionID, stopReason string, turns int) error {
	return s.mutateTask(id, func(t *Task) error {
		t.Result = &result
		t.SessionID = &sessionID
		t.StopReason = &stopReason
		t.Turns = turns
		return nil
	})
}

// AccumulateSubAgentUsage adds token/cost deltas to the task's running totals
// and records the contribution under the named sub-agent in UsageBreakdown.
// agent should be one of the SandboxActivity constants.
func (s *Store) AccumulateSubAgentUsage(_ context.Context, id uuid.UUID, agent SandboxActivity, delta TaskUsage) error {
	return s.mutateTask(id, func(t *Task) error {
		t.Usage.InputTokens += delta.InputTokens
		t.Usage.OutputTokens += delta.OutputTokens
		t.Usage.CacheReadInputTokens += delta.CacheReadInputTokens
		t.Usage.CacheCreationTokens += delta.CacheCreationTokens
		t.Usage.CostUSD += delta.CostUSD
		if t.UsageBreakdown == nil {
			t.UsageBreakdown = make(map[SandboxActivity]TaskUsage)
		}
		prev := t.UsageBreakdown[agent]
		prev.InputTokens += delta.InputTokens
		prev.OutputTokens += delta.OutputTokens
		prev.CacheReadInputTokens += delta.CacheReadInputTokens
		prev.CacheCreationTokens += delta.CacheCreationTokens
		prev.CostUSD += delta.CostUSD
		t.UsageBreakdown[agent] = prev
		return nil
	})
}

// AccumulateTaskUsage is a convenience wrapper that accumulates usage without
// attributing it to a specific sub-agent. Prefer AccumulateSubAgentUsage.
func (s *Store) AccumulateTaskUsage(ctx context.Context, id uuid.UUID, delta TaskUsage) error {
	return s.AccumulateSubAgentUsage(ctx, id, SandboxActivityImplementation, delta)
}

// UpdateTaskPosition updates the task board column sort position.
func (s *Store) UpdateTaskPosition(_ context.Context, id uuid.UUID, position int) error {
	return s.mutateTask(id, func(t *Task) error {
		t.Position = position
		return nil
	})
}

// UpdateTaskScheduledAt sets or clears the scheduled start time for a task.
// Pass nil to clear the schedule (task will be eligible for immediate promotion).
func (s *Store) UpdateTaskScheduledAt(_ context.Context, id uuid.UUID, scheduledAt *time.Time) error {
	return s.mutateTask(id, func(t *Task) error {
		if scheduledAt == nil {
			t.ScheduledAt = nil
		} else {
			ts := *scheduledAt
			t.ScheduledAt = &ts
		}
		return nil
	})
}

// UpdateTaskDependsOn sets the list of task UUID strings that must all reach
// TaskStatusDone before this task is auto-promoted. An empty or nil slice clears
// all dependencies.
func (s *Store) UpdateTaskDependsOn(_ context.Context, id uuid.UUID, dependsOn []string) error {
	return s.mutateTask(id, func(t *Task) error {
		if len(dependsOn) == 0 {
			t.DependsOn = nil // normalise so omitempty keeps JSON clean
		} else {
			t.DependsOn = dependsOn
		}
		return nil
	})
}

// UpdateTaskTags sets the tag labels for a task. An empty or nil slice clears
// all tags.
func (s *Store) UpdateTaskTags(_ context.Context, id uuid.UUID, tags []string) error {
	return s.mutateTask(id, func(t *Task) error {
		if len(tags) == 0 {
			t.Tags = nil
		} else {
			t.Tags = append([]string(nil), tags...)
		}
		if entry, ok := s.searchIndex[id]; ok {
			entry.tags = strings.ToLower(strings.Join(t.Tags, " "))
			s.searchIndex[id] = entry
		}
		return nil
	})
}

// AreDependenciesSatisfied reports whether every task listed in t.DependsOn has
// status TaskStatusDone. A missing or malformed dependency UUID is treated as
// unsatisfied to avoid silent unblocking.
func (s *Store) AreDependenciesSatisfied(_ context.Context, id uuid.UUID) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.tasks[id]
	if !ok {
		return false, fmt.Errorf("task not found: %s", id)
	}
	for _, depStr := range t.DependsOn {
		depID, err := uuid.Parse(depStr)
		if err != nil {
			return false, nil // malformed UUID → unsatisfied
		}
		dep, ok := s.tasks[depID]
		if !ok {
			// Dependency was deleted or purged. Treat as unsatisfied to
			// prevent silent unblocking. In normal operation, DeleteTask
			// cleans up orphaned dependency references, so this path only
			// fires for race conditions or data corruption.
			return false, nil
		}
		if dep.Status != TaskStatusDone {
			return false, nil
		}
	}
	return true, nil
}

// UpdateTaskBacklog edits prompt, goal, timeout, fresh_start, mount_worktrees, and budget limits for backlog tasks.
func (s *Store) UpdateTaskBacklog(_ context.Context, id uuid.UUID, prompt *string, goal *string, timeout *int, freshStart *bool, mountWorktrees *bool, sandboxByActivity *map[SandboxActivity]sandbox.Type, maxCostUSD *float64, maxInputTokens *int) error {
	// Compute the lowercased fields before acquiring the lock so that
	// strings.ToLower does not extend the critical section.
	var loweredPrompt string
	if prompt != nil {
		loweredPrompt = strings.ToLower(*prompt)
	}
	var loweredGoal string
	if goal != nil {
		loweredGoal = strings.ToLower(*goal)
	}
	return s.mutateTask(id, func(t *Task) error {
		if prompt != nil {
			t.Prompt = *prompt
		}
		if goal != nil {
			t.Goal = *goal
			t.GoalManuallySet = true
		}
		if timeout != nil {
			t.Timeout = clampTimeout(*timeout)
		}
		if freshStart != nil {
			t.FreshStart = *freshStart
		}
		if mountWorktrees != nil {
			t.MountWorktrees = *mountWorktrees
		}
		if sandboxByActivity != nil {
			t.SandboxByActivity = normalizeSandboxByActivity(*sandboxByActivity)
		}
		if maxCostUSD != nil {
			v := *maxCostUSD
			if v < 0 {
				v = 0
			}
			t.MaxCostUSD = v
		}
		if maxInputTokens != nil {
			v := *maxInputTokens
			if v < 0 {
				v = 0
			}
			t.MaxInputTokens = v
		}
		if prompt != nil || goal != nil {
			if entry, ok := s.searchIndex[id]; ok {
				if prompt != nil {
					entry.prompt = loweredPrompt
				}
				if goal != nil {
					entry.goal = loweredGoal
				}
				s.searchIndex[id] = entry
			}
		}
		return nil
	})
}

// UpdateTaskBudget updates the max_cost_usd and max_input_tokens guardrails on
// a task. Unlike UpdateTaskBacklog it is not gated on status, so it can be
// called for waiting tasks to "raise the limit" from the UI.
func (s *Store) UpdateTaskBudget(_ context.Context, id uuid.UUID, maxCostUSD *float64, maxInputTokens *int) error {
	return s.mutateTask(id, func(t *Task) error {
		if maxCostUSD != nil {
			v := *maxCostUSD
			if v < 0 {
				v = 0
			}
			t.MaxCostUSD = v
		}
		if maxInputTokens != nil {
			v := *maxInputTokens
			if v < 0 {
				v = 0
			}
			t.MaxInputTokens = v
		}
		return nil
	})
}

// UpdateTaskSandboxByActivity stores task sandbox overrides by activity key.
// Passing an empty map clears the override map.
func (s *Store) UpdateTaskSandboxByActivity(_ context.Context, id uuid.UUID, sandboxByActivity map[SandboxActivity]sandbox.Type) error {
	return s.mutateTask(id, func(t *Task) error {
		t.SandboxByActivity = normalizeSandboxByActivity(sandboxByActivity)
		return nil
	})
}

// UpdateTaskSandbox stores the task sandbox selection (e.g. "claude" or "codex").
func (s *Store) UpdateTaskSandbox(_ context.Context, id uuid.UUID, sb sandbox.Type) error {
	return s.mutateTask(id, func(t *Task) error {
		t.Sandbox = sandbox.Normalize(string(sb))
		return nil
	})
}

// UpdateTaskModelOverride sets or clears the per-task model override.
// Passing a non-empty string sets the override; an empty string clears it (sets to nil).
func (s *Store) UpdateTaskModelOverride(_ context.Context, id uuid.UUID, model string) error {
	model = strings.TrimSpace(model)
	return s.mutateTask(id, func(t *Task) error {
		if model == "" {
			t.ModelOverride = nil
		} else {
			t.ModelOverride = &model
		}
		return nil
	})
}

// UpdateTaskCustomPatterns replaces the custom pass/fail regex pattern slices on a task.
// Passing a nil slice clears the corresponding field; passing a non-nil empty slice also clears it.
func (s *Store) UpdateTaskCustomPatterns(_ context.Context, id uuid.UUID, passPatterns, failPatterns []string) error {
	return s.mutateTask(id, func(t *Task) error {
		if len(passPatterns) == 0 {
			t.CustomPassPatterns = nil
		} else {
			t.CustomPassPatterns = append([]string(nil), passPatterns...)
		}
		if len(failPatterns) == 0 {
			t.CustomFailPatterns = nil
		} else {
			t.CustomFailPatterns = append([]string(nil), failPatterns...)
		}
		return nil
	})
}

// UpdateTaskEnvironment records the execution environment captured at the start of Run().
// The environment is written atomically alongside the task and broadcast to SSE subscribers.
func (s *Store) UpdateTaskEnvironment(_ context.Context, id uuid.UUID, env ExecutionEnvironment) error {
	return s.mutateTask(id, func(t *Task) error {
		t.Environment = &env
		return nil
	})
}

// ResetTaskForRetry moves a done/failed/cancelled task back to backlog with a fresh state.
// freshStart controls whether the task will start a new Claude session (true) or resume the
// previous one (false, the default) when moved to in_progress.
func (s *Store) ResetTaskForRetry(_ context.Context, id uuid.UUID, newPrompt string, freshStart bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}

	// Snapshot the current run's outcome into a RetryRecord before resetting
	// fields. This preserves a condensed audit trail of each lifecycle.
	// Result is truncated to 2000 chars to keep the RetryHistory entries bounded.
	result := ""
	if t.Result != nil {
		result = *t.Result
		if len(result) > 2000 {
			result = result[:2000] + "..."
		}
	}
	sessionID := ""
	if t.SessionID != nil {
		sessionID = *t.SessionID
	}
	// Snapshot FailureCategory before clearing it so the RetryRecord captures
	// the cause of the lifecycle being retired.
	retiredCategory := t.FailureCategory
	t.RetryHistory = append(t.RetryHistory, RetryRecord{
		RetiredAt:       time.Now(),
		Prompt:          t.Prompt,
		Status:          t.Status,
		Result:          result,
		SessionID:       sessionID,
		Turns:           t.Turns,
		CostUSD:         t.Usage.CostUSD,
		FailureCategory: retiredCategory,
	})

	t.FailureCategory = ""

	oldStatus := t.Status
	t.PromptHistory = append(t.PromptHistory, t.Prompt)
	t.Prompt = newPrompt
	t.FreshStart = freshStart
	t.Result = nil
	t.StopReason = nil
	t.Turns = 0
	t.Status = TaskStatusBacklog
	if freshStart {
		t.WorktreePaths = nil
		t.BranchName = ""
	}
	t.CommitHashes = nil
	t.BaseCommitHashes = nil
	t.IsTestRun = false
	t.LastTestResult = ""
	t.PendingTestFeedback = ""
	t.TestFailCount = 0
	// Reset auto-retry counters so that a manual retry after budget exhaustion
	// grants a fresh allowance and the auto-retrier can act on the next failure.
	t.AutoRetryCount = 0
	t.AutoRetryBudget = map[FailureCategory]int{
		FailureCategoryContainerCrash: defaultAutoRetryBudget[FailureCategoryContainerCrash],
		FailureCategorySyncError:      defaultAutoRetryBudget[FailureCategorySyncError],
		FailureCategoryWorktree:       defaultAutoRetryBudget[FailureCategoryWorktree],
	}
	t.CurrentRefinement = nil
	if freshStart {
		t.RefineSessions = nil
	}
	t.UpdatedAt = time.Now()
	s.removeFromStatusIndex(oldStatus, id)
	s.addToStatusIndex(t.Status, id)
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: although Prompt is reset to newPrompt, the
	// index will be refreshed by UpdateTaskTitle when the title-generation
	// runner fires at the start of the next run.
	s.notify(t, false)
	return nil
}

// ArchiveAllDone archives all done and cancelled tasks in a single operation.
// Returns the IDs of tasks that were archived.
func (s *Store) ArchiveAllDone(_ context.Context) ([]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var archived []uuid.UUID
	for id, t := range s.tasks {
		if t.Archived {
			continue
		}
		if t.Status != TaskStatusDone && t.Status != TaskStatusCancelled {
			continue
		}
		t.Archived = true
		t.UpdatedAt = time.Now()
		if err := s.saveTask(id, t); err != nil {
			return archived, err
		}
		// Search index not updated: archived flag is not a search-indexed field.
		archived = append(archived, id)
		s.notify(t, false)
	}
	return archived, nil
}

// SetTaskArchived sets the archived flag on a task.
func (s *Store) SetTaskArchived(_ context.Context, id uuid.UUID, archived bool) error {
	return s.mutateTask(id, func(t *Task) error {
		t.Archived = archived
		return nil
	})
}

// ResumeTask transitions a failed task back to in_progress, optionally updating timeout.
func (s *Store) ResumeTask(_ context.Context, id uuid.UUID, timeout *int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}

	s.removeFromStatusIndex(t.Status, id)
	t.Status = TaskStatusInProgress
	s.addToStatusIndex(t.Status, id)
	if t.StartedAt == nil {
		now := time.Now()
		t.StartedAt = &now
	}
	if timeout != nil {
		t.Timeout = clampTimeout(*timeout)
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: status and timeout are not search-indexed fields.
	s.notify(t, false)
	return nil
}
