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

// TestTasksForPrincipal_OrgViewIsStrict: caller in org-a sees only
// org-a records. Personal + legacy + other-org records all stay
// hidden. This is the user-facing contract: switching into an org
// gives you a clean workspace, not a merged view with private data.
func TestTasksForPrincipal_OrgViewIsStrict(t *testing.T) {
	s, insert := newFiltStore(t)
	insert("", "")                          // legacy — hidden in org view
	insert("", "alice")                     // alice's personal — hidden
	insert("org-a", "alice")                // visible
	anotherAID := insert("org-a", "alice2") // visible
	insert("org-b", "bob")                  // hidden (different org)

	got := s.TasksForPrincipal(context.Background(), &store.Principal{Sub: "bob", OrgID: "org-a"}, false)
	if len(got) != 2 {
		t.Fatalf("bob@orgA saw %d tasks, want 2 (strictly org-a)", len(got))
	}
	for _, task := range got {
		if task.OrgID != "org-a" {
			t.Errorf("leaked non-org-a task into org-a view: %+v", task)
		}
	}

	// Sanity: the second orgA task is in the set.
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

// TestTasksForPrincipal_PersonalViewSeesLegacy covers the
// signed-in-to-personal case: legacy (no-owner) records are
// visible there so a single-user upgrade doesn't lose history.
// The org view no longer admits them.
func TestTasksForPrincipal_PersonalViewSeesLegacy(t *testing.T) {
	s, insert := newFiltStore(t)
	for i := 0; i < 3; i++ {
		insert("", "") // three legacy local-mode tasks
	}

	personal := s.TasksForPrincipal(context.Background(), &store.Principal{Sub: "alice"}, false)
	if len(personal) != 3 {
		t.Errorf("personal view saw %d legacy tasks, want 3", len(personal))
	}

	// Same store, now with an org-scoped caller — legacy should be
	// hidden from the org view (strict isolation).
	orgView := s.TasksForPrincipal(context.Background(), &store.Principal{OrgID: "org-fresh"}, false)
	if len(orgView) != 0 {
		t.Errorf("org view leaked %d legacy tasks", len(orgView))
	}
}

// TestTasksForPrincipal_NoOrgSeesOwnPersonalAndLegacy covers the
// signed-in user with no current org: sees their own personal-space
// records (OrgID=="", CreatedBy==Sub) PLUS legacy records (no owner),
// not other users' personal records, not other orgs.
func TestTasksForPrincipal_NoOrgSeesOwnPersonalAndLegacy(t *testing.T) {
	s, insert := newFiltStore(t)
	insert("", "")        // legacy — visible
	insert("", "alice")   // alice's personal — visible to alice
	insert("", "alice")   // alice's personal — visible to alice
	insert("", "bob")     // bob's personal — hidden from alice
	insert("org-a", "cc") // org task — hidden (alice has no org claim)

	got := s.TasksForPrincipal(context.Background(), &store.Principal{Sub: "alice"}, false)
	if len(got) != 3 {
		t.Fatalf("alice saw %d tasks, want 3 (2 personal + 1 legacy)", len(got))
	}
	for _, task := range got {
		if task.OrgID != "" {
			t.Errorf("leaked org task into alice's view: %+v", task)
		}
		if task.CreatedBy == "bob" {
			t.Errorf("leaked bob's personal task into alice's view: %+v", task)
		}
	}
}

// TestTasksForPrincipal_PersonalSpaceIsolationAcrossUsers is the
// dedicated regression guard for the "personal OrgID=” is not legacy"
// insight. Without proper owner filtering, user A's personal tasks
// would leak to user B simply because both are signed in.
func TestTasksForPrincipal_PersonalSpaceIsolationAcrossUsers(t *testing.T) {
	s, insert := newFiltStore(t)
	insert("", "alice")
	insert("", "bob")

	aliceView := s.TasksForPrincipal(context.Background(), &store.Principal{Sub: "alice"}, false)
	if len(aliceView) != 1 || aliceView[0].CreatedBy != "alice" {
		t.Fatalf("alice should see only her task, got %+v", aliceView)
	}
	bobView := s.TasksForPrincipal(context.Background(), &store.Principal{Sub: "bob"}, false)
	if len(bobView) != 1 || bobView[0].CreatedBy != "bob" {
		t.Fatalf("bob should see only his task, got %+v", bobView)
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
