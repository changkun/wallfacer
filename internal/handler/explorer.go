package handler

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/pkg/atomicfile"
	"latere.ai/x/wallfacer/internal/pkg/httpjson"
	"latere.ai/x/wallfacer/internal/pkg/sse"
	"latere.ai/x/wallfacer/internal/store"
)

// maxFileWriteSize is the maximum content size accepted by ExplorerWriteFile.
// Set to 2 MB to prevent accidental or malicious large writes through the API.
const maxFileWriteSize = 2 << 20

// explorerFileDebounce coalesces the burst of fsnotify events an atomic
// write (temp file + rename) emits into a single change notification.
const explorerFileDebounce = 150 * time.Millisecond

// explorerFilePollInterval is the fallback fingerprint check. fsnotify drives
// sub-second notifications; this ticker guarantees eventual convergence even
// when a platform or filesystem drops the underlying event, so the stream is
// never worse than the poll it replaces.
const explorerFilePollInterval = 3 * time.Second

// explorerEntry is a single directory or file entry returned by ExplorerTree.
type explorerEntry struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	Size     int64     `json:"size,omitempty"`
	Modified time.Time `json:"modified"`
}

// isWithinWorkspace resolves symlinks and cleans both paths, then verifies
// that requestedPath is equal to or a child of workspace. This is the primary
// path-traversal guard for the file explorer: all read/write/tree operations
// pass through this function to ensure user input cannot escape the workspace.
// Returns the cleaned, resolved path or an error if the path escapes the workspace.
func isWithinWorkspace(requestedPath, workspace string) (string, error) {
	resolvedWS, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", errors.New("workspace path not accessible")
	}
	resolvedWS = filepath.Clean(resolvedWS)

	resolvedReq, err := filepath.EvalSymlinks(requestedPath)
	if err != nil {
		return "", errors.New("requested path not accessible")
	}
	resolvedReq = filepath.Clean(resolvedReq)

	if resolvedReq != resolvedWS && !strings.HasPrefix(resolvedReq, resolvedWS+string(filepath.Separator)) {
		return "", errors.New("path is outside workspace")
	}
	return resolvedReq, nil
}

// ExplorerTree lists one level of a workspace directory.
func (h *Handler) ExplorerTree(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	workspace := r.URL.Query().Get("workspace")

	if path == "" || workspace == "" {
		http.Error(w, "path and workspace query params required", http.StatusBadRequest)
		return
	}

	if !h.isAllowedWorkspace(r.Context(), workspace) {
		http.Error(w, "workspace not configured", http.StatusBadRequest)
		return
	}

	resolved, err := isWithinWorkspace(path, workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "directory not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to read directory", http.StatusInternalServerError)
		return
	}

	result := make([]explorerEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue // skip entries we can't stat
		}
		typ := "file"
		if e.IsDir() {
			typ = "dir"
		}
		entry := explorerEntry{
			Name:     e.Name(),
			Type:     typ,
			Modified: info.ModTime().UTC().Truncate(time.Second),
		}
		if typ == "file" {
			entry.Size = info.Size()
		}
		result = append(result, entry)
	}

	// Sort: directories first, then files; case-insensitive alphabetical within each group.
	slices.SortFunc(result, func(a, b explorerEntry) int {
		if a.Type != b.Type {
			if a.Type == "dir" {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})

	httpjson.Write(w, http.StatusOK, result)
}

// ExplorerStream sends SSE notifications when workspace directory contents
// change. The client provides a comma-separated list of its expanded directory
// paths via the "paths" query param. The server polls the visible workspace
// roots plus those expanded directories every 3 seconds and sends a "refresh"
// event (data: {"paths": [...]}) whenever a content fingerprint changes, so the
// client can re-fetch the affected nodes.
func (h *Handler) ExplorerStream(w http.ResponseWriter, r *http.Request) {
	stream := sse.NewWriter(w)
	if stream == nil {
		return
	}

	// Compute a fingerprint of a directory listing (names + types + sizes + mtimes).
	fingerprint := func(dirPath string) string {
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			return ""
		}
		hash := sha256.New()
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(hash, "%s:%v:%d:%d;", e.Name(), e.IsDir(), info.Size(), info.ModTime().UnixNano())
		}
		return fmt.Sprintf("%x", hash.Sum(nil))
	}

	// The client passes its currently-expanded directory paths via "paths"
	// (comma-separated, absolute). Fingerprinting only the workspace roots
	// would miss content edits and structural changes more than one level deep,
	// so the dirs whose contents are actually rendered (root + expanded) are all
	// watched. The expanded set is fixed for the life of the stream; the client
	// re-opens the stream when it expands or collapses a directory.
	expandedRaw := r.URL.Query().Get("paths")

	// targets returns the dirs to fingerprint this tick: the visible workspace
	// roots plus the client's validated expanded dirs. visibleWorkspaces (not
	// currentWorkspaces) and per-tick re-validation ensure a session that cannot
	// see the active org-scoped group never receives refresh events naming those
	// paths, matching GitStatusStream and the other explorer handlers' gate.
	targets := func() []string {
		dirs := h.visibleWorkspaces(r.Context())
		return append(dirs, h.validatedExpandedDirs(r.Context(), expandedRaw)...)
	}

	prevFingerprints := make(map[string]string)
	for _, d := range targets() {
		prevFingerprints[d] = fingerprint(d)
	}

	if err := stream.Event("connected", []byte("{}")); err != nil {
		return
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	keepalive := time.NewTicker(constants.SSEKeepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if err := stream.Heartbeat(); err != nil {
				return
			}
		case <-ticker.C:
			var changed []string
			newFingerprints := make(map[string]string)
			for _, d := range targets() {
				fp := fingerprint(d)
				newFingerprints[d] = fp
				if fp != prevFingerprints[d] {
					changed = append(changed, d)
				}
			}
			prevFingerprints = newFingerprints

			if len(changed) > 0 {
				if err := stream.JSON("refresh", map[string]any{"paths": changed}); err != nil {
					return
				}
			}
		}
	}
}

// maxExpandedDirs bounds how many client-supplied expanded directories the
// stream will fingerprint each tick, so a client cannot drive unbounded stat
// load.
const maxExpandedDirs = 256

// validatedExpandedDirs parses the comma-separated absolute directory paths the
// explorer client reports as expanded and returns those that resolve inside a
// currently-visible workspace, de-duplicated and capped at maxExpandedDirs.
// Paths that escape every visible workspace (or no longer exist) are dropped;
// their parent root or expanded ancestor still catches structural changes.
func (h *Handler) validatedExpandedDirs(ctx context.Context, raw string) []string {
	if raw == "" {
		return nil
	}
	workspaces := h.visibleWorkspaces(ctx)
	seen := make(map[string]bool)
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		for _, ws := range workspaces {
			resolved, err := isWithinWorkspace(p, ws)
			if err != nil {
				continue
			}
			if !seen[resolved] {
				seen[resolved] = true
				out = append(out, resolved)
			}
			break
		}
		if len(out) >= maxExpandedDirs {
			break
		}
	}
	return out
}

// isBinaryContent reports whether data contains a null byte, indicating
// binary content.
func isBinaryContent(data []byte) bool {
	return slices.Contains(data, 0)
}

// ExplorerReadFile returns file contents for preview, with binary detection
// and size limits.
func (h *Handler) ExplorerReadFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	workspace := r.URL.Query().Get("workspace")

	if path == "" || workspace == "" {
		http.Error(w, "path and workspace query params required", http.StatusBadRequest)
		return
	}

	if !h.isAllowedWorkspace(r.Context(), workspace) {
		http.Error(w, "workspace not configured", http.StatusBadRequest)
		return
	}

	resolved, err := isWithinWorkspace(path, workspace)
	if err != nil {
		// isWithinWorkspace fails for non-existent paths because
		// EvalSymlinks requires the target to exist. Distinguish a
		// genuinely missing file from a path-escape attempt by
		// cleaning the raw path and checking containment manually.
		cleaned := filepath.Clean(path)
		wsClean := filepath.Clean(workspace)
		if cleaned == wsClean || strings.HasPrefix(cleaned, wsClean+string(filepath.Separator)) {
			// Path is within workspace but doesn't exist.
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	info, err := os.Stat(resolved)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to stat file", http.StatusInternalServerError)
		return
	}

	if info.IsDir() {
		http.Error(w, "path is a directory", http.StatusBadRequest)
		return
	}

	size := info.Size()
	w.Header().Set("X-File-Size", strconv.FormatInt(size, 10))

	if size > constants.ExplorerMaxFileSize {
		httpjson.Write(w, http.StatusRequestEntityTooLarge, map[string]any{
			"error": "file too large",
			"size":  size,
			"max":   constants.ExplorerMaxFileSize,
		})
		return
	}

	f, err := os.Open(resolved)
	if err != nil {
		http.Error(w, "failed to open file", http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	// Read first 8192 bytes to detect binary content.
	head := make([]byte, 8192)
	n, err := f.Read(head)
	if err != nil && err != io.EOF {
		http.Error(w, "failed to read file", http.StatusInternalServerError)
		return
	}
	head = head[:n]

	if isBinaryContent(head) {
		w.Header().Set("X-File-Binary", "true")
		httpjson.Write(w, http.StatusOK, map[string]any{
			"binary": true,
			"size":   size,
		})
		return
	}

	// Text file: write the head we already read, then copy the rest.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(head)
	_, _ = io.Copy(w, f)
}

// fileFingerprint returns a size:mtime fingerprint of the file at path, or ""
// if it is missing or unreadable. A change in either field (a write replaces
// both via atomic rename) yields a different fingerprint, so equality means
// "no observable content change" — chmod and atime touches are ignored.
func fileFingerprint(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
}

// ExplorerFileStream sends an SSE "changed" event whenever a single watched
// file's contents change, so clients can refetch via ExplorerReadFile instead
// of polling. The client passes the file "path" and its "workspace" as query
// params (same validation as ExplorerReadFile).
//
// Change detection watches the file's parent directory, not the file itself:
// editors and ExplorerWriteFile write atomically (temp file + rename), which
// replaces the inode and would be missed by a watch on the file path. fsnotify
// gives sub-second latency; a fallback ticker (explorerFilePollInterval)
// guarantees eventual convergence if a platform drops the event.
//
//	event: connected — stream attached (data: {})
//	event: changed   — the file changed; refetch it (data: {"path": "..."})
func (h *Handler) ExplorerFileStream(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	workspace := r.URL.Query().Get("workspace")
	if path == "" || workspace == "" {
		http.Error(w, "path and workspace query params required", http.StatusBadRequest)
		return
	}
	if !h.isAllowedWorkspace(r.Context(), workspace) {
		http.Error(w, "workspace not configured", http.StatusBadRequest)
		return
	}
	resolved, err := isWithinWorkspace(path, workspace)
	if err != nil {
		// Distinguish a missing-but-contained path from a path-escape attempt,
		// mirroring ExplorerReadFile (EvalSymlinks fails on non-existent paths).
		cleaned := filepath.Clean(path)
		wsClean := filepath.Clean(workspace)
		if cleaned == wsClean || strings.HasPrefix(cleaned, wsClean+string(filepath.Separator)) {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(resolved)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "path is a directory", http.StatusBadRequest)
		return
	}

	stream := sse.NewWriter(w)
	if stream == nil {
		return
	}

	// Attach the watcher to the parent dir. A nil watcher (or failed Add) is
	// non-fatal: the fallback poll ticker below still drives change detection,
	// just at coarser latency.
	base := filepath.Base(resolved)
	var watcherEvents <-chan fsnotify.Event
	var watcherErrors <-chan error
	if watcher, werr := fsnotify.NewWatcher(); werr == nil {
		if addErr := watcher.Add(filepath.Dir(resolved)); addErr != nil {
			_ = watcher.Close()
		} else {
			defer func() { _ = watcher.Close() }()
			watcherEvents = watcher.Events
			watcherErrors = watcher.Errors
		}
	}

	if err := stream.Event("connected", []byte("{}")); err != nil {
		return
	}

	last := fileFingerprint(resolved)
	emitIfChanged := func() error {
		fp := fileFingerprint(resolved)
		if fp == last {
			return nil
		}
		last = fp
		return stream.JSON("changed", map[string]string{"path": path})
	}

	keepalive := time.NewTicker(constants.SSEKeepaliveInterval)
	defer keepalive.Stop()
	poll := time.NewTicker(explorerFilePollInterval)
	defer poll.Stop()

	// debounce is nil (blocks forever) until an fsnotify event arms it; each
	// event re-arms it, so the change fires once after the write burst settles.
	var debounce <-chan time.Time
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if err := stream.Heartbeat(); err != nil {
				return
			}
		case <-poll.C:
			if err := emitIfChanged(); err != nil {
				return
			}
		case evt, ok := <-watcherEvents:
			if !ok {
				watcherEvents = nil
				continue
			}
			if filepath.Base(evt.Name) != base {
				continue
			}
			debounce = time.After(explorerFileDebounce)
		case err, ok := <-watcherErrors:
			if !ok {
				watcherErrors = nil
				continue
			}
			logger.Handler.Warn("explorer file stream: fsnotify error", "error", err)
		case <-debounce:
			debounce = nil
			if err := emitIfChanged(); err != nil {
				return
			}
		}
	}
}

// isGitPath reports whether p refers to a path inside a .git directory.
func isGitPath(p string) bool {
	return strings.Contains(p, "/.git/") || strings.HasSuffix(p, "/.git") ||
		strings.Contains(p, "\\.git\\") || strings.HasSuffix(p, "\\.git")
}

// ExplorerWriteFile writes content to a file in a workspace using atomic
// temp-file + rename. It rejects paths inside .git directories, content
// exceeding 2 MB, and paths whose parent directory does not exist.
func (h *Handler) ExplorerWriteFile(w http.ResponseWriter, r *http.Request) {
	req, ok := httpjson.DecodeBody[struct {
		Path      string `json:"path"`
		Workspace string `json:"workspace"`
		Content   string `json:"content"`
	}](w, r)
	if !ok {
		return
	}

	if req.Path == "" || req.Workspace == "" {
		http.Error(w, "path and workspace are required", http.StatusBadRequest)
		return
	}

	if !h.isAllowedWorkspace(r.Context(), req.Workspace) {
		http.Error(w, "workspace not configured", http.StatusBadRequest)
		return
	}

	if len(req.Content) > maxFileWriteSize {
		httpjson.Write(w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": "content exceeds " + strconv.Itoa(maxFileWriteSize) + " byte limit",
		})
		return
	}

	if isGitPath(req.Path) {
		http.Error(w, "writing to .git directories is not allowed", http.StatusBadRequest)
		return
	}

	resolved, err := isWithinWorkspace(req.Path, req.Workspace)
	if err != nil {
		// isWithinWorkspace fails for non-existent paths because EvalSymlinks
		// requires the target to exist. For writes the target file may not
		// exist yet, but its parent directory must (we do not create missing
		// directories). Resolve the parent through symlinks and verify IT is
		// within the workspace, so a symlinked parent cannot redirect the
		// write outside the tree (a plain textual prefix check would not).
		cleaned := filepath.Clean(req.Path)
		parentResolved, perr := filepath.EvalSymlinks(filepath.Dir(cleaned))
		if perr != nil {
			http.Error(w, "parent directory does not exist", http.StatusBadRequest)
			return
		}
		if _, werr := isWithinWorkspace(parentResolved, req.Workspace); werr != nil {
			http.Error(w, werr.Error(), http.StatusBadRequest)
			return
		}
		resolved = filepath.Join(parentResolved, filepath.Base(cleaned))
	}
	if isGitPath(resolved) {
		http.Error(w, "writing to .git directories is not allowed", http.StatusBadRequest)
		return
	}

	// Verify parent directory exists — do not create missing directories.
	dir := filepath.Dir(resolved)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "parent directory does not exist", http.StatusBadRequest)
			return
		}
		http.Error(w, "failed to stat parent directory", http.StatusInternalServerError)
		return
	}

	// Atomic write: temp file in the same directory, then rename.
	data := []byte(req.Content)
	if err := atomicfile.Write(resolved, data, 0o644); err != nil {
		http.Error(w, "failed to write file", http.StatusInternalServerError)
		return
	}

	httpjson.Write(w, http.StatusOK, map[string]any{
		"status": "ok",
		"size":   len(data),
	})
}

// taskPromptEntry is a single entry in the Task Prompts virtual section.
type taskPromptEntry struct {
	TaskID    string    `json:"task_id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ExplorerTaskPrompts returns backlog (and optionally waiting) tasks as virtual
// explorer entries for the Task Prompts section. The ?status= query param is a
// comma-separated list; the default is "backlog". Only "backlog" and "waiting"
// are valid; any other value returns 422. Archived and tombstoned tasks are
// excluded. Results are sorted by UpdatedAt descending.
func (h *Handler) ExplorerTaskPrompts(w http.ResponseWriter, r *http.Request) {
	statusParam := r.URL.Query().Get("status")
	if statusParam == "" {
		statusParam = "backlog"
	}

	parts := strings.Split(statusParam, ",")
	var statuses []store.TaskStatus
	seenStatus := make(map[store.TaskStatus]bool)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		s := store.TaskStatus(p)
		switch s {
		case store.TaskStatusBacklog, store.TaskStatusWaiting:
			if !seenStatus[s] {
				seenStatus[s] = true
				statuses = append(statuses, s)
			}
		default:
			http.Error(w, "invalid status: only backlog and waiting are allowed", http.StatusUnprocessableEntity)
			return
		}
	}

	st, ok := h.requireStore(w)
	if !ok {
		return
	}

	seenID := make(map[string]bool)
	var entries []taskPromptEntry

	for _, status := range statuses {
		tasks, err := st.ListTasksByStatus(r.Context(), status)
		if err != nil {
			http.Error(w, "failed to list tasks", http.StatusInternalServerError)
			return
		}
		for _, t := range tasks {
			if t.Archived {
				continue
			}
			id := t.ID.String()
			if seenID[id] {
				continue
			}
			seenID[id] = true
			title := t.Title
			if title == "" {
				title = t.Prompt
				if len(title) > 80 {
					title = title[:80] + "..."
				}
			}
			entries = append(entries, taskPromptEntry{
				TaskID:    id,
				Title:     title,
				Status:    string(t.Status),
				UpdatedAt: t.UpdatedAt.UTC(),
			})
		}
	}

	// Sort by UpdatedAt descending (most recently updated first).
	slices.SortFunc(entries, func(a, b taskPromptEntry) int {
		return b.UpdatedAt.Compare(a.UpdatedAt)
	})

	if entries == nil {
		entries = []taskPromptEntry{}
	}

	httpjson.Write(w, http.StatusOK, entries)
}
