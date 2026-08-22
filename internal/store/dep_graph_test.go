package store

import (
	"testing"

	"github.com/google/uuid"
)

// TestCriticalPathScores verifies the basic chain and sibling-branch cases.
//
// Graph:
//
//	A → B → C   (longest chain: length 3)
//	A → D       (sibling branch: length 1)
func TestCriticalPathScores(t *testing.T) {
	s := newTestStore(t)

	taskA, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "A", Timeout: 15})
	taskB, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "B", Timeout: 15})
	taskC, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "C", Timeout: 15})
	taskD, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "D", Timeout: 15})

	// B depends on A, C depends on B, D depends on A.
	_ = s.UpdateTaskDependsOn(bg(), taskB.ID, []string{taskA.ID.String()})
	_ = s.UpdateTaskDependsOn(bg(), taskC.ID, []string{taskB.ID.String()})
	_ = s.UpdateTaskDependsOn(bg(), taskD.ID, []string{taskA.ID.String()})

	tests := []struct {
		name string
		id   uuid.UUID
		want int
	}{
		{"A (root of longest chain)", taskA.ID, 3},
		{"B (middle of chain)", taskB.ID, 2},
		{"C (leaf)", taskC.ID, 1},
		{"D (sibling leaf)", taskD.ID, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.CriticalPathScores([]uuid.UUID{tc.id})[tc.id]; got != tc.want {
				t.Errorf("CriticalPathScores = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestCriticalPathScores_UnknownTask verifies that an unknown task ID returns 0.
func TestCriticalPathScores_UnknownTask(t *testing.T) {
	s := newTestStore(t)
	if got := unknownScore(s); got != 0 {
		t.Errorf("CriticalPathScores(unknown) = %d, want 0", got)
	}
}

// TestCriticalPathScores_Cycle verifies that a dependency cycle returns a finite
// value (>= 1) rather than causing a stack overflow.
func TestCriticalPathScores_Cycle(t *testing.T) {
	s := newTestStore(t)

	taskA, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "A", Timeout: 15})
	taskC, _ := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "C", Timeout: 15})

	// Create a cycle: A depends on C, C depends on A.
	_ = s.UpdateTaskDependsOn(bg(), taskA.ID, []string{taskC.ID.String()})
	_ = s.UpdateTaskDependsOn(bg(), taskC.ID, []string{taskA.ID.String()})

	scoreA := s.CriticalPathScores([]uuid.UUID{taskA.ID})[taskA.ID]
	scoreC := s.CriticalPathScores([]uuid.UUID{taskC.ID})[taskC.ID]

	if scoreA < 1 {
		t.Errorf("CriticalPathScores(A) in cycle = %d, want >= 1 (finite)", scoreA)
	}
	if scoreC < 1 {
		t.Errorf("CriticalPathScores(C) in cycle = %d, want >= 1 (finite)", scoreC)
	}
}

// unknownScore scores a freshly minted id that belongs to no task.
func unknownScore(s *Store) int {
	id := uuid.New()
	return s.CriticalPathScores([]uuid.UUID{id})[id]
}
