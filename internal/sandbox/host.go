package sandbox

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

	"changkun.de/x/wallfacer/internal/logger"
)

// HostBackendConfig configures a HostBackend. Empty binary paths trigger a
// $PATH lookup; tests use explicit paths to inject a fake agent.
type HostBackendConfig struct {
	ClaudeBinary string // path to `claude` CLI; empty ⇒ exec.LookPath
	CodexBinary  string // path to `codex` CLI;  empty ⇒ exec.LookPath
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
//   - spec.Env["WALLFACER_INSTRUCTIONS_PATH"] is delivered to the agent
//     via --append-system-prompt when supported, or by prepending to the
//     -p prompt value as a fallback (feature-detected on first Launch per
//     agent type; cached thereafter).
//
// spec.CPUs / spec.Memory / spec.Network / spec.Entrypoint / spec.Volumes /
// spec.Labels are ignored by this backend (labels are surfaced via
// ContainerInfo.TaskID on List()).
type HostBackend struct {
	claudeBinary string
	codexBinary  string

	probeMu       sync.Mutex
	probedOnce    map[Type]bool
	probedSupport map[Type]bool

	procMu sync.Mutex
	procs  map[string]*hostHandle // keyed by container name
}

// NewHostBackend resolves binaries and returns a HostBackend ready to
// Launch. Claude is required — failing here surfaces a clear message
// instead of a cryptic first-task failure. Codex is optional for now
// (host mode rejects codex launches anyway, see Launch); its binary is
// resolved best-effort so the backend can still report a path to
// `wallfacer doctor` but an unresolved codex does not block startup.
func NewHostBackend(cfg HostBackendConfig) (*HostBackend, error) {
	claude, err := resolveBinary(cfg.ClaudeBinary, "claude")
	if err != nil {
		return nil, err
	}
	// Best-effort: unresolved codex becomes an empty path; Launch rejects
	// codex anyway until host-mode codex support lands.
	codex, _ := resolveBinary(cfg.CodexBinary, "codex")
	return &HostBackend{
		claudeBinary:  claude,
		codexBinary:   codex,
		probedOnce:    make(map[Type]bool),
		probedSupport: make(map[Type]bool),
		procs:         make(map[string]*hostHandle),
	}, nil
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
func (b *HostBackend) binaryFor(t Type) (string, error) {
	switch t {
	case Claude:
		if b.claudeBinary == "" {
			return "", fmt.Errorf("claude binary not resolved")
		}
		return b.claudeBinary, nil
	case Codex:
		if b.codexBinary == "" {
			return "", fmt.Errorf("codex binary not resolved")
		}
		return b.codexBinary, nil
	default:
		return "", fmt.Errorf("unknown sandbox type %q", t)
	}
}

// SupportsAppendSystemPrompt reports whether the agent CLI for the given
// type accepts --append-system-prompt. Result is cached; the probe runs at
// most once per agent type per backend lifetime, on first query.
func (b *HostBackend) SupportsAppendSystemPrompt(t Type) bool {
	b.probeMu.Lock()
	if b.probedOnce[t] {
		result := b.probedSupport[t]
		b.probeMu.Unlock()
		return result
	}
	b.probeMu.Unlock()

	supported := b.probeAppendSystemPrompt(t)

	b.probeMu.Lock()
	b.probedOnce[t] = true
	b.probedSupport[t] = supported
	b.probeMu.Unlock()
	return supported
}

// probeAppendSystemPrompt runs `<bin> --help` and looks for the flag name in
// the combined output. A failed probe (timeout, missing binary, non-zero
// exit) is treated as "not supported" so callers fall back cleanly.
func (b *HostBackend) probeAppendSystemPrompt(t Type) bool {
	bin, err := b.binaryFor(t)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, bin, "--help").CombinedOutput()
	return strings.Contains(string(out), "--append-system-prompt")
}

// Launch execs the selected agent binary and returns a Handle the runner
// drains and reaps like any other backend. Dispatches to launchClaude or
// launchCodex based on spec.Env["WALLFACER_AGENT"]; each sub-launcher
// handles CLI-specific argv + output wrangling.
func (b *HostBackend) Launch(ctx context.Context, spec ContainerSpec) (Handle, error) {
	agentStr := spec.Env["WALLFACER_AGENT"]
	agent, ok := Parse(agentStr)
	if !ok {
		return nil, fmt.Errorf("host backend: spec.Env[WALLFACER_AGENT] is missing or unknown (got %q)", agentStr)
	}

	// Reject container paths early: a leftover /workspace/<basename> here
	// would run the agent in the wrong directory and produce confusing diffs.
	if strings.HasPrefix(spec.WorkDir, "/workspace/") || spec.WorkDir == "/workspace" {
		return nil, fmt.Errorf("host backend: WorkDir %q is a container path; runner must translate to a host path", spec.WorkDir)
	}

	switch agent {
	case Claude:
		return b.launchClaude(ctx, spec)
	case Codex:
		return b.launchCodex(ctx, spec)
	default:
		return nil, fmt.Errorf("host backend: unsupported agent %q", agent)
	}
}

// launchClaude execs the claude CLI with the runner's argv passed through
// almost verbatim — claude accepts the declarative `-p ... --verbose
// --output-format stream-json` invocation the runner already builds. The
// only transformation is instructions delivery: if the runner set
// WALLFACER_INSTRUCTIONS_PATH, we either add --append-system-prompt (when
// claude --help advertises it) or prepend the instructions content to the
// -p prompt value.
func (b *HostBackend) launchClaude(ctx context.Context, spec ContainerSpec) (Handle, error) {
	bin, err := b.binaryFor(Claude)
	if err != nil {
		return nil, err
	}

	env := b.buildChildEnv(spec)

	// Mirror the container's claude-agent.sh wrapper: without
	// --dangerously-skip-permissions claude waits for interactive
	// permission prompts in a piped non-TTY context, which buffers all
	// stream-json output until the task ends. --append-system-prompt "/fast"
	// activates the Claude Code fast mode when WALLFACER_SANDBOX_FAST is
	// enabled (the default).
	argv := []string{"--dangerously-skip-permissions"}
	if sandboxFast(spec.Env, env) {
		argv = append(argv, "--append-system-prompt", "/fast")
	}
	argv = append(argv, spec.Cmd...)
	if instrPath := spec.Env["WALLFACER_INSTRUCTIONS_PATH"]; instrPath != "" {
		if b.SupportsAppendSystemPrompt(Claude) {
			argv = append(argv, "--append-system-prompt", instrPath)
		} else {
			data, rErr := os.ReadFile(instrPath)
			if rErr != nil {
				logger.Runner.Warn("host backend: read instructions file", "path", instrPath, "error", rErr)
			} else if len(data) > 0 {
				argv = prependToPromptFlag(argv, string(data))
			}
		}
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
		fromFile, err := parseEnvFile(spec.EnvFile)
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

// parseEnvFile reads KEY=VAL lines from an env file. Blank lines and lines
// starting with # are skipped; one layer of surrounding quotes is stripped.
// This is a passthrough layer for child process env, not a config parser —
// values are not expanded or typed.
func parseEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out, nil
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

// prependToPromptFlag finds "-p <value>" in argv and prepends content
// (separated by a blank line and a rule) to the value. Returns argv
// unchanged when no -p flag is found — callers log that case upstream if
// they care.
func prependToPromptFlag(argv []string, content string) []string {
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "-p" {
			argv[i+1] = content + "\n\n---\n\n" + argv[i+1]
			return argv
		}
	}
	return argv
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

	killOnce sync.Once // ensures SIGTERM→SIGKILL escalation runs at most once
}

// newHostHandle constructs a hostHandle with state initialised to Creating.
// All construction goes through this so the initial state is never ambiguous.
func newHostHandle(name string, cmd *exec.Cmd, stdout, stderr io.ReadCloser, taskID string, backend *HostBackend) *hostHandle {
	h := &hostHandle{
		name:    name,
		cmd:     cmd,
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
	pid := h.cmd.Process.Pid
	_ = h.cmd.Process.Signal(syscall.SIGTERM)
	go func() {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		<-timer.C
		// ProcessState is nil until Wait() reaps the child. If it's still nil
		// after the grace period, escalate. syscall.Kill on a reaped pid is a
		// no-op on Unix (or returns ESRCH) so it's safe against a race with Wait.
		if h.cmd.ProcessState == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
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
