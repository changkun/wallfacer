package sandbox

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync/atomic"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
)

// LocalBackend implements Backend using a local container runtime
// (podman or docker) via os/exec.
type LocalBackend struct {
	command string // path to podman or docker binary
}

// NewLocalBackend creates a LocalBackend that uses the given container runtime
// binary (e.g. "/opt/podman/bin/podman" or "docker").
func NewLocalBackend(command string) *LocalBackend {
	return &LocalBackend{command: command}
}

// Launch starts a container from spec and returns a handle for interacting
// with it. The container process is started non-blocking; call handle.Wait()
// to block until it exits.
func (b *LocalBackend) Launch(ctx context.Context, spec ContainerSpec) (Handle, error) {
	name := spec.Name
	args := spec.Build()

	// Remove any leftover container from a previous interrupted run.
	if err := cmdexec.New(b.command, "rm", "-f", name).Run(); err != nil {
		logger.Runner.Debug("remove leftover container", "name", name, "error", err)
	}

	cmd := exec.CommandContext(ctx, b.command, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	h := &localHandle{
		name:    name,
		cmd:     cmd,
		stdout:  stdout,
		stderr:  stderr,
		command: b.command,
	}
	h.state.Store(int32(StateCreating))

	if err := cmd.Start(); err != nil {
		h.state.Store(int32(StateFailed))
		return nil, fmt.Errorf("start container: %w", err)
	}

	h.state.Store(int32(StateRunning))
	return h, nil
}

// List returns info about all running wallfacer containers by shelling out
// to `<runtime> ps -a --filter name=wallfacer --format json`.
func (b *LocalBackend) List(ctx context.Context) ([]ContainerInfo, error) {
	out, err := cmdexec.New(b.command, "ps", "-a",
		"--filter", "name=wallfacer",
		"--format", "json",
	).WithContext(ctx).OutputBytes()
	if err != nil {
		return nil, err
	}
	return ParseContainerList(out)
}

// localHandle is a stateful reference to a container launched by LocalBackend.
type localHandle struct {
	name    string
	cmd     *exec.Cmd
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	command string // runtime binary, needed for kill/rm
	state   atomic.Int32
}

// State returns the current lifecycle state, read atomically so it is safe
// to call from any goroutine.
func (h *localHandle) State() BackendState {
	return BackendState(h.state.Load())
}

// Stdout returns the container's stdout pipe, established before Start().
func (h *localHandle) Stdout() io.ReadCloser {
	return h.stdout
}

// Stderr returns the container's stderr pipe, established before Start().
func (h *localHandle) Stderr() io.ReadCloser {
	return h.stderr
}

// Wait blocks until the container process exits. A non-zero exit code from
// the container is returned as (exitCode, nil) with state set to Stopped.
// Only unexpected errors (e.g. wait syscall failures) return a non-nil error
// with state set to Failed.
func (h *localHandle) Wait() (int, error) {
	err := h.cmd.Wait()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			h.state.Store(int32(StateStopped))
			return exitErr.ExitCode(), nil
		}
		h.state.Store(int32(StateFailed))
		return -1, err
	}
	h.state.Store(int32(StateStopped))
	return 0, nil
}

// Kill forcibly stops and removes the container. It first sends a kill signal,
// then force-removes to clean up. Errors from kill/rm are logged but not
// returned, since the goal is best-effort cleanup.
func (h *localHandle) Kill() error {
	h.state.Store(int32(StateStopping))

	if err := cmdexec.New(h.command, "kill", h.name).Run(); err != nil {
		logger.Runner.Debug("kill container", "name", h.name, "error", err)
	}
	if err := cmdexec.New(h.command, "rm", "-f", h.name).Run(); err != nil {
		logger.Runner.Debug("remove container", "name", h.name, "error", err)
	}

	h.state.Store(int32(StateStopped))
	return nil
}

// Name returns the container name assigned at launch time.
func (h *localHandle) Name() string {
	return h.name
}

// Compile-time interface checks.
var (
	_ Backend = (*LocalBackend)(nil)
	_ Handle  = (*localHandle)(nil)
)
