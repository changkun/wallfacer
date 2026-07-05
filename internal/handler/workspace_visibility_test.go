package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"latere.ai/x/wallfacer/internal/auth"
	"latere.ai/x/wallfacer/internal/workspace"
)

// TestArchiveSpec_ForbiddenForHiddenWorkspace pins the mutation-side guard:
// a workspace-scoped mutation (spec archive) must be refused with 403 when the
// active workspace is org-stamped and the caller's principal cannot see it,
// rather than mutating a workspace /api/config reports as absent.
func TestArchiveSpec_ForbiddenForHiddenWorkspace(t *testing.T) {
	h, _, ws := newTestHandlerWithRealWorkspaceManager(t)
	// cloudMode left false: isolation is by principal presence, not deployment
	// mode, so a signed-in local caller is scoped the same as in the cloud.
	if err := workspace.SaveGroups(h.configDir, []workspace.Workspace{
		{Folders: []string{ws}, CreatedBy: "owner", OrgID: "org-a"},
	}); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{"path": "specs/foo.md"})

	// Personal caller (cannot see the org workspace): 403.
	personal := httptest.NewRequest(http.MethodPost, "/api/specs/archive", bytes.NewReader(body))
	personal = personal.WithContext(auth.WithIdentity(personal.Context(), &auth.Identity{Sub: "u", OrgID: ""}))
	pw := httptest.NewRecorder()
	h.ArchiveSpec(pw, personal)
	if pw.Code != http.StatusForbidden {
		t.Fatalf("personal caller: expected 403, got %d: %s", pw.Code, pw.Body.String())
	}

	// Owning org caller passes the visibility guard (and fails later on the
	// missing spec file, not the guard) — proving the guard is principal-scoped.
	owner := httptest.NewRequest(http.MethodPost, "/api/specs/archive", bytes.NewReader(body))
	owner = owner.WithContext(auth.WithIdentity(owner.Context(), &auth.Identity{Sub: "owner", OrgID: "org-a"}))
	ow := httptest.NewRecorder()
	h.ArchiveSpec(ow, owner)
	if ow.Code == http.StatusForbidden {
		t.Fatalf("owning org caller should pass the visibility guard, got 403: %s", ow.Body.String())
	}
}

// TestVisibleWorkspaces_LocalIsolatesByPrincipal is the reproducer for the
// per-account/org isolation fix: on a local run (cloudMode false, the default),
// an org-stamped workspace is hidden from a personal or different-org signed-in
// caller and visible only to the owning org. An anonymous caller (no session,
// the local default) still sees everything. Before the fix, local mode showed
// the workspace to every principal.
func TestVisibleWorkspaces_LocalIsolatesByPrincipal(t *testing.T) {
	h, _, ws := newTestHandlerWithRealWorkspaceManager(t)
	// cloudMode left at its default (false) — this is a local run.
	if err := workspace.SaveGroups(h.configDir, []workspace.Workspace{
		{Folders: []string{ws}, CreatedBy: "owner", OrgID: "org-a"},
	}); err != nil {
		t.Fatal(err)
	}

	// Anonymous (no principal in context): sees everything, mutation allowed.
	anon := context.Background()
	if got := h.visibleWorkspaces(anon); len(got) != 1 || got[0] != ws {
		t.Errorf("anonymous: visibleWorkspaces = %v, want [%s]", got, ws)
	}

	// Signed-in but not the owning org: the org workspace is hidden and a
	// workspace-scoped mutation is refused.
	for _, id := range []*auth.Identity{
		{Sub: "u", OrgID: ""},      // personal view
		{Sub: "u", OrgID: "org-b"}, // different org
	} {
		ctx := auth.WithIdentity(context.Background(), id)
		if got := h.visibleWorkspaces(ctx); len(got) != 0 {
			t.Errorf("signed-in %+v: visibleWorkspaces = %v, want [] (isolated)", id, got)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/specs/archive", bytes.NewReader([]byte("{}")))
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		h.ArchiveSpec(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("signed-in %+v: mutation code %d, want 403 (isolated)", id, w.Code)
		}
	}

	// The owning org sees it and passes the visibility guard.
	ownerCtx := auth.WithIdentity(context.Background(), &auth.Identity{Sub: "owner", OrgID: "org-a"})
	if got := h.visibleWorkspaces(ownerCtx); len(got) != 1 || got[0] != ws {
		t.Errorf("owning org: visibleWorkspaces = %v, want [%s]", got, ws)
	}
}
