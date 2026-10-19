package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWorkspaceCRUDLifecycle exercises create -> list -> activate -> edit
// folders -> delete-guard end to end through the HTTP handlers.
func TestWorkspaceCRUDLifecycle(t *testing.T) {
	h, _, ws := newTestHandlerWithRealWorkspaceManager(t)
	ws2 := t.TempDir()

	// Create.
	body, _ := json.Marshal(map[string]any{"name": "proj", "folders": []string{ws}})
	rec := httptest.NewRecorder()
	h.CreateWorkspace(rec, httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d: %s", rec.Code, rec.Body.String())
	}
	var created workspaceDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.ID == "" || created.Name != "proj" || created.Active {
		t.Fatalf("unexpected created workspace: %+v", created)
	}

	// List contains it, not yet active.
	lrec := httptest.NewRecorder()
	h.ListWorkspaces(lrec, httptest.NewRequest(http.MethodGet, "/api/workspaces", nil))
	var list struct {
		Workspaces []workspaceDTO `json:"workspaces"`
		ActiveID   string         `json:"active_id"`
	}
	if err := json.Unmarshal(lrec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Workspaces) == 0 {
		t.Fatal("list returned no workspaces")
	}

	// Activate by id; config carries the active id.
	arec := httptest.NewRecorder()
	areq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+created.ID+"/activate", nil)
	areq.SetPathValue("id", created.ID)
	h.ActivateWorkspace(arec, areq)
	if arec.Code != http.StatusOK {
		t.Fatalf("activate: got %d: %s", arec.Code, arec.Body.String())
	}
	var cfg map[string]any
	_ = json.Unmarshal(arec.Body.Bytes(), &cfg)
	if cfg["workspace_id"] != created.ID {
		t.Fatalf("config workspace_id = %v, want %q", cfg["workspace_id"], created.ID)
	}

	// Re-point to ws2. Identity preserved, still active.
	ubody, _ := json.Marshal(map[string]any{"folders": []string{ws2}})
	urec := httptest.NewRecorder()
	ureq := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+created.ID, bytes.NewReader(ubody))
	ureq.SetPathValue("id", created.ID)
	h.UpdateWorkspace(urec, ureq)
	if urec.Code != http.StatusOK {
		t.Fatalf("update: got %d: %s", urec.Code, urec.Body.String())
	}
	var updated workspaceDTO
	_ = json.Unmarshal(urec.Body.Bytes(), &updated)
	if updated.ID != created.ID || len(updated.Folders) != 1 || updated.Folders[0] != ws2 || !updated.Active {
		t.Fatalf("update did not preserve identity/activeness: %+v", updated)
	}

	// Deleting the active workspace is allowed; it returns 200 with the new
	// config and (no other workspace exists) the empty active state.
	drec := httptest.NewRecorder()
	dreq := httptest.NewRequest(http.MethodDelete, "/api/workspaces/"+created.ID, nil)
	dreq.SetPathValue("id", created.ID)
	h.DeleteWorkspace(drec, dreq)
	if drec.Code != http.StatusOK {
		t.Fatalf("delete active: got %d, want 200: %s", drec.Code, drec.Body.String())
	}
	var delCfg map[string]any
	_ = json.Unmarshal(drec.Body.Bytes(), &delCfg)
	// The active workspace must have switched away from the deleted one (to
	// another workspace or the empty state) — never still point at it.
	if delCfg["workspace_id"] == created.ID {
		t.Fatalf("active workspace still points at the deleted one: %v", delCfg["workspace_id"])
	}
	// The workspace is gone from the registry.
	lrec2 := httptest.NewRecorder()
	h.ListWorkspaces(lrec2, httptest.NewRequest(http.MethodGet, "/api/workspaces", nil))
	var list2 struct {
		Workspaces []workspaceDTO `json:"workspaces"`
	}
	_ = json.Unmarshal(lrec2.Body.Bytes(), &list2)
	for _, ws := range list2.Workspaces {
		if ws.ID == created.ID {
			t.Fatal("deleted workspace still listed")
		}
	}
}

// TestWorkspaceCRUD_SeveralFoldersRefused verifies that create and update
// answer 400 with a message saying a workspace is one folder when the request
// names more than one, and that a refused update leaves the folder unchanged.
func TestWorkspaceCRUD_SeveralFoldersRefused(t *testing.T) {
	h, _, ws := newTestHandlerWithRealWorkspaceManager(t)
	ws2 := t.TempDir()

	body, _ := json.Marshal(map[string]any{"name": "two", "folders": []string{ws, ws2}})
	rec := httptest.NewRecorder()
	h.CreateWorkspace(rec, httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create with two folders: got %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "a workspace is one folder") {
		t.Fatalf("create with two folders: body %q does not say a workspace is one folder", rec.Body.String())
	}

	body, _ = json.Marshal(map[string]any{"name": "one", "folders": []string{ws}})
	rec = httptest.NewRecorder()
	h.CreateWorkspace(rec, httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with one folder: got %d: %s", rec.Code, rec.Body.String())
	}
	var created workspaceDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}

	ubody, _ := json.Marshal(map[string]any{"folders": []string{ws, ws2}})
	urec := httptest.NewRecorder()
	ureq := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+created.ID, bytes.NewReader(ubody))
	ureq.SetPathValue("id", created.ID)
	h.UpdateWorkspace(urec, ureq)
	if urec.Code != http.StatusBadRequest {
		t.Fatalf("update with two folders: got %d, want 400: %s", urec.Code, urec.Body.String())
	}
	if !strings.Contains(urec.Body.String(), "a workspace is one folder") {
		t.Fatalf("update with two folders: body %q does not say a workspace is one folder", urec.Body.String())
	}
	got, found, err := h.workspace.WorkspaceByID(created.ID)
	if err != nil || !found {
		t.Fatalf("WorkspaceByID: found=%v err=%v", found, err)
	}
	if len(got.Folders) != 1 || got.Folders[0] != ws {
		t.Fatalf("a refused update changed the folder: %v", got.Folders)
	}
}

// TestWorkspaceUpdate_Limits verifies per-workspace parallel overrides:
// a present field is applied, an absent field is left unchanged, and a present
// null clears the override.
func TestWorkspaceUpdate_Limits(t *testing.T) {
	h, _, ws := newTestHandlerWithRealWorkspaceManager(t)
	body, _ := json.Marshal(map[string]any{"name": "A", "folders": []string{ws}})
	rec := httptest.NewRecorder()
	h.CreateWorkspace(rec, httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewReader(body)))
	var created workspaceDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	put := func(payload string) workspaceDTO {
		r := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+created.ID, bytes.NewReader([]byte(payload)))
		r.SetPathValue("id", created.ID)
		w := httptest.NewRecorder()
		h.UpdateWorkspace(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("update %s: %d %s", payload, w.Code, w.Body.String())
		}
		var dto workspaceDTO
		_ = json.Unmarshal(w.Body.Bytes(), &dto)
		return dto
	}

	// Set max_parallel=2.
	d := put(`{"max_parallel":2}`)
	if d.MaxParallel == nil || *d.MaxParallel != 2 {
		t.Fatalf("after set max_parallel: %+v", d)
	}
	// A rename leaves max_parallel absent -> unchanged (still 2).
	d = put(`{"name":"B"}`)
	if d.MaxParallel == nil || *d.MaxParallel != 2 {
		t.Fatalf("absent field should be preserved: %+v", d)
	}
	// Clear max_parallel via null.
	d = put(`{"max_parallel":null}`)
	if d.MaxParallel != nil {
		t.Fatalf("null should clear max_parallel: %+v", d)
	}
}
