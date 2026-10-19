package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/store/storetest"
)

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
		name           string
		initialStatus  store.TaskStatus
		wantStatus     store.TaskStatus
		wantEventTypes []store.EventType
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
			name:          "in_progress moves to waiting",
			initialStatus: store.TaskStatusInProgress,
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
			s, err := storetest.NewFileStore(t, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })

			ctx := t.Context()

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

			// 3. Recover.
			RecoverOrphanedTasks(ctx, s, nil)

			// 4. Assert final task status.
			updated, err := s.GetTask(context.Background(), task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if updated.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", updated.Status, tc.wantStatus)
			}

			// 5. Assert events contain exactly the expected EventTypes in order.
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
	s, err := storetest.NewFileStore(t, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := t.Context()

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

	RecoverOrphanedTasks(ctx, s, nil)

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
		s, err := storetest.NewFileStore(t, t.TempDir())
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

		ctx := t.Context()
		RecoverOrphanedTasks(ctx, s, nil)

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

		ctx := t.Context()
		RecoverOrphanedTasks(ctx, s, nil)

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

// failOnStatusBackend wraps a StorageBackend and fails SaveTask when the task
// is persisted in a chosen status, so a test can make one specific recovery
// write fail while the committing seed still succeeds.
type failOnStatusBackend struct {
	store.StorageBackend
	failStatus store.TaskStatus
}

func (b *failOnStatusBackend) SaveTask(t *store.Task) error {
	if t.Status == b.failStatus {
		return errInjectedRecoverySave
	}
	return b.StorageBackend.SaveTask(t)
}

type errInjectedRecovery string

func (e errInjectedRecovery) Error() string { return string(e) }

const errInjectedRecoverySave = errInjectedRecovery("injected save failure")

// Crash recovery used to write the committing -> failed state-change event
// whether or not the status update persisted, so a task that stayed in
// committing carried a trace asserting it had failed. Recovery is the one path
// a user cannot redrive from the UI, so the event must not outrun the write.
func TestRecoverOrphanedTasks_NoStateChangeEventWhenStatusWriteFails(t *testing.T) {
	const branchName = "task/test-recovery-save-fail"
	ctx := context.Background()

	repoDir := setupTestRepo(t)
	// Branch at HEAD: its only commit predates the task's UpdatedAt, so
	// recovery takes the "commit never landed, mark failed" path.
	gitRun(t, repoDir, "checkout", "-b", branchName)
	gitRun(t, repoDir, "checkout", "main")

	fsBackend, err := store.NewFilesystemBackend(t.TempDir())
	if err != nil {
		t.Fatalf("NewFilesystemBackend: %v", err)
	}
	s, err := storetest.NewStore(t, &failOnStatusBackend{
		StorageBackend: fsBackend,
		failStatus:     store.TaskStatusFailed,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{
		Prompt: "test prompt", Kind: store.TaskKindTask,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.UpdateTaskWorktrees(ctx, task.ID, map[string]string{repoDir: repoDir}, branchName); err != nil {
		t.Fatalf("UpdateTaskWorktrees: %v", err)
	}
	if err := s.ForceUpdateTaskStatus(ctx, task.ID, store.TaskStatusCommitting); err != nil {
		t.Fatalf("seed committing: %v", err)
	}

	RecoverOrphanedTasks(ctx, s, nil)

	events, err := s.GetEvents(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	for _, ev := range events {
		if ev.EventType != store.EventTypeStateChange {
			continue
		}
		if strings.Contains(string(ev.Data), string(store.TaskStatusFailed)) {
			t.Fatalf("recovery recorded a committing -> failed state change even though the status write failed: %s", ev.Data)
		}
	}
}
