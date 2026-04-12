package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/pkg/statemachine"
	"changkun.de/x/wallfacer/internal/spec"
)

// collectSpecTree merges the spec trees across all workspaces into a
// single TreeResponse and attaches the roadmap index (specs/README.md
// from the first workspace that has one). Shared by GetSpecTree and
// the SSE SpecTreeStream so both surfaces emit the same shape.
func (h *Handler) collectSpecTree() spec.TreeResponse {
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

	index, err := spec.ResolveIndex(workspaces)
	if err != nil {
		slog.Warn("resolve roadmap index failed", "err", err)
	}

	return spec.TreeResponse{
		Nodes:    allNodes,
		Progress: allProgress,
		Index:    index,
	}
}

// GetSpecTree returns the full spec tree with metadata, progress, and
// an optional roadmap index for all workspaces. Each workspace's specs/
// directory is scanned and the results are merged into a single response.
func (h *Handler) GetSpecTree(w http.ResponseWriter, _ *http.Request) {
	httpjson.Write(w, http.StatusOK, h.collectSpecTree())
}

// SpecTreeStream sends SSE notifications when the spec tree changes.
// The server polls the spec directories every 3 seconds and sends the
// full tree data only when it differs from the previous snapshot.
// Changes to the roadmap (specs/README.md) also fire a snapshot via
// this path since the poller serialises the full TreeResponse and
// compares the JSON — any field-level change drives a new event.
func (h *Handler) SpecTreeStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	collectTree := h.collectSpecTree

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

// ArchiveSpec transitions a spec's status to archived, cascading to all
// descendants so the subtree moves as a unit. All status changes land in a
// single commit; unarchive reverses the cascade by reverting that commit.
func (h *Handler) ArchiveSpec(w http.ResponseWriter, r *http.Request) {
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

	// Collect parent + descendants that are eligible for archival. Descendants
	// already archived are skipped (idempotent); any blocker (invalid transition
	// or live dispatched_task_id) on the primary or an eligible descendant
	// rejects the whole cascade.
	targets, err := collectArchiveTargets(absPath, req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, t := range targets {
		if err := spec.StatusMachine.Validate(t.spec.Status, spec.StatusArchived); err != nil {
			if errors.Is(err, statemachine.ErrInvalidTransition) {
				http.Error(w,
					fmt.Sprintf("%s: %v", t.relPath, err),
					http.StatusUnprocessableEntity)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if t.spec.DispatchedTaskID != nil {
			http.Error(w,
				fmt.Sprintf("%s: cancel the dispatched task before archiving", t.relPath),
				http.StatusConflict)
			return
		}
	}

	// Apply the status change to every target in a loop. If a mid-loop write
	// fails, surface the error — the caller sees a partial apply, but the
	// already-written files remain archived on disk (no rollback). This
	// matches how dispatch handles partial writes (error bubbles up).
	now := time.Now()
	for _, t := range targets {
		if err := spec.UpdateFrontmatter(t.absPath, map[string]any{
			"status":  string(spec.StatusArchived),
			"updated": now,
		}); err != nil {
			http.Error(w,
				fmt.Sprintf("update %s: %v", t.relPath, err),
				http.StatusInternalServerError)
			return
		}
	}

	// Commit all frontmatter changes in one commit so unarchive can revert it.
	subject := archiveCommitSubject(req.Path, len(targets)-1)
	paths := make([]string, 0, len(targets))
	for _, t := range targets {
		paths = append(paths, t.relPath)
	}
	if err := commitSpecChanges(r.Context(), workspaces, absPath, paths, subject); err != nil {
		slog.Warn("spec archive commit failed",
			"path", req.Path, "err", err)
	}

	httpjson.Write(w, http.StatusOK, specTransitionResponse{
		Path:   req.Path,
		Status: string(spec.StatusArchived),
	})
}

// UnarchiveSpec reverses a prior archive by reverting the archive commit, which
// restores every spec in the subtree to its pre-archive status losslessly. Falls
// back to a single-spec `archived → drafted` transition when no matching archive
// commit can be found (spec was archived by hand, outside the UI).
func (h *Handler) UnarchiveSpec(w http.ResponseWriter, r *http.Request) {
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
	if s.Status != spec.StatusArchived {
		http.Error(w, "spec is not archived", http.StatusUnprocessableEntity)
		return
	}

	ws := findWorkspaceRoot(workspaces, absPath)
	if ws != "" && isGitRepo(r.Context(), ws) {
		if sha := findArchiveCommit(r.Context(), ws, req.Path); sha != "" {
			err := revertArchiveCommit(r.Context(), ws, sha)
			if err == nil {
				httpjson.Write(w, http.StatusOK, specTransitionResponse{
					Path:   req.Path,
					Status: string(spec.StatusDrafted),
				})
				return
			}
			slog.Warn("revert archive commit failed, falling back to single-spec unarchive",
				"path", req.Path, "sha", sha, "err", err)
		}
	}

	// Fallback: single-spec transition archived → drafted.
	if err := spec.UpdateFrontmatter(absPath, map[string]any{
		"status":  string(spec.StatusDrafted),
		"updated": time.Now(),
	}); err != nil {
		http.Error(w, fmt.Sprintf("update frontmatter: %v", err), http.StatusInternalServerError)
		return
	}
	if err := commitSpecTransition(r.Context(), workspaces, absPath, req.Path, spec.StatusDrafted); err != nil {
		slog.Warn("unarchive fallback commit failed",
			"path", req.Path, "err", err)
	}

	httpjson.Write(w, http.StatusOK, specTransitionResponse{
		Path:   req.Path,
		Status: string(spec.StatusDrafted),
	})
}

// archiveTarget bundles a spec's filesystem path, tree-relative path, and
// parsed frontmatter for validation during cascade collection.
type archiveTarget struct {
	absPath string
	relPath string
	spec    *spec.Spec
}

// collectArchiveTargets returns the primary spec plus every descendant spec
// under its companion directory. Already-archived descendants are skipped.
func collectArchiveTargets(primaryAbs, primaryRel string) ([]archiveTarget, error) {
	primary, err := spec.ParseFile(primaryAbs)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", primaryRel, err)
	}
	targets := []archiveTarget{{primaryAbs, primaryRel, primary}}

	// The companion directory for foo.md is foo/ (stripped of .md).
	companion := strings.TrimSuffix(primaryAbs, ".md")
	info, err := os.Stat(companion)
	if err != nil || !info.IsDir() {
		return targets, nil
	}

	err = filepath.WalkDir(companion, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		s, err := spec.ParseFile(path)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if s.Status == spec.StatusArchived {
			return nil
		}
		// Reconstruct the tree-relative path for git staging.
		descendantRel := primaryRel[:len(primaryRel)-3] + path[len(companion):]
		descendantRel = filepath.ToSlash(descendantRel)
		targets = append(targets, archiveTarget{path, descendantRel, s})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return targets, nil
}

// archiveCommitSubject is the unique subject used for archive commits so
// unarchive can locate them via `git log --grep`. Descendant count is
// informational only — the grep pattern matches the fixed prefix.
func archiveCommitSubject(relPath string, descendants int) string {
	if descendants <= 0 {
		return fmt.Sprintf("%s: archive", relPath)
	}
	return fmt.Sprintf("%s: archive (1 + %d descendants)", relPath, descendants)
}

// archiveCommitSubjectPrefix is the pinned prefix used to locate the archive
// commit for a given spec path during unarchive.
func archiveCommitSubjectPrefix(relPath string) string {
	return relPath + ": archive"
}

// commitSpecChanges stages the given set of paths and commits them with the
// given subject. Non-fatal on missing git repo or empty staged set.
func commitSpecChanges(
	ctx context.Context,
	workspaces []string,
	absPath string,
	relPaths []string,
	subject string,
) error {
	ws := findWorkspaceRoot(workspaces, absPath)
	if ws == "" {
		return nil
	}
	if !isGitRepo(ctx, ws) {
		return nil
	}
	addArgs := append([]string{"add", "--"}, relPaths...)
	if err := cmdexec.Git(ws, addArgs...).WithContext(ctx).Run(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	diffArgs := append([]string{"diff", "--cached", "--name-only", "--"}, relPaths...)
	staged, err := cmdexec.Git(ws, diffArgs...).WithContext(ctx).Output()
	if err != nil {
		return err
	}
	if strings.TrimSpace(staged) == "" {
		return nil
	}
	args := []string{"-C", ws}
	args = append(args, hostGitIdentityOverrides(ctx)...)
	args = append(args, "commit", "-m", subject)
	if err := cmdexec.New("git", args...).WithContext(ctx).Run(); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

func isGitRepo(ctx context.Context, ws string) bool {
	return cmdexec.Git(ws, "rev-parse", "--git-dir").WithContext(ctx).Run() == nil
}

// findArchiveCommit looks up the most recent commit whose subject begins with
// `<relPath>: archive`. Returns empty string if none is found.
func findArchiveCommit(ctx context.Context, ws, relPath string) string {
	// `--grep` uses a regex; anchor the start and escape the spec path so a
	// path with regex metachars (unlikely but possible) doesn't confuse it.
	prefix := archiveCommitSubjectPrefix(relPath)
	pattern := "^" + regexpQuote(prefix)
	out, err := cmdexec.Git(ws,
		"log", "--format=%H", "-1", "--grep", pattern, "--", relPath).
		WithContext(ctx).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// regexpQuote escapes regex metacharacters for use in grep patterns.
func regexpQuote(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '.', '+', '*', '?', '(', ')', '[', ']', '{', '}', '^', '$', '|', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// revertArchiveCommit creates a revert commit that undoes the given archive
// commit, restoring each descendant's prior status. If `git revert` can't
// proceed (e.g. conflicts with later edits), it aborts cleanly and returns the
// error so the caller can fall back to single-spec unarchive.
func revertArchiveCommit(ctx context.Context, ws, sha string) error {
	args := []string{"-C", ws}
	args = append(args, hostGitIdentityOverrides(ctx)...)
	args = append(args, "revert", "--no-edit", sha)
	if err := cmdexec.New("git", args...).WithContext(ctx).Run(); err != nil {
		// Best-effort cleanup so the working tree isn't left in a half-reverted state.
		_ = cmdexec.Git(ws, "revert", "--abort").WithContext(ctx).Run()
		return fmt.Errorf("git revert %s: %w", sha, err)
	}
	return nil
}

type specTransitionRequest struct {
	Path string `json:"path"`
}

type specTransitionResponse struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}
