package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"latere.ai/x/wallfacer/internal/pkg/cmdexec"
	"latere.ai/x/wallfacer/internal/pkg/httpjson"
	"latere.ai/x/wallfacer/internal/spec"
)

// StaleCandidates runs the advisory cross-tree staleness scan: for every
// non-archived complete spec, it flags the spec when any of its affects paths
// has a commit newer than the spec's updated date. It catches drift from code
// changes that bypass the spec-dispatch and chat-edit flows (manual edits,
// refactors). Nothing is mutated; the response is advisory.
func (h *Handler) StaleCandidates(w http.ResponseWriter, r *http.Request) {
	workspaces := h.visibleWorkspaces(r.Context())
	candidates := []spec.StaleCandidate{}
	for _, ws := range workspaces {
		tree, err := spec.BuildTree(filepath.Join(ws, "specs"))
		if err != nil {
			continue
		}
		candidates = append(candidates, spec.ScanStaleCandidates(tree, gitChangedSince(r.Context(), ws))...)
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"candidates": candidates})
}

// DismissAllStaleCandidates bumps the updated timestamp on every flagged stale
// candidate, in one commit per workspace — the bulk "I reviewed these, the
// designs still hold" action. Status is never changed. Returns the count.
func (h *Handler) DismissAllStaleCandidates(w http.ResponseWriter, r *http.Request) {
	if !h.requireVisibleWorkspace(w, r) {
		return
	}
	workspaces := h.currentWorkspaces()
	if len(workspaces) == 0 {
		http.Error(w, "no workspaces configured", http.StatusInternalServerError)
		return
	}

	dismissed := 0
	for _, ws := range workspaces {
		tree, err := spec.BuildTree(filepath.Join(ws, "specs"))
		if err != nil {
			continue
		}
		candidates := spec.ScanStaleCandidates(tree, gitChangedSince(r.Context(), ws))
		if len(candidates) == 0 {
			continue
		}
		var rels []string
		var firstAbs string
		for _, c := range candidates {
			abs := filepath.Join(ws, filepath.FromSlash(c.Path))
			if err := spec.UpdateFrontmatter(abs, map[string]any{"updated": time.Now()}); err != nil {
				slog.Warn("dismiss-all: update frontmatter", "spec", c.Path, "err", err)
				continue
			}
			if firstAbs == "" {
				firstAbs = abs
			}
			rels = append(rels, c.Path)
			dismissed++
		}
		if len(rels) > 0 {
			subject := fmt.Sprintf("specs: dismiss %d stale candidate(s)", len(rels))
			if err := commitSpecChanges(r.Context(), workspaces, firstAbs, rels, subject); err != nil {
				slog.Warn("dismiss-all: commit", "ws", ws, "err", err)
			}
		}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"dismissed": dismissed})
}

// gitChangedSince returns a ChangedSinceFunc backed by git log over the given
// workspace. For each path it asks whether any commit landed after since,
// matching on commit date (`--since`). Paths that error (no git repo, bad
// pathspec) are treated as unchanged.
func gitChangedSince(ctx context.Context, ws string) spec.ChangedSinceFunc {
	return func(since time.Time, paths []string) ([]string, error) {
		// updated is date-granular (midnight). Flag only commits on a day
		// strictly after it, so a same-day commit does not perpetually flag a
		// spec and Dismiss (which bumps updated to today) actually clears it.
		threshold := since.AddDate(0, 0, 1)
		var changed []string
		for _, p := range paths {
			out, err := cmdexec.Git(ws, "log",
				"--since="+threshold.Format(time.RFC3339), "-n", "1", "--format=%H",
				"--", filepath.FromSlash(p)).WithContext(ctx).Output()
			if err != nil {
				continue
			}
			if strings.TrimSpace(out) != "" {
				changed = append(changed, p)
			}
		}
		return changed, nil
	}
}
