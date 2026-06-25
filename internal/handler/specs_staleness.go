package handler

import (
	"context"
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

// gitChangedSince returns a ChangedSinceFunc backed by git log over the given
// workspace. For each path it asks whether any commit landed after since,
// matching on commit date (`--since`). Paths that error (no git repo, bad
// pathspec) are treated as unchanged.
func gitChangedSince(ctx context.Context, ws string) spec.ChangedSinceFunc {
	return func(since time.Time, paths []string) ([]string, error) {
		var changed []string
		for _, p := range paths {
			out, err := cmdexec.Git(ws, "log",
				"--since="+since.Format(time.RFC3339), "-n", "1", "--format=%H",
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
