package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
)

// TaskCommit preserves a commit as first observed, before an agent amend,
// task rebase, or snapshot cleanup can make its object unreachable.
// Patches live in task blobs, never in the board's Task payload.
type TaskCommit struct {
	Repository     string    `json:"repository"`
	Hash           string    `json:"hash"`
	Subject        string    `json:"subject"`
	Author         string    `json:"author"`
	AuthoredAt     time.Time `json:"authored_at"`
	Attempt        int       `json:"attempt"`
	Turn           int       `json:"turn"`
	Patch          string    `json:"patch"`
	PatchTruncated bool      `json:"patch_truncated,omitempty"`
}

// CurrentAttempt returns the task's execution attempt independently of the
// bounded retry-history payload. Legacy tasks start from retained history.
func (t *Task) CurrentAttempt() int {
	if t.ExecutionAttempt > 0 {
		return t.ExecutionAttempt
	}
	return len(t.RetryHistory) + 1
}

// MergeTaskCommits preserves the first observation of each repository/hash.
func MergeTaskCommits(saved, discovered []TaskCommit) []TaskCommit {
	merged := append([]TaskCommit{}, saved...)
	seen := make(map[string]bool, len(saved))
	for _, c := range saved {
		seen[c.Repository+"\x00"+c.Hash] = true
	}
	for _, c := range discovered {
		key := c.Repository + "\x00" + c.Hash
		if !seen[key] {
			merged = append(merged, c)
			seen[key] = true
		}
	}
	return merged
}

func (s *Store) readTaskCommits(id uuid.UUID) ([]TaskCommit, error) {
	data, err := s.backend.ReadBlob(id, "commit-history.json")
	if errors.Is(err, os.ErrNotExist) {
		return []TaskCommit{}, nil
	}
	if err != nil {
		return nil, err
	}
	var commits []TaskCommit
	if err := json.Unmarshal(data, &commits); err != nil {
		return nil, fmt.Errorf("decode commit history: %w", err)
	}
	return commits, nil
}

// GetTaskCommits reads the durable history independently of worktree lifetime.
func (s *Store) GetTaskCommits(id uuid.UUID) ([]TaskCommit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.tasks[id]; !ok {
		return nil, fmt.Errorf("task %s not found", id)
	}
	return s.readTaskCommits(id)
}

// AppendTaskCommits atomically adds new observations without rewriting the
// attribution or patches of commits captured during earlier turns or retries.
func (s *Store) AppendTaskCommits(id uuid.UUID, commits []TaskCommit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[id]; !ok {
		return fmt.Errorf("task %s not found", id)
	}
	saved, err := s.readTaskCommits(id)
	if err != nil {
		return err
	}
	merged := MergeTaskCommits(saved, commits)
	if len(merged) == len(saved) {
		return nil
	}
	data, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	return s.backend.SaveBlob(id, "commit-history.json", data)
}
