package store

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"changkun.de/wallfacer/internal/logger"
	"github.com/google/uuid"
)

// ListSummaries returns all task summaries found in data/*/summary.json.
// It walks the data directory and reads each summary file independently,
// without loading the full task.json. Tasks that completed before summary.json
// was introduced will simply have no entry in the returned slice.
func (s *Store) ListSummaries() ([]TaskSummary, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir))
	if err != nil {
		return nil, fmt.Errorf("read data dir: %w", err)
	}

	var summaries []TaskSummary
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id, err := uuid.Parse(entry.Name())
		if err != nil {
			continue // skip non-UUID directories
		}
		summary, err := s.LoadSummary(id)
		if err != nil {
			logger.Store.Warn("failed to load summary", "id", id, "error", err)
			continue
		}
		if summary != nil {
			summaries = append(summaries, *summary)
		}
	}
	return summaries, nil
}

// ErrRefinementAlreadyRunning is returned by StartRefinementJobIfIdle when a
// refinement job is already in "running" state for the given task.
var ErrRefinementAlreadyRunning = errors.New("refinement already running")

const refinementRecentCompleteWindow = 500 * time.Millisecond

// ListTasks returns all tasks sorted by position then creation time.
// Archived tasks are excluded unless includeArchived is true.
func (s *Store) ListTasks(_ context.Context, includeArchived bool) ([]Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if !includeArchived && t.Archived {
			continue
		}
		cp := *t
		if t.CurrentRefinement != nil {
			jobCopy := *t.CurrentRefinement
			cp.CurrentRefinement = &jobCopy
		}
		tasks = append(tasks, cp)
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Position != tasks[j].Position {
			return tasks[i].Position < tasks[j].Position
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
	return tasks, nil
}

// ListTasksAndSeq returns all tasks (same as ListTasks) together with the
// current delta sequence number, both read under the same s.mu.RLock() so
// the snapshot and the sequence ID are guaranteed to be consistent.
// Callers use the returned seq as the SSE "id:" field on the snapshot event;
// reconnecting clients replay only deltas with Seq > seq.
func (s *Store) ListTasksAndSeq(_ context.Context, includeArchived bool) ([]Task, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if !includeArchived && t.Archived {
			continue
		}
		cp := *t
		if t.CurrentRefinement != nil {
			jobCopy := *t.CurrentRefinement
			cp.CurrentRefinement = &jobCopy
		}
		tasks = append(tasks, cp)
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Position != tasks[j].Position {
			return tasks[i].Position < tasks[j].Position
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
	return tasks, s.deltaSeq.Load(), nil
}

// GetTask returns a deep copy of the task with the given ID.
// Pointer fields (CurrentRefinement) are copied so callers cannot
// accidentally mutate shared store state.
func (s *Store) GetTask(_ context.Context, id uuid.UUID) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	cp := *t
	if t.CurrentRefinement != nil {
		jobCopy := *t.CurrentRefinement
		cp.CurrentRefinement = &jobCopy
	}
	return &cp, nil
}

// CreateTask creates a new task in backlog status and persists it.
// kind identifies the execution mode (TaskKindTask or TaskKindIdeaAgent).
// Optional tags are attached to the task for categorisation (e.g. "idea-agent").
func (s *Store) CreateTask(_ context.Context, prompt string, timeout int, mountWorktrees bool, _ string, kind TaskKind, tags ...string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	minPos := 0
	hasBacklog := false
	for _, t := range s.tasks {
		if t.Status == TaskStatusBacklog {
			if !hasBacklog || t.Position < minPos {
				minPos = t.Position
				hasBacklog = true
			}
		}
	}
	newPosition := 0
	if hasBacklog {
		newPosition = minPos - 1
	}

	timeout = clampTimeout(timeout)

	now := time.Now()
	task := &Task{
		SchemaVersion:  CurrentTaskSchemaVersion,
		ID:             uuid.New(),
		Prompt:         prompt,
		Status:         TaskStatusBacklog,
		Turns:          0,
		Timeout:        timeout,
		MountWorktrees: mountWorktrees,
		Kind:           kind,
		Tags:           tags,
		Position:       newPosition,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	taskDir := filepath.Join(s.dir, task.ID.String())
	tracesDir := filepath.Join(taskDir, "traces")
	if err := os.MkdirAll(tracesDir, 0755); err != nil {
		return nil, err
	}

	if err := s.saveTask(task.ID, task); err != nil {
		return nil, err
	}

	s.tasks[task.ID] = task
	s.events[task.ID] = nil
	s.nextSeq[task.ID] = 1
	s.notify(task, false)

	ret := *task
	return &ret, nil
}

func normalizeSandboxByActivity(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(SandboxActivities))
	for _, key := range SandboxActivities {
		allowed[key] = struct{}{}
	}
	out := make(map[string]string)
	for k, v := range input {
		key := strings.ToLower(strings.TrimSpace(k))
		if _, ok := allowed[key]; !ok {
			continue
		}
		val := strings.ToLower(strings.TrimSpace(v))
		if val == "" {
			continue
		}
		out[key] = val
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// DeleteTask removes a task and all its on-disk data.
func (s *Store) DeleteTask(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}

	taskDir := filepath.Join(s.dir, id.String())
	if err := os.RemoveAll(taskDir); err != nil {
		return fmt.Errorf("remove task dir: %w", err)
	}

	delete(s.tasks, id)
	delete(s.events, id)
	delete(s.nextSeq, id)
	s.notify(t, true)
	return nil
}

// UpdateTaskStatus sets a task's status field, enforcing the state machine.
// Returns ErrInvalidTransition if the requested transition is not allowed.
// When transitioning to TaskStatusDone, a summary.json is written atomically
// before subscribers are notified, so the file is always present by the time
// any observer sees the done state.
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
	t.Status = status
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	if status == TaskStatusDone {
		s.buildAndSaveSummary(*t)
	}
	s.notify(t, false)
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
	}

	if err := s.SaveSummary(task.ID, summary); err != nil {
		logger.Store.Warn("failed to save task summary", "task", task.ID, "error", err)
	}
}

// ForceUpdateTaskStatus sets a task's status field without validating the
// transition. Use this only for server recovery paths that must succeed
// regardless of current state, and for test fixtures that need arbitrary
// initial states. Prefer UpdateTaskStatus for all normal code paths.
func (s *Store) ForceUpdateTaskStatus(_ context.Context, id uuid.UUID, status TaskStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Status = status
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify(t, false)
	return nil
}

// UpdateTaskTitle sets a task's display title.
func (s *Store) UpdateTaskTitle(_ context.Context, id uuid.UUID, title string) error {
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

// UpdateTaskBacklog edits prompt, timeout, fresh_start, and mount_worktrees for backlog tasks.
func (s *Store) UpdateTaskBacklog(_ context.Context, id uuid.UUID, prompt *string, timeout *int, freshStart *bool, mountWorktrees *bool, sandboxByActivity *map[string]string) error {
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
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify(t, false)
	return nil
}

// UpdateTaskSandboxByActivity stores task sandbox overrides by activity key.
// Passing an empty map clears the override map.
func (s *Store) UpdateTaskSandboxByActivity(_ context.Context, id uuid.UUID, sandboxByActivity map[string]string) error {
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
	s.notify(t, false)
	return nil
}

// UpdateTaskSandbox stores the task sandbox selection (e.g. "claude" or "codex").
func (s *Store) UpdateTaskSandbox(_ context.Context, id uuid.UUID, sandbox string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Sandbox = strings.TrimSpace(sandbox)
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
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
	t.RetryHistory = append(t.RetryHistory, RetryRecord{
		RetiredAt: time.Now(),
		Prompt:    t.Prompt,
		Status:    t.Status,
		Result:    result,
		SessionID: sessionID,
		Turns:     t.Turns,
		CostUSD:   t.Usage.CostUSD,
	})

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
	t.UpdatedAt = time.Now()
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

	t.Status = TaskStatusInProgress
	if timeout != nil {
		t.Timeout = clampTimeout(*timeout)
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify(t, false)
	return nil
}

// UpdateTaskWorktrees persists the worktree paths and branch name for a task.
func (s *Store) UpdateTaskWorktrees(_ context.Context, id uuid.UUID, worktreePaths map[string]string, branchName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.WorktreePaths = worktreePaths
	t.BranchName = branchName
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify(t, false)
	return nil
}

// UpdateTaskCommitHashes stores the post-merge commit hash per repo path.
func (s *Store) UpdateTaskCommitHashes(_ context.Context, id uuid.UUID, hashes map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.CommitHashes = hashes
	t.UpdatedAt = time.Now()
	return s.saveTask(id, t)
}

// UpdateTaskTestRun sets the IsTestRun flag and LastTestResult on a task atomically.
// Call with isTestRun=true and empty lastTestResult to mark the start of a test run;
// call with isTestRun=false and a verdict ("pass"/"fail"/"") when the test completes.
func (s *Store) UpdateTaskTestRun(_ context.Context, id uuid.UUID, isTestRun bool, lastTestResult string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.IsTestRun = isTestRun
	t.LastTestResult = lastTestResult
	if isTestRun {
		// Record the current turn count so we know which turn files belong to
		// the implementation phase vs the test phase.
		t.TestRunStartTurn = t.Turns
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify(t, false)
	return nil
}

// UpdateTaskBaseCommitHashes stores the default-branch HEAD captured before merge.
func (s *Store) UpdateTaskBaseCommitHashes(_ context.Context, id uuid.UUID, hashes map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.BaseCommitHashes = hashes
	t.UpdatedAt = time.Now()
	return s.saveTask(id, t)
}

// UpdateRefinementJob persists the current refinement job state.
// Pass nil to clear the active refinement job.
func (s *Store) UpdateRefinementJob(_ context.Context, id uuid.UUID, job *RefinementJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if job != nil {
		jobCopy := *job
		t.CurrentRefinement = &jobCopy
	} else {
		t.CurrentRefinement = nil
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify(t, false)
	return nil
}

// StartRefinementJobIfIdle atomically checks that no refinement is currently
// running for the task and, if so, persists the new job. Returns
// ErrRefinementAlreadyRunning without modifying the store when the existing
// CurrentRefinement.Status == "running". If the existing job completed very
// recently and recorded an error or output, it is also treated as still
// in-flight to avoid concurrent duplicate starts during fast failure races.
// The guard uses task.UpdatedAt so a just-completed runner job does not
// immediately become eligible for a second start in a tight race.
func (s *Store) StartRefinementJobIfIdle(_ context.Context, id uuid.UUID, job *RefinementJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if t.CurrentRefinement != nil {
		status := t.CurrentRefinement.Status
		if status == "running" {
			return ErrRefinementAlreadyRunning
		}
		if t.CurrentRefinement.Source == "runner" && (status == "failed" || status == "done") {
			elapsed := time.Since(t.UpdatedAt)
			if elapsed >= 0 && elapsed < refinementRecentCompleteWindow && (t.CurrentRefinement.Error != "" || t.CurrentRefinement.Result != "") {
				return ErrRefinementAlreadyRunning
			}
		}
	}
	jobCopy := *job
	t.CurrentRefinement = &jobCopy
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify(t, false)
	return nil
}

// ApplyRefinement saves a refinement session and updates the task prompt.
// The current prompt is pushed into PromptHistory before being replaced.
// The CurrentRefinement job is cleared after applying.
func (s *Store) ApplyRefinement(_ context.Context, id uuid.UUID, newPrompt string, session RefinementSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	session.ResultPrompt = newPrompt
	t.PromptHistory = append(t.PromptHistory, t.Prompt)
	t.RefineSessions = append(t.RefineSessions, session)
	t.Prompt = newPrompt
	t.CurrentRefinement = nil
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify(t, false)
	return nil
}

// DismissRefinement clears the current refinement job without changing the prompt.
// Used when the user chooses not to apply the refined prompt.
func (s *Store) DismissRefinement(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.CurrentRefinement = nil
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify(t, false)
	return nil
}

const maxSearchResults = 50
const snippetPadding = 60

// SearchTasks performs a case-insensitive substring search across title, prompt,
// tags (joined), and oversight summary text. Search order favours the cheapest
// fields first. Each task produces at most one result (first matching field).
// Results are capped at maxSearchResults. Archived tasks are included.
func (s *Store) SearchTasks(_ context.Context, query string) ([]TaskSearchResult, error) {
	q := strings.ToLower(query)

	// Snapshot task pointers under RLock; oversight reads happen outside the lock.
	s.mu.RLock()
	snapshot := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		cp := *t
		snapshot = append(snapshot, &cp)
	}
	s.mu.RUnlock()

	results := make([]TaskSearchResult, 0)
	for _, t := range snapshot {
		if len(results) >= maxSearchResults {
			break
		}
		if field, snippet, ok := matchTask(t, q, s); ok {
			results = append(results, TaskSearchResult{
				Task:         t,
				MatchedField: field,
				Snippet:      snippet,
			})
		}
	}
	return results, nil
}

// matchTask checks each field in cheapest-first order. Returns (field, snippet, true)
// on the first match, or ("", "", false) if nothing matches.
func matchTask(t *Task, q string, s *Store) (field, snippet string, ok bool) {
	if idx := strings.Index(strings.ToLower(t.Title), q); idx != -1 {
		return "title", buildSnippet(t.Title, idx, len(q)), true
	}
	if idx := strings.Index(strings.ToLower(t.Prompt), q); idx != -1 {
		return "prompt", buildSnippet(t.Prompt, idx, len(q)), true
	}
	joined := strings.Join(t.Tags, " ")
	if idx := strings.Index(strings.ToLower(joined), q); idx != -1 {
		return "tags", buildSnippet(joined, idx, len(q)), true
	}
	// Oversight is the most expensive check (disk read) — always last.
	if text, err := s.LoadOversightText(t.ID); err == nil && text != "" {
		if idx := strings.Index(strings.ToLower(text), q); idx != -1 {
			return "oversight", buildSnippet(text, idx, len(q)), true
		}
	}
	return "", "", false
}

// buildSnippet returns an HTML-escaped substring of src centred on the match at
// [idx, idx+matchLen) with up to snippetPadding bytes of context on each side.
// Truncation points are adjusted to UTF-8 rune boundaries, and ellipsis markers
// are prepended/appended when the window is shorter than src.
func buildSnippet(src string, idx, matchLen int) string {
	start := idx - snippetPadding
	prefix := "…"
	if start <= 0 {
		start = 0
		prefix = ""
	}
	end := idx + matchLen + snippetPadding
	suffix := "…"
	if end >= len(src) {
		end = len(src)
		suffix = ""
	}
	// Align to UTF-8 rune boundaries.
	for start > 0 && !utf8.RuneStart(src[start]) {
		start--
	}
	for end < len(src) && !utf8.RuneStart(src[end]) {
		end++
	}
	return html.EscapeString(prefix + src[start:end] + suffix)
}

// clampTimeout ensures timeout stays in [1, 1440] minutes with a default of 60.
func clampTimeout(v int) int {
	if v <= 0 {
		return 60
	}
	if v > 1440 {
		return 1440
	}
	return v
}
