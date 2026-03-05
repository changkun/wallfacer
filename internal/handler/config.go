package handler

import (
	"encoding/json"
	"net/http"

	"changkun.de/wallfacer/internal/instructions"
)

// GetConfig returns the server configuration (workspaces, instructions path).
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"workspaces":        h.runner.Workspaces(),
		"instructions_path": instructions.FilePath(h.configDir, h.workspaces),
		"autopilot":         h.AutopilotEnabled(),
	})
}

// UpdateConfig handles PUT /api/config to update server-level settings.
func (h *Handler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Autopilot *bool `json:"autopilot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Autopilot != nil {
		h.SetAutopilot(*req.Autopilot)
	}
	// Re-trigger auto-promotion in case autopilot was just enabled.
	if h.AutopilotEnabled() {
		go h.tryAutoPromote(r.Context())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"autopilot": h.AutopilotEnabled(),
	})
}
