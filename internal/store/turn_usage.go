package store

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// turnUsagePath returns the JSONL file path for a task's per-turn usage log.
func (s *Store) turnUsagePath(taskID uuid.UUID) string {
	return filepath.Join(s.dir, taskID.String(), "turn-usage.jsonl")
}

// AppendTurnUsage appends a single TurnUsageRecord to the task's JSONL log.
// The file is created on first write. Each line is a complete JSON object.
// No store lock is taken because filesystem appends < 4KB are atomic on Linux.
func (s *Store) AppendTurnUsage(taskID uuid.UUID, rec TurnUsageRecord) error {
	path := s.turnUsagePath(taskID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	line, err := json.Marshal(rec)
	if err != nil {
		_ = f.Close()
		return err
	}
	if _, err = f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// GetTurnUsages reads and returns all TurnUsageRecord entries for a task.
// Returns an empty (non-nil) slice if no log exists yet.
func (s *Store) GetTurnUsages(taskID uuid.UUID) ([]TurnUsageRecord, error) {
	path := s.turnUsagePath(taskID)
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return []TurnUsageRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	var records []TurnUsageRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var rec TurnUsageRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err == nil {
			records = append(records, rec)
		}
	}
	if records == nil {
		records = []TurnUsageRecord{}
	}
	return records, scanner.Err()
}
