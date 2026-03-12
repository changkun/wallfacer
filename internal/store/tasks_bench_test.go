// Benchmarks for tasks.go: measure lock hold time reduction for
// buildIndexEntry-before-lock optimisations.
package store

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// newBenchStore creates a Store backed by a fresh temporary directory for use
// in benchmarks (b.TempDir requires *testing.B, not *testing.T).
func newBenchStore(b *testing.B) *Store {
	b.Helper()
	s, err := NewStore(b.TempDir())
	if err != nil {
		b.Fatalf("NewStore: %v", err)
	}
	return s
}

// BenchmarkUpdateTaskTitle measures lock hold time for UpdateTaskTitle under
// concurrent read pressure.  A pool of goroutines continuously calls GetTask
// while the benchmark loop calls UpdateTaskTitle, so any work that stays inside
// the write lock (e.g. strings.ToLower) contends directly with the readers.
func BenchmarkUpdateTaskTitle(b *testing.B) {
	s := newBenchStore(b)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "benchmark prompt", 60, false, "", TaskKindTask)
	if err != nil {
		b.Fatalf("CreateTask: %v", err)
	}

	// Launch background readers to simulate concurrent GetTask calls.
	const readers = 8
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					s.GetTask(ctx, task.ID) //nolint:errcheck
				}
			}
		}()
	}

	title := strings.Repeat("benchmark title word ", 20) // ~400 chars

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.UpdateTaskTitle(ctx, task.ID, title); err != nil {
			b.Fatalf("UpdateTaskTitle: %v", err)
		}
	}
	b.StopTimer()

	close(stop)
	wg.Wait()
}

// BenchmarkCreateTask_LargeOversight measures the RestoreTask path with a
// large (~55 KB) oversight string.  Before the optimisation, LoadOversightText
// and buildIndexEntry ran inside the write lock; after, they run before it.
// The benchmark calls RestoreTask b.N times, re-deleting the task between
// iterations so the task stays available in the deleted map.
func BenchmarkCreateTask_LargeOversight(b *testing.B) {
	s := newBenchStore(b)
	ctx := context.Background()

	// Build a ~55 KB oversight payload (50 phases × (100 B title + 1 000 B summary)).
	const phaseCount = 50
	const summarySize = 1000 // bytes per phase summary
	phases := make([]OversightPhase, phaseCount)
	for i := range phases {
		phases[i] = OversightPhase{
			Timestamp: time.Now(),
			Title:     strings.Repeat("T", summarySize/10),
			Summary:   strings.Repeat("S", summarySize),
		}
	}
	oversight := TaskOversight{
		Status:      OversightStatusReady,
		GeneratedAt: time.Now(),
		Phases:      phases,
	}

	// Pre-create the task in the deleted state with the large oversight on disk.
	task0, err := s.CreateTask(ctx, "bench prompt", 60, false, "", TaskKindTask)
	if err != nil {
		b.Fatalf("initial CreateTask: %v", err)
	}
	if err := s.SaveOversight(task0.ID, oversight); err != nil {
		b.Fatalf("initial SaveOversight: %v", err)
	}
	if err := s.DeleteTask(ctx, task0.ID, "bench setup"); err != nil {
		b.Fatalf("initial DeleteTask: %v", err)
	}
	benchID := task0.ID

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.RestoreTask(ctx, benchID); err != nil {
			b.Fatalf("RestoreTask: %v", err)
		}
		b.StopTimer()
		// Re-delete so the next iteration can restore again.
		if err := s.DeleteTask(ctx, benchID, "bench teardown"); err != nil {
			b.Fatalf("re-DeleteTask: %v", err)
		}
		b.StartTimer()
	}
}
