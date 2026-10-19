package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"latere.ai/x/pkg/httpjson"

	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/workspace"
)

// parseLimit decodes a per-workspace concurrency override from a present JSON
// value: null (or blank) clears the override (nil); a non-negative integer caps
// it. A negative value is treated as "clear".
func parseLimit(raw json.RawMessage) *int {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil || n < 0 {
		return nil
	}
	return &n
}

// workspaceDTO is the JSON shape of a workspace returned by the CRUD endpoints.
type workspaceDTO struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Folders     []string `json:"folders"`
	Dormant     bool     `json:"dormant"`
	Active      bool     `json:"active"`
	MaxParallel *int     `json:"max_parallel,omitempty"`
}

func (h *Handler) workspaceDTO(ws workspace.Workspace) workspaceDTO {
	// A dormant workspace may have no folders yet; serialize an empty array
	// rather than null so the frontend can safely .map/.join over it.
	folders := ws.Folders
	if folders == nil {
		folders = []string{}
	}
	return workspaceDTO{
		ID:          ws.ID,
		Name:        ws.Name,
		Folders:     folders,
		Dormant:     ws.Dormant,
		Active:      ws.ID != "" && ws.ID == h.activeWorkspaceID(),
		MaxParallel: ws.MaxParallel,
	}
}

// activeWorkspaceID returns the id of the currently active workspace, or "".
func (h *Handler) activeWorkspaceID() string {
	if h.workspace == nil {
		return ""
	}
	return h.workspace.Snapshot().WorkspaceID
}

// workspaceExists reports whether a workspace with the given id is saved, so
// the id-addressed handlers answer 404 for an unknown one.
func (h *Handler) workspaceExists(id string) bool {
	_, found, err := h.workspace.WorkspaceByID(id)
	return err == nil && found
}

// ListWorkspaces returns the workspaces visible to the request's principal,
// each flagged with whether it is the active one.
func (h *Handler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	list, err := h.workspace.ListWorkspaces()
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
	ws, err := h.workspace.Create(req.Name, req.Folders)
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
	if !h.workspaceExists(id) {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	req, ok := httpjson.DecodeBody[struct {
		Name    *string  `json:"name"`
		Folders []string `json:"folders"`
		// RawMessage so a present-but-null value (clear the override) is
		// distinguishable from an absent key (leave the override unchanged).
		MaxParallel json.RawMessage `json:"max_parallel"`
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
	if req.MaxParallel != nil {
		// A present (incl. null) field is applied; an absent one leaves the
		// override unchanged.
		if !h.workspaceExists(id) {
			http.Error(w, "workspace not found", http.StatusNotFound)
			return
		}
		if ws, err = h.workspace.SetLimits(id, parseLimit(req.MaxParallel)); err != nil {
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

// DeleteWorkspace permanently deletes a workspace and wipes all its session
// data. Deleting the active workspace auto-switches the board to the next
// usable workspace (or the empty state). Returns the resulting config so the
// caller can refresh the sidebar and active board in one round-trip.
func (h *Handler) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.workspaceExists(id) {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}
	snap, err := h.workspace.Delete(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if snap.Store != nil {
		h.runner.PruneUnknownWorktrees()
	}
	httpjson.Write(w, http.StatusOK, h.buildConfigResponse(r.Context(), nil))
}

// ActivateWorkspace switches the active workspace by id and returns the updated
// config payload (mirroring the legacy path-based switch).
func (h *Handler) ActivateWorkspace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.workspaceExists(id) {
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
