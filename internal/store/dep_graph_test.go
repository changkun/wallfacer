package store

import (
	"testing"

	"github.com/google/uuid"
)

// TestCriticalPathScore verifies the basic chain and sibling-branch cases.
//
// Graph:
//
//	A → B → C   (longest chain: length 3)
//	A → D       (sibling branch: length 1)
func TestCriticalPathScore(t *testing.T) {
	s := newTestStore(t)

	taskA, _ := s.CreateTask(bg(), "A", 15, false, "", "")
	taskB, _ := s.CreateTask(bg(), "B", 15, false, "", "")
	taskC, _ := s.CreateTask(bg(), "C", 15, false, "", "")
	taskD, _ := s.CreateTask(bg(), "D", 15, false, "", "")

	// B depends on A, C depends on B, D depends on A.
	s.UpdateTaskDependsOn(bg(), taskB.ID, []string{taskA.ID.String()})
	s.UpdateTaskDependsOn(bg(), taskC.ID, []string{taskB.ID.String()})
	s.UpdateTaskDependsOn(bg(), taskD.ID, []string{taskA.ID.String()})

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
			if got := s.CriticalPathScore(tc.id); got != tc.want {
				t.Errorf("CriticalPathScore = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestCriticalPathScore_UnknownTask verifies that an unknown task ID returns 0.
func TestCriticalPathScore_UnknownTask(t *testing.T) {
	s := newTestStore(t)
	if got := s.CriticalPathScore(uuid.New()); got != 0 {
		t.Errorf("CriticalPathScore(unknown) = %d, want 0", got)
	}
}

// TestCriticalPathScore_Cycle verifies that a dependency cycle returns a finite
// value (>= 1) rather than causing a stack overflow.
func TestCriticalPathScore_Cycle(t *testing.T) {
	s := newTestStore(t)

	taskA, _ := s.CreateTask(bg(), "A", 15, false, "", "")
	taskC, _ := s.CreateTask(bg(), "C", 15, false, "", "")

	// Create a cycle: A depends on C, C depends on A.
	s.UpdateTaskDependsOn(bg(), taskA.ID, []string{taskC.ID.String()})
	s.UpdateTaskDependsOn(bg(), taskC.ID, []string{taskA.ID.String()})

	scoreA := s.CriticalPathScore(taskA.ID)
	scoreC := s.CriticalPathScore(taskC.ID)

	if scoreA < 1 {
		t.Errorf("CriticalPathScore(A) in cycle = %d, want >= 1 (finite)", scoreA)
	}
	if scoreC < 1 {
		t.Errorf("CriticalPathScore(C) in cycle = %d, want >= 1 (finite)", scoreC)
	}
}
