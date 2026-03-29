package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
)

// taskWorker manages a long-lived per-task container that serves all agent
// invocations for that task via podman/docker exec. The container is created
// with a sleep entrypoint and kept alive across turns; each invocation
// becomes an exec call inside the existing container.
type taskWorker struct {
	mu            sync.Mutex
	command       string   // container runtime binary (podman/docker)
	containerName string   // e.g. "wallfacer-worker-abcd1234"
	createArgs    []string // args for podman create (no runtime binary, no "create" verb)
	entrypoint    string   // entrypoint script to invoke via exec (e.g. "/usr/local/bin/entrypoint.sh")
	volumeCount   int      // number of volumes in the spec that created this worker
	alive         bool     // true when the container is running
}

// newTaskWorker creates a taskWorker. createArgs are the arguments for
// `podman create` (excluding the binary path). entrypoint is the script
// to invoke via `podman exec` (since exec does not use the image ENTRYPOINT).
// volumeCount records how many volumes the creating spec had, so the caller
// can detect when a subsequent spec needs more mounts and recreate the worker.
func newTaskWorker(command, containerName string, createArgs []string, entrypoint string, volumeCount int) *taskWorker {
	return &taskWorker{
		command:       command,
		containerName: containerName,
		createArgs:    createArgs,
		entrypoint:    entrypoint,
		volumeCount:   volumeCount,
	}
}

// ensureRunning makes sure the worker container is alive. If not, it cleans
// up any leftover and creates+starts a fresh container.
func (w *taskWorker) ensureRunning(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.alive {
		// Verify the container is actually running.
		out, err := cmdexec.New(w.command, "inspect",
			"--format", "{{.State.Running}}",
			w.containerName,
		).WithContext(ctx).Output()
		if err == nil && strings.TrimSpace(out) == "true" {
			return nil
		}
		// Not running — fall through to recreate.
		w.alive = false
	}

	// Clean up any leftover container.
	if err := cmdexec.New(w.command, "rm", "-f", w.containerName).Run(); err != nil {
		logger.Runner.Debug("worker: remove leftover", "name", w.containerName, "error", err)
	}

	// Create the container with a sleep entrypoint.
	logger.Runner.Debug("worker create", "container", w.containerName, "volumes", w.volumeCount, "createArgs", w.createArgs)
	if err := cmdexec.New(w.command, w.createArgs...).WithContext(ctx).Run(); err != nil {
		return fmt.Errorf("worker create: %w", err)
	}

	// Start the container.
	if err := cmdexec.New(w.command, "start", w.containerName).WithContext(ctx).Run(); err != nil {
		// Clean up the container we just created to avoid leaving it in "Created" state.
		_ = cmdexec.New(w.command, "rm", "-f", w.containerName).Run()
		return fmt.Errorf("worker start: %w", err)
	}

	w.alive = true
	return nil
}

// exec runs a command inside the worker container via podman/docker exec.
// workDir is passed as -w to the exec call so the agent starts in the correct
// directory (podman/docker exec does not inherit -w from create).
// The returned Handle has the same interface as an ephemeral container handle.
func (w *taskWorker) exec(ctx context.Context, cmd []string, workDir string) (Handle, error) {
	// Ensure the worker container is alive (acquires and releases w.mu).
	if err := w.ensureRunning(ctx); err != nil {
		return nil, fmt.Errorf("worker ensure running: %w", err)
	}

	// Build the exec command. podman exec does not use the image ENTRYPOINT
	// or the -w from create, so we must set both explicitly.
	args := make([]string, 0, 5+len(cmd))
	args = append(args, "exec")
	if workDir != "" {
		args = append(args, "-w", workDir)
	}
	args = append(args, w.containerName)
	if w.entrypoint != "" {
		args = append(args, w.entrypoint)
	}
	args = append(args, cmd...)

	logger.Runner.Debug("worker exec", "container", w.containerName, "workdir", workDir, "args", args)

	c := exec.CommandContext(ctx, w.command, args...)

	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("exec stdout pipe: %w", err)
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("exec stderr pipe: %w", err)
	}

	lh := &localHandle{
		name:    w.containerName,
		cmd:     c,
		stdout:  stdout,
		stderr:  stderr,
		command: w.command,
	}
	lh.state.Store(int32(StateCreating))

	if err := c.Start(); err != nil {
		lh.state.Store(int32(StateFailed))
		return nil, fmt.Errorf("exec start: %w", err)
	}

	lh.state.Store(int32(StateRunning))
	// Return an execHandle (not localHandle) so Kill only terminates the
	// exec process, leaving the worker container alive for subsequent turns.
	return &execHandle{localHandle: lh}, nil
}

// stop forcibly removes the worker container. Safe to call multiple times.
func (w *taskWorker) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := cmdexec.New(w.command, "rm", "-f", w.containerName).Run(); err != nil {
		logger.Runner.Debug("worker: stop", "name", w.containerName, "error", err)
	}
	w.alive = false
}

// isAlive reports whether the worker believes its container is running.
// This does not perform an actual health check — use ensureRunning for that.
func (w *taskWorker) isAlive() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.alive
}

// execHandle wraps a localHandle returned by taskWorker.exec but overrides
// Kill so it only kills the exec process without removing the worker container.
type execHandle struct {
	*localHandle
}

// Kill terminates the exec process without removing the worker container.
// The worker container stays alive for subsequent exec calls.
func (h *execHandle) Kill() error {
	h.state.Store(int32(StateStopping))

	// Kill the exec process only — not the worker container.
	if h.cmd.Process != nil {
		if err := h.cmd.Process.Kill(); err != nil {
			logger.Runner.Debug("exec kill", "name", h.name, "error", err)
		}
	}

	h.state.Store(int32(StateStopped))
	return nil
}

var _ Handle = (*execHandle)(nil)
