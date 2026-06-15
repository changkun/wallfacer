package handler

import (
	"bytes"
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
