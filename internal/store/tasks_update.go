package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/sandbox"
	"github.com/google/uuid"
)

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
		go func(taskID uuid.UUID) {
			if err := s.compactTaskEvents(taskID); err != nil {
				logger.Store.Error("failed to compact task traces", "task", taskID, "error", err)
			}
		}(id)
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

	summary := TaskSummary{
		TaskID:          task.ID,
		Title:           task.Title,
		Status:          task.Status,
		CompletedAt:     task.UpdatedAt,
		CreatedAt:       task.CreatedAt,
		DurationSeconds: duration,
		TotalTurns:      task.Turns,
		TotalCostUSD:    task.Usage.CostUSD,
		ByActivity:      task.UsageBreakdown,
		TestResult:      task.LastTestResult,
		PhaseCount:      phaseCount,
		FailureCategory: task.FailureCategory,
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

	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Title = title
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	if entry, ok := s.searchIndex[id]; ok {
		entry.title = loweredTitle
		s.searchIndex[id] = entry
	}
	s.notify(t, false)
	return nil
}

// UpdateTaskExecutionPrompt sets the full execution prompt used at runtime.
// When non-empty, the runner passes ExecutionPrompt to the sandbox instead of
// Prompt, so Prompt can be kept as a short human-readable card label.
func (s *Store) UpdateTaskExecutionPrompt(_ context.Context, id uuid.UUID, executionPrompt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.ExecutionPrompt = executionPrompt
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: ExecutionPrompt is not a search-indexed field;
	// the indexed prompt is the human-readable card label stored in Prompt.
	s.notify(t, false)
	return nil
}

// UpdateTaskTurns updates only the turn counter for a task, leaving all other
// fields (Result, SessionID, StopReason) unchanged. Used during test runs so
// that the implementation agent's output is not overwritten.
func (s *Store) UpdateTaskTurns(_ context.Context, id uuid.UUID, turns int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Turns = turns
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: turn count is not a search-indexed field.
	s.notify(t, false)
	return nil
}

// UpdateTaskResult stores the final output, session ID, stop reason, and turn count.
func (s *Store) UpdateTaskResult(_ context.Context, id uuid.UUID, result, sessionID, stopReason string, turns int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Result = &result
	t.SessionID = &sessionID
	t.StopReason = &stopReason
	t.Turns = turns
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: result, sessionID, stopReason, and turns are
	// not search-indexed fields (title, prompt, tags, oversight).
	s.notify(t, false)
	return nil
}

// AccumulateSubAgentUsage adds token/cost deltas to the task's running totals
// and records the contribution under the named sub-agent in UsageBreakdown.
// agent should be one of: "implementation", "test", "title", "oversight",
// "oversight-test", "refinement".
func (s *Store) AccumulateSubAgentUsage(_ context.Context, id uuid.UUID, agent string, delta TaskUsage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	// Accumulate into the aggregate total.
	t.Usage.InputTokens += delta.InputTokens
	t.Usage.OutputTokens += delta.OutputTokens
	t.Usage.CacheReadInputTokens += delta.CacheReadInputTokens
	t.Usage.CacheCreationTokens += delta.CacheCreationTokens
	t.Usage.CostUSD += delta.CostUSD
	// Accumulate into the per-sub-agent breakdown.
	if t.UsageBreakdown == nil {
		t.UsageBreakdown = make(map[string]TaskUsage)
	}
	prev := t.UsageBreakdown[agent]
	prev.InputTokens += delta.InputTokens
	prev.OutputTokens += delta.OutputTokens
	prev.CacheReadInputTokens += delta.CacheReadInputTokens
	prev.CacheCreationTokens += delta.CacheCreationTokens
	prev.CostUSD += delta.CostUSD
	t.UsageBreakdown[agent] = prev
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: token/cost usage is not a search-indexed field.
	s.notify(t, false)
	return nil
}

// AccumulateTaskUsage is a convenience wrapper that accumulates usage without
// attributing it to a specific sub-agent. Prefer AccumulateSubAgentUsage.
func (s *Store) AccumulateTaskUsage(ctx context.Context, id uuid.UUID, delta TaskUsage) error {
	return s.AccumulateSubAgentUsage(ctx, id, "implementation", delta)
}

// UpdateTaskPosition updates the task board column sort position.
func (s *Store) UpdateTaskPosition(_ context.Context, id uuid.UUID, position int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Position = position
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: board position is not a search-indexed field.
	s.notify(t, false)
	return nil
}

// UpdateTaskScheduledAt sets or clears the scheduled start time for a task.
// Pass nil to clear the schedule (task will be eligible for immediate promotion).
func (s *Store) UpdateTaskScheduledAt(_ context.Context, id uuid.UUID, scheduledAt *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if scheduledAt == nil {
		t.ScheduledAt = nil
	} else {
		ts := *scheduledAt
		t.ScheduledAt = &ts
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: scheduled time is not a search-indexed field.
	s.notify(t, false)
	return nil
}

// UpdateTaskDependsOn sets the list of task UUID strings that must all reach
// TaskStatusDone before this task is auto-promoted. An empty or nil slice clears
// all dependencies.
func (s *Store) UpdateTaskDependsOn(_ context.Context, id uuid.UUID, dependsOn []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if len(dependsOn) == 0 {
		t.DependsOn = nil // normalise so omitempty keeps JSON clean
	} else {
		t.DependsOn = dependsOn
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: dependency list is not a search-indexed field.
	s.notify(t, false)
	return nil
}

// UpdateTaskTags sets the tag labels for a task. An empty or nil slice clears
// all tags.
func (s *Store) UpdateTaskTags(_ context.Context, id uuid.UUID, tags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if len(tags) == 0 {
		t.Tags = nil
	} else {
		t.Tags = append([]string(nil), tags...)
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	if entry, ok := s.searchIndex[id]; ok {
		entry.tags = strings.ToLower(strings.Join(t.Tags, " "))
		s.searchIndex[id] = entry
	}
	s.notify(t, false)
	return nil
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
			return false, nil // deleted dependency → unsatisfied (conservative)
		}
		if dep.Status != TaskStatusDone {
			return false, nil
		}
	}
	return true, nil
}

// UpdateTaskBacklog edits prompt, timeout, fresh_start, mount_worktrees, and budget limits for backlog tasks.
func (s *Store) UpdateTaskBacklog(_ context.Context, id uuid.UUID, prompt *string, timeout *int, freshStart *bool, mountWorktrees *bool, sandboxByActivity *map[string]sandbox.Type, maxCostUSD *float64, maxInputTokens *int) error {
	// Compute the lowercased prompt before acquiring the lock so that
	// strings.ToLower does not extend the critical section.
	var loweredPrompt string
	if prompt != nil {
		loweredPrompt = strings.ToLower(*prompt)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if prompt != nil {
		t.Prompt = *prompt
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
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	if prompt != nil {
		if entry, ok := s.searchIndex[id]; ok {
			entry.prompt = loweredPrompt
			s.searchIndex[id] = entry
		}
	}
	s.notify(t, false)
	return nil
}

// UpdateTaskBudget updates the max_cost_usd and max_input_tokens guardrails on
// a task. Unlike UpdateTaskBacklog it is not gated on status, so it can be
// called for waiting tasks to "raise the limit" from the UI.
func (s *Store) UpdateTaskBudget(_ context.Context, id uuid.UUID, maxCostUSD *float64, maxInputTokens *int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
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
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: budget limits are not search-indexed fields.
	s.notify(t, false)
	return nil
}

// UpdateTaskSandboxByActivity stores task sandbox overrides by activity key.
// Passing an empty map clears the override map.
func (s *Store) UpdateTaskSandboxByActivity(_ context.Context, id uuid.UUID, sandboxByActivity map[string]sandbox.Type) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.SandboxByActivity = normalizeSandboxByActivity(sandboxByActivity)
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: sandbox selection is not a search-indexed field.
	s.notify(t, false)
	return nil
}

// UpdateTaskSandbox stores the task sandbox selection (e.g. "claude" or "codex").
func (s *Store) UpdateTaskSandbox(_ context.Context, id uuid.UUID, sb sandbox.Type) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Sandbox = sandbox.Normalize(string(sb))
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: sandbox selection is not a search-indexed field.
	s.notify(t, false)
	return nil
}

// UpdateTaskModelOverride sets or clears the per-task model override.
// Passing a non-empty string sets the override; an empty string clears it (sets to nil).
func (s *Store) UpdateTaskModelOverride(_ context.Context, id uuid.UUID, model string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	model = strings.TrimSpace(model)
	if model == "" {
		t.ModelOverride = nil
	} else {
		t.ModelOverride = &model
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: model override is not a search-indexed field.
	s.notify(t, false)
	return nil
}

// UpdateTaskEnvironment records the execution environment captured at the start of Run().
// The environment is written atomically alongside the task and broadcast to SSE subscribers.
func (s *Store) UpdateTaskEnvironment(_ context.Context, id uuid.UUID, env ExecutionEnvironment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Environment = &env
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: execution environment is not a search-indexed field.
	s.notify(t, false)
	return nil
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
	t.WorktreePaths = nil
	t.BranchName = ""
	t.CommitHashes = nil
	t.BaseCommitHashes = nil
	t.IsTestRun = false
	t.LastTestResult = ""
	t.PendingTestFeedback = ""
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
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Archived = archived
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	// Search index not updated: archived flag is not a search-indexed field.
	s.notify(t, false)
	return nil
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

// UpdateTaskWorktrees persists the worktree paths and branch name for a task.
