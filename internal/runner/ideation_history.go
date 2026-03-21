package runner

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/logger"
)

// ideationHistoryTTL is how long rejected-idea records are retained.
// Override via WALLFACER_IDEATION_HISTORY_TTL (e.g. "720h").
var ideationHistoryTTL = func() time.Duration {
	if v := os.Getenv("WALLFACER_IDEATION_HISTORY_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 30 * 24 * time.Hour
}()

// HistoryEntry records the outcome of a single idea evaluated during ideation.
type HistoryEntry struct {
	Title      string    `json:"title"`
	// Reason is one of: "accepted", "rejected_threshold", "rejected_duplicate", "rejected_degenerate"
	Reason     string    `json:"reason"`
	TaskID     string    `json:"task_id,omitempty"` // set for accepted entries
	RecordedAt time.Time `json:"recorded_at"`
}

// IdeationHistory is an append-only log of idea outcomes stored as JSONL.
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

	f, err := os.Open(h.path)
	if err != nil {
		if os.IsNotExist(err) {
			return h, nil
		}
		return nil, fmt.Errorf("open ideation history: %w", err)
	}
	defer func() { _ = f.Close()
 }()

	cutoff := time.Now().Add(-ideationHistoryTTL)
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e HistoryEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			logger.Runner.Warn("ideation history: skipping malformed line",
				"path", h.path, "line", lineNum, "error", err)
			continue
		}
		if e.RecordedAt.Before(cutoff) {
			continue // expired
		}
		h.entries = append(h.entries, e)
	}
	if err := scanner.Err(); err != nil {
		// Truncated file mid-line — log a warning but return what we have.
		logger.Runner.Warn("ideation history: read error (truncated file?)",
			"path", h.path, "error", err)
	}
	return h, nil
}

// Append atomically appends one entry to the history file.
// It opens the file in append mode so no existing data is read or rewritten.
func (h *IdeationHistory) Append(e HistoryEntry) error {
	if e.RecordedAt.IsZero() {
		e.RecordedAt = time.Now().UTC()
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal history entry: %w", err)
	}

	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open ideation history for append: %w", err)
	}

	if _, err := fmt.Fprintf(f, "%s\n", data); err != nil {
		_ = f.Close()

		return fmt.Errorf("write ideation history entry: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close ideation history: %w", err)
	}
	return nil
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
