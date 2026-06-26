//go:build !windows

package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"latere.ai/x/wallfacer/internal/store"
	"latere.ai/x/wallfacer/internal/store/storetest"
)

// buildFakeAgentForTest compiles the sandbox package's fakeagent helper into
// a temp binary. Used to stand in for a real claude/codex install so the host
// backend's NewHostBackend resolves its binaries.
func buildFakeAgentForTest(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fakeagent")
	// Run the build from the sandbox package so the relative `testdata/...`
	// path resolves.
	cmd := exec.Command("go", "build", "-o", bin, "../executor/testdata/fakeagent/main.go")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fakeagent: %v\n%s", err, out)
	}
	return bin
}

func newStoreForTest(t *testing.T) *store.Store {
	t.Helper()
	dataDir := t.TempDir()
	s, err := storetest.NewFileStore(t, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestRunner_BuildsHostBackend verifies that NewRunner wires up a host
// backend from resolved binaries.
func TestRunner_BuildsHostBackend(t *testing.T) {
	bin := buildFakeAgentForTest(t)

	s := newStoreForTest(t)
	r := NewRunner(s, RunnerConfig{
		Command:          "echo",
		HostClaudeBinary: bin,
		HostCodexBinary:  bin,
	})
	t.Cleanup(func() { r.Shutdown() })

	if r.SandboxBackend() == nil {
		t.Error("SandboxBackend() returned nil")
	}
}

// TestNewRunner_UnresolvableClaudeDoesNotExit is a regression test for the
// bug where NewRunner called logger.Fatal (os.Exit) when the claude binary
// could not be resolved. That killed the whole test binary on any host
// without the claude CLI installed (e.g. CI), failing the cli, handler, and
// runner packages wholesale. Construction must now be best-effort: the runner
// builds, and a launch surfaces the error instead.
func TestNewRunner_UnresolvableClaudeDoesNotExit(t *testing.T) {
	s := newStoreForTest(t)
	r := NewRunner(s, RunnerConfig{
		Command:          "true",
		HostClaudeBinary: "/no/such/claude/binary",
	})
	t.Cleanup(func() { r.Shutdown() })

	if r == nil {
		t.Fatal("NewRunner returned nil")
	}
	if r.SandboxBackend() == nil {
		t.Error("SandboxBackend() returned nil; runner must build a degraded backend")
	}
}

// TestSandboxForTaskActivity_PassesCodexThrough verifies that codex routing is
// not coerced — the host backend supports codex natively, so explicit or
// env-routed codex choices pass through to the backend's codex launcher.
func TestSandboxForTaskActivity_PassesCodexThrough(t *testing.T) {
	s := newStoreForTest(t)
	r := NewRunner(s, RunnerConfig{Command: "echo"})
	t.Cleanup(func() { r.Shutdown() })

	task := &store.Task{Sandbox: "codex"}
	got := r.sandboxForTaskActivity(task, activityImplementation)
	if string(got) != "codex" {
		t.Errorf("codex should pass through; got %q", got)
	}
}
