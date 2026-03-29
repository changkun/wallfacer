//go:build windows

package handler

import (
	"net/http"

	"changkun.de/x/wallfacer/internal/pkg/httpjson"
)

// HandleTerminalWS is not supported on Windows.
func (h *Handler) HandleTerminalWS(w http.ResponseWriter, _ *http.Request) {
	httpjson.Write(w, http.StatusNotImplemented, map[string]string{"error": "terminal not supported on windows"})
}
