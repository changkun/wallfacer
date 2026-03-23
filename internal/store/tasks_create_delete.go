package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/atomicfile"
	"changkun.de/x/wallfacer/internal/sandbox"
	"github.com/google/uuid"
)

// defaultAutoRetryBudget defines the per-category retry allowances granted to
// every newly created task. ResetTaskForRetry uses this same map so that a
// manual retry after a budget-exhausted auto-retry cycle restores the full
// budget, allowing the auto-retrier to act again on the next failure.
var defaultAutoRetryBudget = map[FailureCategory]int{
	FailureCategoryContainerCrash: 2,
	FailureCategorySyncError:      2,
	FailureCategoryWorktree:       1,
}

// TaskCreateOptions holds parameters for creating a new task.
type TaskCreateOptions struct {
	// ID is an optional pre-assigned UUID. When zero, a new UUID is generated.
	ID                 uuid.UUID
	Prompt             string
	Goal               string
	Timeout            int
	MountWorktrees     bool
	Kind               TaskKind
	Tags               []string
	Sandbox            sandbox.Type
	SandboxByActivity  map[SandboxActivity]sandbox.Type
	MaxCostUSD         float64
	MaxInputTokens     int
	ScheduledAt        *time.Time
	DependsOn          []string
	ModelOverride      string
	CustomPassPatterns []string
	CustomFailPatterns []string
}

// CreateTaskWithOptions creates a new backlog task in a single atomic write.
// All fields in opts are normalised (sandbox maps, budget clamps) and persisted
// together, so no watcher or SSE subscriber can observe a partially-initialised
// task.  notify is called exactly once.
func (s *Store) CreateTaskWithOptions(_ context.Context, opts TaskCreateOptions) (*Task, error) {
	// Build the task struct from opts outside the lock.  Position is the only
	// field that requires a locked scan of s.tasks; it is set inside the lock
	// below.  All search-indexed fields (Prompt, Tags) are available from opts
	// so buildIndexEntry can run before we take any lock.
	id := opts.ID
	if id == (uuid.UUID{}) {
		id = uuid.New()
	}

	now := time.Now()
	// Default Goal to the user's original prompt text so the card shows
	// their own words before any refinement takes place.
	goal := opts.Goal
	if goal == "" {
		goal = opts.Prompt
	}

	task := &Task{
		SchemaVersion:   constants.CurrentTaskSchemaVersion,
		ID:              id,
		Goal:            goal,
		GoalManuallySet: opts.Goal != "",
		Prompt:          opts.Prompt,
		Status:          TaskStatusBacklog,
		Turns:           0,
		Timeout:         clampTimeout(opts.Timeout),
		MountWorktrees:  opts.MountWorktrees,
		Kind:            opts.Kind,
		// Position is set under the lock after scanning existing backlog tasks.
		CreatedAt: now,
		UpdatedAt: now,
		// AutoRetryBudget provides per-category retry allowances for transient
		// failures. Budget is only granted for categories where retrying is safe.
		AutoRetryBudget: map[FailureCategory]int{
			FailureCategoryContainerCrash: defaultAutoRetryBudget[FailureCategoryContainerCrash],
			FailureCategorySyncError:      defaultAutoRetryBudget[FailureCategorySyncError],
			FailureCategoryWorktree:       defaultAutoRetryBudget[FailureCategoryWorktree],
		},
	}

	// Tags: deep-copy to protect store state from caller mutation.
	if len(opts.Tags) > 0 {
		task.Tags = append([]string(nil), opts.Tags...)
	}

	// DependsOn: deep-copy.
	if len(opts.DependsOn) > 0 {
		task.DependsOn = append([]string(nil), opts.DependsOn...)
	}

	// Sandbox.
	if opts.Sandbox != "" {
		task.Sandbox = sandbox.Normalize(string(opts.Sandbox))
	}

	// SandboxByActivity: normalise (validates keys, strips invalid entries).
	if len(opts.SandboxByActivity) > 0 {
		task.SandboxByActivity = normalizeSandboxByActivity(opts.SandboxByActivity)
	}

	// Budget limits: clamp negatives to 0 (0 means unlimited).
	if opts.MaxCostUSD < 0 {
		task.MaxCostUSD = 0
	} else {
		task.MaxCostUSD = opts.MaxCostUSD
	}
	if opts.MaxInputTokens < 0 {
		task.MaxInputTokens = 0
	} else {
		task.MaxInputTokens = opts.MaxInputTokens
	}

	// ScheduledAt: copy to avoid aliasing the caller's pointer.
	if opts.ScheduledAt != nil {
		ts := *opts.ScheduledAt
		task.ScheduledAt = &ts
	}

	// ModelOverride: nil when empty so omitempty keeps JSON clean.
	if model := strings.TrimSpace(opts.ModelOverride); model != "" {
		task.ModelOverride = &model
	}

	// CustomPassPatterns / CustomFailPatterns: deep-copy.
	if len(opts.CustomPassPatterns) > 0 {
		task.CustomPassPatterns = append([]string(nil), opts.CustomPassPatterns...)
	}
	if len(opts.CustomFailPatterns) > 0 {
		task.CustomFailPatterns = append([]string(nil), opts.CustomFailPatterns...)
	}

	// Build the search index entry before acquiring the lock.  Position is not
	// a search-indexed field, so the entry is fully accurate even though
	// task.Position has not been set yet.
	entry := buildIndexEntry(task, "")

	// Create the task directory outside the lock; the directory name is derived
	// from the UUID which is already fixed, so no race is possible.
	taskDir := filepath.Join(s.dir, task.ID.String())
	tracesDir := filepath.Join(taskDir, "traces")
	if err := os.MkdirAll(tracesDir, 0755); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Compute top-of-backlog position under the lock.
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

// CreateTask creates a new task in backlog status and persists it.
// kind identifies the execution mode (TaskKindTask or TaskKindIdeaAgent).
// Optional tags are attached to the task for categorisation.
//
// Deprecated: prefer CreateTaskWithOptions for full initialization in one write.
func (s *Store) CreateTask(ctx context.Context, prompt string, timeout int, mountWorktrees bool, _ string, kind TaskKind, tags ...string) (*Task, error) {
	return s.CreateTaskWithOptions(ctx, TaskCreateOptions{
		Prompt:         prompt,
		Timeout:        timeout,
		MountWorktrees: mountWorktrees,
		Kind:           kind,
		Tags:           tags,
	})
}

func normalizeSandboxByActivity(input map[SandboxActivity]sandbox.Type) map[SandboxActivity]sandbox.Type {
	if len(input) == 0 {
		return nil
	}
	allowed := make(map[SandboxActivity]struct{}, len(SandboxActivities))
	for _, key := range SandboxActivities {
		allowed[key] = struct{}{}
	}
	out := make(map[SandboxActivity]sandbox.Type)
	for k, v := range input {
		key := SandboxActivity(strings.ToLower(strings.TrimSpace(string(k))))
		if _, ok := allowed[key]; !ok {
			continue
		}
		val, ok := sandbox.Parse(string(v))
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
// After the tombstone is written, any non-terminal tasks that depend on id have
// id removed from their DependsOn list so they are no longer permanently blocked.
func (s *Store) DeleteTask(ctx context.Context, id uuid.UUID, reason string) error {
	s.mu.Lock()

	t, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("task not found: %s", id)
	}
	tomb := Tombstone{DeletedAt: time.Now(), Reason: reason}
	tombPath := filepath.Join(s.dir, id.String(), "tombstone.json")
	if err := atomicfile.WriteJSON(tombPath, tomb, 0644); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("write tombstone: %w", err)
	}
	s.removeFromStatusIndex(t.Status, id)
	delete(s.tasks, id)
	delete(s.searchIndex, id)
	s.deleted[id] = t
	s.notify(t, true) // task-deleted delta — SSE clients work unchanged
	s.mu.Unlock()

	// Clean up orphaned dependencies: any backlog/in_progress/waiting/failed task
	// that listed id in DependsOn is now permanently blocked, so remove id from
	// each dependent's DependsOn slice.
	s.removeOrphanedDependents(ctx, id)
	return nil
}

// removeOrphanedDependents finds all non-terminal tasks that depend on cancelledID
// and removes cancelledID from their DependsOn slices. This unblocks tasks whose
// only blocker was a task that is now cancelled or deleted.
func (s *Store) removeOrphanedDependents(ctx context.Context, cancelledID uuid.UUID) {
	deps, err := s.TasksDependingOn(ctx, cancelledID)
	if err != nil {
		logger.Store.Warn("orphan cleanup: list dependents", "task", cancelledID, "error", err)
		return
	}
	cancelledIDStr := cancelledID.String()
	for _, dep := range deps {
		if err := s.mutateTask(dep.ID, func(t *Task) error {
			updated := t.DependsOn[:0:0] // preserve nil if empty
			for _, idStr := range t.DependsOn {
				if idStr != cancelledIDStr {
					updated = append(updated, idStr)
				}
			}
			if len(updated) == 0 {
				t.DependsOn = nil
			} else {
				t.DependsOn = updated
			}
			return nil
		}); err != nil {
			logger.Store.Warn("orphan cleanup: remove dep", "dependent", dep.ID, "cancelled", cancelledID, "error", err)
		} else {
			logger.Store.Warn("removed orphaned dependency", "dependent", dep.ID, "cancelled_dep", cancelledID)
		}
	}
}

// CancelTask moves a task to TaskStatusCancelled using ForceUpdateTaskStatus
// (which bypasses the normal state-machine transitions) and then removes the
// task's ID from the DependsOn list of any non-terminal tasks that reference it.
// Cancelled tasks can never reach done, so their dependents would otherwise be
// permanently blocked by the auto-promoter's dependency check.
func (s *Store) CancelTask(ctx context.Context, id uuid.UUID) error {
	if err := s.ForceUpdateTaskStatus(ctx, id, TaskStatusCancelled); err != nil {
		return err
	}
	s.removeOrphanedDependents(ctx, id)
	return nil
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

	// Disk I/O and CPU work outside the write lock.
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
	tombPath := filepath.Join(s.dir, id.String(), "tombstone.json")
	if err := os.Remove(tombPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove tombstone: %w", err)
	}
	delete(s.deleted, id)
	s.tasks[id] = t
	s.addToStatusIndex(t.Status, id)
	s.searchIndex[id] = entry
	s.notify(t, false)
	return nil
}

// PurgeTask permanently removes a tombstoned task's directory and all in-memory
// state. It can only be called on tasks already in s.deleted.
func (s *Store) PurgeTask(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.purgeTaskLocked(id)
}

// purgeTaskLocked is the internal implementation of PurgeTask, called while
// s.mu is already held for writing. It is shared between PurgeTask and
// PurgeExpiredTombstones to avoid re-locking.
func (s *Store) purgeTaskLocked(id uuid.UUID) error {
	if _, ok := s.deleted[id]; !ok {
		return fmt.Errorf("no tombstoned task: %s", id)
	}
	taskDir := filepath.Join(s.dir, id.String())
	if err := os.RemoveAll(taskDir); err != nil {
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
		tombPath := filepath.Join(s.dir, id.String(), "tombstone.json")
		raw, err := os.ReadFile(tombPath)
		if err != nil {
			logger.Store.Warn("purge: read tombstone", "task", id, "error", err)
			continue
		}
		var tomb Tombstone
		if err := jsonUnmarshal(raw, &tomb); err != nil {
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
// Returns ErrInvalidTransition if the requested transition is not allowed.
// When transitioning to TaskStatusDone, a summary.json is written atomically
// before subscribers are notified, so the file is always present by the time
// any observer sees the done state.
