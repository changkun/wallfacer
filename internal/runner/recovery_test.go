package runner

import (
	"context"
	"errors"
	"testing"

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
			name:          "committing always becomes failed",
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
				if err := s.UpdateTaskStatus(ctx, task.ID, tc.initialStatus); err != nil {
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
