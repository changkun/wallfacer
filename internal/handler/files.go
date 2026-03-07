package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
func (h *Handler) GetFiles(w http.ResponseWriter, r *http.Request) {
	workspaces := h.runner.Workspaces()
	files := make([]string, 0, 256)

	for _, ws := range workspaces {
		if len(files) >= maxFileListSize {
			break
		}
		base := filepath.Base(ws)
		_ = filepath.Walk(ws, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				name := info.Name()
				// Skip hidden dirs and known generated/dependency dirs.
				if strings.HasPrefix(name, ".") || skipDirs[name] {
					return filepath.SkipDir
				}
				return nil
			}
			if len(files) >= maxFileListSize {
				return filepath.SkipAll
			}
			rel, relErr := filepath.Rel(ws, path)
			if relErr != nil {
				return nil
			}
			files = append(files, filepath.ToSlash(filepath.Join(base, rel)))
			return nil
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}
