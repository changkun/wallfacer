package runner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"changkun.de/wallfacer/internal/store"
)

// mockLister is a test double for ContainerLister.
type mockLister struct {
	containers []ContainerInfo
	err        error
}

func (m *mockLister) ListContainers() ([]ContainerInfo, error) {
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
			s, err := store.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// 2. Create a task and set its initial status.
			task, err := s.CreateTask(ctx, "test task", 5, false, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if tc.initialStatus != store.TaskStatusBacklog {
				if err := s.ForceUpdateTaskStatus(ctx, task.ID, tc.initialStatus); err != nil {
					t.Fatal(err)
				}
			}

			// 3. Build the mock lister.
			var containers []ContainerInfo
			if tc.useTaskIDAsContainer {
				containers = []ContainerInfo{
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
		s, err := store.NewStore(t.TempDir())
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		task, err := s.CreateTask(ctx, "test prompt", 0, false, "", store.TaskKindTask)
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
