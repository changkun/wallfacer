package planner

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
	"changkun.de/x/wallfacer/internal/sandbox"
)

// planningWorker manages a long-lived planning container that serves all
// agent invocations via podman/docker exec. The container is created with
// a sleep entrypoint and kept alive across sessions; each invocation
// becomes an exec call inside the existing container.
type planningWorker struct {
	mu            sync.Mutex
	command       string   // container runtime binary (podman/docker)
	containerName string   // e.g. "wallfacer-plan-<fingerprint8>"
	createArgs    []string // args for podman create (no binary, no "create" verb)
	entrypoint    string   // entrypoint script for exec calls
	alive         bool
}

func newPlanningWorker(command, containerName string, createArgs []string, entrypoint string) *planningWorker {
	return &planningWorker{
		command:       command,
		containerName: containerName,
		createArgs:    createArgs,
		entrypoint:    entrypoint,
	}
}

// ensureRunning makes sure the planning container is alive. If not, it
// cleans up any leftover and creates+starts a fresh container.
func (w *planningWorker) ensureRunning(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.alive {
		out, err := cmdexec.New(w.command, "inspect",
			"--format", "{{.State.Running}}",
			w.containerName,
		).WithContext(ctx).Output()
		if err == nil && strings.TrimSpace(out) == "true" {
			return nil
		}
		w.alive = false
	}

	// Clean up any leftover container.
	if err := cmdexec.New(w.command, "rm", "-f", w.containerName).Run(); err != nil {
		logger.Runner.Debug("planner: remove leftover", "name", w.containerName, "error", err)
	}

	// Create the container with a sleep entrypoint.
	logger.Runner.Debug("planner create", "container", w.containerName, "createArgs", w.createArgs)
	if err := cmdexec.New(w.command, w.createArgs...).WithContext(ctx).Run(); err != nil {
		return fmt.Errorf("planner create: %w", err)
	}

	if err := cmdexec.New(w.command, "start", w.containerName).WithContext(ctx).Run(); err != nil {
		_ = cmdexec.New(w.command, "rm", "-f", w.containerName).Run()
		return fmt.Errorf("planner start: %w", err)
	}

	w.alive = true
	return nil
}

// exec runs a command inside the planning container via podman/docker exec.
func (w *planningWorker) exec(ctx context.Context, cmd []string, workDir string) (sandbox.Handle, error) {
	if err := w.ensureRunning(ctx); err != nil {
		return nil, fmt.Errorf("planner ensure running: %w", err)
	}

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

	logger.Runner.Debug("planner exec", "container", w.containerName, "workdir", workDir, "args", args)

	c := exec.CommandContext(ctx, w.command, args...)

	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("exec stdout pipe: %w", err)
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("exec stderr pipe: %w", err)
	}

	h := &planningHandle{
		name:   w.containerName,
		cmd:    c,
		stdout: stdout,
		stderr: stderr,
	}
	h.state.Store(int32(sandbox.StateCreating))

	if err := c.Start(); err != nil {
		h.state.Store(int32(sandbox.StateFailed))
		return nil, fmt.Errorf("exec start: %w", err)
	}

	h.state.Store(int32(sandbox.StateRunning))
	return &planningExecHandle{planningHandle: h}, nil
}

// stop forcibly removes the planning container. Safe to call multiple times.
func (w *planningWorker) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := cmdexec.New(w.command, "rm", "-f", w.containerName).Run(); err != nil {
		logger.Runner.Debug("planner: stop", "name", w.containerName, "error", err)
	}
	w.alive = false
}

func (w *planningWorker) isAlive() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.alive
}

// planningHandle implements sandbox.Handle for a process running inside
// the planning container.
type planningHandle struct {
	name   string
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr io.ReadCloser
	state  atomic.Int32
}

func (h *planningHandle) State() sandbox.BackendState { return sandbox.BackendState(h.state.Load()) }
func (h *planningHandle) Stdout() io.ReadCloser       { return h.stdout }
func (h *planningHandle) Stderr() io.ReadCloser       { return h.stderr }
func (h *planningHandle) Name() string                { return h.name }

func (h *planningHandle) Wait() (int, error) {
	err := h.cmd.Wait()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			h.state.Store(int32(sandbox.StateStopped))
			return exitErr.ExitCode(), nil
		}
		h.state.Store(int32(sandbox.StateFailed))
		return -1, err
	}
	h.state.Store(int32(sandbox.StateStopped))
	return 0, nil
}

func (h *planningHandle) Kill() error {
	h.state.Store(int32(sandbox.StateStopping))
	if err := cmdexec.New(h.cmd.Path, "kill", h.name).Run(); err != nil {
		logger.Runner.Debug("planner kill", "name", h.name, "error", err)
	}
	if err := cmdexec.New(h.cmd.Path, "rm", "-f", h.name).Run(); err != nil {
		logger.Runner.Debug("planner rm", "name", h.name, "error", err)
	}
	h.state.Store(int32(sandbox.StateStopped))
	return nil
}

// planningExecHandle wraps planningHandle but only kills the exec process,
// not the planning container itself.
type planningExecHandle struct {
	*planningHandle
}

func (h *planningExecHandle) Kill() error {
	h.state.Store(int32(sandbox.StateStopping))
	if h.cmd.Process != nil {
		if err := h.cmd.Process.Kill(); err != nil {
			logger.Runner.Debug("planner exec kill", "name", h.name, "error", err)
		}
	}
	h.state.Store(int32(sandbox.StateStopped))
	return nil
}

var (
	_ sandbox.Handle = (*planningHandle)(nil)
	_ sandbox.Handle = (*planningExecHandle)(nil)
)
