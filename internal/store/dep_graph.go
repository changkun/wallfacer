package store

import (
	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/pkg/dagscorer"
)

// CriticalPathScore returns the length of the longest downstream dependency chain
// rooted at id — i.e., 1 + max(CriticalPathScore of every task that directly or
// transitively depends on id). A task with no dependents returns 1, an unknown
// task returns 0. Must be called without s.mu held; acquires its own RLock.
func (s *Store) CriticalPathScore(id uuid.UUID) int {
	return s.CriticalPathScores([]uuid.UUID{id})[id]
}

// CriticalPathScores returns CriticalPathScore for every id in one pass. It
// builds the reverse-adjacency map once under a single RLock, then scores each
// id against the shared map, so a caller scoring N candidates does O(tasks)
// reverse-graph work instead of O(N*tasks). Per-id results are identical to
// CriticalPathScore (unknown ids return 0). Must be called without s.mu held;
// acquires its own RLock.
func (s *Store) CriticalPathScores(ids []uuid.UUID) map[uuid.UUID]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build reverse adjacency map: for each task, which tasks depend on it.
	// The forward graph is "A depends on B" (A.DependsOn contains B);
	// the reverse graph is "B is depended on by A" — needed because we
	// are computing the longest downstream chain from each id.
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

	scores := make(map[uuid.UUID]int, len(ids))
	for _, id := range ids {
		if _, ok := s.tasks[id]; !ok {
			scores[id] = 0 // unknown task
			continue
		}
		scores[id] = dagscorer.Score(id, func(n uuid.UUID) []uuid.UUID {
			return reverseAdj[n]
		})
	}
	return scores
}
