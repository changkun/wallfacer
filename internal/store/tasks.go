package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"
)

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
		tasks = append(tasks, *t)
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Position != tasks[j].Position {
			return tasks[i].Position < tasks[j].Position
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
	return tasks, nil
}

// GetTask returns a copy of the task with the given ID.
func (s *Store) GetTask(_ context.Context, id uuid.UUID) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	cp := *t
	return &cp, nil
}

// CreateTask creates a new task in backlog status and persists it.
func (s *Store) CreateTask(_ context.Context, prompt string, timeout int) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	maxPos := -1
	for _, t := range s.tasks {
		if t.Status == "backlog" && t.Position > maxPos {
			maxPos = t.Position
		}
	}

	timeout = clampTimeout(timeout)

	now := time.Now()
	task := &Task{
		ID:        uuid.New(),
		Prompt:    prompt,
		Status:    "backlog",
		Turns:     0,
		Timeout:   timeout,
		Position:  maxPos + 1,
		CreatedAt: now,
		UpdatedAt: now,
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
	s.notify()

	ret := *task
	return &ret, nil
}

// DeleteTask removes a task and all its on-disk data.
func (s *Store) DeleteTask(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tasks[id]; !ok {
		return fmt.Errorf("task not found: %s", id)
	}

	taskDir := filepath.Join(s.dir, id.String())
	if err := os.RemoveAll(taskDir); err != nil {
		return fmt.Errorf("remove task dir: %w", err)
	}

	delete(s.tasks, id)
	delete(s.events, id)
	delete(s.nextSeq, id)
	s.notify()
	return nil
}

// UpdateTaskStatus sets a task's status field.
func (s *Store) UpdateTaskStatus(_ context.Context, id uuid.UUID, status string) error {
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
	s.notify()
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
	s.notify()
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
	s.notify()
	return nil
}

// AccumulateTaskUsage adds token/cost deltas to the task's running totals.
func (s *Store) AccumulateTaskUsage(_ context.Context, id uuid.UUID, delta TaskUsage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Usage.InputTokens += delta.InputTokens
	t.Usage.OutputTokens += delta.OutputTokens
	t.Usage.CacheReadInputTokens += delta.CacheReadInputTokens
	t.Usage.CacheCreationTokens += delta.CacheCreationTokens
	t.Usage.CostUSD += delta.CostUSD
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify()
	return nil
}

// UpdateTaskPosition updates the Kanban column sort position.
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
	s.notify()
	return nil
}

// UpdateTaskBacklog edits prompt, timeout, and fresh_start for backlog tasks.
func (s *Store) UpdateTaskBacklog(_ context.Context, id uuid.UUID, prompt *string, timeout *int, freshStart *bool) error {
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
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify()
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

	t.PromptHistory = append(t.PromptHistory, t.Prompt)
	t.Prompt = newPrompt
	t.FreshStart = freshStart
	t.Result = nil
	t.StopReason = nil
	t.Turns = 0
	t.Status = "backlog"
	t.WorktreePaths = nil
	t.BranchName = ""
	t.CommitHashes = nil
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify()
	return nil
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
	s.notify()
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

	t.Status = "in_progress"
	if timeout != nil {
		t.Timeout = clampTimeout(*timeout)
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify()
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
	s.notify()
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

// clampTimeout ensures timeout stays in [1, 1440] minutes with a default of 5.
func clampTimeout(v int) int {
	if v <= 0 {
		return 5
	}
	if v > 1440 {
		return 1440
	}
	return v
}
