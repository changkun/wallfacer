package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/logger"
)

// TaskCreateOptions holds parameters for creating a new task.
type TaskCreateOptions struct {
	// Title is the task's display title. When blank after trimming, the
	// title is derived from Prompt by DeriveTitle.
	Title   string
	Prompt  string
	Timeout int
	Kind    TaskKind
	// FlowID sets Task.FlowID, a record field the runner does not read for
	// dispatch (see Task.FlowID). No handler sets it; it lets a caller build
	// a record that carries the field, as a stored task may.
	FlowID            string
	Tags              []string
	Sandbox           harness.ID
	SandboxByActivity map[SandboxActivity]harness.ID
	MaxCostUSD        float64
	MaxInputTokens    int
	SpecSourcePath    string
	ModelOverride     string
}

// CreateTaskWithOptions creates a new backlog task in a single atomic write.
// All fields in opts are normalized (sandbox maps, budget clamps) and persisted
// together, so no SSE subscriber can observe a partially-initialized
// task.  notify is called exactly once.
func (s *Store) CreateTaskWithOptions(_ context.Context, opts TaskCreateOptions) (*Task, error) {
	// Build the task struct from opts outside the lock.  Position is the only
	// field that requires a locked scan of s.tasks; it is set inside the lock
	// below.  All search-indexed fields (Prompt, Tags) are available from opts
	// so buildIndexEntry can run before we take any lock.
	id := uuid.New()

	now := time.Now()

	task := &Task{
		SchemaVersion: constants.CurrentTaskSchemaVersion,
		ID:            id,
		Title:         strings.TrimSpace(opts.Title),
		Prompt:        opts.Prompt,
		Status:        TaskStatusBacklog,
		Turns:         0,
		Timeout:       clampTimeout(opts.Timeout),
		Kind:          opts.Kind,
		FlowID:        opts.FlowID,
		// Position is set under the lock after scanning existing backlog tasks.
		CreatedAt: now,
		UpdatedAt: now,
	}

	if task.Title == "" {
		task.Title = DeriveTitle(opts.Prompt)
	}

	// Tags: deep-copy to protect store state from caller mutation.
	if len(opts.Tags) > 0 {
		task.Tags = append([]string(nil), opts.Tags...)
	}

	task.SpecSourcePath = opts.SpecSourcePath

	// Sandbox.
	if opts.Sandbox != "" {
		task.Sandbox = harness.NormalizeID(string(opts.Sandbox))
	}

	// SandboxByActivity: normalize (validates keys, strips invalid entries).
	if len(opts.SandboxByActivity) > 0 {
		task.SandboxByActivity = normalizeSandboxByActivity(opts.SandboxByActivity)
	}

	// Budget limits: clamp negatives to 0 (0 means unlimited).
	if opts.MaxCostUSD < 0 {
		task.MaxCostUSD = 0
	} else {
		task.MaxCostUSD = opts.MaxCostUSD
	}
	task.MaxInputTokens = max(opts.MaxInputTokens, 0)

	// ModelOverride: nil when empty so omitempty keeps JSON clean.
	if model := strings.TrimSpace(opts.ModelOverride); model != "" {
		task.ModelOverride = &model
	}

	// Build the search index entry before acquiring the lock.  Position is not
	// a search-indexed field, so the entry is fully accurate even though
	// task.Position has not been set yet.
	entry := buildIndexEntry(task, "")

	// Create the task directory outside the lock; the directory name is derived
	// from the UUID which is already fixed, so no race is possible.
	if err := s.backend.Init(task.ID); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Compute top-of-backlog position under the lock.
	// New tasks get position = (minimum existing backlog position) - 1 so
	// they appear at the top of the backlog column in the UI. When no
	// backlog tasks exist, position defaults to 0.
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
	if hasBacklog {
		task.Position = minPos - 1
	}

	if err := s.saveTask(task.ID, task); err != nil {
		return nil, err
	}

	s.tasks[task.ID] = task
	s.addToStatusIndex(task.Status, task.ID)
	s.events[task.ID] = nil
	s.nextSeq[task.ID] = 1
	s.eventsLoaded[task.ID] = true
	s.searchIndex[task.ID] = entry
	s.notify(task, false)

	ret := deepCloneTask(task)
	return &ret, nil
}

// normalizeSandboxByActivity validates and normalizes a sandbox-by-activity map.
// It lowercases and trims activity keys, validates them against the allowed set,
// parses sandbox type values, and drops invalid entries. Returns nil for an
// empty result so omitempty keeps JSON clean.
func normalizeSandboxByActivity(input map[SandboxActivity]harness.ID) map[SandboxActivity]harness.ID {
	if len(input) == 0 {
		return nil
	}
	allowed := make(map[SandboxActivity]struct{}, len(SandboxActivities))
	for _, key := range SandboxActivities {
		allowed[key] = struct{}{}
	}
	out := make(map[SandboxActivity]harness.ID)
	for k, v := range input {
		key := SandboxActivity(strings.ToLower(strings.TrimSpace(string(k))))
		if _, ok := allowed[key]; !ok {
			continue
		}
		val, ok := harness.ParseID(string(v))
		if !ok {
			continue
		}
		out[key] = val
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// DeleteTask soft-deletes a task by writing a tombstone.json marker.
// The task directory is retained on disk; the task is moved from s.tasks to
// s.deleted so it no longer appears in ListTasks but can be restored.
// reason is optional human-readable context for why the task was deleted.
func (s *Store) DeleteTask(_ context.Context, id uuid.UUID, reason string) error {
	s.mu.Lock()

	t, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("task not found: %s", id)
	}
	tomb := Tombstone{DeletedAt: time.Now(), Reason: reason}
	tombData, err := json.Marshal(tomb)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("marshal tombstone: %w", err)
	}
	if err := s.backend.SaveBlob(id, "tombstone.json", tombData); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("write tombstone: %w", err)
	}
	s.removeFromStatusIndex(t.Status, id)
	delete(s.tasks, id)
	delete(s.searchIndex, id)
	s.deleted[id] = t
	s.notify(t, true) // task-deleted delta — SSE clients work unchanged
	s.mu.Unlock()
	return nil
}

// CancelTask moves a task to TaskStatusCancelled using ForceUpdateTaskStatus,
// which bypasses the normal state-machine transitions.
func (s *Store) CancelTask(ctx context.Context, id uuid.UUID) error {
	return s.ForceUpdateTaskStatus(ctx, id, TaskStatusCancelled)
}

// ListDeletedTasks returns all soft-deleted (tombstoned) tasks sorted by UpdatedAt DESC.
func (s *Store) ListDeletedTasks(_ context.Context) ([]Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Task, 0, len(s.deleted))
	for _, t := range s.deleted {
		out = append(out, deepCloneTask(t))
	}
	slices.SortFunc(out, func(a, b Task) int {
		return b.UpdatedAt.Compare(a.UpdatedAt)
	})
	return out, nil
}

// RestoreTask removes the tombstone from a soft-deleted task, moving it back
// into the active task map so it reappears in ListTasks.
// The method uses a two-phase locking strategy: first a read lock to snapshot
// the task and perform disk I/O (oversight text loading), then a write lock
// to apply the state change. The write lock re-checks the task still exists
// in s.deleted to guard against concurrent Restore or Purge calls.
func (s *Store) RestoreTask(_ context.Context, id uuid.UUID) error {
	// Snapshot the deleted task pointer under a brief read lock so the
	// oversight disk read and buildIndexEntry can run outside the write lock.
	s.mu.RLock()
	t, ok := s.deleted[id]
	if !ok {
		s.mu.RUnlock()
		return fmt.Errorf("deleted task not found: %s", id)
	}
	s.mu.RUnlock()

	// Disk I/O and CPU work outside the write lock. Loading oversight text
	// can be slow for tasks with large oversight files, so keeping this
	// outside the lock avoids blocking all concurrent readers.
	oversightRaw, _ := s.LoadOversightText(id)
	entry := buildIndexEntry(t, oversightRaw)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-check under the write lock to guard against a concurrent Restore or
	// Purge that may have removed the task from s.deleted between the read
	// lock above and now.
	if _, ok := s.deleted[id]; !ok {
		return fmt.Errorf("deleted task not found: %s", id)
	}
	if err := s.backend.DeleteBlob(id, "tombstone.json"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove tombstone: %w", err)
	}
	delete(s.deleted, id)
	s.tasks[id] = t
	s.addToStatusIndex(t.Status, id)
	s.searchIndex[id] = entry
	s.notify(t, false)
	return nil
}

// purgeTaskLocked permanently removes a tombstoned task's directory and all
// in-memory state. It can only be called on tasks already in s.deleted, and
// only while s.mu is held for writing, so the tombstone sweep in
// PurgeExpiredTombstones purges a batch under a single lock.
func (s *Store) purgeTaskLocked(id uuid.UUID) error {
	if _, ok := s.deleted[id]; !ok {
		return fmt.Errorf("no tombstoned task: %s", id)
	}
	if err := s.backend.RemoveTask(id); err != nil {
		return fmt.Errorf("purge task dir: %w", err)
	}
	delete(s.deleted, id)
	delete(s.events, id)
	delete(s.nextSeq, id)
	delete(s.eventsLoaded, id)
	return nil
}

// PurgeExpiredTombstones permanently removes all tombstoned tasks whose
// tombstone was written more than retentionDays days ago. It reads each
// tombstone file from disk to get the authoritative DeletedAt time, so that
// manually back-dated files are handled correctly. Errors for individual tasks
// are logged and skipped so a single bad file does not block the whole sweep.
func (s *Store) PurgeExpiredTombstones(retentionDays int) {
	threshold := time.Now().AddDate(0, 0, -retentionDays)

	s.mu.Lock()
	defer s.mu.Unlock()

	for id := range s.deleted {
		raw, err := s.backend.ReadBlob(id, "tombstone.json")
		if err != nil {
			logger.Store.Warn("purge: read tombstone", "task", id, "error", err)
			continue
		}
		var tomb Tombstone
		if err := json.Unmarshal(raw, &tomb); err != nil {
			logger.Store.Warn("purge: parse tombstone", "task", id, "error", err)
			continue
		}
		if tomb.DeletedAt.Before(threshold) {
			if err := s.purgeTaskLocked(id); err != nil {
				logger.Store.Warn("purge: remove task", "task", id, "error", err)
			} else {
				logger.Store.Info("purged expired tombstone", "task", id, "deleted_at", tomb.DeletedAt)
			}
		}
	}
}

// UpdateTaskStatus sets a task's status field, enforcing the state machine.
// Returns statemachine.ErrInvalidTransition if the requested transition is not allowed.
// When transitioning to TaskStatusDone, a summary.json is written atomically
// before subscribers are notified, so the file is always present by the time
// any observer sees the done state.
