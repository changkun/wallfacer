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
	h.SetCloudMode(true) // org isolation only applies to cloud deployments
	if err := workspace.SaveGroups(h.configDir, []workspace.Group{
		{Workspaces: []string{ws}, CreatedBy: "owner", OrgID: "org-a"},
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

// TestVisibleWorkspaces_LocalModeShowsOrgStampedWorkspace is the regression
// guard for the org-switch lockout: on a local single-user run (cloudMode
// false, the default), an org-stamped workspace stays visible to every
// principal — personal or any org — so switching org labels never hides the
// user's own workspace. Only a cloud deployment isolates.
func TestVisibleWorkspaces_LocalModeShowsOrgStampedWorkspace(t *testing.T) {
	h, _, ws := newTestHandlerWithRealWorkspaceManager(t)
	// cloudMode left at its default (false) — this is a local run.
	if err := workspace.SaveGroups(h.configDir, []workspace.Group{
		{Workspaces: []string{ws}, CreatedBy: "owner", OrgID: "org-a"},
	}); err != nil {
		t.Fatal(err)
	}

	// Personal caller and a different org both still see the workspace locally.
	for _, id := range []*auth.Identity{
		{Sub: "u", OrgID: ""},      // personal
		{Sub: "u", OrgID: "org-b"}, // different org
	} {
		ctx := auth.WithIdentity(context.Background(), id)
		if got := h.visibleWorkspaces(ctx); len(got) != 1 || got[0] != ws {
			t.Errorf("local mode, principal %+v: visibleWorkspaces = %v, want [%s]", id, got, ws)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/specs/archive", bytes.NewReader([]byte("{}")))
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		h.ArchiveSpec(w, req)
		if w.Code == http.StatusForbidden {
			t.Errorf("local mode, principal %+v: mutation forbidden, want allowed", id)
		}
	}
}
