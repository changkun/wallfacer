package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/logger"
)

// requestFromClaudeSpec translates a runner-built ContainerSpec (whose Cmd
// holds the legacy `-p ... --verbose --output-format stream-json [--model
// m] [--resume sid]` shape) into the canonical harness.Request. This shim
// exists so the harness owns the wire knowledge; once upstream callers pass
// Request directly to Launch, the function disappears.
func requestFromClaudeSpec(spec ContainerSpec) harness.Request {
	var req harness.Request
	cmd := spec.Cmd
	for i := 0; i < len(cmd); i++ {
		switch cmd[i] {
		case "-p":
			if i+1 < len(cmd) {
				req.Prompt = cmd[i+1]
				i++
			}
		case "--model", "-m":
			if i+1 < len(cmd) {
				req.Model = cmd[i+1]
				i++
			}
		case "--resume":
			if i+1 < len(cmd) {
				req.SessionID = cmd[i+1]
				i++
			}
		}
	}
	return req
}

// HostBackendConfig configures a HostBackend. Empty binary paths trigger a
// $PATH lookup; tests use explicit paths to inject a fake agent.
type HostBackendConfig struct {
	ClaudeBinary   string // path to `claude` CLI; empty ⇒ exec.LookPath
	CodexBinary    string // path to `codex` CLI;  empty ⇒ exec.LookPath
	CursorBinary   string // path to `cursor-agent` CLI; empty ⇒ exec.LookPath
	OpenCodeBinary string // path to `opencode` CLI; empty ⇒ exec.LookPath
	PiBinary       string // path to `pi` CLI; empty ⇒ exec.LookPath
}

// HostBackend runs the agent CLI directly as a host process — no container.
// It reinterprets ContainerSpec fields as host semantics:
//
//   - spec.Env["WALLFACER_AGENT"] selects the binary (claude or codex).
//   - spec.EnvFile is parsed and merged into cmd.Env.
//   - spec.Env is overlaid on top (wins on collision).
//   - spec.WorkDir becomes cmd.Dir and MUST be an absolute host path;
//     a leftover container path (/workspace/...) is rejected so bugs in
//     the caller's path translation fail fast instead of silently running
//     in the wrong directory.
//
// spec.CPUs / spec.Memory / spec.Network / spec.Entrypoint / spec.Volumes /
// spec.Labels are ignored by this backend (labels are surfaced via
// ContainerInfo.TaskID on List()).
type HostBackend struct {
	binaryMu       sync.RWMutex
	claudeBinary   string
	codexBinary    string
	cursorBinary   string
	openCodeBinary string
	piBinary       string

	procMu sync.Mutex
	procs  map[string]*hostHandle // keyed by container name
}

// SetBinaryForTest overrides the resolved binary path for the given agent
// type. Used by tests that need to swap in a fake-cmd script after the
// backend is constructed.
func (b *HostBackend) SetBinaryForTest(t harness.ID, path string) {
	b.binaryMu.Lock()
	defer b.binaryMu.Unlock()
	switch t {
	case harness.Claude:
		b.claudeBinary = path
	case harness.Codex:
		b.codexBinary = path
	case harness.Cursor:
		b.cursorBinary = path
	case harness.OpenCode:
		b.openCodeBinary = path
	case harness.Pi:
		b.piBinary = path
	}
}

// NewHostBackend resolves binaries best-effort and returns a HostBackend
// ready to Launch. An unresolved binary becomes an empty path: Launch then
// fails with a clear "not resolved" error (see binaryFor) rather than
// blocking construction. This keeps the runner constructible — and testable
// — on hosts without the agent CLI installed; `wallfacer run` enforces claude
// availability up front via RequireClaude.
func NewHostBackend(cfg HostBackendConfig) (*HostBackend, error) {
	claude, _ := resolveBinary(cfg.ClaudeBinary, "claude")
	codex, _ := resolveBinary(cfg.CodexBinary, "codex")
	cursor, _ := resolveBinary(cfg.CursorBinary, "cursor-agent")
	opencode, _ := resolveBinary(cfg.OpenCodeBinary, "opencode")
	pi, _ := resolveBinary(cfg.PiBinary, "pi")
	return &HostBackend{
		claudeBinary:   claude,
		codexBinary:    codex,
		cursorBinary:   cursor,
		openCodeBinary: opencode,
		piBinary:       pi,
		procs:          make(map[string]*hostHandle),
	}, nil
}

// RequireClaude verifies the claude binary can be resolved, returning the
// actionable error used by `wallfacer run` to fail fast at startup. Backend
// construction is best-effort (see NewHostBackend); this is the explicit gate
// for the run command so an operator gets a clear message instead of a cryptic
// first-task failure.
func RequireClaude(explicit string) error {
	_, err := resolveBinary(explicit, "claude")
	return err
}

// resolveBinary returns the explicit path if non-empty and stat-able,
// otherwise looks the name up on $PATH.
func resolveBinary(explicit, name string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("%s binary not found at %q: %w", name, explicit, err)
		}
		return explicit, nil
	}
	path, err := exec.LookPath(name)
	if err != nil {
		envKey := "WALLFACER_HOST_" + strings.ToUpper(name) + "_BINARY"
		return "", fmt.Errorf("%s binary not found in PATH; install it (e.g. 'npm i -g @anthropic-ai/claude-code') or set %s", name, envKey)
	}
	return path, nil
}

// binaryFor returns the resolved binary path for the given agent type.
// Returns an error when the type is unknown or when the binary for a known
// type was not resolvable at construction time.
func (b *HostBackend) binaryFor(t harness.ID) (string, error) {
	b.binaryMu.RLock()
	defer b.binaryMu.RUnlock()
	switch t {
	case harness.Claude:
		if b.claudeBinary == "" {
			return "", fmt.Errorf("claude binary not resolved")
		}
		return b.claudeBinary, nil
	case harness.Codex:
		if b.codexBinary == "" {
			return "", fmt.Errorf("codex binary not resolved")
		}
		return b.codexBinary, nil
	case harness.Cursor:
		if b.cursorBinary == "" {
			return "", fmt.Errorf("cursor-agent binary not resolved")
		}
		return b.cursorBinary, nil
	case harness.OpenCode:
		if b.openCodeBinary == "" {
			return "", fmt.Errorf("opencode binary not resolved")
		}
		return b.openCodeBinary, nil
	case harness.Pi:
		if b.piBinary == "" {
			return "", fmt.Errorf("pi binary not resolved")
		}
		return b.piBinary, nil
	default:
		return "", fmt.Errorf("unknown sandbox type %q", t)
	}
}

// Launch execs the selected agent binary and returns a Handle the runner
// drains and reaps like any other backend. Dispatches to launchClaude or
// launchCodex based on spec.Env["WALLFACER_AGENT"]; each sub-launcher
// handles CLI-specific argv + output wrangling.
func (b *HostBackend) Launch(ctx context.Context, spec ContainerSpec) (Handle, error) {
	agentStr := spec.Env["WALLFACER_AGENT"]
	agent, ok := harness.ParseID(agentStr)
	if !ok {
		return nil, fmt.Errorf("host backend: spec.Env[WALLFACER_AGENT] is missing or unknown (got %q)", agentStr)
	}

	// Reject container paths early: a leftover /workspace/<basename> here
	// would run the agent in the wrong directory and produce confusing diffs.
	if strings.HasPrefix(spec.WorkDir, "/workspace/") || spec.WorkDir == "/workspace" {
		return nil, fmt.Errorf("host backend: WorkDir %q is a container path; runner must translate to a host path", spec.WorkDir)
	}

	switch agent {
	case harness.Claude:
		return b.launchClaude(ctx, spec)
	case harness.Codex:
		return b.launchCodex(ctx, spec)
	case harness.Cursor:
		return b.launchCursor(ctx, spec)
	case harness.OpenCode:
		return b.launchOpenCode(ctx, spec)
	case harness.Pi:
		return b.launchPi(ctx, spec)
	default:
		return nil, fmt.Errorf("host backend: unsupported agent %q", agent)
	}
}

// launchClaude execs the claude CLI. The argv is assembled by
// harness.Claude from a Request extracted from spec; this keeps the
// claude wire knowledge in one place and lets the runner's spec.Cmd
// stay a thin translation layer until upstream code passes Request
// directly.
func (b *HostBackend) launchClaude(ctx context.Context, spec ContainerSpec) (Handle, error) {
	bin, err := b.binaryFor(harness.Claude)
	if err != nil {
		return nil, err
	}

	env := b.buildChildEnv(spec)
	req := requestFromClaudeSpec(spec)
	claudeH, _ := harness.Lookup(harness.Claude)
	argv, _, argvErr := claudeH.BuildArgv(req)
	if argvErr != nil {
		return nil, fmt.Errorf("host backend: claude argv: %w", argvErr)
	}

	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Env = env
	if spec.WorkDir != "" {
		cmd.Dir = spec.WorkDir
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	taskID := spec.Labels["wallfacer.task.id"]
	h := newHostHandle(spec.Name, cmd, stdout, stderr, taskID, b)

	if err := cmd.Start(); err != nil {
		transition(&h.state, StateFailed)
		return nil, fmt.Errorf("start host agent: %w", err)
	}
	transition(&h.state, StateRunning)

	b.procMu.Lock()
	b.procs[spec.Name] = h
	b.procMu.Unlock()

	return h, nil
}

// buildChildEnv returns os.Environ() with spec.EnvFile values merged in
// and spec.Env overlaid on top. spec.Env wins on collision.
func (b *HostBackend) buildChildEnv(spec ContainerSpec) []string {
	env := os.Environ()
	if spec.EnvFile != "" {
		fromFile, err := envconfig.ReadRaw(spec.EnvFile)
		if err != nil {
			logger.Runner.Warn("host backend: parse env file", "path", spec.EnvFile, "error", err)
		} else {
			for k, v := range fromFile {
				env = setEnv(env, k, v)
			}
		}
	}
	for k, v := range spec.Env {
		env = setEnv(env, k, v)
	}
	return env
}

// List returns info about the host processes currently tracked by the
// backend. Image is reported as "host" so the container monitor UI can
// distinguish these from podman-managed containers.
func (b *HostBackend) List(_ context.Context) ([]ContainerInfo, error) {
	b.procMu.Lock()
	defer b.procMu.Unlock()

	out := make([]ContainerInfo, 0, len(b.procs))
	for name, h := range b.procs {
		pid := 0
		if h.cmd.Process != nil {
			pid = h.cmd.Process.Pid
		}
		out = append(out, ContainerInfo{
			ID:     shortName(name),
			Name:   name,
			TaskID: h.taskID,
			Image:  "host",
			State:  "running",
			Status: fmt.Sprintf("Host PID %d", pid),
		})
	}
	return out, nil
}

// shortName returns a short identifier for display; mirrors the short-ID
// convention used elsewhere.
func shortName(name string) string {
	if len(name) <= 12 {
		return name
	}
	return name[len(name)-12:]
}

// setEnv returns env with key=value, replacing any existing entry.
func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			env[i] = key + "=" + value
			return env
		}
	}
	return append(env, key+"="+value)
}

// hostHandle is a Handle backed by an os/exec.Cmd.
type hostHandle struct {
	name    string
	cmd     *exec.Cmd
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	taskID  string
	state   atomic.Int32
	backend *HostBackend

	killOnce sync.Once     // ensures SIGTERM→SIGKILL escalation runs at most once
	done     chan struct{} // closed after cmd.Wait() returns
}

// newHostHandle constructs a hostHandle with state initialised to Creating.
// All construction goes through this so the initial state is never ambiguous.
func newHostHandle(name string, cmd *exec.Cmd, stdout, stderr io.ReadCloser, taskID string, backend *HostBackend) *hostHandle {
	h := &hostHandle{
		name:    name,
		cmd:     cmd,
		done:    make(chan struct{}),
		stdout:  stdout,
		stderr:  stderr,
		taskID:  taskID,
		backend: backend,
	}
	h.state.Store(int32(StateCreating))
	return h
}

func (h *hostHandle) State() BackendState   { return BackendState(h.state.Load()) }
func (h *hostHandle) Stdout() io.ReadCloser { return h.stdout }
func (h *hostHandle) Stderr() io.ReadCloser { return h.stderr }
func (h *hostHandle) Name() string          { return h.name }

// Wait blocks on cmd.Wait, transitions state, and unregisters the handle
// from the backend's map. A non-zero exit returns (code, nil) to match
// LocalBackend's convention — only unexpected errors surface as non-nil.
func (h *hostHandle) Wait() (int, error) {
	err := h.cmd.Wait()
	close(h.done)
	defer h.removeFromBackend()

	terminal := func() bool {
		s := BackendState(h.state.Load())
		return s == StateStopped || s == StateFailed
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if !terminal() {
				transition(&h.state, StateStopped)
			}
			return exitErr.ExitCode(), nil
		}
		if !terminal() {
			transition(&h.state, StateFailed)
		}
		return -1, err
	}
	if !terminal() {
		transition(&h.state, StateStopped)
	}
	return 0, nil
}

// Kill signals the process (SIGTERM, escalating to SIGKILL after 5 s) and
// returns immediately. The caller's goroutine running Wait() reaps the
// process and performs the final state transition. Matches the LocalBackend
// pattern where Kill does not block on process exit.
func (h *hostHandle) Kill() error {
	if s := BackendState(h.state.Load()); s == StateStopped || s == StateFailed {
		return nil
	}
	transition(&h.state, StateStopping)
	h.killOnce.Do(h.signalAndEscalate)
	return nil
}

// signalAndEscalate sends SIGTERM, waits 5 s, then SIGKILL if the process is
// still running. Runs in a goroutine so Kill() stays non-blocking.
func (h *hostHandle) signalAndEscalate() {
	if h.cmd.Process == nil {
		return
	}
	_ = h.cmd.Process.Signal(syscall.SIGTERM)
	go func() {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-h.done:
			// Wait() returned — process already reaped, nothing to escalate.
		case <-timer.C:
			// Grace period elapsed without the process exiting. os.Process.Kill
			// is cross-platform and safe against a race with Wait reaping the
			// child (the os package guards the Pid with an internal mutex).
			_ = h.cmd.Process.Kill()
		}
	}()
}

func (h *hostHandle) removeFromBackend() {
	h.backend.procMu.Lock()
	delete(h.backend.procs, h.name)
	h.backend.procMu.Unlock()
}

// Compile-time interface checks.
var (
	_ Backend = (*HostBackend)(nil)
	_ Handle  = (*hostHandle)(nil)
)
