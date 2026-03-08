package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// oversightPath returns the filesystem path for a task's oversight.json file.
func (s *Store) oversightPath(taskID uuid.UUID) string {
	return filepath.Join(s.dir, taskID.String(), "oversight.json")
}

// testOversightPath returns the filesystem path for a task's oversight-test.json file.
func (s *Store) testOversightPath(taskID uuid.UUID) string {
	return filepath.Join(s.dir, taskID.String(), "oversight-test.json")
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

// SaveTestOversight atomically writes the test-agent oversight summary for a task.
func (s *Store) SaveTestOversight(taskID uuid.UUID, oversight TaskOversight) error {
	return atomicWriteJSON(s.testOversightPath(taskID), oversight)
}

// LoadOversightText reads oversight.json for taskID and concatenates all
// phase Title and Summary fields into a single searchable string.
// Returns ("", nil) when the file does not exist (task never generated oversight).
func (s *Store) LoadOversightText(taskID uuid.UUID) (string, error) {
	data, err := os.ReadFile(s.oversightPath(taskID))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var o TaskOversight
	if err := json.Unmarshal(data, &o); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, phase := range o.Phases {
		if phase.Title != "" {
			sb.WriteString(phase.Title)
			sb.WriteByte(' ')
		}
		if phase.Summary != "" {
			sb.WriteString(phase.Summary)
			sb.WriteByte(' ')
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

// GetTestOversight reads the test-agent oversight summary for a task.
// Returns a pending TaskOversight when no oversight-test.json exists yet.
func (s *Store) GetTestOversight(taskID uuid.UUID) (*TaskOversight, error) {
	data, err := os.ReadFile(s.testOversightPath(taskID))
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
