package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/tail"
	"github.com/google/uuid"
)

// pruneTaskPayload trims the three unboundedly-growing slice fields on t to
// their configured limits, retaining the most-recent (tail) entries. It
// operates in-place and is a no-op when a limit is 0 or the slice is already
// within bounds.
func (s *Store) pruneTaskPayload(t *Task) {
	t.RetryHistory = tail.Of(t.RetryHistory, s.retryHistoryLimit)
	t.RefineSessions = tail.Of(t.RefineSessions, s.refineSessionsLimit)
	t.PromptHistory = tail.Of(t.PromptHistory, s.promptHistoryLimit)
}

// saveTask stamps the schema version, prunes payload, and delegates
// persistence to the storage backend.
// Must be called with s.mu held for writing.
func (s *Store) saveTask(id uuid.UUID, task *Task) error {
	_ = id // task.ID is authoritative; id kept for call-site clarity
	task.SchemaVersion = constants.CurrentTaskSchemaVersion
	pruned := *task // shallow copy; in-memory slices are not modified
	s.pruneTaskPayload(&pruned)
	return s.backend.SaveTask(&pruned)
}

// truncateTurnData applies the per-turn output size budget to data. If
// s.maxTurnOutputBytes > 0 and len(data) exceeds the limit, it scans backwards
// from the limit to find the last newline so a JSON line is not split
// mid-record, then appends a NDJSON truncation_notice sentinel and returns the
// truncated slice together with the original length. Returns (data, 0) when no
// truncation is needed.
func (s *Store) truncateTurnData(data []byte) ([]byte, int) {
	if s.maxTurnOutputBytes <= 0 || len(data) <= s.maxTurnOutputBytes {
		return data, 0
	}
	originalLen := len(data)

	// Find the last newline at or before the budget boundary so we do not split
	// a NDJSON record in the middle.
	cutoff := bytes.LastIndexByte(data[:s.maxTurnOutputBytes], '\n')
	if cutoff < 0 {
		// No newline within the budget; hard-cut at the limit.
		cutoff = s.maxTurnOutputBytes
	}

	sentinel := fmt.Sprintf(
		`{"type":"system","subtype":"truncation_notice","total_bytes":%d,"truncated_at":%d}`,
		originalLen, cutoff,
	)

	result := make([]byte, 0, cutoff+1+len(sentinel)+1)
	result = append(result, data[:cutoff]...)
	result = append(result, '\n')
	result = append(result, sentinel...)
	result = append(result, '\n')
	return result, originalLen
}

// SaveTurnOutput persists raw stdout/stderr for a given turn via the backend.
func (s *Store) SaveTurnOutput(taskID uuid.UUID, turn int, stdout, stderr []byte) error {
	truncated := false

	// Apply the server-side per-turn stdout size budget.
	if truncatedStdout, originalLen := s.truncateTurnData(stdout); originalLen > 0 {
		logger.Store.Warn("turn output truncated",
			"task", taskID, "turn", turn, "original_bytes", originalLen)
		stdout = truncatedStdout
		truncated = true
	}

	stdoutKey := fmt.Sprintf("outputs/turn-%04d.json", turn)
	if err := s.backend.SaveBlob(taskID, stdoutKey, stdout); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}

	if len(stderr) > 0 {
		// Apply the server-side per-turn stderr size budget.
		if truncatedStderr, originalLen := s.truncateTurnData(stderr); originalLen > 0 {
			logger.Store.Warn("turn output truncated",
				"task", taskID, "turn", turn, "original_bytes", originalLen)
			stderr = truncatedStderr
			truncated = true
		}

		stderrKey := fmt.Sprintf("outputs/turn-%04d.stderr.txt", turn)
		if err := s.backend.SaveBlob(taskID, stderrKey, stderr); err != nil {
			return fmt.Errorf("write stderr: %w", err)
		}
	}

	if truncated {
		if err := s.MarkTurnTruncated(context.Background(), taskID, turn); err != nil {
			logger.Store.Warn("failed to mark turn truncated",
				"task", taskID, "turn", turn, "error", err)
		}
	}

	return nil
}

// SaveSummary atomically writes the immutable task summary for a completed task.
func (s *Store) SaveSummary(id uuid.UUID, summary TaskSummary) error {
	data, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	return s.backend.SaveBlob(id, "summary.json", data)
}

// LoadSummary reads the task summary for a completed task.
// Returns (nil, nil) when no summary file exists (task completed before this
// feature was introduced, or task is not in done status).
func (s *Store) LoadSummary(id uuid.UUID) (*TaskSummary, error) {
	data, err := s.backend.ReadBlob(id, "summary.json")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var summary TaskSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

// jsonUnmarshal is a thin wrapper around json.Unmarshal used internally.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
