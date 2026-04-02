package runner

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/envutil"
	"changkun.de/x/wallfacer/internal/pkg/ndjson"
)

// ideationHistoryTTL is how long rejected-idea records are retained (default 30 days).
// Override via WALLFACER_IDEATION_HISTORY_TTL (e.g. "720h"). Evaluated once at init.
var ideationHistoryTTL = envutil.DurationMin("WALLFACER_IDEATION_HISTORY_TTL", 30*24*time.Hour, 1)

// HistoryEntry records the outcome of a single idea evaluated during ideation.
type HistoryEntry struct {
	Title string `json:"title"`
	// Reason is one of: "accepted", "rejected_threshold", "rejected_duplicate", "rejected_degenerate"
	Reason     string    `json:"reason"`
	TaskID     string    `json:"task_id,omitempty"` // set for accepted entries
	RecordedAt time.Time `json:"recorded_at"`
}

// IdeationHistory is an append-only log of idea outcomes stored as JSONL.
// It records both accepted and rejected ideas so that future brainstorm
// runs can avoid proposing duplicates of recently rejected titles. Entries
// older than ideationHistoryTTL are pruned on load.
type IdeationHistory struct {
	path    string
	entries []HistoryEntry
}

// ideationHistoryPath returns the path to the history file for the given data dir.
func ideationHistoryPath(dataDir string) string {
	return filepath.Join(dataDir, "ideation-history.jsonl")
}

// LoadHistory reads the ideation history from {dataDir}/ideation-history.jsonl.
// Entries older than the configured TTL are silently dropped.
// Truncated or corrupt trailing lines are skipped with a warning.
// Returns an empty (non-nil) history when the file does not exist yet.
func LoadHistory(dataDir string) (*IdeationHistory, error) {
	h := &IdeationHistory{path: ideationHistoryPath(dataDir)}

	cutoff := time.Now().Add(-ideationHistoryTTL)
	err := ndjson.ReadFileFunc[HistoryEntry](h.path, func(e HistoryEntry) bool {
		if !e.RecordedAt.Before(cutoff) {
			h.entries = append(h.entries, e)
		}
		return true
	}, ndjson.WithOnError(func(lineNum int, err error) {
		logger.Runner.Warn("ideation history: skipping malformed line",
			"path", h.path, "line", lineNum, "error", err)
	}))
	if err != nil {
		return nil, fmt.Errorf("read ideation history: %w", err)
	}
	return h, nil
}

// Append atomically appends one entry to the history file.
// It opens the file in append mode so no existing data is read or rewritten.
func (h *IdeationHistory) Append(e HistoryEntry) error {
	if e.RecordedAt.IsZero() {
		e.RecordedAt = time.Now().UTC()
	}
	return ndjson.AppendFile(h.path, e)
}

// Round returns the number of ideation rounds recorded in the history.
// This counts distinct accepted entries as a proxy for past brainstorm runs.
func (h *IdeationHistory) Round() int {
	n := 0
	for _, e := range h.entries {
		if e.Reason == "accepted" {
			n++
		}
	}
	return n
}

// RejectedTitles returns the titles of all entries within the TTL whose reason
// is not "accepted". The returned slice is deduplicated by lower-cased title.
func (h *IdeationHistory) RejectedTitles() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, e := range h.entries {
		if e.Reason == "accepted" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(e.Title))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, e.Title)
	}
	return out
}
