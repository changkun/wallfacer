package sandbox

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/metrics"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
)

// LocalBackend implements Backend using a local container runtime
// (podman or docker) via os/exec.
type LocalBackend struct {
	command           string                  // path to podman or docker binary
	taskWorkers       map[string]*taskWorker  // key = task ID string
	taskWorkersMu     sync.Mutex
	enableTaskWorkers bool                    // WALLFACER_TASK_WORKERS (default true)
	reg               *metrics.Registry       // optional; nil disables metric collection

	// Atomic counters for worker lifecycle (also sent to Prometheus via reg).
	workerCreates   atomic.Uint64
	workerExecs     atomic.Uint64
	workerFallbacks atomic.Uint64
}

// LocalBackendConfig holds optional settings for LocalBackend.
type LocalBackendConfig struct {
	EnableTaskWorkers bool              // WALLFACER_TASK_WORKERS (default true)
	Reg               *metrics.Registry // optional; nil disables metric collection
}

// NewLocalBackend creates a LocalBackend that uses the given container runtime
// binary (e.g. "/opt/podman/bin/podman" or "docker").
func NewLocalBackend(command string, cfg LocalBackendConfig) *LocalBackend {
	return &LocalBackend{
		command:           command,
		taskWorkers:       make(map[string]*taskWorker),
		enableTaskWorkers: cfg.EnableTaskWorkers,
		reg:               cfg.Reg,
	}
}

// incWorkerMetric increments a worker lifecycle counter. No-op when reg is nil.
func (b *LocalBackend) incWorkerMetric(name string) {
	if b.reg == nil {
		return
	}
	b.reg.Counter(name, "").Inc(nil)
}

// Launch starts a container from spec and returns a handle for interacting
// with it. When task workers are enabled and the spec has a task ID label,
// the invocation is routed through a per-task worker container (reusing it
// across turns). Otherwise, an ephemeral container is created.
func (b *LocalBackend) Launch(ctx context.Context, spec ContainerSpec) (Handle, error) {
	taskID := spec.Labels["wallfacer.task.id"]

	if taskID != "" && b.enableTaskWorkers {
		h, err := b.launchViaTaskWorker(ctx, spec, taskID)
		if err != nil {
			// Worker failed — fall back to ephemeral.
			logger.Runner.Warn("task worker failed, falling back to ephemeral",
				"task", taskID, "error", err)
			b.workerFallbacks.Add(1)
			b.incWorkerMetric("wallfacer_container_worker_fallbacks_total")
			return b.launchEphemeral(ctx, spec)
		}
		b.workerExecs.Add(1)
		b.incWorkerMetric("wallfacer_container_worker_execs_total")
		return h, nil
	}

	return b.launchEphemeral(ctx, spec)
}

// launchViaTaskWorker routes the invocation through a per-task worker
// container, creating one if it doesn't exist yet.
func (b *LocalBackend) launchViaTaskWorker(ctx context.Context, spec ContainerSpec, taskID string) (Handle, error) {
	b.taskWorkersMu.Lock()
	w, ok := b.taskWorkers[taskID]
	if !ok {
		workerName := "wallfacer-worker-" + taskID[:min(8, len(taskID))]
		w = newTaskWorker(b.command, workerName, spec.BuildCreate())
		b.taskWorkers[taskID] = w
		b.workerCreates.Add(1)
		b.incWorkerMetric("wallfacer_container_worker_creates_total")
	}
	b.taskWorkersMu.Unlock()

	return w.exec(ctx, spec.Cmd)
}

// launchEphemeral creates an ephemeral container (the original behavior).
func (b *LocalBackend) launchEphemeral(ctx context.Context, spec ContainerSpec) (Handle, error) {
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

// StopTaskWorker stops and removes the worker for the given task ID.
// Called by the runner when a task completes, is cancelled, or fails.
func (b *LocalBackend) StopTaskWorker(taskID string) {
	b.taskWorkersMu.Lock()
	w, ok := b.taskWorkers[taskID]
	delete(b.taskWorkers, taskID)
	b.taskWorkersMu.Unlock()

	if ok {
		w.stop()
	}
}

// ShutdownWorkers stops all active task workers. Called during server shutdown.
func (b *LocalBackend) ShutdownWorkers() {
	b.taskWorkersMu.Lock()
	workers := make([]*taskWorker, 0, len(b.taskWorkers))
	for _, w := range b.taskWorkers {
		workers = append(workers, w)
	}
	b.taskWorkers = make(map[string]*taskWorker)
	b.taskWorkersMu.Unlock()

	for _, w := range workers {
		w.stop()
	}
}

// WorkerStats returns aggregate statistics about the worker lifecycle.
func (b *LocalBackend) WorkerStats() WorkerStatsInfo {
	b.taskWorkersMu.Lock()
	active := len(b.taskWorkers)
	b.taskWorkersMu.Unlock()
	return WorkerStatsInfo{
		Enabled:       b.enableTaskWorkers,
		ActiveWorkers: active,
		Creates:       b.workerCreates.Load(),
		Execs:         b.workerExecs.Load(),
		Fallbacks:     b.workerFallbacks.Load(),
	}
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
