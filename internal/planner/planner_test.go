package planner

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"changkun.de/x/wallfacer/internal/sandbox"
)

func TestPlannerNew(t *testing.T) {
	cfg := Config{
		Command:     "/usr/bin/podman",
		Image:       "wallfacer:latest",
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

func TestPlannerUpdateWorkspaces(t *testing.T) {
	p := New(Config{
		Command:     "podman",
		Workspaces:  []string{"/old/path"},
		Fingerprint: "old",
	})

	p.UpdateWorkspaces([]string{"/new/path"}, "new")
	if len(p.workspaces) != 1 || p.workspaces[0] != "/new/path" {
		t.Errorf("workspaces after update = %v, want [/new/path]", p.workspaces)
	}
	if p.fingerprint != "new" {
		t.Errorf("fingerprint after update = %q, want %q", p.fingerprint, "new")
	}
	if p.worker != nil {
		t.Error("worker should be nil after UpdateWorkspaces")
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
		Image:            "wallfacer:latest",
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
	if spec.Image != "wallfacer:latest" {
		t.Errorf("Image = %q, want %q", spec.Image, "wallfacer:latest")
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
		Image:       "wallfacer:latest",
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
	// Workspace without specs/ subdirectory.
	tmpDir := t.TempDir()

	p := New(Config{
		Command:     "podman",
		Image:       "wallfacer:latest",
		Workspaces:  []string{tmpDir},
		Fingerprint: "nospecs",
	})

	spec := p.buildContainerSpec("wallfacer-plan-nospecs", sandbox.Claude)

	// Should have workspace RO mount but no specs RW mount.
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
