package sandbox

import (
	"context"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/metrics"
)

// TestLaunchEphemeralWhenDisabled verifies that when task workers are disabled,
// Launch always creates an ephemeral container even when a task ID label is present.
func TestLaunchEphemeralWhenDisabled(t *testing.T) {
	rt := containerRuntime(t)
	ensureTestImage(t, rt)

	b := NewLocalBackend(rt, LocalBackendConfig{EnableTaskWorkers: false})

	spec := ContainerSpec{
		Runtime: rt,
		Name:    "wallfacer-test-disabled-" + t.Name(),
		Image:   testImage,
		Labels:  map[string]string{"wallfacer.task.id": "task-abc"},
		Cmd:     []string{"echo", "ephemeral"},
	}

	h, err := b.Launch(context.Background(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer func() { _ = h.Kill() }()

	buf := make([]byte, 256)
	n, _ := h.Stdout().Read(buf)
	output := strings.TrimSpace(string(buf[:n]))
	if output != "ephemeral" {
		t.Fatalf("expected 'ephemeral', got %q", output)
	}
	_, _ = h.Wait()

	// No worker should have been created.
	b.taskWorkersMu.Lock()
	count := len(b.taskWorkers)
	b.taskWorkersMu.Unlock()
	if count != 0 {
		t.Fatalf("expected 0 workers when disabled, got %d", count)
	}
}

// TestLaunchEphemeralWithoutTaskID verifies that Launch creates an ephemeral
// container when no wallfacer.task.id label is set, even with workers enabled.
func TestLaunchEphemeralWithoutTaskID(t *testing.T) {
	rt := containerRuntime(t)
	ensureTestImage(t, rt)

	b := NewLocalBackend(rt, LocalBackendConfig{EnableTaskWorkers: true})

	spec := ContainerSpec{
		Runtime: rt,
		Name:    "wallfacer-test-noid-" + t.Name(),
		Image:   testImage,
		Labels:  map[string]string{}, // no task ID
		Cmd:     []string{"echo", "no-id"},
	}

	h, err := b.Launch(context.Background(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer func() { _ = h.Kill() }()

	buf := make([]byte, 256)
	n, _ := h.Stdout().Read(buf)
	output := strings.TrimSpace(string(buf[:n]))
	if output != "no-id" {
		t.Fatalf("expected 'no-id', got %q", output)
	}
	_, _ = h.Wait()

	b.taskWorkersMu.Lock()
	count := len(b.taskWorkers)
	b.taskWorkersMu.Unlock()
	if count != 0 {
		t.Fatalf("expected 0 workers without task ID, got %d", count)
	}
}

// TestLaunchCreatesWorker verifies that Launch creates a persistent worker
// container when task workers are enabled and a task ID label is present.
func TestLaunchCreatesWorker(t *testing.T) {
	rt := containerRuntime(t)
	ensureTestImage(t, rt)

	b := NewLocalBackend(rt, LocalBackendConfig{EnableTaskWorkers: true})
	taskID := "test-task-create"

	spec := ContainerSpec{
		Runtime: rt,
		Name:    "wallfacer-test-create-" + t.Name(),
		Image:   testImage,
		Labels:  map[string]string{"wallfacer.task.id": taskID},
		Cmd:     []string{"echo", "worker-output"},
	}

	h, err := b.Launch(context.Background(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	buf := make([]byte, 256)
	n, _ := h.Stdout().Read(buf)
	output := strings.TrimSpace(string(buf[:n]))
	if output != "worker-output" {
		t.Fatalf("expected 'worker-output', got %q", output)
	}
	_, _ = h.Wait()

	// Worker should exist in the map.
	b.taskWorkersMu.Lock()
	_, exists := b.taskWorkers[taskID]
	b.taskWorkersMu.Unlock()
	if !exists {
		t.Fatal("expected worker to be created in map")
	}

	// Cleanup.
	b.StopTaskWorker(taskID)
}

// TestLaunchReusesWorker verifies that consecutive Launch calls for the same
// task ID reuse the same worker container instead of creating a new one.
func TestLaunchReusesWorker(t *testing.T) {
	rt := containerRuntime(t)
	ensureTestImage(t, rt)

	b := NewLocalBackend(rt, LocalBackendConfig{EnableTaskWorkers: true})
	taskID := "test-task-reuse"

	spec := ContainerSpec{
		Runtime: rt,
		Name:    "wallfacer-test-reuse-" + t.Name(),
		Image:   testImage,
		Labels:  map[string]string{"wallfacer.task.id": taskID},
		Cmd:     []string{"echo", "first"},
	}

	// First launch creates the worker.
	h1, err := b.Launch(context.Background(), spec)
	if err != nil {
		t.Fatalf("first Launch: %v", err)
	}
	_, _ = h1.Wait()

	b.taskWorkersMu.Lock()
	w1 := b.taskWorkers[taskID]
	b.taskWorkersMu.Unlock()

	// Second launch should reuse the same worker.
	spec.Cmd = []string{"echo", "second"}
	h2, err := b.Launch(context.Background(), spec)
	if err != nil {
		t.Fatalf("second Launch: %v", err)
	}
	_, _ = h2.Wait()

	b.taskWorkersMu.Lock()
	w2 := b.taskWorkers[taskID]
	b.taskWorkersMu.Unlock()

	if w1 != w2 {
		t.Fatal("expected same worker pointer for both launches")
	}

	b.StopTaskWorker(taskID)
}

// TestWorkerMetricsRecorded verifies that worker lifecycle events (creates,
// execs, fallbacks) are tracked in the Prometheus-compatible metrics registry.
func TestWorkerMetricsRecorded(t *testing.T) {
	rt := containerRuntime(t)
	ensureTestImage(t, rt)

	reg := metrics.NewRegistry()
	b := NewLocalBackend(rt, LocalBackendConfig{EnableTaskWorkers: true, Reg: reg})
	taskID := "test-task-metrics"

	spec := ContainerSpec{
		Runtime: rt,
		Name:    "wallfacer-test-metrics-" + t.Name(),
		Image:   testImage,
		Labels:  map[string]string{"wallfacer.task.id": taskID},
		Cmd:     []string{"echo", "metrics"},
	}

	// Launch creates a worker (create counter) then either execs (exec counter)
	// or falls back (fallback counter).
	h, err := b.Launch(context.Background(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	_, _ = h.Wait()
	b.StopTaskWorker(taskID)

	// Verify at least one of creates or fallbacks was counted.
	var buf strings.Builder
	reg.WritePrometheus(&buf)
	output := buf.String()
	hasCreate := strings.Contains(output, "wallfacer_container_worker_creates_total")
	hasFallback := strings.Contains(output, "wallfacer_container_worker_fallbacks_total")
	hasExec := strings.Contains(output, "wallfacer_container_worker_execs_total")
	if !hasCreate && !hasFallback && !hasExec {
		t.Errorf("expected at least one worker metric, got:\n%s", output)
	}
}

// TestStopTaskWorker verifies that StopTaskWorker removes the worker from the
// internal map and that calling it again is a safe no-op.
func TestStopTaskWorker(t *testing.T) {
	rt := containerRuntime(t)
	ensureTestImage(t, rt)

	b := NewLocalBackend(rt, LocalBackendConfig{EnableTaskWorkers: true})
	taskID := "test-task-stop"

	spec := ContainerSpec{
		Runtime: rt,
		Name:    "wallfacer-test-stop-" + t.Name(),
		Image:   testImage,
		Labels:  map[string]string{"wallfacer.task.id": taskID},
		Cmd:     []string{"echo", "stopping"},
	}

	h, err := b.Launch(context.Background(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	_, _ = h.Wait()

	b.StopTaskWorker(taskID)

	b.taskWorkersMu.Lock()
	_, exists := b.taskWorkers[taskID]
	b.taskWorkersMu.Unlock()
	if exists {
		t.Fatal("expected worker to be removed from map after StopTaskWorker")
	}

	// Stopping again should not panic.
	b.StopTaskWorker(taskID)
}
