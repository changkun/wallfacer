package runner

import (
	"context"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
)

// ContainerExecutor abstracts the container runtime (podman/docker) for testing.
type ContainerExecutor interface {
	// RunArgs launches a container with the given name and arguments and returns
	// its combined stdout/stderr, or an error if the process exited non-zero.
	RunArgs(ctx context.Context, name string, args []string) (stdout, stderr []byte, err error)
	// Kill forcibly stops a running container by name.
	Kill(name string)
}

// osContainerExecutor is the production ContainerExecutor that calls the real
// container runtime binary (podman or docker).
type osContainerExecutor struct {
	command string
}

// RunArgs removes any leftover container with the given name, then launches a
// new container using cmdexec and returns the combined output.
func (e *osContainerExecutor) RunArgs(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
	// Remove any leftover container from a previous interrupted run.
	if err := cmdexec.New(e.command, "rm", "-f", name).Run(); err != nil {
		logger.Runner.Debug("remove leftover container", "name", name, "error", err)
	}

	return cmdexec.New(e.command, args...).WithContext(ctx).Capture()
}

// Kill forcibly stops and removes the named container.
func (e *osContainerExecutor) Kill(name string) {
	if err := cmdexec.New(e.command, "kill", name).Run(); err != nil {
		logger.Runner.Debug("kill container", "name", name, "error", err)
	}
	if err := cmdexec.New(e.command, "rm", "-f", name).Run(); err != nil {
		logger.Runner.Debug("remove container", "name", name, "error", err)
	}
}
