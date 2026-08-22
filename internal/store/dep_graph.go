package store

import (
	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/pkg/dagscorer"
)

// CriticalPathScores returns, for every id, the length of the longest
// downstream dependency chain rooted at that id — i.e., 1 + max(score of every
// task that directly or transitively depends on it). A task with no dependents
// scores 1, an unknown task scores 0.
//
// It builds the reverse-adjacency map once under a single RLock, then scores
// each id against the shared map, so a caller scoring N candidates does
// O(tasks) reverse-graph work instead of O(N*tasks). Must be called without
// s.mu held; acquires its own RLock.
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
