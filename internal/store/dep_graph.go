package store

import (
	"changkun.de/x/wallfacer/internal/pkg/dagscorer"
	"github.com/google/uuid"
)

// CriticalPathScore returns the length of the longest downstream dependency chain
// rooted at id — i.e., 1 + max(CriticalPathScore of every task that directly or
// transitively depends on id). A task with no dependents returns 1, an unknown
// task returns 0. Must be called without s.mu held; acquires its own RLock.
func (s *Store) CriticalPathScore(id uuid.UUID) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.tasks[id]; !ok {
		return 0 // unknown task
	}

	// Build reverse adjacency map: for each task, which tasks depend on it.
	// The forward graph is "A depends on B" (A.DependsOn contains B);
	// the reverse graph is "B is depended on by A" — needed because we
	// are computing the longest downstream chain from id.
	reverseAdj := make(map[uuid.UUID][]uuid.UUID)
	for _, t := range s.tasks {
		for _, depStr := range t.DependsOn {
			depID, err := uuid.Parse(depStr)
			if err != nil {
				continue
			}
			reverseAdj[depID] = append(reverseAdj[depID], t.ID)
		}
	}

	return dagscorer.Score(id, func(n uuid.UUID) []uuid.UUID {
		return reverseAdj[n]
	})
}
