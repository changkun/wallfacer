package handler

import (
	"encoding/json"
	"net/http"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/instructions"
)

// availableModels is the list of Claude models users can select per task.
var availableModels = []string{
	"claude-sonnet-4-6-20250514",
	"claude-opus-4-6-20250610",
	"claude-haiku-4-5-20251001",
}

// GetConfig returns the server configuration (workspaces, instructions path).
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	// Read the current default model from the env file.
	defaultModel := ""
	if h.envFile != "" {
		if cfg, err := envconfig.Parse(h.envFile); err == nil {
			defaultModel = cfg.Model
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"workspaces":        h.runner.Workspaces(),
		"instructions_path": instructions.FilePath(h.configDir, h.workspaces),
		"autopilot":         h.AutopilotEnabled(),
		"models":            availableModels,
		"default_model":     defaultModel,
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
