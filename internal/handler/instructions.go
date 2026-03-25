package handler

import (
	"net/http"
	"os"

	"changkun.de/x/wallfacer/prompts"
)

// GetInstructions returns the current workspace AGENTS.md content.
func (h *Handler) GetInstructions(w http.ResponseWriter, _ *http.Request) {
	path := h.currentInstructionsPath()
	if path == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no workspaces configured"})
		return
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]string{"content": ""})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": string(content)})
}

// UpdateInstructions replaces the workspace AGENTS.md with the provided content.
func (h *Handler) UpdateInstructions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	path := h.currentInstructionsPath()
	if path == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no workspaces configured"})
		return
	}
	if err := os.WriteFile(path, []byte(req.Content), 0644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ReinitInstructions rebuilds the workspace AGENTS.md from defaults and repo files.
func (h *Handler) ReinitInstructions(w http.ResponseWriter, _ *http.Request) {
	path := h.currentInstructionsPath()
	if path == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no workspaces configured"})
		return
	}
	path, err := prompts.ReinitInstructions(h.configDir, h.currentWorkspaces())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	content, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": string(content)})
}
