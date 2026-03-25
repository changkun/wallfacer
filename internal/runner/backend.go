package runner

import (
	"context"
	"io"
)

// SandboxState represents the lifecycle state of a sandbox container.
type SandboxState int

// Sandbox lifecycle states.
const (
	SandboxCreating  SandboxState = iota // Backend is provisioning the container.
	SandboxRunning                       // Container process is alive but has not yet produced output.
	SandboxStreaming                     // Container is alive and output is being read.
	SandboxStopping                      // Kill() has been called; waiting for exit.
	SandboxStopped                       // Container exited (success or non-zero). Terminal.
	SandboxFailed                        // Container could not be created or crashed. Terminal.
)

// String returns the human-readable name of the sandbox state.
func (s SandboxState) String() string {
	switch s {
	case SandboxCreating:
		return "creating"
	case SandboxRunning:
		return "running"
	case SandboxStreaming:
		return "streaming"
	case SandboxStopping:
		return "stopping"
	case SandboxStopped:
		return "stopped"
	case SandboxFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// SandboxBackend launches and lists sandbox containers.
type SandboxBackend interface {
	// Launch starts a new sandbox container from the given spec and returns
	// a handle for interacting with it. The container may still be starting
	// when Launch returns (check handle.State()).
	Launch(ctx context.Context, spec ContainerSpec) (SandboxHandle, error)
	// List returns info about all running wallfacer containers.
	List(ctx context.Context) ([]ContainerInfo, error)
}

// SandboxHandle is a stateful reference to a running sandbox container.
type SandboxHandle interface {
	// State returns the current lifecycle state of the container.
	State() SandboxState
	// Stdout returns a reader for the container's combined stdout/stderr stream.
	Stdout() io.ReadCloser
	// Wait blocks until the container exits and returns its exit code.
	Wait() (exitCode int, err error)
	// Kill forcibly stops the container.
	Kill() error
	// Name returns the container name.
	Name() string
}
