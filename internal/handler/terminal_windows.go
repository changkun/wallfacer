//go:build windows

package handler

import "net/http"

// HandleTerminalWS is not supported on Windows.
func (h *Handler) HandleTerminalWS(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "terminal not supported on windows"})
}
