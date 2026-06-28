package handler

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"latere.ai/x/wallfacer/internal/pkg/atomicfile"
	"latere.ai/x/wallfacer/internal/pkg/httpjson"
)

// The whiteboard is a per-workspace infinite drawing canvas. Its scene is
// persisted as a single opaque JSON file in the active workspace's scoped data
// directory: ~/.wallfacer/data/<workspace-key>/whiteboard.json. This is
// workspace-level (not task-level) data, so it lives directly under
// ScopedDataDir rather than under a task UUID. There is a single writer (the
// one local browser session), so the server stores the bytes verbatim and never
// parses or validates the scene.

// whiteboardMu serializes reads and writes to the whiteboard scene file. It is
// package-level (like templatesMu) because the file is shared across handler
// instances.
var whiteboardMu sync.RWMutex

// currentWhiteboardPath returns the path to the active workspace's whiteboard
// scene file, or "" when no workspace is configured. The whiteboard is
// per-workspace, so a workspace must be active; the manager always resolves a
// ScopedDataDir (even for an empty workspace set), so the workspace set, not the
// directory, is the "no workspace" signal.
func (h *Handler) currentWhiteboardPath() string {
	if len(h.currentWorkspaces()) == 0 {
		return ""
	}
	h.snapshotMu.RLock()
	dir := h.scopedDataDir
	h.snapshotMu.RUnlock()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "whiteboard.json")
}

// GetWhiteboard handles GET /api/whiteboard. It returns the active workspace's
// saved scene JSON verbatim, or an empty 200 body when no scene has been saved
// yet. Returns 503 when no workspace is configured.
func (h *Handler) GetWhiteboard(w http.ResponseWriter, _ *http.Request) {
	path := h.currentWhiteboardPath()
	if path == "" {
		httpjson.Write(w, http.StatusServiceUnavailable, map[string]string{"error": "no workspace configured"})
		return
	}

	whiteboardMu.RLock()
	data, err := os.ReadFile(path)
	whiteboardMu.RUnlock()

	if errors.Is(err, os.ErrNotExist) {
		// No scene yet: empty 200 body. The client treats this as a blank
		// canvas, mirroring the empty-content convention used elsewhere for
		// missing workspace-scoped files.
		w.WriteHeader(http.StatusOK)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// PutWhiteboard handles PUT /api/whiteboard. The request body is the raw
// Excalidraw scene JSON; it is written atomically and overwrites any existing
// scene. Returns 503 when no workspace is configured, 400 on an empty body, and
// 200 ({"status":"ok"}) on success.
func (h *Handler) PutWhiteboard(w http.ResponseWriter, r *http.Request) {
	path := h.currentWhiteboardPath()
	if path == "" {
		httpjson.Write(w, http.StatusServiceUnavailable, map[string]string{"error": "no workspace configured"})
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			httpjson.Write(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Reject an empty body so a malformed save cannot silently clobber an
	// existing scene; an empty Excalidraw canvas still serializes to a non-empty
	// JSON object.
	if len(data) == 0 {
		http.Error(w, "empty whiteboard scene", http.StatusBadRequest)
		return
	}

	// The scoped data directory normally exists (it holds the task store), but
	// create it defensively so the first save cannot fail on a fresh group.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	whiteboardMu.Lock()
	err = atomicfile.Write(path, data, 0o644)
	whiteboardMu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	httpjson.Write(w, http.StatusOK, map[string]string{"status": "ok"})
}
