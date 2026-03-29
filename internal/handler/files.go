package handler

import (
	"net/http"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
)

// skipDirs lists directory names that should never be traversed during file listing.
var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	".next":        true,
	"__pycache__":  true,
	"dist":         true,
	"build":        true,
	".cache":       true,
	".tox":         true,
	".venv":        true,
	"venv":         true,
	"target":       true, // Rust/Maven build output
}

// GetFiles returns a flat list of files across all workspace directories.
// Hidden directories and common generated/dependency directories are skipped.
// Paths are prefixed with the workspace base name (matching the /workspace/<name>/
// mount path inside containers), making them directly usable in task prompts.
// Results are served from a per-workspace cache; see fileIndex for invalidation policy.
func (h *Handler) GetFiles(w http.ResponseWriter, _ *http.Request) {
	workspaces := h.currentWorkspaces()
	files := make([]string, 0, 256)

	for _, ws := range workspaces {
		if len(files) >= constants.MaxFileListSize {
			break
		}
		wsFiles := h.fileIndex.Files(ws)
		remaining := constants.MaxFileListSize - len(files)
		if len(wsFiles) > remaining {
			wsFiles = wsFiles[:remaining]
		}
		files = append(files, wsFiles...)
	}

	httpjson.Write(w, http.StatusOK, map[string]any{"files": files})
}
