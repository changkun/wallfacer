package sandbox

import (
	"context"
	"strings"
	"testing"
)

func TestLaunchEphemeralWhenDisabled(t *testing.T) {
	rt := containerRuntime(t)
	ensureTestImage(t, rt)

	b := NewLocalBackend(rt)
	b.enableTaskWorkers = false

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
	defer h.Kill()

	buf := make([]byte, 256)
	n, _ := h.Stdout().Read(buf)
	output := strings.TrimSpace(string(buf[:n]))
	if output != "ephemeral" {
		t.Fatalf("expected 'ephemeral', got %q", output)
	}
	h.Wait()

	// No worker should have been created.
	b.taskWorkersMu.Lock()
	count := len(b.taskWorkers)
	b.taskWorkersMu.Unlock()
	if count != 0 {
		t.Fatalf("expected 0 workers when disabled, got %d", count)
	}
}

func TestLaunchEphemeralWithoutTaskID(t *testing.T) {
	rt := containerRuntime(t)
	ensureTestImage(t, rt)

	b := NewLocalBackend(rt)

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
	defer h.Kill()

	buf := make([]byte, 256)
	n, _ := h.Stdout().Read(buf)
	output := strings.TrimSpace(string(buf[:n]))
	if output != "no-id" {
		t.Fatalf("expected 'no-id', got %q", output)
	}
	h.Wait()

	b.taskWorkersMu.Lock()
	count := len(b.taskWorkers)
	b.taskWorkersMu.Unlock()
	if count != 0 {
		t.Fatalf("expected 0 workers without task ID, got %d", count)
	}
}

func TestLaunchCreatesWorker(t *testing.T) {
	rt := containerRuntime(t)
	ensureTestImage(t, rt)

	b := NewLocalBackend(rt)
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
	h.Wait()

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

func TestLaunchReusesWorker(t *testing.T) {
	rt := containerRuntime(t)
	ensureTestImage(t, rt)

	b := NewLocalBackend(rt)
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
	h1.Wait()

	b.taskWorkersMu.Lock()
	w1 := b.taskWorkers[taskID]
	b.taskWorkersMu.Unlock()

	// Second launch should reuse the same worker.
	spec.Cmd = []string{"echo", "second"}
	h2, err := b.Launch(context.Background(), spec)
	if err != nil {
		t.Fatalf("second Launch: %v", err)
	}
	h2.Wait()

	b.taskWorkersMu.Lock()
	w2 := b.taskWorkers[taskID]
	b.taskWorkersMu.Unlock()

	if w1 != w2 {
		t.Fatal("expected same worker pointer for both launches")
	}

	b.StopTaskWorker(taskID)
}

func TestStopTaskWorker(t *testing.T) {
	rt := containerRuntime(t)
	ensureTestImage(t, rt)

	b := NewLocalBackend(rt)
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
	h.Wait()

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
