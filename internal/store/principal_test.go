package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"changkun.de/x/wallfacer/internal/store"
	"github.com/google/uuid"
)

// newFiltStore returns a FileStore and a helper that inserts a task with
// the given OrgID so individual tests can populate fixtures with one
// line each. Using FileStore (not an in-memory fake) exercises the real
// JSON round-trip for the new fields.
func newFiltStore(t *testing.T) (*store.Store, func(orgID, createdBy string) uuid.UUID) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	s, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	t.Cleanup(s.WaitCompaction)

	insert := func(orgID, createdBy string) uuid.UUID {
		t.Helper()
		task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{
			Prompt:    "test-" + orgID,
			Timeout:   120,
			OrgID:     orgID,
			CreatedBy: createdBy,
		})
		if err != nil {
			t.Fatalf("CreateTaskWithOptions: %v", err)
		}
		return task.ID
	}
	return s, insert
}

// TestTask_CreatedByAndOrgID_RoundTrip confirms the two new fields
// survive a write + close + reopen cycle. Catches accidental drops
// from the JSON tag layout or the clone helper generator.
func TestTask_CreatedByAndOrgID_RoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	task, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{
		Prompt:    "hello",
		Timeout:   120,
		OrgID:     "org-42",
		CreatedBy: "user-abc",
	})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}
	id := task.ID
	s.WaitCompaction()

	// Reopen from disk.
	s2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore (reopen): %v", err)
	}
	t.Cleanup(s2.WaitCompaction)

	got, err := s2.GetTask(context.Background(), id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.OrgID != "org-42" {
		t.Errorf("OrgID = %q, want org-42", got.OrgID)
	}
	if got.CreatedBy != "user-abc" {
		t.Errorf("CreatedBy = %q, want user-abc", got.CreatedBy)
	}
}

// TestTasksForPrincipal_NilReturnsAll mirrors local-mode behavior:
// a nil principal sees every task regardless of OrgID.
func TestTasksForPrincipal_NilReturnsAll(t *testing.T) {
	s, insert := newFiltStore(t)
	insert("", "")
	insert("org-a", "user-a")
	insert("org-b", "user-b")

	got := s.TasksForPrincipal(context.Background(), nil, false)
	if len(got) != 3 {
		t.Fatalf("nil principal saw %d tasks, want 3", len(got))
	}
}

// TestTasksForPrincipal_OrgIsolatesFromOtherOrgs covers the core
// multi-tenant contract: a caller scoped to orgA never sees orgB's
// records and never sees anonymous legacy records.
func TestTasksForPrincipal_OrgIsolatesFromOtherOrgs(t *testing.T) {
	s, insert := newFiltStore(t)
	insert("", "")                          // legacy anonymous, now visible
	insert("org-a", "alice")                // visible
	anotherAID := insert("org-a", "alice2") // visible
	insert("org-b", "bob")                  // hidden

	got := s.TasksForPrincipal(context.Background(), &store.Principal{OrgID: "org-a"}, false)
	if len(got) != 3 {
		t.Fatalf("orgA saw %d tasks, want 3 (2 org-a + 1 legacy)", len(got))
	}
	for _, task := range got {
		if task.OrgID != "org-a" && task.OrgID != "" {
			t.Errorf("leaked task OrgID=%q into org-a view", task.OrgID)
		}
	}

	// And a sanity check that the second orgA task is in the set.
	foundSecond := false
	for _, task := range got {
		if task.ID == anotherAID {
			foundSecond = true
		}
	}
	if !foundSecond {
		t.Error("second orgA task missing from filtered list")
	}
}

// TestTasksForPrincipal_LegacyTasksVisibleAfterCloudSignIn is the
// regression guard for the "tasks disappeared" UX bug: a user who
// turns cloud mode on and signs in with an org must still see the
// tasks they created in local mode (OrgID=""). The strict-isolation
// original behavior was correct per spec but shipped a broken
// migration story; this test locks in the relaxed filter.
func TestTasksForPrincipal_LegacyTasksVisibleAfterCloudSignIn(t *testing.T) {
	s, insert := newFiltStore(t)
	for i := 0; i < 3; i++ {
		insert("", "") // three legacy local-mode tasks
	}

	got := s.TasksForPrincipal(context.Background(), &store.Principal{OrgID: "org-fresh"}, false)
	if len(got) != 3 {
		t.Fatalf("signed-in user sees %d legacy tasks, want 3", len(got))
	}
}

// TestTasksForPrincipal_NoOrgSeesOnlyAnonymous covers the signed-in
// user who has no current org context: they see their own anonymous
// records, not other orgs' data.
func TestTasksForPrincipal_NoOrgSeesOnlyAnonymous(t *testing.T) {
	s, insert := newFiltStore(t)
	insert("", "alice")
	insert("", "alice") // two legacy anonymous records
	insert("org-a", "alice")

	got := s.TasksForPrincipal(context.Background(), &store.Principal{Sub: "alice"}, false)
	if len(got) != 2 {
		t.Fatalf("no-org principal saw %d tasks, want 2", len(got))
	}
	for _, task := range got {
		if task.OrgID != "" {
			t.Errorf("leaked orgA task into anonymous view: %+v", task)
		}
	}
}

// TestTasksForPrincipal_ArchivedFlag confirms includeArchived behaves
// the same as ListTasks: false excludes archived records.
func TestTasksForPrincipal_ArchivedFlag(t *testing.T) {
	s, _ := newFiltStore(t)

	liveTask, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{
		Prompt: "live", Timeout: 120, OrgID: "org-a",
	})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}
	live := liveTask.ID

	archTask, err := s.CreateTaskWithOptions(context.Background(), store.TaskCreateOptions{
		Prompt: "archived", Timeout: 120, OrgID: "org-a",
	})
	if err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}
	if err := s.SetTaskArchived(context.Background(), archTask.ID, true); err != nil {
		t.Fatalf("SetTaskArchived: %v", err)
	}

	got := s.TasksForPrincipal(context.Background(), &store.Principal{OrgID: "org-a"}, false)
	if len(got) != 1 || got[0].ID != live {
		t.Fatalf("archived=false saw %d tasks, want 1 live", len(got))
	}
	got = s.TasksForPrincipal(context.Background(), &store.Principal{OrgID: "org-a"}, true)
	if len(got) != 2 {
		t.Fatalf("archived=true saw %d tasks, want 2", len(got))
	}
}
