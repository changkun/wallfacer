package workspace_test

import (
	"testing"

	"latere.ai/x/wallfacer/internal/workspace"
)

// fixture builds a groups slice spanning the three shapes.
func fixture() []workspace.Workspace {
	return []workspace.Workspace{
		{Folders: []string{"/legacy"}},                                               // legacy
		{Folders: []string{"/alice-personal"}, CreatedBy: "alice"},                   // alice's personal
		{Folders: []string{"/bob-personal"}, CreatedBy: "bob"},                       // bob's personal
		{Folders: []string{"/org-a-shared"}, CreatedBy: "alice", OrgID: "org-a"},     // org-a
		{Folders: []string{"/org-a-other"}, CreatedBy: "contractor", OrgID: "org-a"}, // org-a (different owner)
		{Folders: []string{"/org-b-shared"}, CreatedBy: "bob", OrgID: "org-b"},       // org-b
	}
}

// TestGroupsForPrincipal_NilShowsAll covers local-mode.
func TestGroupsForPrincipal_NilShowsAll(t *testing.T) {
	groups := fixture()
	got := workspace.WorkspacesForPrincipal(groups, nil)
	if len(got) != len(groups) {
		t.Fatalf("nil principal filtered %d → %d", len(groups), len(got))
	}
}

// TestGroupsForPrincipal_AlicePersonal covers alice signed in with
// no active org: she sees legacy + her personal, nothing else.
func TestGroupsForPrincipal_AlicePersonal(t *testing.T) {
	got := workspace.WorkspacesForPrincipal(fixture(), &workspace.Principal{Sub: "alice"})
	wantPaths := []string{"/legacy", "/alice-personal"}
	if len(got) != len(wantPaths) {
		t.Fatalf("alice personal view saw %d groups, want %d: %v", len(got), len(wantPaths), got)
	}
	seen := map[string]bool{}
	for _, g := range got {
		seen[g.Folders[0]] = true
	}
	for _, p := range wantPaths {
		if !seen[p] {
			t.Errorf("alice personal view missing %s", p)
		}
	}
}

// TestGroupsForPrincipal_AliceInOrgA: alice in org-a sees only
// org-a groups. Her own personal, legacy, other users' personal,
// other orgs — all hidden. Org view is strict.
func TestGroupsForPrincipal_AliceInOrgA(t *testing.T) {
	got := workspace.WorkspacesForPrincipal(fixture(), &workspace.Principal{Sub: "alice", OrgID: "org-a"})
	wantPaths := []string{"/org-a-shared", "/org-a-other"}
	if len(got) != len(wantPaths) {
		t.Fatalf("alice@org-a saw %d groups, want %d (strict org): %+v", len(got), len(wantPaths), got)
	}
	for _, g := range got {
		if g.OrgID != "org-a" {
			t.Errorf("leaked non-org-a group into alice's view: %+v", g)
		}
	}
}

// TestGroupsForPrincipal_FreshUserInOrgSeesEmpty is the UX you
// described: a user who signed in to a fresh org with no claimed
// workspace groups yet should see an empty list (plus legacy).
func TestGroupsForPrincipal_FreshUserInOrgSeesEmpty(t *testing.T) {
	// No legacy; only alice's stuff and an org-a record.
	groups := []workspace.Workspace{
		{Folders: []string{"/alice-personal"}, CreatedBy: "alice"},
		{Folders: []string{"/org-a-shared"}, CreatedBy: "alice", OrgID: "org-a"},
	}
	got := workspace.WorkspacesForPrincipal(groups, &workspace.Principal{Sub: "carol", OrgID: "org-c"})
	if len(got) != 0 {
		t.Fatalf("fresh user in empty org saw %d groups, want 0: %v", len(got), got)
	}
}
