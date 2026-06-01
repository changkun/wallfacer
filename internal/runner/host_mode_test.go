//go:build !windows

package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"changkun.de/x/wallfacer/internal/store"
)

// buildFakeAgentForTest compiles the sandbox package's fakeagent helper into
// a temp binary. Used to stand in for a real claude/codex install so the host
// backend's NewHostBackend resolves its binaries.
func buildFakeAgentForTest(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fakeagent")
	// Run the build from the sandbox package so the relative `testdata/...`
	// path resolves.
	cmd := exec.Command("go", "build", "-o", bin, "../sandbox/testdata/fakeagent/main.go")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fakeagent: %v\n%s", err, out)
	}
	return bin
}

func newStoreForTest(t *testing.T) *store.Store {
	t.Helper()
	dataDir := t.TempDir()
	s, err := store.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestRunner_HostMode_AlwaysOn verifies that NewRunner builds a HostBackend
// and flips hostMode on.
func TestRunner_HostMode_AlwaysOn(t *testing.T) {
	bin := buildFakeAgentForTest(t)

	s := newStoreForTest(t)
	r := NewRunner(s, RunnerConfig{
		Command:          "echo",
		SandboxImage:     "test:latest",
		HostClaudeBinary: bin,
		HostCodexBinary:  bin,
	})
	t.Cleanup(func() { r.Shutdown() })

	if !r.HostMode() {
		t.Error("HostMode() = false; want true")
	}
	if r.SandboxBackend() == nil {
		t.Error("SandboxBackend() returned nil")
	}
}

// TestSandboxForTaskActivity_HostMode_PassesCodexThrough verifies that host
// mode no longer coerces codex routing — the host backend supports codex
// natively now, so explicit or env-routed codex choices pass through to the
// backend's codex launcher.
func TestSandboxForTaskActivity_HostMode_PassesCodexThrough(t *testing.T) {
	s := newStoreForTest(t)
	r := NewRunner(s, RunnerConfig{Command: "echo"})
	t.Cleanup(func() { r.Shutdown() })
	r.hostMode = true

	task := &store.Task{Sandbox: "codex"}
	got := r.sandboxForTaskActivity(task, activityImplementation)
	if string(got) != "codex" {
		t.Errorf("host mode should pass codex through; got %q", got)
	}
}
