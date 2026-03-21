package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"changkun.de/x/wallfacer/internal/logger"
	"github.com/google/uuid"
)

// pruneTaskPayload trims the three unboundedly-growing slice fields on t to
// their configured limits, retaining the most-recent (tail) entries. It
// operates in-place and is a no-op when a limit is 0 or the slice is already
// within bounds.
func (s *Store) pruneTaskPayload(t *Task) {
	if s.retryHistoryLimit > 0 && len(t.RetryHistory) > s.retryHistoryLimit {
		t.RetryHistory = t.RetryHistory[len(t.RetryHistory)-s.retryHistoryLimit:]
	}
	if s.refineSessionsLimit > 0 && len(t.RefineSessions) > s.refineSessionsLimit {
		t.RefineSessions = t.RefineSessions[len(t.RefineSessions)-s.refineSessionsLimit:]
	}
	if s.promptHistoryLimit > 0 && len(t.PromptHistory) > s.promptHistoryLimit {
		t.PromptHistory = t.PromptHistory[len(t.PromptHistory)-s.promptHistoryLimit:]
	}
}

// saveTask atomically writes a task's metadata to its task.json file.
// Must be called with s.mu held for writing.
// It stamps SchemaVersion = CurrentTaskSchemaVersion on every write so that
// all on-disk files are always at the current schema version.
// A shallow copy is taken before pruning so that the in-memory task pointer
// retains full slice history for the current server lifetime; only the
// persisted file is bounded.
func (s *Store) saveTask(id uuid.UUID, task *Task) error {
	task.SchemaVersion = CurrentTaskSchemaVersion
	pruned := *task // shallow copy; in-memory slices are not modified
	s.pruneTaskPayload(&pruned)
	path := filepath.Join(s.dir, id.String(), "task.json")
	return atomicWriteJSON(path, &pruned)
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

// SaveTurnOutput persists raw stdout/stderr for a given turn to the outputs directory.
func (s *Store) SaveTurnOutput(taskID uuid.UUID, turn int, stdout, stderr []byte) error {
	outputsDir := filepath.Join(s.dir, taskID.String(), "outputs")
	if err := os.MkdirAll(outputsDir, 0755); err != nil {
		return fmt.Errorf("create outputs dir: %w", err)
	}

	truncated := false

	// Apply the server-side per-turn stdout size budget.
	if truncatedStdout, originalLen := s.truncateTurnData(stdout); originalLen > 0 {
		logger.Store.Warn("turn output truncated",
			"task", taskID, "turn", turn, "original_bytes", originalLen)
		stdout = truncatedStdout
		truncated = true
	}

	name := fmt.Sprintf("turn-%04d.json", turn)
	if err := os.WriteFile(filepath.Join(outputsDir, name), stdout, 0644); err != nil {
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

		stderrName := fmt.Sprintf("turn-%04d.stderr.txt", turn)
		if err := os.WriteFile(filepath.Join(outputsDir, stderrName), stderr, 0644); err != nil {
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

// summaryPath returns the filesystem path for a task's summary.json file.
func (s *Store) summaryPath(id uuid.UUID) string {
	return filepath.Join(s.dir, id.String(), "summary.json")
}

// SaveSummary atomically writes the immutable task summary for a completed task.
func (s *Store) SaveSummary(id uuid.UUID, summary TaskSummary) error {
	return atomicWriteJSON(s.summaryPath(id), summary)
}

// LoadSummary reads the task summary for a completed task.
// Returns (nil, nil) when no summary file exists (task completed before this
// feature was introduced, or task is not in done status).
func (s *Store) LoadSummary(id uuid.UUID) (*TaskSummary, error) {
	data, err := os.ReadFile(s.summaryPath(id))
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

// atomicWriteJSON marshals v to JSON and writes it atomically via temp+rename.
func atomicWriteJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// jsonUnmarshal is a thin wrapper around json.Unmarshal used internally.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
