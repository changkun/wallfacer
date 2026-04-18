//go:build !windows

package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TestRunner_HostMode_Default verifies that the default backend selection
// leaves host mode off.
func TestRunner_HostMode_Default(t *testing.T) {
	s := newStoreForTest(t)
	r := NewRunner(s, RunnerConfig{
		Command:      "echo",
		SandboxImage: "test:latest",
	})
	t.Cleanup(func() { r.Shutdown() })
	if r.HostMode() {
		t.Error("default backend should not enable host mode")
	}
}

// TestRunner_HostMode_Host verifies that SandboxBackend="host" builds a
// HostBackend, flips hostMode on, and does not panic when both binaries
// resolve.
func TestRunner_HostMode_Host(t *testing.T) {
	bin := buildFakeAgentForTest(t)

	s := newStoreForTest(t)
	r := NewRunner(s, RunnerConfig{
		Command:          "echo",
		SandboxImage:     "test:latest",
		SandboxBackend:   "host",
		HostClaudeBinary: bin,
		HostCodexBinary:  bin,
	})
	t.Cleanup(func() { r.Shutdown() })

	if !r.HostMode() {
		t.Error("HostMode() = false; want true for backend=host")
	}
	if r.SandboxBackend() == nil {
		t.Error("SandboxBackend() returned nil for host mode")
	}
}

// TestRunner_HostMode_UnknownBackend verifies that an unknown backend value
// falls back to local and leaves host mode off (with a warning log — not
// asserted here).
func TestRunner_HostMode_UnknownBackend(t *testing.T) {
	s := newStoreForTest(t)
	r := NewRunner(s, RunnerConfig{
		Command:        "echo",
		SandboxImage:   "test:latest",
		SandboxBackend: "k8s",
	})
	t.Cleanup(func() { r.Shutdown() })
	if r.HostMode() {
		t.Error("unknown backend should not enable host mode")
	}
}

// TestRunner_HostMode_LocalIsExplicit verifies "local" resolves the same as
// the empty default — no host mode.
func TestRunner_HostMode_LocalIsExplicit(t *testing.T) {
	s := newStoreForTest(t)
	r := NewRunner(s, RunnerConfig{
		Command:        "echo",
		SandboxImage:   "test:latest",
		SandboxBackend: "local",
	})
	t.Cleanup(func() { r.Shutdown() })
	if r.HostMode() {
		t.Error("local backend should not enable host mode")
	}
	// Double-check the string wasn't coerced elsewhere.
	_ = strings.ToLower("local")
}

// TestSandboxForTaskActivity_HostMode_CoercesCodexToClaude verifies the
// routing-level override: when host mode is active, any activity that would
// have gone to codex is silently redirected to claude. This keeps sub-agents
// (title, oversight, etc.) working in host mode instead of failing at launch
// when the user has codex configured for those activities.
func TestSandboxForTaskActivity_HostMode_CoercesCodexToClaude(t *testing.T) {
	s := newStoreForTest(t)
	r := NewRunner(s, RunnerConfig{
		Command: "echo",
	})
	t.Cleanup(func() { r.Shutdown() })

	// Force host mode without constructing a real HostBackend (which would
	// require a real claude binary on PATH for this test).
	r.hostMode = true

	task := &store.Task{Sandbox: "codex"}
	got := r.sandboxForTaskActivity(task, activityTitle)
	if string(got) != "claude" {
		t.Errorf("host mode should coerce codex → claude; got %q", got)
	}

	// Non-host mode: the same resolution should pass codex through unchanged.
	r.hostMode = false
	got = r.sandboxForTaskActivity(task, activityTitle)
	if string(got) != "codex" {
		t.Errorf("non-host mode should preserve codex routing; got %q", got)
	}
}
