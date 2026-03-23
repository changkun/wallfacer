package store

import (
	"path/filepath"

	"changkun.de/x/wallfacer/internal/pkg/ndjson"
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
	return ndjson.AppendFile(s.turnUsagePath(taskID), rec)
}

// GetTurnUsages reads and returns all TurnUsageRecord entries for a task.
// Returns an empty (non-nil) slice if no log exists yet.
func (s *Store) GetTurnUsages(taskID uuid.UUID) ([]TurnUsageRecord, error) {
	return ndjson.ReadFile[TurnUsageRecord](s.turnUsagePath(taskID))
}
