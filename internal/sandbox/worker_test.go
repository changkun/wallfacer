package sandbox

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// containerRuntime returns the path to podman or docker, skipping the test
// if neither is available.
func containerRuntime(t *testing.T) string {
	t.Helper()
	for _, bin := range []string{"podman", "docker"} {
		if path, err := exec.LookPath(bin); err == nil {
			return path
		}
	}
	t.Skip("no container runtime available (podman or docker)")
	return ""
}

// testImage returns a minimal container image for testing. Uses alpine
// which is small and widely available.
const testImage = "alpine:latest"

// ensureTestImage pulls the test image if not already cached.
func ensureTestImage(t *testing.T, runtime string) {
	t.Helper()
	out, err := exec.Command(runtime, "image", "exists", testImage).CombinedOutput()
	if err == nil {
		return
	}
	t.Logf("pulling test image %s (output: %s)", testImage, string(out))
	if pullOut, pullErr := exec.Command(runtime, "pull", testImage).CombinedOutput(); pullErr != nil {
		t.Skipf("cannot pull test image %s: %v\n%s", testImage, pullErr, string(pullOut))
	}
}

func TestTaskWorkerEnsureRunning(t *testing.T) {
	rt := containerRuntime(t)
	ensureTestImage(t, rt)

	name := "wallfacer-test-worker-ensure-" + t.Name()
	w := newTaskWorker(rt, name, []string{
		"create", "--name", name,
		"--entrypoint", `["sleep","infinity"]`,
		testImage,
	})
	t.Cleanup(w.stop)

	ctx := context.Background()
	if err := w.ensureRunning(ctx); err != nil {
		t.Fatalf("ensureRunning: %v", err)
	}
	if !w.isAlive() {
		t.Fatal("expected worker to be alive after ensureRunning")
	}

	// Calling again should be a no-op (container already running).
	if err := w.ensureRunning(ctx); err != nil {
		t.Fatalf("ensureRunning (second call): %v", err)
	}
}

func TestTaskWorkerExec(t *testing.T) {
	rt := containerRuntime(t)
	ensureTestImage(t, rt)

	name := "wallfacer-test-worker-exec-" + t.Name()
	w := newTaskWorker(rt, name, []string{
		"create", "--name", name,
		"--entrypoint", `["sleep","infinity"]`,
		testImage,
	})
	t.Cleanup(w.stop)

	ctx := context.Background()
	h, err := w.exec(ctx, []string{"echo", "hello world"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	// Read stdout.
	buf := make([]byte, 256)
	n, _ := h.Stdout().Read(buf)
	output := strings.TrimSpace(string(buf[:n]))
	if output != "hello world" {
		t.Fatalf("expected 'hello world', got %q", output)
	}

	exitCode, err := h.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestTaskWorkerStop(t *testing.T) {
	rt := containerRuntime(t)
	ensureTestImage(t, rt)

	name := "wallfacer-test-worker-stop-" + t.Name()
	w := newTaskWorker(rt, name, []string{
		"create", "--name", name,
		"--entrypoint", `["sleep","infinity"]`,
		testImage,
	})

	ctx := context.Background()
	if err := w.ensureRunning(ctx); err != nil {
		t.Fatalf("ensureRunning: %v", err)
	}
	if !w.isAlive() {
		t.Fatal("expected alive before stop")
	}

	w.stop()

	if w.isAlive() {
		t.Fatal("expected not alive after stop")
	}

	// Verify container no longer exists.
	out, err := exec.Command(rt, "inspect", name).CombinedOutput()
	if err == nil {
		t.Fatalf("expected container to be removed, but inspect succeeded: %s", string(out))
	}
}

func TestTaskWorkerExecAfterStop(t *testing.T) {
	rt := containerRuntime(t)
	ensureTestImage(t, rt)

	name := "wallfacer-test-worker-recover-" + t.Name()
	w := newTaskWorker(rt, name, []string{
		"create", "--name", name,
		"--entrypoint", `["sleep","infinity"]`,
		testImage,
	})
	t.Cleanup(w.stop)

	ctx := context.Background()

	// Start, stop, then exec — should auto-recover.
	if err := w.ensureRunning(ctx); err != nil {
		t.Fatalf("initial ensureRunning: %v", err)
	}
	w.stop()

	// Exec after stop should recreate the container.
	h, err := w.exec(ctx, []string{"echo", "recovered"})
	if err != nil {
		t.Fatalf("exec after stop: %v", err)
	}

	buf := make([]byte, 256)
	n, _ := h.Stdout().Read(buf)
	output := strings.TrimSpace(string(buf[:n]))
	if output != "recovered" {
		t.Fatalf("expected 'recovered', got %q", output)
	}

	exitCode, _ := h.Wait()
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestExecHandleKillDoesNotRemoveContainer(t *testing.T) {
	rt := containerRuntime(t)
	ensureTestImage(t, rt)

	name := "wallfacer-test-worker-kill-" + t.Name()
	w := newTaskWorker(rt, name, []string{
		"create", "--name", name,
		"--entrypoint", `["sleep","infinity"]`,
		testImage,
	})
	t.Cleanup(w.stop)

	ctx := context.Background()

	// Start a long-running exec.
	h, err := w.exec(ctx, []string{"sleep", "60"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	// Kill the exec process.
	if err := h.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	// Reap the process so it doesn't leak.
	h.Wait()

	// Worker container should still exist and be running.
	out, inspErr := exec.Command(rt, "inspect",
		"--format", "{{.State.Running}}", name).CombinedOutput()
	if inspErr != nil {
		t.Fatalf("inspect after kill: %v\n%s", inspErr, string(out))
	}
	if strings.TrimSpace(string(out)) != "true" {
		t.Fatalf("expected container still running after exec kill, got %q", string(out))
	}
}
