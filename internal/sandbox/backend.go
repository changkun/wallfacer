package sandbox

import (
	"context"
	"io"
	"sync/atomic"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/statemachine"
)

// BackendState represents the lifecycle state of a sandbox container.
type BackendState int

// Backend lifecycle states.
const (
	StateCreating  BackendState = iota // Backend is provisioning the container.
	StateRunning                       // Container process is alive but has not yet produced output.
	StateStreaming                     // Container is alive and output is being read.
	StateStopping                      // Kill() has been called; waiting for exit.
	StateStopped                       // Container exited (success or non-zero). Terminal.
	StateFailed                        // Container could not be created or crashed. Terminal.
)

// String returns the human-readable name of the backend state.
func (s BackendState) String() string {
	switch s {
	case StateCreating:
		return "creating"
	case StateRunning:
		return "running"
	case StateStreaming:
		return "streaming"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// StateMachine defines the valid lifecycle transitions for sandbox containers.
var StateMachine = statemachine.New(map[BackendState][]BackendState{
	StateCreating:  {StateRunning, StateFailed},
	StateRunning:   {StateStreaming, StateStopping, StateStopped, StateFailed},
	StateStreaming: {StateStopping, StateStopped, StateFailed},
	StateStopping:  {StateStopped},
	StateStopped:   {},
	StateFailed:    {},
})

// transition atomically validates and applies a state change using
// compare-and-swap. If the transition is invalid, the state is left
// unchanged and the violation is logged.
func transition(state *atomic.Int32, to BackendState) {
	for {
		from := BackendState(state.Load())
		if err := StateMachine.Validate(from, to); err != nil {
			logger.Runner.Warn("invalid sandbox state transition",
				"from", from, "to", to, "error", err)
			return
		}
		if state.CompareAndSwap(int32(from), int32(to)) {
			return
		}
		// State changed between Load and CAS — retry with new state.
	}
}

// Backend launches and lists sandbox containers.
type Backend interface {
	// Launch starts a new sandbox container from the given spec and returns
	// a handle for interacting with it. The container may still be starting
	// when Launch returns (check handle.State()).
	Launch(ctx context.Context, spec ContainerSpec) (Handle, error)
	// List returns info about all running wallfacer containers.
	List(ctx context.Context) ([]ContainerInfo, error)
}

// Handle is a stateful reference to a running sandbox container.
type Handle interface {
	// State returns the current lifecycle state of the container.
	State() BackendState
	// Stdout returns a reader for the container's stdout stream.
	Stdout() io.ReadCloser
	// Stderr returns a reader for the container's stderr stream.
	Stderr() io.ReadCloser
	// Wait blocks until the container exits and returns its exit code.
	Wait() (exitCode int, err error)
	// Kill forcibly stops the container.
	Kill() error
	// Name returns the container name.
	Name() string
}

// WorkerManager is an optional interface that backends can implement to
// support per-task worker containers. The runner uses this to clean up
// workers when tasks complete, are cancelled, or during sync operations.
type WorkerManager interface {
	// StopTaskWorker stops and removes the worker for the given task ID.
	StopTaskWorker(taskID string)
	// ShutdownWorkers stops all active task workers.
	ShutdownWorkers()
	// WorkerStats returns aggregate worker lifecycle statistics.
	WorkerStats() WorkerStatsInfo
}

// WorkerStatsInfo holds aggregate worker lifecycle statistics.
type WorkerStatsInfo struct {
	Enabled       bool                       `json:"enabled"`
	ActiveWorkers int                        `json:"active_workers"`
	Creates       uint64                     `json:"creates"`
	Execs         uint64                     `json:"execs"`
	Fallbacks     uint64                     `json:"fallbacks"`
	ByActivity    map[string]ActivityCounter `json:"by_activity,omitempty"`
}

// ActivityCounter holds per-activity create/exec counts.
type ActivityCounter struct {
	Creates uint64 `json:"creates"`
	Execs   uint64 `json:"execs"`
}

// ContainerInfo holds runtime metadata about a container, used by List()
// and the container monitor UI.
type ContainerInfo struct {
	ID        string `json:"id"`         // short container ID
	Name      string `json:"name"`       // full container name (e.g. wallfacer-<slug>-<uuid8>)
	TaskID    string `json:"task_id"`    // task UUID from label, empty if not a task container
	TaskTitle string `json:"task_title"` // task title populated by the handler from the store
	Image     string `json:"image"`      // image name
	State     string `json:"state"`      // running | exited | paused | …
	Status    string `json:"status"`     // human-readable status (e.g. "Up 5 minutes")
	CreatedAt int64  `json:"created_at"` // unix timestamp
}
