package store

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/google/uuid"
)

// oversightText concatenates all phase titles and summaries from an oversight
// object into a single space-separated string suitable for indexing or search.
func oversightText(o TaskOversight) string {
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
	return strings.TrimSpace(sb.String())
}

// SaveOversight writes the oversight summary for a task via the backend and
// updates the in-memory search index.
func (s *Store) SaveOversight(taskID uuid.UUID, oversight TaskOversight) error {
	data, err := json.Marshal(oversight)
	if err != nil {
		return err
	}
	if err := s.backend.SaveBlob(taskID, "oversight.json", data); err != nil {
		return err
	}
	raw := oversightText(oversight)
	s.mu.Lock()
	if entry, ok := s.searchIndex[taskID]; ok {
		entry.oversight = strings.ToLower(raw)
		entry.oversightRaw = raw
		s.searchIndex[taskID] = entry
	}
	s.mu.Unlock()
	return nil
}

// GetOversight reads the oversight summary for a task.
// Returns a pending TaskOversight when no oversight file exists yet.
func (s *Store) GetOversight(taskID uuid.UUID) (*TaskOversight, error) {
	data, err := s.backend.ReadBlob(taskID, "oversight.json")
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

// SaveTestOversight writes the test-agent oversight summary for a task via the backend.
func (s *Store) SaveTestOversight(taskID uuid.UUID, oversight TaskOversight) error {
	data, err := json.Marshal(oversight)
	if err != nil {
		return err
	}
	return s.backend.SaveBlob(taskID, "oversight-test.json", data)
}

// LoadOversightText reads oversight.json for taskID and concatenates all
// phase Title and Summary fields into a single searchable string.
// Returns ("", nil) when the file does not exist.
func (s *Store) LoadOversightText(taskID uuid.UUID) (string, error) {
	data, err := s.backend.ReadBlob(taskID, "oversight.json")
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
	return oversightText(o), nil
}

// GetTestOversight reads the test-agent oversight summary for a task.
// Returns a pending TaskOversight when no oversight-test.json exists yet.
func (s *Store) GetTestOversight(taskID uuid.UUID) (*TaskOversight, error) {
	data, err := s.backend.ReadBlob(taskID, "oversight-test.json")
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
