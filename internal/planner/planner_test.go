package planner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/sandbox"
)

func TestPlannerNew(t *testing.T) {
	cfg := Config{
		Command:     "/usr/bin/podman",
		Image:       "sandbox-agents:latest",
		Workspaces:  []string{"/home/user/repo"},
		EnvFile:     "/home/user/.env",
		Fingerprint: "abc123def456",
		Network:     "host",
		CPUs:        "2.0",
		Memory:      "4g",
	}
	p := New(cfg)
	if p.command != cfg.Command {
		t.Errorf("command = %q, want %q", p.command, cfg.Command)
	}
	if p.image != cfg.Image {
		t.Errorf("image = %q, want %q", p.image, cfg.Image)
	}
	if len(p.workspaces) != 1 || p.workspaces[0] != "/home/user/repo" {
		t.Errorf("workspaces = %v, want [/home/user/repo]", p.workspaces)
	}
	if p.fingerprint != cfg.Fingerprint {
		t.Errorf("fingerprint = %q, want %q", p.fingerprint, cfg.Fingerprint)
	}
	if p.network != cfg.Network {
		t.Errorf("network = %q, want %q", p.network, cfg.Network)
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
	hb, err := sandbox.NewHostBackend(sandbox.HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin})
	if err != nil {
		t.Fatalf("NewHostBackend: %v", err)
	}

	p := New(Config{
		Backend:    hb,
		Command:    "/usr/bin/podman",
		Image:      "sandbox-agents:latest",
		Workspaces: []string{tmpDir},
	})
	spec := p.buildContainerSpec("wallfacer-plan-test", sandbox.Claude)

	if spec.WorkDir != tmpDir {
		t.Errorf("host mode WorkDir = %q, want host path %q", spec.WorkDir, tmpDir)
	}
	if strings.HasPrefix(spec.WorkDir, "/workspace") {
		t.Errorf("host mode WorkDir must not be a container path, got %q", spec.WorkDir)
	}
	if spec.Entrypoint != "" {
		t.Errorf("host mode spec must not carry a container entrypoint, got %q", spec.Entrypoint)
	}
	if len(spec.Volumes) != 0 {
		t.Errorf("host mode spec must not carry volumes, got %d", len(spec.Volumes))
	}
}

func TestBuildContainerSpec(t *testing.T) {
	// Create a temp workspace with a specs/ subdirectory.
	tmpDir := t.TempDir()
	specsDir := filepath.Join(tmpDir, "specs")
	if err := os.Mkdir(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a temp instructions file.
	instrFile := filepath.Join(t.TempDir(), "CLAUDE.md")
	if err := os.WriteFile(instrFile, []byte("# instructions"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := New(Config{
		Command:          "/usr/bin/podman",
		Image:            "sandbox-agents:latest",
		Workspaces:       []string{tmpDir},
		EnvFile:          "/tmp/test.env",
		Fingerprint:      "abc123",
		InstructionsPath: instrFile,
		Network:          "bridge",
		CPUs:             "1.0",
		Memory:           "2g",
	})

	spec := p.buildContainerSpec("wallfacer-plan-test", sandbox.Claude)

	// Basic fields.
	if spec.Name != "wallfacer-plan-test" {
		t.Errorf("Name = %q, want %q", spec.Name, "wallfacer-plan-test")
	}
	// The host backend and the container entrypoint script both branch
	// on WALLFACER_AGENT. Without it, host-backend planner execs error
	// out with "WALLFACER_AGENT is missing or unknown". Regression test
	// for a bug where planner spec didn't thread the agent through.
	if got := spec.Env["WALLFACER_AGENT"]; got != string(sandbox.Claude) {
		t.Errorf("spec.Env[WALLFACER_AGENT] = %q, want %q", got, sandbox.Claude)
	}
	if spec.Image != "sandbox-agents:latest" {
		t.Errorf("Image = %q, want %q", spec.Image, "sandbox-agents:latest")
	}
	if spec.EnvFile != "/tmp/test.env" {
		t.Errorf("EnvFile = %q, want %q", spec.EnvFile, "/tmp/test.env")
	}
	if spec.Network != "bridge" {
		t.Errorf("Network = %q, want %q", spec.Network, "bridge")
	}
	if spec.CPUs != "1.0" {
		t.Errorf("CPUs = %q, want %q", spec.CPUs, "1.0")
	}
	if spec.Memory != "2g" {
		t.Errorf("Memory = %q, want %q", spec.Memory, "2g")
	}
	if spec.Entrypoint != "/usr/local/bin/entrypoint.sh" {
		t.Errorf("Entrypoint = %q, want %q", spec.Entrypoint, "/usr/local/bin/entrypoint.sh")
	}

	// Check labels for backend worker routing.
	if spec.Labels["wallfacer.task.id"] != planningTaskID {
		t.Errorf("task.id label = %q, want %q", spec.Labels["wallfacer.task.id"], planningTaskID)
	}
	if spec.Labels["wallfacer.task.activity"] != "planning" {
		t.Errorf("task.activity label = %q, want %q", spec.Labels["wallfacer.task.activity"], "planning")
	}

	// Check volumes.
	wantRO := mountOpts("z", "ro")
	wantRW := mountOpts("z")

	var hasNamedConfig, hasWorkspaceRO, hasSpecsRW, hasInstructions bool
	for _, v := range spec.Volumes {
		switch {
		case v.Named && v.Host == "claude-config":
			hasNamedConfig = true
		case v.Container == "/workspace/"+filepath.Base(tmpDir) && v.Options == wantRO:
			hasWorkspaceRO = true
		case v.Container == "/workspace/"+filepath.Base(tmpDir)+"/specs" && v.Options == wantRW:
			hasSpecsRW = true
		case v.Host == instrFile && v.Options == wantRO:
			hasInstructions = true
		}
	}

	if !hasNamedConfig {
		t.Error("missing claude-config named volume")
	}
	if !hasWorkspaceRO {
		t.Error("missing read-only workspace mount")
	}
	if !hasSpecsRW {
		t.Error("missing read-write specs mount")
	}
	if !hasInstructions {
		t.Error("missing instructions mount")
	}

	// Working directory for single workspace.
	wantWorkDir := "/workspace/" + filepath.Base(tmpDir)
	if spec.WorkDir != wantWorkDir {
		t.Errorf("WorkDir = %q, want %q", spec.WorkDir, wantWorkDir)
	}
}

func TestBuildContainerSpecMultiWorkspace(t *testing.T) {
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	// Create specs/ in both workspaces.
	for _, d := range []string{tmpDir1, tmpDir2} {
		if err := os.Mkdir(filepath.Join(d, "specs"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	p := New(Config{
		Command:     "podman",
		Image:       "sandbox-agents:latest",
		Workspaces:  []string{tmpDir1, tmpDir2},
		Fingerprint: "multi",
	})

	spec := p.buildContainerSpec("wallfacer-plan-multi", sandbox.Claude)

	// Multi-workspace: working directory should be /workspace.
	if spec.WorkDir != "/workspace" {
		t.Errorf("WorkDir = %q, want %q", spec.WorkDir, "/workspace")
	}

	// Should have RO mount and RW specs mount for each workspace.
	roCount := 0
	rwSpecsCount := 0
	wantRO := mountOpts("z", "ro")
	wantRW := mountOpts("z")
	for _, v := range spec.Volumes {
		if v.Named {
			continue
		}
		if v.Options == wantRO && !isInstructionsMount(v) {
			roCount++
		}
		if v.Options == wantRW {
			rwSpecsCount++
		}
	}

	if roCount != 2 {
		t.Errorf("read-only workspace mounts = %d, want 2", roCount)
	}
	if rwSpecsCount != 2 {
		t.Errorf("read-write specs mounts = %d, want 2", rwSpecsCount)
	}
}

func TestBuildContainerSpecNoSpecsDir(t *testing.T) {
	tmpDir := t.TempDir()

	p := New(Config{
		Command:     "podman",
		Image:       "sandbox-agents:latest",
		Workspaces:  []string{tmpDir},
		Fingerprint: "nospecs",
	})

	spec := p.buildContainerSpec("wallfacer-plan-nospecs", sandbox.Claude)

	wantRW := mountOpts("z")
	for _, v := range spec.Volumes {
		if v.Options == wantRW && !v.Named {
			t.Error("should not have a read-write specs mount when specs/ dir doesn't exist")
		}
	}
}

func TestMountOpts(t *testing.T) {
	got := mountOpts("z", "ro")
	if runtime.GOOS == "linux" {
		if got != "z,ro" {
			t.Errorf("mountOpts on linux = %q, want %q", got, "z,ro")
		}
	} else {
		if got != "ro" {
			t.Errorf("mountOpts on non-linux = %q, want %q", got, "ro")
		}
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

func isInstructionsMount(v sandbox.VolumeMount) bool {
	return filepath.Base(v.Container) == "CLAUDE.md" || filepath.Base(v.Container) == "AGENTS.md"
}

// --- Mock types for testing ---

type mockHandle struct{}

func (h *mockHandle) State() sandbox.BackendState { return sandbox.StateRunning }
func (h *mockHandle) Stdout() io.ReadCloser       { return io.NopCloser(strings.NewReader("")) }
func (h *mockHandle) Stderr() io.ReadCloser       { return io.NopCloser(strings.NewReader("")) }
func (h *mockHandle) Wait() (int, error)          { return 0, nil }
func (h *mockHandle) Kill() error                 { return nil }
func (h *mockHandle) Name() string                { return "mock" }

type mockBackend struct {
	launchErr error
}

func (b *mockBackend) Launch(_ context.Context, _ sandbox.ContainerSpec) (sandbox.Handle, error) {
	if b.launchErr != nil {
		return nil, b.launchErr
	}
	return &mockHandle{}, nil
}

func (b *mockBackend) List(_ context.Context) ([]sandbox.ContainerInfo, error) {
	return nil, nil
}

type mockWorkerBackend struct {
	mockBackend
	stopCalled bool
}

func (b *mockWorkerBackend) StopTaskWorker(_ string) { b.stopCalled = true }
func (b *mockWorkerBackend) ShutdownWorkers()        {}
func (b *mockWorkerBackend) WorkerStats() sandbox.WorkerStatsInfo {
	return sandbox.WorkerStatsInfo{}
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

// --- Stop with handle and WorkerManager ---

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

func TestPlannerStop_WithWorkerManager(t *testing.T) {
	wb := &mockWorkerBackend{}
	p := New(Config{Command: "podman"})
	p.backend = wb
	_ = p.Start(context.Background())
	p.Stop()
	if !wb.stopCalled {
		t.Error("expected StopTaskWorker to be called")
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

// --- appendInstructionsMount Codex case ---

func TestAppendInstructionsMount_Codex(t *testing.T) {
	instrFile := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(instrFile, []byte("# agents"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := New(Config{
		Command:          "podman",
		Image:            "sandbox-agents:latest",
		Workspaces:       []string{"/workspace/repo"},
		InstructionsPath: instrFile,
	})

	volumes := p.appendInstructionsMount(nil, sandbox.Codex, []string{"repo"})
	if len(volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(volumes))
	}
	if !strings.Contains(volumes[0].Container, "AGENTS.md") {
		t.Errorf("expected AGENTS.md mount, got %q", volumes[0].Container)
	}
}

func TestAppendInstructionsMount_MultiWorkspace(t *testing.T) {
	instrFile := filepath.Join(t.TempDir(), "CLAUDE.md")
	if err := os.WriteFile(instrFile, []byte("# instr"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := New(Config{
		Command:          "podman",
		InstructionsPath: instrFile,
	})

	volumes := p.appendInstructionsMount(nil, sandbox.Claude, []string{"repo1", "repo2"})
	if len(volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(volumes))
	}
	if volumes[0].Container != "/workspace/CLAUDE.md" {
		t.Errorf("expected /workspace/CLAUDE.md, got %q", volumes[0].Container)
	}
}

func TestAppendInstructionsMount_MissingFile(t *testing.T) {
	p := New(Config{
		Command:          "podman",
		InstructionsPath: "/nonexistent/path/CLAUDE.md",
	})

	volumes := p.appendInstructionsMount(nil, sandbox.Claude, []string{"repo"})
	if len(volumes) != 0 {
		t.Errorf("expected no volumes for missing instructions file, got %d", len(volumes))
	}
}

// --- buildContainerSpec empty workspace and no env file ---

func TestBuildContainerSpec_EmptyWorkspace(t *testing.T) {
	p := New(Config{
		Command:     "podman",
		Image:       "sandbox-agents:latest",
		Workspaces:  []string{"", "  "},
		Fingerprint: "fp",
	})

	spec := p.buildContainerSpec("test", sandbox.Claude)
	// Empty workspaces should be skipped, so WorkDir should be empty.
	if spec.WorkDir != "" {
		t.Errorf("WorkDir = %q, want empty for all-blank workspaces", spec.WorkDir)
	}
}

func TestBuildContainerSpec_NoEnvFile(t *testing.T) {
	p := New(Config{
		Command:     "podman",
		Image:       "sandbox-agents:latest",
		Workspaces:  []string{t.TempDir()},
		Fingerprint: "fp",
	})

	spec := p.buildContainerSpec("test", sandbox.Claude)
	if spec.EnvFile != "" {
		t.Errorf("EnvFile = %q, want empty when not configured", spec.EnvFile)
	}
}
