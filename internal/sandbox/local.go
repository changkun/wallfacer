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
	command           string                 // path to podman or docker binary
	taskWorkers       map[string]*taskWorker // key = task ID string
	taskWorkersMu     sync.Mutex
	enableTaskWorkers bool              // WALLFACER_TASK_WORKERS (default true)
	reg               *metrics.Registry // optional; nil disables metric collection

	// Atomic counters for worker lifecycle (also sent to Prometheus via reg).
	workerCreates   atomic.Uint64
	workerExecs     atomic.Uint64
	workerFallbacks atomic.Uint64

	// Per-activity breakdown of creates/execs.
	activityStatsMu sync.Mutex
	activityStats   map[string]*[2]atomic.Uint64 // [0]=creates, [1]=execs
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
		activityStats:     make(map[string]*[2]atomic.Uint64),
	}
}

// incActivityStat increments a per-activity counter (0=creates, 1=execs).
func (b *LocalBackend) incActivityStat(activity string, idx int) {
	if activity == "" {
		return
	}
	b.activityStatsMu.Lock()
	counters, ok := b.activityStats[activity]
	if !ok {
		counters = &[2]atomic.Uint64{}
		b.activityStats[activity] = counters
	}
	b.activityStatsMu.Unlock()
	counters[idx].Add(1)
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
		activity := spec.Labels["wallfacer.task.activity"]
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
		b.incActivityStat(activity, 1)
		b.incWorkerMetric("wallfacer_container_worker_execs_total")
		return h, nil
	}

	return b.launchEphemeral(ctx, spec)
}

// launchViaTaskWorker routes the invocation through a per-task worker
// container, creating one if it doesn't exist yet. If the existing worker
// was created from a lighter spec (e.g. title generation with no workspace
// mounts) and the new spec has more volumes, the worker is torn down and
// recreated so the container has the correct mounts.
func (b *LocalBackend) launchViaTaskWorker(ctx context.Context, spec ContainerSpec, taskID string) (Handle, error) {
	b.taskWorkersMu.Lock()
	w, ok := b.taskWorkers[taskID]
	if ok && len(spec.Volumes) > w.volumeCount {
		// The new spec has more mounts than the worker was created with
		// (e.g., title spec had 0 workspace mounts, implementation has 5).
		// Tear down the old worker so we recreate with the full spec.
		w.stop()
		delete(b.taskWorkers, taskID)
		ok = false
		b.incWorkerMetric("wallfacer_container_worker_upgrades_total")
	}
	if !ok {
		workerName := "wallfacer-worker-" + taskID[:min(8, len(taskID))]
		// Override the spec name so BuildCreate() uses the worker name
		// in --name, matching what ensureRunning() passes to podman start.
		spec.Name = workerName
		w = newTaskWorker(b.command, workerName, spec.BuildCreate(), spec.Entrypoint, len(spec.Volumes))
		b.taskWorkers[taskID] = w
		b.workerCreates.Add(1)
		b.incActivityStat(spec.Labels["wallfacer.task.activity"], 0)
		b.incWorkerMetric("wallfacer_container_worker_creates_total")
	}
	b.taskWorkersMu.Unlock()

	return w.exec(ctx, spec.Cmd, spec.WorkDir)
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

	h := newLocalHandle(name, cmd, stdout, stderr, b.command)

	if err := cmd.Start(); err != nil {
		transition(&h.state, StateFailed)
		return nil, fmt.Errorf("start container: %w", err)
	}

	transition(&h.state, StateRunning)
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

// ShutdownWorkers stops all active task workers in parallel.
// Called during server shutdown.
func (b *LocalBackend) ShutdownWorkers() {
	b.taskWorkersMu.Lock()
	workers := make([]*taskWorker, 0, len(b.taskWorkers))
	for _, w := range b.taskWorkers {
		workers = append(workers, w)
	}
	b.taskWorkers = make(map[string]*taskWorker)
	b.taskWorkersMu.Unlock()

	var wg sync.WaitGroup
	for _, w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.stop()
		}()
	}
	wg.Wait()
}

// WorkerStats returns aggregate statistics about the worker lifecycle.
func (b *LocalBackend) WorkerStats() WorkerStatsInfo {
	b.taskWorkersMu.Lock()
	active := len(b.taskWorkers)
	b.taskWorkersMu.Unlock()

	b.activityStatsMu.Lock()
	byActivity := make(map[string]ActivityCounter, len(b.activityStats))
	for k, v := range b.activityStats {
		byActivity[k] = ActivityCounter{
			Creates: v[0].Load(),
			Execs:   v[1].Load(),
		}
	}
	b.activityStatsMu.Unlock()

	return WorkerStatsInfo{
		Enabled:       b.enableTaskWorkers,
		ActiveWorkers: active,
		Creates:       b.workerCreates.Load(),
		Execs:         b.workerExecs.Load(),
		Fallbacks:     b.workerFallbacks.Load(),
		ByActivity:    byActivity,
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

// newLocalHandle constructs a localHandle with its state explicitly set to
// StateCreating. All construction of localHandle must go through this function
// so the initial state is never ambiguous.
func newLocalHandle(name string, cmd *exec.Cmd, stdout, stderr io.ReadCloser, command string) *localHandle {
	h := &localHandle{
		name:    name,
		cmd:     cmd,
		stdout:  stdout,
		stderr:  stderr,
		command: command,
	}
	h.state.Store(int32(StateCreating))
	return h
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
			transition(&h.state, StateStopped)
			return exitErr.ExitCode(), nil
		}
		transition(&h.state, StateFailed)
		return -1, err
	}
	transition(&h.state, StateStopped)
	return 0, nil
}

// Kill forcibly stops and removes the container. It first sends a kill signal,
// then force-removes to clean up. Errors from kill/rm are logged but not
// returned, since the goal is best-effort cleanup.
func (h *localHandle) Kill() error {
	transition(&h.state, StateStopping)

	if err := cmdexec.New(h.command, "kill", h.name).Run(); err != nil {
		logger.Runner.Debug("kill container", "name", h.name, "error", err)
	}
	if err := cmdexec.New(h.command, "rm", "-f", h.name).Run(); err != nil {
		logger.Runner.Debug("remove container", "name", h.name, "error", err)
	}

	transition(&h.state, StateStopped)
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
