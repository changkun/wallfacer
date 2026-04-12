package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/pkg/statemachine"
	"changkun.de/x/wallfacer/internal/spec"
)

// GetSpecTree returns the full spec tree with metadata and progress for
// all workspaces. Each workspace's specs/ directory is scanned and the
// results are merged into a single response.
func (h *Handler) GetSpecTree(w http.ResponseWriter, _ *http.Request) {
	workspaces := h.currentWorkspaces()

	var allNodes []spec.NodeResponse
	allProgress := make(map[string]spec.Progress)

	for _, ws := range workspaces {
		specsDir := filepath.Join(ws, "specs")
		tree, err := spec.BuildTree(specsDir)
		if err != nil {
			continue // workspace has no specs/ — skip silently
		}
		resp := spec.SerializeTree(tree)
		allNodes = append(allNodes, resp.Nodes...)
		maps.Copy(allProgress, resp.Progress)
	}

	httpjson.Write(w, http.StatusOK, spec.TreeResponse{
		Nodes:    allNodes,
		Progress: allProgress,
	})
}

// SpecTreeStream sends SSE notifications when the spec tree changes.
// The server polls the spec directories every 3 seconds and sends the
// full tree data only when it differs from the previous snapshot.
func (h *Handler) SpecTreeStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	collectTree := func() spec.TreeResponse {
		workspaces := h.currentWorkspaces()
		var allNodes []spec.NodeResponse
		allProgress := make(map[string]spec.Progress)
		for _, ws := range workspaces {
			specsDir := filepath.Join(ws, "specs")
			tree, err := spec.BuildTree(specsDir)
			if err != nil {
				continue
			}
			resp := spec.SerializeTree(tree)
			allNodes = append(allNodes, resp.Nodes...)
			maps.Copy(allProgress, resp.Progress)
		}
		return spec.TreeResponse{Nodes: allNodes, Progress: allProgress}
	}

	send := func(tree spec.TreeResponse) ([]byte, bool) {
		data, err := json.Marshal(tree)
		if err != nil {
			return nil, false
		}
		if _, err := fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data); err != nil {
			return nil, false
		}
		flusher.Flush()
		return data, true
	}

	current := collectTree()
	curData, ok := send(current)
	if !ok {
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
			if _, err := fmt.Fprintf(w, "event: heartbeat\ndata: {}\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			next := collectTree()
			nextData, err := json.Marshal(next)
			if err != nil {
				continue
			}
			if string(nextData) != string(curData) {
				if _, err := fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", nextData); err != nil {
					return
				}
				flusher.Flush()
				curData = nextData
			}
		}
	}
}

// commitSpecTransition stages and commits a spec frontmatter change so the
// resulting mutation is tracked in git rather than leaving the worktree dirty.
// Returns nil (without committing) when the workspace is not a git repo, when
// nothing is staged (e.g. the file was already at the target status on disk),
// or when git is unavailable. All other errors are returned to the caller.
func commitSpecTransition(
	ctx context.Context,
	workspaces []string,
	absPath, relPath string,
	toStatus spec.Status,
) error {
	ws := findWorkspaceRoot(workspaces, absPath)
	if ws == "" {
		return nil
	}
	// Skip when the workspace is not a git repository — the .env-only
	// workspaces and test harnesses should not block archival.
	if err := cmdexec.Git(ws, "rev-parse", "--git-dir").WithContext(ctx).Run(); err != nil {
		return nil
	}
	if err := cmdexec.Git(ws, "add", relPath).WithContext(ctx).Run(); err != nil {
		return fmt.Errorf("git add %s: %w", relPath, err)
	}
	// Nothing to commit — the frontmatter write may have produced no diff
	// (idempotent re-write with identical bytes); skip silently.
	staged, err := cmdexec.Git(ws, "diff", "--cached", "--name-only", "--", relPath).
		WithContext(ctx).Output()
	if err != nil || strings.TrimSpace(staged) == "" {
		return err
	}
	subject := fmt.Sprintf("%s: mark %s", relPath, toStatus)
	args := []string{"-C", ws}
	args = append(args, hostGitIdentityOverrides(ctx)...)
	args = append(args, "commit", "-m", subject)
	if err := cmdexec.New("git", args...).WithContext(ctx).Run(); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// findWorkspaceRoot returns the workspace directory containing absPath, or
// empty string if no configured workspace is an ancestor.
func findWorkspaceRoot(workspaces []string, absPath string) string {
	for _, ws := range workspaces {
		abs, err := filepath.Abs(ws)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(abs, absPath)
		if err != nil {
			continue
		}
		if rel == "" || strings.HasPrefix(rel, "..") {
			continue
		}
		return abs
	}
	return ""
}

// ArchiveSpec transitions a spec's status to archived.
func (h *Handler) ArchiveSpec(w http.ResponseWriter, r *http.Request) {
	h.transitionSpec(w, r, spec.StatusArchived)
}

// UnarchiveSpec transitions an archived spec back to drafted.
func (h *Handler) UnarchiveSpec(w http.ResponseWriter, r *http.Request) {
	h.transitionSpec(w, r, spec.StatusDrafted)
}

type specTransitionRequest struct {
	Path string `json:"path"`
}

type specTransitionResponse struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// transitionSpec validates and writes a status transition for a single spec.
// Invalid transitions return 422; archiving a spec with a live dispatched_task_id
// returns 409.
func (h *Handler) transitionSpec(w http.ResponseWriter, r *http.Request, toStatus spec.Status) {
	req, ok := httpjson.DecodeBody[specTransitionRequest](w, r)
	if !ok {
		return
	}
	if req.Path == "" {
		http.Error(w, "path must not be empty", http.StatusBadRequest)
		return
	}

	workspaces := h.currentWorkspaces()
	if len(workspaces) == 0 {
		http.Error(w, "no workspaces configured", http.StatusInternalServerError)
		return
	}

	absPath := findSpecFile(workspaces, req.Path)
	if absPath == "" {
		http.Error(w, "spec file not found in any workspace", http.StatusNotFound)
		return
	}

	s, err := spec.ParseFile(absPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("parse error: %v", err), http.StatusBadRequest)
		return
	}

	if err := spec.StatusMachine.Validate(s.Status, toStatus); err != nil {
		if errors.Is(err, statemachine.ErrInvalidTransition) {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if toStatus == spec.StatusArchived && s.DispatchedTaskID != nil {
		http.Error(w,
			"spec has a dispatched task — cancel the dispatched task before archiving",
			http.StatusConflict)
		return
	}

	if err := spec.UpdateFrontmatter(absPath, map[string]any{
		"status":  string(toStatus),
		"updated": time.Now(),
	}); err != nil {
		http.Error(w, fmt.Sprintf("update frontmatter: %v", err), http.StatusInternalServerError)
		return
	}

	// Commit the status change. Failure is non-fatal — the frontmatter is
	// already updated on disk and the client gets a success response either
	// way. A missed commit just leaves a dirty worktree the user can stage
	// manually; blocking the UI on git issues would be worse UX.
	if err := commitSpecTransition(r.Context(), workspaces, absPath, req.Path, toStatus); err != nil {
		slog.Warn("spec transition commit failed",
			"path", req.Path, "status", toStatus, "err", err)
	}

	httpjson.Write(w, http.StatusOK, specTransitionResponse{
		Path:   req.Path,
		Status: string(toStatus),
	})
}
