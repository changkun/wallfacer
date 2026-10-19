package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/pkg/sanitize"

	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/logger"
)

// UpdateTaskStatus transitions the task identified by id to the given status.
func (s *Store) UpdateTaskStatus(_ context.Context, id uuid.UUID, status TaskStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if err := TaskMachine.Validate(t.Status, status); err != nil {
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
		if s.OnDone != nil {
			taskCopy := deepCloneTask(t)
			go s.OnDone(taskCopy)
		}
	}
	// Search index not updated: status is not a search-indexed field
	// (title, prompt, tags, oversight).
	s.notify(t, false)
	if isTerminalStatus(status) {
		s.scheduleTerminalCompaction(id)
	}
	return nil
}

// scheduleTerminalCompaction compacts the task's event trace files in the
// background after a terminal-state transition. It captures the highest
// sequence number from in-memory state while the caller still holds s.mu, so
// the goroutine only compacts events that belong to the session that just
// finished — an immediate retry would otherwise bundle new-session events into
// the previous session's compact file. The caller must hold s.mu.
func (s *Store) scheduleTerminalCompaction(id uuid.UUID) {
	// Don't start new background work once Close has published the closed
	// flag: WaitCompaction may already be draining, and a late Add(1) would
	// race the Wait. Close publishes closed under s.mu and this runs under
	// s.mu, so the check and the publish are serialized. Skipping compaction
	// at shutdown only leaves trace events uncompacted; they load fine.
	if s.closed.Load() {
		return
	}
	maxSeq := int64(s.nextSeq[id] - 1)
	s.compactWg.Add(1)
	go func(taskID uuid.UUID, maxSeq int64) {
		defer s.compactWg.Done()
		if err := s.compactTaskEvents(taskID, maxSeq); err != nil {
			logger.Store.Error("failed to compact task traces", "task", taskID, "error", err)
		}
	}(id, maxSeq)
}

// buildAndSaveSummary constructs a TaskSummary from the in-memory task and
// persists it to data/<uuid>/summary.json atomically. It is called while
// s.mu is held for writing so the file is present before any subscriber is
// notified of the done transition. GetOversight reads directly from disk and
// does not acquire s.mu, so it is safe to call here.
func (s *Store) buildAndSaveSummary(task Task) {
	oversight, oversightErr := s.GetOversight(task.ID)
	if oversightErr != nil {
		logger.Store.Warn("buildAndSaveSummary: GetOversight failed", "task", task.ID, "error", oversightErr)
	}
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
		if s.OnDone != nil {
			taskCopy := deepCloneTask(t)
			go s.OnDone(taskCopy)
		}
	}
	// Search index not updated: status is not a search-indexed field
	// (title, prompt, tags, oversight).
	s.notify(t, false)
	if isTerminalStatus(status) {
		s.scheduleTerminalCompaction(id)
	}
	return nil
}

// UpdateTaskTitleGenerating publishes whether a title agent is running.
func (s *Store) UpdateTaskTitleGenerating(_ context.Context, id uuid.UUID, generating bool) error {
	return s.mutateTask(id, func(t *Task) error {
		t.TitleGenerating = generating
		return nil
	})
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

// UpdateTaskCriteria sets a task's free-form acceptance Criteria. Callers gate
// this to backlog status (same constraint as editing the prompt); the store
// records it unconditionally.
func (s *Store) UpdateTaskCriteria(_ context.Context, id uuid.UUID, criteria string) error {
	return s.mutateTask(id, func(t *Task) error {
		t.Criteria = criteria
		return nil
	})
}

// AccumulateSubAgentUsage adds token/cost deltas to the task's running totals
// and records the contribution under the named sub-agent in UsageBreakdown.
// agent should be one of the SandboxActivity constants.
func (s *Store) AccumulateSubAgentUsage(_ context.Context, id uuid.UUID, agent SandboxActivity, delta TaskUsage) error {
	return s.mutateTask(id, func(t *Task) error {
		t.Usage.Add(delta)
		if t.UsageBreakdown == nil {
			t.UsageBreakdown = make(map[SandboxActivity]TaskUsage)
		}
		prev := t.UsageBreakdown[agent]
		prev.Add(delta)
		t.UsageBreakdown[agent] = prev
		return nil
	})
}

// UpdateTaskPosition updates the task board column sort position.
func (s *Store) UpdateTaskPosition(_ context.Context, id uuid.UUID, position int) error {
	return s.mutateTask(id, func(t *Task) error {
		t.Position = position
		return nil
	})
}

// UpdateTaskDependsOn sets the list of task UUID strings this task builds on.
// An empty or nil slice clears all dependencies.
func (s *Store) UpdateTaskDependsOn(_ context.Context, id uuid.UUID, dependsOn []string) error {
	return s.mutateTask(id, func(t *Task) error {
		if len(dependsOn) == 0 {
			t.DependsOn = nil // normalize so omitempty keeps JSON clean
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

// UpdateTaskBacklog edits prompt, timeout, fresh_start, mount_worktrees, and budget limits for backlog tasks.
func (s *Store) UpdateTaskBacklog(_ context.Context, id uuid.UUID, prompt *string, timeout *int, freshStart *bool, mountWorktrees *bool, sandboxByActivity *map[SandboxActivity]harness.ID, maxCostUSD *float64, maxInputTokens *int) error {
	var loweredPrompt string
	if prompt != nil {
		loweredPrompt = strings.ToLower(*prompt)
	}
	return s.mutateTask(id, func(t *Task) error {
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
			v := max(*maxInputTokens, 0)
			t.MaxInputTokens = v
		}
		if prompt != nil {
			if entry, ok := s.searchIndex[id]; ok {
				entry.prompt = loweredPrompt
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
			v := max(*maxInputTokens, 0)
			t.MaxInputTokens = v
		}
		return nil
	})
}

// UpdateTaskSandbox stores the task sandbox selection (e.g. "claude" or "codex").
func (s *Store) UpdateTaskSandbox(_ context.Context, id uuid.UUID, sb harness.ID) error {
	return s.mutateTask(id, func(t *Task) error {
		t.Sandbox = harness.NormalizeID(string(sb))
		return nil
	})
}

// UpdateTaskModelOverride replaces the task's per-task model pin;
// empty string clears it.
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
	// Result is truncated to 2000 runes to keep the RetryHistory entries bounded.
	result := ""
	if t.Result != nil {
		result = sanitize.Truncate(*t.Result, 2000)
	}
	sessionID := ""
	if t.SessionID != nil {
		sessionID = *t.SessionID
	}
	// Snapshot FailureCategory before clearing it so the RetryRecord captures
	// the cause of the lifecycle being retired.
	retiredCategory := t.FailureCategory
	t.ExecutionAttempt = t.CurrentAttempt() + 1
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
	// Keep the search index in sync with the new prompt, matching the peer
	// prompt mutators (UpdateTaskBacklog, UpdateTaskPromptDirect, ApplyRefinement).
	if entry, ok := s.searchIndex[id]; ok {
		entry.prompt = strings.ToLower(newPrompt)
		s.searchIndex[id] = entry
	}
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

// UpdateTaskPromptDirect sets the prompt on a backlog or waiting task.
// Returns the previous prompt and whether the task is in waiting state (resume_hint).
// Unlike UpdateTaskBacklog it is not restricted to backlog status.
func (s *Store) UpdateTaskPromptDirect(_ context.Context, id uuid.UUID, newPrompt string) (prevPrompt string, resumeHint bool, err error) {
	err = s.mutateTask(id, func(t *Task) error {
		if t.Status != TaskStatusBacklog && t.Status != TaskStatusWaiting {
			return fmt.Errorf("task status %q does not allow prompt updates; must be backlog or waiting", t.Status)
		}
		prevPrompt = t.Prompt
		resumeHint = t.Status == TaskStatusWaiting
		t.Prompt = newPrompt
		lowered := strings.ToLower(newPrompt)
		if entry, ok := s.searchIndex[id]; ok {
			entry.prompt = lowered
			s.searchIndex[id] = entry
		}
		return nil
	})
	return
}
