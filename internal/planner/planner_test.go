package planner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/executor"
	"latere.ai/x/wallfacer/internal/harness"
)

func TestPlannerNew(t *testing.T) {
	cfg := Config{
		Command:     "/usr/bin/podman",
		Workspaces:  []string{"/home/user/repo"},
		EnvFile:     "/home/user/.env",
		Fingerprint: "abc123def456",
	}
	p := New(cfg)
	if p.command != cfg.Command {
		t.Errorf("command = %q, want %q", p.command, cfg.Command)
	}
	if len(p.workspaces) != 1 || p.workspaces[0] != "/home/user/repo" {
		t.Errorf("workspaces = %v, want [/home/user/repo]", p.workspaces)
	}
	if p.fingerprint != cfg.Fingerprint {
		t.Errorf("fingerprint = %q, want %q", p.fingerprint, cfg.Fingerprint)
	}
}

func TestPlannerIsRunningWhenNotStarted(t *testing.T) {
	p := New(Config{Command: "podman"})
	if p.IsRunning() {
		t.Error("IsRunning() = true, want false for a new planner")
	}
}

func TestPlannerStartStop(t *testing.T) {
	p := New(Config{Command: "podman"})

	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !p.IsRunning() {
		t.Error("IsRunning() = false after Start")
	}

	p.Stop()
	if p.IsRunning() {
		t.Error("IsRunning() = true after Stop")
	}
}

func TestPlannerUpdateWorkspaces(t *testing.T) {
	p := New(Config{
		Command:     "podman",
		Workspaces:  []string{"/old/path"},
		Fingerprint: "old",
	})

	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	p.UpdateWorkspaces([]string{"/new/path"}, "new")
	if len(p.workspaces) != 1 || p.workspaces[0] != "/new/path" {
		t.Errorf("workspaces after update = %v, want [/new/path]", p.workspaces)
	}
	if p.fingerprint != "new" {
		t.Errorf("fingerprint after update = %q, want %q", p.fingerprint, "new")
	}
	if p.IsRunning() {
		t.Error("IsRunning() should be false after UpdateWorkspaces (calls Stop)")
	}
}

func TestPlannerExecNotStarted(t *testing.T) {
	p := New(Config{Command: "podman"})
	_, err := p.Exec(context.Background(), []string{"echo", "hello"})
	if err == nil {
		t.Error("Exec should fail when planner is not started")
	}
}

func TestPlannerExecNoBackend(t *testing.T) {
	p := New(Config{Command: "podman"})
	_ = p.Start(context.Background())
	_, err := p.Exec(context.Background(), []string{"echo", "hello"})
	if err == nil {
		t.Error("Exec should fail when no backend is configured")
	}
}

// TestBuildContainerSpec_HostBackend verifies that when the planner is
// configured with the sandbox HostBackend, its WorkDir is a real host
// path rather than the container-only /workspace/<basename>. Without
// this, planner execs fail with "host backend: WorkDir is a container
// path; runner must translate to a host path" as the HostBackend
// actively rejects container paths to prevent silent CWD drift.
func TestBuildContainerSpec_HostBackend(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmpDir, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a stub claude binary so NewHostBackend succeeds.
	bin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	hb, err := executor.NewHostBackend(executor.HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin})
	if err != nil {
		t.Fatalf("NewHostBackend: %v", err)
	}

	p := New(Config{
		Backend:    hb,
		Command:    "/usr/bin/podman",
		Workspaces: []string{tmpDir},
	})
	spec := p.buildSpec("wallfacer-plan-test", harness.Claude)

	if spec.WorkDir != tmpDir {
		t.Errorf("host mode WorkDir = %q, want host path %q", spec.WorkDir, tmpDir)
	}
	if strings.HasPrefix(spec.WorkDir, "/workspace") {
		t.Errorf("host mode WorkDir must not be a container path, got %q", spec.WorkDir)
	}
}

func TestBuildContainerSpec(t *testing.T) {
	// Create a temp workspace with a specs/ subdirectory.
	tmpDir := t.TempDir()
	specsDir := filepath.Join(tmpDir, "specs")
	if err := os.Mkdir(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	p := New(Config{
		Command:     "/usr/bin/podman",
		Workspaces:  []string{tmpDir},
		EnvFile:     "/tmp/test.env",
		Fingerprint: "abc123",
	})

	spec := p.buildSpec("wallfacer-plan-test", harness.Claude)

	// Basic fields.
	if spec.Name != "wallfacer-plan-test" {
		t.Errorf("Name = %q, want %q", spec.Name, "wallfacer-plan-test")
	}
	// The host backend dispatches to the right CLI based on WALLFACER_AGENT.
	// Without it, host-backend planner execs error out with "WALLFACER_AGENT
	// is missing or unknown". Regression test for a bug where the planner spec
	// didn't thread the agent through.
	if got := spec.Env["WALLFACER_AGENT"]; got != string(harness.Claude) {
		t.Errorf("spec.Env[WALLFACER_AGENT] = %q, want %q", got, harness.Claude)
	}
	if spec.EnvFile != "/tmp/test.env" {
		t.Errorf("EnvFile = %q, want %q", spec.EnvFile, "/tmp/test.env")
	}

	// Check labels for backend worker routing.
	if spec.Labels["wallfacer.task.id"] != planningTaskID {
		t.Errorf("task.id label = %q, want %q", spec.Labels["wallfacer.task.id"], planningTaskID)
	}
	if spec.Labels["wallfacer.task.activity"] != "planning" {
		t.Errorf("task.activity label = %q, want %q", spec.Labels["wallfacer.task.activity"], "planning")
	}

	// Working directory is the host workspace path, not a container path.
	if spec.WorkDir != tmpDir {
		t.Errorf("WorkDir = %q, want host path %q", spec.WorkDir, tmpDir)
	}
}

func TestTruncFingerprint(t *testing.T) {
	if got := truncFingerprint("abcdef123456789"); got != "abcdef123456" {
		t.Errorf("truncFingerprint long = %q, want %q", got, "abcdef123456")
	}
	if got := truncFingerprint("short"); got != "short" {
		t.Errorf("truncFingerprint short = %q, want %q", got, "short")
	}
}

// --- Mock types for testing ---

type mockHandle struct{}

func (h *mockHandle) State() executor.BackendState { return executor.StateRunning }
func (h *mockHandle) Stdout() io.ReadCloser        { return io.NopCloser(strings.NewReader("")) }
func (h *mockHandle) Stderr() io.ReadCloser        { return io.NopCloser(strings.NewReader("")) }
func (h *mockHandle) Wait() (int, error)           { return 0, nil }
func (h *mockHandle) Kill() error                  { return nil }
func (h *mockHandle) Name() string                 { return "mock" }

type mockBackend struct {
	launchErr error
}

func (b *mockBackend) Launch(_ context.Context, _ executor.ContainerSpec) (executor.Handle, error) {
	if b.launchErr != nil {
		return nil, b.launchErr
	}
	return &mockHandle{}, nil
}

func (b *mockBackend) List(_ context.Context) ([]executor.ContainerInfo, error) {
	return nil, nil
}

// --- StartLiveLog / CloseLiveLog / LogReader ---

func TestPlannerStartLiveLog(t *testing.T) {
	p := New(Config{Command: "podman"})
	l := p.StartLiveLog()
	if l == nil {
		t.Fatal("StartLiveLog returned nil")
	}
	// Write something and verify LogReader works.
	_, _ = l.Write([]byte("hello"))
	r := p.LogReader("")
	if r == nil {
		t.Fatal("LogReader returned nil while live log active")
	}

	p.CloseLiveLog()
	// After close, LogReader should return nil.
	r2 := p.LogReader("")
	if r2 != nil {
		t.Error("LogReader should return nil after CloseLiveLog")
	}
}

func TestPlannerLogReader_NoLiveLog(t *testing.T) {
	p := New(Config{Command: "podman"})
	if p.LogReader("") != nil {
		t.Error("LogReader should return nil when no live log")
	}
}

func TestPlannerCloseLiveLog_NoOp(_ *testing.T) {
	p := New(Config{Command: "podman"})
	// Should not panic.
	p.CloseLiveLog()
}

// --- Interrupt ---

func TestPlannerInterrupt_NotBusy(t *testing.T) {
	p := New(Config{Command: "podman"})
	err := p.Interrupt()
	if err == nil {
		t.Error("expected error when interrupting non-busy planner")
	}
}

func TestPlannerInterrupt_Busy(t *testing.T) {
	p := New(Config{Command: "podman"})
	p.SetBusy(true, "")
	p.handle = &mockHandle{}
	l := p.StartLiveLog()
	_ = l

	err := p.Interrupt()
	if err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	if p.IsBusy() {
		t.Error("should not be busy after Interrupt")
	}
	if p.LogReader("") != nil {
		t.Error("live log should be closed after Interrupt")
	}
}

// --- Stop with handle ---

func TestPlannerStop_WithHandle(t *testing.T) {
	p := New(Config{Command: "podman"})
	_ = p.Start(context.Background())
	p.handle = &mockHandle{}
	p.Stop()
	if p.handle != nil {
		t.Error("handle should be nil after Stop")
	}
	if p.IsRunning() {
		t.Error("should not be running after Stop")
	}
}

// --- Exec with mock backend ---

func TestPlannerExec_Success(t *testing.T) {
	mb := &mockBackend{}
	p := New(Config{Command: "podman", Fingerprint: "abc123def456789"})
	p.backend = mb
	_ = p.Start(context.Background())

	h, err := p.Exec(context.Background(), []string{"echo", "hello"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if h == nil {
		t.Error("expected non-nil handle")
	}
}

func TestPlannerExec_BackendError(t *testing.T) {
	mb := &mockBackend{launchErr: fmt.Errorf("container failed")}
	p := New(Config{Command: "podman", Fingerprint: "abc"})
	p.backend = mb
	_ = p.Start(context.Background())

	_, err := p.Exec(context.Background(), []string{"echo"})
	if err == nil {
		t.Error("expected error from backend")
	}
}

// --- buildSpec workspace selection, empty workspace, no env file ---

// TestBuildContainerSpec_MultiWorkspaceUsesFirst documents that the host
// planner runs in the first configured workspace; subsequent workspaces are
// reachable as siblings but do not change the process CWD.
func TestBuildContainerSpec_MultiWorkspaceUsesFirst(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	p := New(Config{
		Command:     "podman",
		Workspaces:  []string{first, second},
		Fingerprint: "multi",
	})

	spec := p.buildSpec("wallfacer-plan-multi", harness.Claude)
	if spec.WorkDir != first {
		t.Errorf("WorkDir = %q, want first workspace %q", spec.WorkDir, first)
	}
}

func TestBuildContainerSpec_EmptyWorkspace(t *testing.T) {
	p := New(Config{
		Command:     "podman",
		Workspaces:  []string{"", "  "},
		Fingerprint: "fp",
	})

	spec := p.buildSpec("test", harness.Claude)
	// Empty workspaces should be skipped, so WorkDir should be empty.
	if spec.WorkDir != "" {
		t.Errorf("WorkDir = %q, want empty for all-blank workspaces", spec.WorkDir)
	}
}

func TestBuildContainerSpec_NoEnvFile(t *testing.T) {
	p := New(Config{
		Command:     "podman",
		Workspaces:  []string{t.TempDir()},
		Fingerprint: "fp",
	})

	spec := p.buildSpec("test", harness.Claude)
	if spec.EnvFile != "" {
		t.Errorf("EnvFile = %q, want empty when not configured", spec.EnvFile)
	}
}
