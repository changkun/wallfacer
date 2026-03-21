package handler

import (
	"net/http"
)

// maxFileListSize caps the total number of files returned to keep responses fast.
const maxFileListSize = 8000

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
		if len(files) >= maxFileListSize {
			break
		}
		wsFiles := h.fileIndex.Files(ws)
		remaining := maxFileListSize - len(files)
		if len(wsFiles) > remaining {
			wsFiles = wsFiles[:remaining]
		}
		files = append(files, wsFiles...)
	}

	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}
