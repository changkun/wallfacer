package store

import "github.com/google/uuid"

// CriticalPathScore returns the length of the longest downstream dependency chain
// rooted at id — i.e., 1 + max(CriticalPathScore of every task that directly or
// transitively depends on id). A task with no dependents returns 1, an unknown
// task returns 0. Must be called without s.mu held; acquires its own RLock.
func (s *Store) CriticalPathScore(id uuid.UUID) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.criticalPathScoreLocked(id, make(map[uuid.UUID]int), make(map[uuid.UUID]bool))
}

func (s *Store) criticalPathScoreLocked(id uuid.UUID, memo map[uuid.UUID]int, visiting map[uuid.UUID]bool) int {
	if v, ok := memo[id]; ok {
		return v
	}
	if _, ok := s.tasks[id]; !ok {
		return 0 // unknown task
	}
	if visiting[id] {
		return 1 // cycle guard
	}
	visiting[id] = true
	defer func() { visiting[id] = false }()
	best := 0
	for _, t := range s.tasks {
		for _, depStr := range t.DependsOn {
			depID, err := uuid.Parse(depStr)
			if err != nil || depID != id {
				continue
			}
			if child := s.criticalPathScoreLocked(t.ID, memo, visiting); child > best {
				best = child
			}
		}
	}
	score := 1 + best
	memo[id] = score
	return score
}
