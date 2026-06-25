package spec

import (
	"fmt"
	"sort"
	"time"
)

// StaleCandidate flags a complete spec whose affects files changed since the
// spec was last updated. It is advisory: surfaced in the explorer for a human
// to accept (mark stale) or dismiss. No status is mutated automatically.
type StaleCandidate struct {
	Path   string   `json:"path"`   // spec tree path
	Files  []string `json:"files"`  // affects paths that changed since updated
	Reason string   `json:"reason"` // human-readable summary
}

// ChangedSinceFunc reports which of the given paths had commits after since.
// The handler implements it via git log; tests inject a deterministic stub.
type ChangedSinceFunc func(since time.Time, paths []string) ([]string, error)

// ScanStaleCandidates flags every non-archived complete spec whose affects
// paths changed since its updated date. Archived, doc, and non-complete specs
// are skipped, as are complete specs with no affects. The scan never mutates
// status — it only surfaces candidates. Results are sorted by path.
func ScanStaleCandidates(tree *Tree, changedSince ChangedSinceFunc) []StaleCandidate {
	var out []StaleCandidate
	for path, node := range tree.All {
		s := node.Value
		if s == nil || s.Doc || s.Status != StatusComplete || len(s.Affects) == 0 {
			continue
		}
		changed, err := changedSince(s.Updated.Time, s.Affects)
		if err != nil || len(changed) == 0 {
			continue
		}
		out = append(out, StaleCandidate{
			Path:   path,
			Files:  changed,
			Reason: fmt.Sprintf("%d affects path(s) changed since %s", len(changed), s.Updated.Format(time.DateOnly)),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
