package handler

import (
	"net/http"
	"os"

	"changkun.de/wallfacer/internal/instructions"
)

// GetInstructions returns the current workspace AGENTS.md content.
func (h *Handler) GetInstructions(w http.ResponseWriter, r *http.Request) {
	path := instructions.FilePath(h.configDir, h.workspaces)
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
	path := instructions.FilePath(h.configDir, h.workspaces)
	if err := os.WriteFile(path, []byte(req.Content), 0644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ReinitInstructions rebuilds the workspace AGENTS.md from defaults and repo files.
func (h *Handler) ReinitInstructions(w http.ResponseWriter, r *http.Request) {
	path, err := instructions.Reinit(h.configDir, h.workspaces)
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
