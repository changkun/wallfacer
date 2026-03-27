package runner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
)

// mockLister is a test double for ContainerLister.
type mockLister struct {
	containers []sandbox.ContainerInfo
	err        error
}

func (m *mockLister) ListContainers() ([]sandbox.ContainerInfo, error) {
	return m.containers, m.err
}

// eventTypes extracts EventType values from a slice of TaskEvents in order.
func eventTypes(events []store.TaskEvent) []store.EventType {
	out := make([]store.EventType, len(events))
	for i, e := range events {
		out[i] = e.EventType
	}
	return out
}

func TestRecoverOrphanedTasks(t *testing.T) {
	cases := []struct {
		name                 string
		initialStatus        store.TaskStatus
		useTaskIDAsContainer bool // if true, include the task's own ID in the running container list
		listErr              error
		wantStatus           store.TaskStatus
		wantEventTypes       []store.EventType
	}{
		{
			name:          "committing with no worktree paths becomes failed",
			initialStatus: store.TaskStatusCommitting,
			wantStatus:    store.TaskStatusFailed,
			wantEventTypes: []store.EventType{
				store.EventTypeError,
				store.EventTypeStateChange,
			},
		},
		{
			name:          "in_progress with no container moves to waiting",
			initialStatus: store.TaskStatusInProgress,
			// containerIDs is empty — task not in running list
			wantStatus: store.TaskStatusWaiting,
			wantEventTypes: []store.EventType{
				store.EventTypeSystem,
				store.EventTypeStateChange,
			},
		},
		{
			name:                 "in_progress with running container stays in_progress",
			initialStatus:        store.TaskStatusInProgress,
			useTaskIDAsContainer: true,
			wantStatus:           store.TaskStatusInProgress,
			wantEventTypes: []store.EventType{
				store.EventTypeSystem,
			},
		},
		{
			name:          "list error treats all in_progress as stopped",
			initialStatus: store.TaskStatusInProgress,
			listErr:       errors.New("runtime unavailable"),
			wantStatus:    store.TaskStatusWaiting,
			wantEventTypes: []store.EventType{
				store.EventTypeSystem,
				store.EventTypeStateChange,
			},
		},
		{
			name:           "backlog task is untouched",
			initialStatus:  store.TaskStatusBacklog,
			wantStatus:     store.TaskStatusBacklog,
			wantEventTypes: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 1. Create store in a temp directory.
			s, err := store.NewFileStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// 2. Create a task and set its initial status.
			task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 5})
			if err != nil {
				t.Fatal(err)
			}
			if tc.initialStatus != store.TaskStatusBacklog {
				if err := s.ForceUpdateTaskStatus(ctx, task.ID, tc.initialStatus); err != nil {
					t.Fatal(err)
				}
			}

			// 3. Build the mock lister.
			var containers []sandbox.ContainerInfo
			if tc.useTaskIDAsContainer {
				containers = []sandbox.ContainerInfo{
					{TaskID: task.ID.String(), State: "running"},
				}
			}
			lister := &mockLister{containers: containers, err: tc.listErr}

			// 4. Call RecoverOrphanedTasks. Cancel the context immediately
			// after to prevent any spawned monitor goroutine from outliving
			// the test (it exits silently on cancellation).
			RecoverOrphanedTasks(ctx, s, lister)
			cancel()

			// 5. Assert final task status.
			updated, err := s.GetTask(context.Background(), task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if updated.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", updated.Status, tc.wantStatus)
			}

			// 6. Assert events contain exactly the expected EventTypes in order.
			events, err := s.GetEvents(context.Background(), task.ID)
			if err != nil {
				t.Fatal(err)
			}
			got := eventTypes(events)
			if len(tc.wantEventTypes) == 0 {
				if len(got) != 0 {
					t.Errorf("expected no events, got %v", got)
				}
				return
			}
			if len(got) != len(tc.wantEventTypes) {
				t.Errorf("event types = %v, want %v", got, tc.wantEventTypes)
				return
			}
			for i, want := range tc.wantEventTypes {
				if got[i] != want {
					t.Errorf("event[%d] = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

func TestRecoverOrphanedTasks_InProgressMissingWorktreeBecomesFailed(t *testing.T) {
	s, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test task", Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{"/repo": filepath.Join(t.TempDir(), "missing-wt")}, "task/branch"); err != nil {
		t.Fatal(err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}

	RecoverOrphanedTasks(ctx, s, &mockLister{})

	updated, err := s.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != store.TaskStatusFailed {
		t.Fatalf("status = %q, want %q", updated.Status, store.TaskStatusFailed)
	}
	if updated.FailureCategory != store.FailureCategoryWorktree {
		t.Fatalf("failure_category = %q, want %q", updated.FailureCategory, store.FailureCategoryWorktree)
	}

	events, err := s.GetEvents(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := eventTypes(events)
	want := []store.EventType{store.EventTypeError, store.EventTypeStateChange}
	if len(got) != len(want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRecoverOrphanedTasks_CommittingGitCheck verifies the git-based recovery
// path: tasks in committing state are promoted to done when a commit on the
// task branch has a timestamp after the task's UpdatedAt, and marked failed
// when no such commit exists.
func TestRecoverOrphanedTasks_CommittingGitCheck(t *testing.T) {
	const branchName = "task/test-recovery"

	// newCommittingTask creates a store and a task in committing state with
	// WorktreePaths pointing to repoDir/branchName.
	// It returns the task (with the final UpdatedAt) and the store.
	newCommittingTask := func(t *testing.T, repoDir string) (*store.Task, *store.Store) {
		t.Helper()
		ctx := context.Background()
		s, err := store.NewFileStore(t.TempDir())
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "test prompt", Timeout: 0, Kind: store.TaskKindTask})
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		if err := s.UpdateTaskWorktrees(ctx, task.ID,
			map[string]string{repoDir: repoDir}, branchName); err != nil {
			t.Fatalf("UpdateTaskWorktrees: %v", err)
		}
		// Set status last so UpdatedAt reflects the committing transition.
		if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusCommitting); err != nil {
			t.Fatalf("ForceUpdateTaskStatus: %v", err)
		}
		updated, err := s.GetTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		return updated, s
	}

	t.Run("promotes to done when commit landed after UpdatedAt", func(t *testing.T) {
		repoDir := setupTestRepo(t)
		gitRun(t, repoDir, "checkout", "-b", branchName)
		gitRun(t, repoDir, "checkout", "main")

		task, s := newCommittingTask(t, repoDir)

		// Make a commit on the task branch with an author date strictly after
		// the task's UpdatedAt so BranchTipCommit returns a later timestamp.
		futureDate := task.UpdatedAt.Add(2 * time.Second).UTC().Format(time.RFC3339)
		gitRun(t, repoDir, "checkout", branchName)
		commitCmd := gitCmdWithEnv(repoDir,
			[]string{
				"GIT_AUTHOR_DATE=" + futureDate,
				"GIT_COMMITTER_DATE=" + futureDate,
			},
			"commit", "--allow-empty", "-m", "task work done",
		)
		if out, err := commitCmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit: %v\n%s", err, out)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		RecoverOrphanedTasks(ctx, s, &mockLister{})

		got, err := s.GetTask(context.Background(), task.ID)
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		if got.Status != store.TaskStatusDone {
			t.Errorf("status = %q, want %q", got.Status, store.TaskStatusDone)
		}
	})

	t.Run("marks as failed when no post-UpdatedAt commit exists", func(t *testing.T) {
		repoDir := setupTestRepo(t)
		// Create the task branch at the current HEAD — the branch's only commit
		// (the initial one) was made before the task's UpdatedAt, so the
		// commit pipeline did not complete.
		gitRun(t, repoDir, "checkout", "-b", branchName)
		gitRun(t, repoDir, "checkout", "main")

		_, s := newCommittingTask(t, repoDir)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		RecoverOrphanedTasks(ctx, s, &mockLister{})

		tasks, err := s.ListTasks(context.Background(), true)
		if err != nil || len(tasks) != 1 {
			t.Fatalf("ListTasks: %v, len=%d", err, len(tasks))
		}
		if tasks[0].Status != store.TaskStatusFailed {
			t.Errorf("status = %q, want %q", tasks[0].Status, store.TaskStatusFailed)
		}
	})
}

// gitCmdWithEnv constructs an exec.Cmd for a git command in dir with
// additional environment variables prepended to the current environment.
func gitCmdWithEnv(dir string, extraEnv []string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), extraEnv...)
	return cmd
}

// listerResponse holds one canned response for sequenceLister.
type listerResponse struct {
	containers []sandbox.ContainerInfo
	err        error
}

// sequenceLister is a stateful ContainerLister that cycles through a fixed
// sequence of responses, repeating the last entry once the sequence is
// exhausted. Safe for concurrent use.
type sequenceLister struct {
	mu  sync.Mutex
	seq []listerResponse
	pos int
}

func (s *sequenceLister) ListContainers() ([]sandbox.ContainerInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.pos
	if idx >= len(s.seq) {
		idx = len(s.seq) - 1
	} else {
		s.pos++
	}
	r := s.seq[idx]
	return r.containers, r.err
}

// setupInProgressTask creates a store in a temp directory and a task whose
// status has been forced to in_progress. Returns the store and the task.
func setupInProgressTask(t *testing.T) (*store.Store, *store.Task) {
	t.Helper()
	s, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "monitor test", Timeout: 5})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusInProgress); err != nil {
		t.Fatalf("ForceUpdateTaskStatus: %v", err)
	}
	task, err = s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return s, task
}

// TestMonitorContainerUntilStoppedWithConfig_ContextCancel verifies that
// cancelling the parent context causes the monitor to exit silently without
// altering task status or inserting any events.
func TestMonitorContainerUntilStoppedWithConfig_ContextCancel(t *testing.T) {
	s, task := setupInProgressTask(t)

	// Lister always reports the container as running.
	lister := &mockLister{containers: []sandbox.ContainerInfo{
		{TaskID: task.ID.String(), State: "running"},
	}}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		monitorContainerUntilStoppedWithConfig(ctx, s, lister, task.ID, 100*time.Millisecond, time.Hour)
	}()

	// Cancel before the first poll tick fires.
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("monitor goroutine did not exit after context cancellation")
	}

	// Task must remain in_progress with no events.
	got, err := s.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != store.TaskStatusInProgress {
		t.Errorf("status = %q, want in_progress", got.Status)
	}

	events, err := s.GetEvents(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events after context cancel, got %d: %v", len(events), eventTypes(events))
	}
}

// TestMonitorContainerUntilStoppedWithConfig_Timeout verifies that when the
// container never stops within maxWait, the monitor transitions the task from
// in_progress to waiting and emits exactly one system + one state_change event.
func TestMonitorContainerUntilStoppedWithConfig_Timeout(t *testing.T) {
	s, task := setupInProgressTask(t)

	// Lister always reports the container as running — it will never stop.
	lister := &mockLister{containers: []sandbox.ContainerInfo{
		{TaskID: task.ID.String(), State: "running"},
	}}

	// Short intervals so the test finishes quickly.
	monitorContainerUntilStoppedWithConfig(
		context.Background(), s, lister, task.ID,
		10*time.Millisecond, // pollInterval
		60*time.Millisecond, // maxWait — triggers timeout path
	)

	got, err := s.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("status = %q, want waiting", got.Status)
	}

	wantEvents := []store.EventType{store.EventTypeSystem, store.EventTypeStateChange}
	events, err := s.GetEvents(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	got2 := eventTypes(events)
	if len(got2) != len(wantEvents) {
		t.Fatalf("event types = %v, want %v", got2, wantEvents)
	}
	for i, want := range wantEvents {
		if got2[i] != want {
			t.Errorf("event[%d] = %q, want %q", i, got2[i], want)
		}
	}
}

func TestMonitorContainerUntilStoppedWithConfig_MissingWorktreeBecomesFailed(t *testing.T) {
	s, task := setupInProgressTask(t)

	if err := s.UpdateTaskWorktrees(context.Background(), task.ID,
		map[string]string{"/repo": filepath.Join(t.TempDir(), "missing-wt")}, "task/branch"); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}

	lister := &mockLister{}

	monitorContainerUntilStoppedWithConfig(
		context.Background(), s, lister, task.ID,
		10*time.Millisecond,
		time.Hour,
	)

	got, err := s.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != store.TaskStatusFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.FailureCategory != store.FailureCategoryWorktree {
		t.Errorf("failure_category = %q, want %q", got.FailureCategory, store.FailureCategoryWorktree)
	}

	wantEvents := []store.EventType{store.EventTypeError, store.EventTypeStateChange}
	events, err := s.GetEvents(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	gotTypes := eventTypes(events)
	if len(gotTypes) != len(wantEvents) {
		t.Fatalf("event types = %v, want %v", gotTypes, wantEvents)
	}
	for i, want := range wantEvents {
		if gotTypes[i] != want {
			t.Errorf("event[%d] = %q, want %q", i, gotTypes[i], want)
		}
	}
}

// TestMonitorContainerUntilStoppedWithConfig_AlreadyTransitioned verifies that
// when the task has already been moved to a terminal state (e.g. cancelled) by
// another code path, the monitor does not overwrite that status when it
// observes the container stopping.
func TestMonitorContainerUntilStoppedWithConfig_AlreadyTransitioned(t *testing.T) {
	s, task := setupInProgressTask(t)

	// Simulate a concurrent external cancellation: force the task to cancelled
	// before the monitor's transitionToWaiting runs.
	if err := s.ForceUpdateTaskStatus(context.Background(), task.ID, store.TaskStatusCancelled); err != nil {
		t.Fatalf("ForceUpdateTaskStatus to cancelled: %v", err)
	}

	// Lister reports no running containers — the monitor will see the
	// container as stopped on the first poll and call transitionToWaiting.
	lister := &mockLister{}

	monitorContainerUntilStoppedWithConfig(
		context.Background(), s, lister, task.ID,
		10*time.Millisecond,
		time.Hour,
	)

	// Task must remain cancelled — transitionToWaiting should have bailed
	// because the status was no longer in_progress.
	got, err := s.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != store.TaskStatusCancelled {
		t.Errorf("status = %q, want cancelled", got.Status)
	}

	// No events should have been inserted by the monitor.
	events, err := s.GetEvents(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d: %v", len(events), eventTypes(events))
	}
}

// TestMonitorContainerUntilStoppedWithConfig_IntermittentErrors verifies that
// transient ListContainers errors do not terminate monitoring: the monitor
// continues polling and eventually transitions the task to waiting once the
// container is observed as stopped.
func TestMonitorContainerUntilStoppedWithConfig_IntermittentErrors(t *testing.T) {
	s, task := setupInProgressTask(t)

	// Response sequence: two errors, one "still running", then stopped.
	runningEntry := listerResponse{
		containers: []sandbox.ContainerInfo{{TaskID: task.ID.String(), State: "running"}},
	}
	stoppedEntry := listerResponse{} // empty containers list → not running

	lister := &sequenceLister{
		seq: []listerResponse{
			{err: errors.New("runtime unavailable")}, // call 1: error → continue
			{err: errors.New("runtime unavailable")}, // call 2: error → continue
			runningEntry,                             // call 3: running → continue
			stoppedEntry,                             // call 4: stopped → transition
		},
	}

	monitorContainerUntilStoppedWithConfig(
		context.Background(), s, lister, task.ID,
		10*time.Millisecond,
		time.Hour,
	)

	got, err := s.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != store.TaskStatusWaiting {
		t.Errorf("status = %q, want waiting", got.Status)
	}

	wantEvents := []store.EventType{store.EventTypeSystem, store.EventTypeStateChange}
	events, err := s.GetEvents(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	gotTypes := eventTypes(events)
	if len(gotTypes) != len(wantEvents) {
		t.Fatalf("event types = %v, want %v", gotTypes, wantEvents)
	}
	for i, want := range wantEvents {
		if gotTypes[i] != want {
			t.Errorf("event[%d] = %q, want %q", i, gotTypes[i], want)
		}
	}
}
