package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/logger"
	"github.com/google/uuid"
)

type TaskCreateOptions struct {
	// ID is an optional pre-assigned UUID. When zero, a new UUID is generated.
	ID                uuid.UUID
	Prompt            string
	Timeout           int
	MountWorktrees    bool
	Kind              TaskKind
	Tags              []string
	Sandbox           string
	SandboxByActivity map[string]string
	MaxCostUSD        float64
	MaxInputTokens    int
	ScheduledAt       *time.Time
	DependsOn         []string
	ModelOverride     string
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
	task := &Task{
		SchemaVersion:  CurrentTaskSchemaVersion,
		ID:             id,
		Prompt:         opts.Prompt,
		Status:         TaskStatusBacklog,
		Turns:          0,
		Timeout:        clampTimeout(opts.Timeout),
		MountWorktrees: opts.MountWorktrees,
		Kind:           opts.Kind,
		// Position is set under the lock after scanning existing backlog tasks.
		CreatedAt: now,
		UpdatedAt: now,
		// AutoRetryBudget provides per-category retry allowances for transient
		// failures. Budget is only granted for categories where retrying is safe.
		AutoRetryBudget: map[FailureCategory]int{
			FailureCategoryContainerCrash: 2,
			FailureCategorySyncError:      2,
			FailureCategoryWorktree:       1,
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
		task.Sandbox = strings.TrimSpace(opts.Sandbox)
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

// CreateForkedTask creates a new backlog task pre-populated with the source's
// sandbox preference and ForkedFrom reference. The caller is responsible for
// calling runner.Fork to set up worktrees before starting the task.
func (s *Store) CreateForkedTask(_ context.Context, sourceID uuid.UUID, prompt string, timeout int) (*Task, error) {
	// Snapshot the source task's sandbox fields under a brief read lock so
	// that the task struct and search index entry can be built outside the
	// write lock, reducing the time the write lock is held.
	s.mu.RLock()
	source, ok := s.tasks[sourceID]
	if !ok {
		s.mu.RUnlock()
		return nil, fmt.Errorf("source task %s not found", sourceID)
	}
	sandboxSnapshot := source.Sandbox
	var sbaSnapshot map[string]string
	if len(source.SandboxByActivity) > 0 {
		sbaSnapshot = make(map[string]string, len(source.SandboxByActivity))
		for k, v := range source.SandboxByActivity {
			sbaSnapshot[k] = v
		}
	}
	s.mu.RUnlock()

	// Build the task struct outside any lock.  Position is set under the write
	// lock below; it is not a search-indexed field.
	timeout = clampTimeout(timeout)
	id := uuid.New()
	fid := sourceID // copy for pointer
	now := time.Now()
	task := &Task{
		SchemaVersion:      CurrentTaskSchemaVersion,
		ID:                 id,
		Prompt:             prompt,
		Status:             TaskStatusBacklog,
		Timeout:            timeout,
		Sandbox:            sandboxSnapshot,
		SandboxByActivity:  sbaSnapshot,
		ForkedFrom:         &fid,
		// Position set under lock below.
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	// Build the search index entry before acquiring the write lock.
	entry := buildIndexEntry(task, "")

	// Create the task directory outside the lock.
	taskDir := filepath.Join(s.dir, id.String())
	tracesDir := filepath.Join(taskDir, "traces")
	if err := os.MkdirAll(tracesDir, 0755); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Compute top-of-backlog position (same logic as CreateTask).
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

	if err := s.saveTask(id, task); err != nil {
		return nil, err
	}
	s.tasks[id] = task
	s.addToStatusIndex(task.Status, id)
	s.events[id] = nil
	s.nextSeq[id] = 1
	s.searchIndex[id] = entry
	s.notify(task, false)

	ret := deepCloneTask(task)
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

// DeleteTask soft-deletes a task by writing a tombstone.json marker.
// The task directory is retained on disk; the task is moved from s.tasks to
// s.deleted so it no longer appears in ListTasks but can be restored.
// reason is optional human-readable context for why the task was deleted.
func (s *Store) DeleteTask(_ context.Context, id uuid.UUID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	tomb := Tombstone{DeletedAt: time.Now(), Reason: reason}
	tombPath := filepath.Join(s.dir, id.String(), "tombstone.json")
	if err := atomicWriteJSON(tombPath, tomb); err != nil {
		return fmt.Errorf("write tombstone: %w", err)
	}
	s.removeFromStatusIndex(t.Status, id)
	delete(s.tasks, id)
	delete(s.searchIndex, id)
	s.deleted[id] = t
	s.notify(t, true) // task-deleted delta — SSE clients work unchanged
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
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
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
