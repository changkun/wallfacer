package store

import (
	"context"

	"github.com/google/uuid"
)

// RebuildSearchIndex reloads the oversight text for every task and rebuilds
// the in-memory search index from scratch. It returns the number of entries
// that differed from the previously indexed value. Safe to call concurrently
// with ongoing reads; it holds s.mu for the minimum duration needed per task.
func (s *Store) RebuildSearchIndex(ctx context.Context) (int, error) {
	s.mu.RLock()
	ids := make([]uuid.UUID, 0, len(s.tasks))
	for id := range s.tasks {
		ids = append(ids, id)
	}
	s.mu.RUnlock()

	repaired := 0
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return repaired, err
		}
		// Per-task processing uses short lock holds: a read lock to snapshot
		// the task pointer, then unlocked disk I/O for oversight, then a
		// write lock only to update the index entry.
		s.mu.RLock()
		task, ok := s.tasks[id]
		s.mu.RUnlock()
		if !ok {
			continue
		}
		oversightRaw, _ := s.LoadOversightText(id)
		entry := buildIndexEntry(task, oversightRaw)
		s.mu.Lock()
		if existing, found := s.searchIndex[id]; !found || existing != entry {
			s.searchIndex[id] = entry
			repaired++
		}
		s.mu.Unlock()
	}
	return repaired, nil
}
