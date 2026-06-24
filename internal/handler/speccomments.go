package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"latere.ai/x/wallfacer/internal/coordinator"
	"latere.ai/x/wallfacer/internal/gitutil"
	"latere.ai/x/wallfacer/internal/spec"
	"latere.ai/x/wallfacer/internal/speccomment"
)

// SetCommentRelay attaches the instance-side spec-comment relay. Without it the
// comment endpoints report coordination unavailable (the feature is off).
func (h *Handler) SetCommentRelay(r *CommentRelay) {
	h.snapshotMu.Lock()
	h.commentRelay = r
	h.snapshotMu.Unlock()
}

// CoordinationToggle is the runtime opt-in gate, set once at startup. The
// settings endpoints read and flip it; the connector re-reads it every cycle.
type CoordinationToggle interface {
	OptedIn() bool
	SetOptedIn(bool)
}

// SetCoordinationToggle attaches the coordination opt-in gate.
func (h *Handler) SetCoordinationToggle(t CoordinationToggle) {
	h.snapshotMu.Lock()
	h.coordToggle = t
	h.snapshotMu.Unlock()
}

func (h *Handler) coordinationToggle() CoordinationToggle {
	h.snapshotMu.RLock()
	defer h.snapshotMu.RUnlock()
	return h.coordToggle
}

// GetCoordinationStatus reports whether coordination is opted in. The browser
// uses it to render the settings switch and decide whether to show the comment
// UI.
func (h *Handler) GetCoordinationStatus(w http.ResponseWriter, _ *http.Request) {
	t := h.coordinationToggle()
	optedIn := t != nil && t.OptedIn()
	writeCommentJSON(w, map[string]any{"opted_in": optedIn, "available": t != nil})
}

// SetCoordinationOptIn flips the coordination opt-in. Body {"enabled": bool}.
func (h *Handler) SetCoordinationOptIn(w http.ResponseWriter, r *http.Request) {
	t := h.coordinationToggle()
	if t == nil {
		http.Error(w, "coordination unavailable", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	t.SetOptedIn(req.Enabled)
	writeCommentJSON(w, map[string]any{"opted_in": req.Enabled})
}

func (h *Handler) relay() *CommentRelay {
	h.snapshotMu.RLock()
	defer h.snapshotMu.RUnlock()
	return h.commentRelay
}

// specCommentThread is a thread plus its display-time reposition result: the
// current line its anchor resolves to in the live spec body, and whether the
// anchor was lost (orphaned). Reposition runs instance-side on each load, the
// portable content-hash path; the coordinator holds no source and never does it.
type specCommentThread struct {
	speccomment.Thread
	Line     int  `json:"line"`     // 1-based current line, 0 when orphaned
	Orphaned bool `json:"orphaned"` // anchor could not be resolved against the body
}

// ListSpecComments returns the comment threads for every repo the visible
// workspaces map to, each repositioned against the current spec body. The
// browser filters by spec path for the inline view and uses the orphaned set for
// the triage list. Repo identity is resolved here (instance-side), never sent by
// the browser.
func (h *Handler) ListSpecComments(w http.ResponseWriter, r *http.Request) {
	relay := h.relay()
	if relay == nil {
		writeCommentJSON(w, map[string]any{"threads": []specCommentThread{}})
		return
	}
	repoToRoot := h.repoRoots(r)
	out := []specCommentThread{}
	for repo, root := range repoToRoot {
		for _, t := range relay.ThreadsForRepo(repo) {
			out = append(out, repositionThread(t, root))
		}
	}
	writeCommentJSON(w, map[string]any{"threads": out})
}

// writeCommentJSON writes v as a JSON 200 response.
func writeCommentJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

// submitSpecCommentReq is the browser's op. Spec is the spec path relative to
// the workspace specs/ dir (the same identifier the spec tree uses); the handler
// resolves the repo from it. For create, StartLine/EndLine are the selected
// source line range; the handler reads the body and computes the anchor.
type submitSpecCommentReq struct {
	Op        string `json:"op"`
	Spec      string `json:"spec"`
	Body      string `json:"body,omitempty"`
	ThreadID  string `json:"thread_id,omitempty"`
	ParentID  string `json:"parent_id,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

// SubmitSpecComment forwards a browser-initiated op (create/reply/resolve/
// reopen) up the coordination connection. The coordinator is authoritative: it
// mints ids, stamps the principal, and echoes the result back down, which the
// relay applies and streams to browsers. This handler builds the wire op only.
func (h *Handler) SubmitSpecComment(w http.ResponseWriter, r *http.Request) {
	relay := h.relay()
	if relay == nil {
		http.Error(w, "coordination unavailable", http.StatusServiceUnavailable)
		return
	}
	var req submitSpecCommentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Spec == "" {
		http.Error(w, "spec required", http.StatusBadRequest)
		return
	}
	repo, root, ok := h.resolveSpecRepo(r, req.Spec)
	if !ok || repo == "" {
		http.Error(w, "spec workspace has no git remote", http.StatusBadRequest)
		return
	}

	ev := speccomment.Event{Op: req.Op, Repo: repo}
	switch req.Op {
	case speccomment.OpCreate:
		body, err := os.ReadFile(filepath.Join(root, "specs", req.Spec))
		if err != nil {
			http.Error(w, "spec not found", http.StatusNotFound)
			return
		}
		parsed, err := spec.ParseBytes(body, req.Spec)
		if err != nil {
			http.Error(w, "spec parse failed", http.StatusUnprocessableEntity)
			return
		}
		anchor := spec.ComputeAnchor(parsed.Body, req.StartLine, req.EndLine)
		ev.Thread = &speccomment.Thread{
			SpecPath: req.Spec,
			Anchor:   anchor,
			Comments: []speccomment.Comment{{Body: req.Body}},
		}
	case speccomment.OpReply:
		ev.Comment = &speccomment.Comment{ThreadID: req.ThreadID, ParentID: req.ParentID, Body: req.Body}
	case speccomment.OpResolve, speccomment.OpReopen:
		ev.Thread = &speccomment.Thread{ID: req.ThreadID}
	default:
		http.Error(w, "unsupported op", http.StatusBadRequest)
		return
	}

	if err := relay.Submit(ev); err != nil {
		if errors.Is(err, ErrCoordinatorUnavailable) {
			http.Error(w, "coordination unavailable", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "submit failed", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// StreamSpecComments is the browser SSE stream for spec-comment events. The
// relay broadcasts every coordinator op here, so a teammate's comment appears
// without a reload.
func (h *Handler) StreamSpecComments(w http.ResponseWriter, r *http.Request) {
	relay := h.relay()
	flusher, ok := w.(http.Flusher)
	if relay == nil || !ok {
		http.Error(w, "stream unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	id, ch := relay.Subscribe()
	defer relay.Unsubscribe(id)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "event: spec-comment\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// repoRoots maps each visible workspace's normalized git remote to its root
// folder. A workspace with no remote contributes nothing (local-only, never
// joins collaboration).
func (h *Handler) repoRoots(r *http.Request) map[string]string {
	out := make(map[string]string)
	for _, ws := range h.visibleWorkspaces(r.Context()) {
		repo := coordinator.NormalizeRemoteURL(gitutil.WorkspaceStatus(ws).RemoteURL)
		if repo != "" {
			out[repo] = ws
		}
	}
	return out
}

// resolveSpecRepo finds the repo and workspace root for a spec path by locating
// the visible workspace whose specs/<spec> exists.
func (h *Handler) resolveSpecRepo(r *http.Request, specPath string) (repo, root string, ok bool) {
	for _, ws := range h.visibleWorkspaces(r.Context()) {
		if _, err := os.Stat(filepath.Join(ws, "specs", specPath)); err != nil {
			continue
		}
		repo = coordinator.NormalizeRemoteURL(gitutil.WorkspaceStatus(ws).RemoteURL)
		return repo, ws, true
	}
	return "", "", false
}

// repositionThread recomputes a thread's anchor against the current spec body
// (the content-hash path). A thread whose anchor is lost is marked orphaned; the
// browser drops it from the inline view and surfaces it in the triage list.
func repositionThread(t speccomment.Thread, root string) specCommentThread {
	if t.Status == speccomment.StatusOutdated {
		return specCommentThread{Thread: t}
	}
	body, err := os.ReadFile(filepath.Join(root, "specs", t.SpecPath))
	if err != nil {
		return specCommentThread{Thread: t, Orphaned: true}
	}
	parsed, err := spec.ParseBytes(body, t.SpecPath)
	if err != nil {
		return specCommentThread{Thread: t, Orphaned: true}
	}
	newAnchor, line, ok := spec.Reposition(parsed.Body, t.Anchor)
	if !ok {
		return specCommentThread{Thread: t, Orphaned: true}
	}
	t.Anchor = newAnchor
	return specCommentThread{Thread: t, Line: line}
}
