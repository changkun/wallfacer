package store

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"changkun.de/x/wallfacer/internal/logger"
	"github.com/google/uuid"
)

// cmpTaskPositionCreatedAt orders tasks by Position ascending, then CreatedAt ascending.
func cmpTaskPositionCreatedAt(a, b Task) int {
	if c := cmp.Compare(a.Position, b.Position); c != 0 {
		return c
	}
	return a.CreatedAt.Compare(b.CreatedAt)
}

// ListTasksByStatus returns all tasks with the given status, sorted by position then creation time.
func (s *Store) ListTasksByStatus(_ context.Context, status TaskStatus) ([]Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.tasksByStatus[status]
	tasks := make([]Task, 0, len(ids))
	for id := range ids {
		t := s.tasks[id]
		if t == nil {
			continue // defensive: index and task map should always be in sync
		}
		tasks = append(tasks, cloneTask(t))
	}
	slices.SortFunc(tasks, cmpTaskPositionCreatedAt)
	return tasks, nil
}

// CountByStatus returns the number of active tasks with the given status in O(1).
func (s *Store) CountByStatus(status TaskStatus) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tasksByStatus[status])
}

// CountRegularInProgress returns the count of in-progress tasks that are not
// test runs. This is O(k) where k is the number of in-progress tasks, not O(n)
// over all tasks. Must not be called while s.mu is held.
func (s *Store) CountRegularInProgress() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for id := range s.tasksByStatus[TaskStatusInProgress] {
		if t := s.tasks[id]; t != nil && !t.IsTestRun {
			count++
		}
	}
	return count
}

// ListSummaries returns all task summaries by finding tasks that have a
// summary.json blob, then reading each one. Tasks that completed before
// summary.json was introduced will simply have no entry in the returned slice.
func (s *Store) ListSummaries() ([]TaskSummary, error) {
	owners, err := s.backend.ListBlobOwners("summary.json")
	if err != nil {
		return nil, fmt.Errorf("list summary owners: %w", err)
	}

	var summaries []TaskSummary
	for _, id := range owners {
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

// ListTasks returns all tasks, optionally including archived ones.
func (s *Store) ListTasks(_ context.Context, includeArchived bool) ([]Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if !includeArchived && t.Archived {
			continue
		}
		tasks = append(tasks, cloneTask(t))
	}
	slices.SortFunc(tasks, cmpTaskPositionCreatedAt)
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
		tasks = append(tasks, cloneTask(t))
	}
	slices.SortFunc(tasks, cmpTaskPositionCreatedAt)
	return tasks, s.hub.LatestSeq(), nil
}

// ListArchivedTasksPage returns a single page of archived tasks ordered by
// UpdatedAt DESC (newest first), with deterministic ID tie-breaking.
//
// Paging semantics:
//   - beforeID: return older tasks after the referenced archived task.
//   - afterID:  return newer tasks before the referenced archived task.
//   - both nil: return the first page (newest archived tasks).
//
// Returns: (page, totalArchived, hasMoreBefore, hasMoreAfter, error).
func (s *Store) ListArchivedTasksPage(_ context.Context, pageSize int, beforeID, afterID *uuid.UUID) ([]Task, int, bool, bool, error) {
	if pageSize < 1 {
		pageSize = 1
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if beforeID != nil && afterID != nil {
		return nil, 0, false, false, fmt.Errorf("before and after cursors are mutually exclusive")
	}

	archived := make([]Task, 0)
	for _, t := range s.tasks {
		if !t.Archived {
			continue
		}
		archived = append(archived, cloneTask(t))
	}
	slices.SortFunc(archived, func(a, b Task) int {
		if c := b.UpdatedAt.Compare(a.UpdatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID.String(), a.ID.String())
	})

	total := len(archived)
	if total == 0 {
		return []Task{}, 0, false, false, nil
	}

	start, end := 0, min(pageSize, total)
	switch {
	case beforeID != nil:
		idx := slices.IndexFunc(archived, func(t Task) bool { return t.ID == *beforeID })
		if idx == -1 {
			return nil, total, false, false, fmt.Errorf("before cursor task not found")
		}
		start = idx + 1
		if start > total {
			start = total
		}
		end = min(start+pageSize, total)
	case afterID != nil:
		idx := slices.IndexFunc(archived, func(t Task) bool { return t.ID == *afterID })
		if idx == -1 {
			return nil, total, false, false, fmt.Errorf("after cursor task not found")
		}
		end = idx
		if end < 0 {
			end = 0
		}
		start = max(0, end-pageSize)
	}

	page := make([]Task, 0, max(0, end-start))
	if start < end {
		page = append(page, archived[start:end]...)
	}
	hasMoreAfter := start > 0
	hasMoreBefore := end < total
	return page, total, hasMoreBefore, hasMoreAfter, nil
}

// TasksDependingOn returns all non-terminal tasks whose DependsOn contains
// taskID. Non-terminal statuses are backlog, in_progress, waiting, and failed.
// The returned slice contains deep copies safe for use outside the lock.
func (s *Store) TasksDependingOn(_ context.Context, taskID uuid.UUID) ([]*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	taskIDStr := taskID.String()
	var result []*Task
	for _, t := range s.tasks {
		switch t.Status {
		case TaskStatusBacklog, TaskStatusInProgress, TaskStatusWaiting, TaskStatusFailed:
		default:
			continue
		}
		if slices.Contains(t.DependsOn, taskIDStr) {
			cp := deepCloneTask(t)
			result = append(result, &cp)
		}
	}
	return result, nil
}

// cloneTask returns a deep copy of t so that callers cannot accidentally
// mutate store-owned state through shared slice/map/pointer fields.
// It is the canonical outward-facing wrapper for deepCloneTask; all read
// paths that expose Task values to callers must go through cloneTask or
// deepCloneTask directly.
func cloneTask(t *Task) Task {
	return deepCloneTask(t)
}

// GetTask returns a deep copy of the task with the given ID.  Every
// mutable field (slices, maps, pointer-backed structs, and pointer-to-string
// fields) is duplicated so that callers cannot mutate store-owned state
// after the lock is released.
func (s *Store) GetTask(_ context.Context, id uuid.UUID) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	cp := deepCloneTask(t)
	return &cp, nil
}
