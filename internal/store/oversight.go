package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// oversightPath returns the filesystem path for a task's oversight.json file.
func (s *Store) oversightPath(taskID uuid.UUID) string {
	return filepath.Join(s.dir, taskID.String(), "oversight.json")
}

// SaveOversight atomically writes the oversight summary for a task.
func (s *Store) SaveOversight(taskID uuid.UUID, oversight TaskOversight) error {
	return atomicWriteJSON(s.oversightPath(taskID), oversight)
}

// GetOversight reads the oversight summary for a task.
// Returns (nil, nil) when no oversight file exists yet (status pending).
func (s *Store) GetOversight(taskID uuid.UUID) (*TaskOversight, error) {
	data, err := os.ReadFile(s.oversightPath(taskID))
	if errors.Is(err, os.ErrNotExist) {
		pending := TaskOversight{Status: OversightStatusPending}
		return &pending, nil
	}
	if err != nil {
		return nil, err
	}
	var o TaskOversight
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, err
	}
	return &o, nil
}
