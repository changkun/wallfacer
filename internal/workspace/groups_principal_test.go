package workspace_test

import (
	"path/filepath"
	"testing"

	"changkun.de/x/wallfacer/internal/workspace"
)

// TestClaimGroup_StampsPrincipalOnce: the first ClaimGroup for a
// previously-unowned group records CreatedBy + OrgID. A second
// ClaimGroup from a different principal must NOT overwrite — the
// group stays with its original owner.
func TestClaimGroup_StampsPrincipalOnce(t *testing.T) {
	dir := t.TempDir()
	if err := workspace.UpsertGroup(dir, []string{"/tmp/ws-a"}); err != nil {
		t.Fatalf("UpsertGroup: %v", err)
	}
	// First claim: alice in org-a.
	if err := workspace.ClaimGroup(dir, []string{"/tmp/ws-a"}, &workspace.Principal{Sub: "alice", OrgID: "org-a"}); err != nil {
		t.Fatalf("ClaimGroup: %v", err)
	}
	// Second claim with bob must be a no-op.
	if err := workspace.ClaimGroup(dir, []string{"/tmp/ws-a"}, &workspace.Principal{Sub: "bob", OrgID: "org-b"}); err != nil {
		t.Fatalf("ClaimGroup second: %v", err)
	}
	got, err := workspace.LoadGroups(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("groups len = %d, want 1", len(got))
	}
	if got[0].CreatedBy != "alice" || got[0].OrgID != "org-a" {
		t.Errorf("got CreatedBy=%q OrgID=%q, want alice/org-a", got[0].CreatedBy, got[0].OrgID)
	}
}

// TestClaimGroup_SurvivesReload writes a claim, reloads from disk,
// confirms the fields round-trip through the JSON layer.
func TestClaimGroup_SurvivesReload(t *testing.T) {
	dir := t.TempDir()
	if err := workspace.UpsertGroup(dir, []string{"/tmp/ws-b"}); err != nil {
		t.Fatal(err)
	}
	if err := workspace.ClaimGroup(dir, []string{"/tmp/ws-b"}, &workspace.Principal{Sub: "alice", OrgID: "org-a"}); err != nil {
		t.Fatal(err)
	}
	// Manually reload by opening the file from a sibling temp var.
	g2, err := workspace.LoadGroups(filepath.Clean(dir))
	if err != nil {
		t.Fatal(err)
	}
	if g2[0].CreatedBy != "alice" || g2[0].OrgID != "org-a" {
		t.Errorf("round-trip lost fields: %+v", g2[0])
	}
}

// TestClaimGroup_NilPrincipalIsNoOp covers the local-mode path
// where ClaimGroup is called without claims in context.
func TestClaimGroup_NilPrincipalIsNoOp(t *testing.T) {
	dir := t.TempDir()
	if err := workspace.UpsertGroup(dir, []string{"/tmp/ws-c"}); err != nil {
		t.Fatal(err)
	}
	if err := workspace.ClaimGroup(dir, []string{"/tmp/ws-c"}, nil); err != nil {
		t.Fatalf("nil principal should not error: %v", err)
	}
	got, _ := workspace.LoadGroups(dir)
	if got[0].CreatedBy != "" || got[0].OrgID != "" {
		t.Errorf("nil principal leaked into group: %+v", got[0])
	}
}

// fixture builds a groups slice spanning the three shapes.
func fixture() []workspace.Group {
	return []workspace.Group{
		{Workspaces: []string{"/legacy"}},                                               // legacy
		{Workspaces: []string{"/alice-personal"}, CreatedBy: "alice"},                   // alice's personal
		{Workspaces: []string{"/bob-personal"}, CreatedBy: "bob"},                       // bob's personal
		{Workspaces: []string{"/org-a-shared"}, CreatedBy: "alice", OrgID: "org-a"},     // org-a
		{Workspaces: []string{"/org-a-other"}, CreatedBy: "contractor", OrgID: "org-a"}, // org-a (different owner)
		{Workspaces: []string{"/org-b-shared"}, CreatedBy: "bob", OrgID: "org-b"},       // org-b
	}
}

// TestGroupsForPrincipal_NilShowsAll covers local-mode.
func TestGroupsForPrincipal_NilShowsAll(t *testing.T) {
	groups := fixture()
	got := workspace.GroupsForPrincipal(groups, nil)
	if len(got) != len(groups) {
		t.Fatalf("nil principal filtered %d → %d", len(groups), len(got))
	}
}

// TestGroupsForPrincipal_AlicePersonal covers alice signed in with
// no active org: she sees legacy + her personal, nothing else.
func TestGroupsForPrincipal_AlicePersonal(t *testing.T) {
	got := workspace.GroupsForPrincipal(fixture(), &workspace.Principal{Sub: "alice"})
	wantPaths := []string{"/legacy", "/alice-personal"}
	if len(got) != len(wantPaths) {
		t.Fatalf("alice personal view saw %d groups, want %d: %v", len(got), len(wantPaths), got)
	}
	seen := map[string]bool{}
	for _, g := range got {
		seen[g.Workspaces[0]] = true
	}
	for _, p := range wantPaths {
		if !seen[p] {
			t.Errorf("alice personal view missing %s", p)
		}
	}
}

// TestGroupsForPrincipal_AliceInOrgA: alice active in org-a sees
// legacy + all org-a groups (both hers and contractor's) +
// her own personal. Bob's personal and org-b stay hidden.
func TestGroupsForPrincipal_AliceInOrgA(t *testing.T) {
	got := workspace.GroupsForPrincipal(fixture(), &workspace.Principal{Sub: "alice", OrgID: "org-a"})
	wantPaths := []string{"/legacy", "/alice-personal", "/org-a-shared", "/org-a-other"}
	if len(got) != len(wantPaths) {
		t.Fatalf("alice@org-a saw %d groups, want %d: %+v", len(got), len(wantPaths), got)
	}
	seen := map[string]bool{}
	for _, g := range got {
		seen[g.Workspaces[0]] = true
	}
	for _, p := range wantPaths {
		if !seen[p] {
			t.Errorf("alice@org-a missing %s", p)
		}
	}
	for _, g := range got {
		if g.Workspaces[0] == "/bob-personal" {
			t.Error("leaked bob's personal into alice's view")
		}
		if g.Workspaces[0] == "/org-b-shared" {
			t.Error("leaked org-b group into org-a view")
		}
	}
}

// TestGroupsForPrincipal_FreshUserInOrgSeesEmpty is the UX you
// described: a user who signed in to a fresh org with no claimed
// workspace groups yet should see an empty list (plus legacy).
func TestGroupsForPrincipal_FreshUserInOrgSeesEmpty(t *testing.T) {
	// No legacy; only alice's stuff and an org-a record.
	groups := []workspace.Group{
		{Workspaces: []string{"/alice-personal"}, CreatedBy: "alice"},
		{Workspaces: []string{"/org-a-shared"}, CreatedBy: "alice", OrgID: "org-a"},
	}
	got := workspace.GroupsForPrincipal(groups, &workspace.Principal{Sub: "carol", OrgID: "org-c"})
	if len(got) != 0 {
		t.Fatalf("fresh user in empty org saw %d groups, want 0: %v", len(got), got)
	}
}
