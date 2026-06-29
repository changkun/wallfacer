package handler

import (
	"net/http"

	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/pkg/httpjson"
	"latere.ai/x/wallfacer/internal/workspace"
)

// workspaceDTO is the JSON shape of a workspace returned by the CRUD endpoints.
type workspaceDTO struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Folders []string `json:"folders"`
	Dormant bool     `json:"dormant"`
	Active  bool     `json:"active"`
}

func (h *Handler) workspaceDTO(ws workspace.Workspace) workspaceDTO {
	return workspaceDTO{
		ID:      ws.ID,
		Name:    ws.Name,
		Folders: ws.Folders,
		Dormant: ws.Dormant,
		Active:  ws.ID != "" && ws.ID == h.activeWorkspaceID(),
	}
}

// activeWorkspaceID returns the id of the currently active workspace, or "".
func (h *Handler) activeWorkspaceID() string {
	if h.workspace == nil {
		return ""
	}
	return h.workspace.Snapshot().WorkspaceID
}

// visibilityPrincipal returns the principal used to scope which workspaces a
// caller may see. Local single-user runs (cloudMode=false) see everything, so
// they return nil; only cloud deployments enforce org/personal isolation.
func (h *Handler) visibilityPrincipal(r *http.Request) *workspace.Principal {
	if !h.cloudMode {
		return nil
	}
	if p := principalFromRequest(r); p != nil {
		return &workspace.Principal{Sub: p.Sub, OrgID: p.OrgID}
	}
	return nil
}

// ownerPrincipal returns the principal to stamp as a new workspace's owner, or
// nil when the request is unauthenticated.
func (h *Handler) ownerPrincipal(r *http.Request) *workspace.Principal {
	if p := principalFromRequest(r); p != nil {
		return &workspace.Principal{Sub: p.Sub, OrgID: p.OrgID}
	}
	return nil
}

// workspaceVisibleByID reports whether the workspace with the given id is
// visible to the request's principal. A missing or out-of-scope workspace is
// reported as not visible so callers return 404 without leaking existence.
func (h *Handler) workspaceVisibleByID(r *http.Request, id string) bool {
	ws, found, err := h.workspace.WorkspaceByID(id)
	if err != nil || !found {
		return false
	}
	p := h.visibilityPrincipal(r)
	if p == nil {
		return true
	}
	return len(workspace.WorkspacesForPrincipal([]workspace.Workspace{ws}, p)) == 1
}

// ListWorkspaces returns the workspaces visible to the request's principal,
// each flagged with whether it is the active one.
func (h *Handler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	list, err := h.workspace.ListWorkspaces(h.visibilityPrincipal(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]workspaceDTO, 0, len(list))
	for _, ws := range list {
		out = append(out, h.workspaceDTO(ws))
	}
	httpjson.Write(w, http.StatusOK, map[string]any{
		"workspaces": out,
		"active_id":  h.activeWorkspaceID(),
	})
}

// CreateWorkspace creates a new workspace owned by the request's principal. It
// does not activate it; the caller activates via POST /api/workspaces/{id}/activate.
func (h *Handler) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
	req, ok := httpjson.DecodeBody[struct {
		Name    string   `json:"name"`
		Folders []string `json:"folders"`
	}](w, r)
	if !ok {
		return
	}
	ws, err := h.workspace.Create(req.Name, req.Folders, h.ownerPrincipal(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	httpjson.Write(w, http.StatusCreated, h.workspaceDTO(ws))
}

// UpdateWorkspace renames a workspace and/or replaces its folder set. Replacing
// folders is the membership edit: the workspace's identity and history are
// preserved. When it is the active workspace, the live view updates in place.
func (h *Handler) UpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.workspaceVisibleByID(r, id) {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	req, ok := httpjson.DecodeBody[struct {
		Name    *string  `json:"name"`
		Folders []string `json:"folders"`
	}](w, r)
	if !ok {
		return
	}
	var (
		ws  workspace.Workspace
		err error
	)
	updated := false
	if req.Folders != nil {
		if ws, err = h.workspace.UpdateFolders(id, req.Folders); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		updated = true
	}
	if req.Name != nil {
		if ws, err = h.workspace.Rename(id, *req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		updated = true
	}
	if !updated {
		var found bool
		if ws, found, err = h.workspace.WorkspaceByID(id); err != nil || !found {
			http.Error(w, "workspace not found", http.StatusNotFound)
			return
		}
	}
	httpjson.Write(w, http.StatusOK, h.workspaceDTO(ws))
}

// DeleteWorkspace removes a workspace record (its data directory is left on
// disk). The active workspace cannot be deleted; switch away first.
func (h *Handler) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.workspaceVisibleByID(r, id) {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	if err := h.workspace.Delete(id); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ActivateWorkspace switches the active workspace by id and returns the updated
// config payload (mirroring the legacy path-based switch).
func (h *Handler) ActivateWorkspace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.workspaceVisibleByID(r, id) {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	snap, err := h.workspace.SwitchByID(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if snap.Store != nil {
		h.runner.PruneUnknownWorktrees()
	}
	var cfg *envconfig.Config
	if h.envFile != "" {
		if parsed, err := envconfig.Parse(h.envFile); err == nil {
			cfg = &parsed
		}
	}
	httpjson.Write(w, http.StatusOK, h.buildConfigResponse(r.Context(), cfg))
}
