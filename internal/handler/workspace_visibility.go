package handler

import (
	"net/http"

	"latere.ai/x/wallfacer/internal/pkg/httpjson"
)

// workspaceHiddenFromRequest reports whether there is an active workspace that
// is hidden from the request's principal. It is true only when a workspace is
// active AND the principal cannot see it; an empty workspace set is "no
// workspace", not "hidden", so handlers keep their existing no-workspace
// behavior instead of returning 403. Mirrors the org/personal isolation
// buildConfigResponse applies via visibleWorkspaces.
func (h *Handler) workspaceHiddenFromRequest(r *http.Request) bool {
	ws := h.currentWorkspaces()
	return len(ws) > 0 && !h.workspaceVisibleTo(r.Context(), ws)
}

// requireVisibleWorkspace writes a 403 and returns false when the active
// workspace is hidden from the caller. Workspace-scoped mutation handlers
// (agent-session sends, spec archive/dispatch) call it as an early guard so a
// session with no visible workspace cannot mutate a hidden one.
func (h *Handler) requireVisibleWorkspace(w http.ResponseWriter, r *http.Request) bool {
	if !h.workspaceHiddenFromRequest(r) {
		return true
	}
	httpjson.Write(w, http.StatusForbidden, map[string]string{"error": "no workspace selected"})
	return false
}
