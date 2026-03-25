package runner

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync/atomic"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
)

// LocalBackend implements SandboxBackend using a local container runtime
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
func (b *LocalBackend) Launch(ctx context.Context, spec ContainerSpec) (SandboxHandle, error) {
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

	// Merge stdout and stderr into a single reader.
	merged := io.NopCloser(io.MultiReader(stdout, stderr))

	h := &localHandle{
		name:    name,
		cmd:     cmd,
		stdout:  merged,
		command: b.command,
	}
	h.state.Store(int32(SandboxCreating))

	if err := cmd.Start(); err != nil {
		h.state.Store(int32(SandboxFailed))
		return nil, fmt.Errorf("start container: %w", err)
	}

	h.state.Store(int32(SandboxRunning))
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

	raw, err := parseContainerList(out)
	if err != nil {
		return nil, err
	}

	result := make([]ContainerInfo, 0, len(raw))
	for _, c := range raw {
		name, nameErr := c.name()
		if nameErr != nil {
			logger.Runner.Warn("List: skipping malformed container entry", "error", nameErr)
			continue
		}

		// Primary: extract task UUID from the wallfacer.task.id label.
		taskID := ""
		if c.Labels != nil {
			taskID = c.Labels["wallfacer.task.id"]
		}
		// Fallback for containers created without labels (old format wallfacer-<uuid>).
		if taskID == "" {
			candidate := strings.TrimPrefix(name, "wallfacer-")
			if candidate != name && isUUID(candidate) {
				taskID = candidate
			}
		}

		result = append(result, ContainerInfo{
			ID:        c.ID,
			Name:      name,
			TaskID:    taskID,
			Image:     c.Image,
			State:     c.State,
			Status:    c.Status,
			CreatedAt: c.createdUnix(),
		})
	}
	return result, nil
}

// localHandle is a stateful reference to a container launched by LocalBackend.
type localHandle struct {
	name    string
	cmd     *exec.Cmd
	stdout  io.ReadCloser
	command string // runtime binary, needed for kill/rm
	state   atomic.Int32
}

func (h *localHandle) State() SandboxState {
	return SandboxState(h.state.Load())
}

func (h *localHandle) Stdout() io.ReadCloser {
	return h.stdout
}

func (h *localHandle) Wait() (int, error) {
	err := h.cmd.Wait()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			h.state.Store(int32(SandboxStopped))
			return exitErr.ExitCode(), nil
		}
		h.state.Store(int32(SandboxFailed))
		return -1, err
	}
	h.state.Store(int32(SandboxStopped))
	return 0, nil
}

func (h *localHandle) Kill() error {
	h.state.Store(int32(SandboxStopping))

	if err := cmdexec.New(h.command, "kill", h.name).Run(); err != nil {
		logger.Runner.Debug("kill container", "name", h.name, "error", err)
	}
	if err := cmdexec.New(h.command, "rm", "-f", h.name).Run(); err != nil {
		logger.Runner.Debug("remove container", "name", h.name, "error", err)
	}

	h.state.Store(int32(SandboxStopped))
	return nil
}

func (h *localHandle) Name() string {
	return h.name
}

// Compile-time interface checks.
var (
	_ SandboxBackend = (*LocalBackend)(nil)
	_ SandboxHandle  = (*localHandle)(nil)
)

// containerJSON helpers (parseContainerList, isUUID, containerJSON.name,
// containerJSON.createdUnix) are defined in runner.go and shared with
// LocalBackend.List().
