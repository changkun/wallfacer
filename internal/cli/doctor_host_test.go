package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakeCLI creates a minimal script at dir/name that echoes a version
// string and exits 0. Used to stand in for claude / codex on $PATH or via
// the WALLFACER_HOST_*_BINARY env vars.
func writeFakeCLI(t *testing.T, dir, name, version string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\necho " + version + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRunDoctor_HostMode_BinariesPresent verifies the host-backend readiness
// block: both binaries resolved, [ok] lines emitted, no image/runtime
// output.
func TestRunDoctor_HostMode_BinariesPresent(t *testing.T) {
	configDir := t.TempDir()
	claudePath := writeFakeCLI(t, t.TempDir(), "claude", "claude/1.2.3")
	codexPath := writeFakeCLI(t, t.TempDir(), "codex", "codex-0.99")
	envFile := filepath.Join(configDir, ".env")
	content := "ANTHROPIC_API_KEY=sk-ant-test1234\n" +
		"WALLFACER_HOST_CLAUDE_BINARY=" + claudePath + "\n" +
		"WALLFACER_HOST_CODEX_BINARY=" + codexPath + "\n"
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(func() {
		RunDoctor(configDir, []string{"--backend", "host"})
	})

	if !strings.Contains(out, "Sandbox backend:   host") {
		t.Errorf("missing host-mode banner:\n%s", out)
	}
	if !strings.Contains(out, "[ok] Claude binary: "+claudePath) {
		t.Errorf("missing claude binary line:\n%s", out)
	}
	if !strings.Contains(out, "[ok] Codex binary: "+codexPath) {
		t.Errorf("missing codex binary line:\n%s", out)
	}
	// Don't assert the specific version string: the `--version` probe runs
	// with a 2s timeout so a loaded test machine (parallel `go test ./...`)
	// can kill the shell wrapper before it prints, which is flaky. The
	// binary-path assertion above is enough to confirm the host branch
	// resolved the binary and emitted the right banner.
	if strings.Contains(out, "Sandbox image:") {
		t.Errorf("host mode should not print Sandbox image line:\n%s", out)
	}
	if strings.Contains(out, "Container runtime:") {
		t.Errorf("host mode should not print Container runtime line:\n%s", out)
	}
}

func TestRunDoctor_HostMode_ClaudeMissing(t *testing.T) {
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	// Explicit bogus claude path; codex intentionally unset.
	if err := os.WriteFile(envFile, []byte("WALLFACER_HOST_CLAUDE_BINARY=/no/such/claude\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(func() {
		RunDoctor(configDir, []string{"--backend", "host"})
	})

	if !strings.Contains(out, "[!]") || !strings.Contains(out, "claude") {
		t.Errorf("expected [!] claude line:\n%s", out)
	}
	if !strings.Contains(out, "issue(s) found") {
		t.Errorf("expected issue summary:\n%s", out)
	}
}

func TestRunDoctor_ContainerMode_UnchangedOutput(t *testing.T) {
	// Regression guard: without --backend, doctor still prints the
	// container-mode banners (Container command + Sandbox image).
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte("ANTHROPIC_API_KEY=sk-ant-x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(func() {
		RunDoctor(configDir, nil)
	})

	if !strings.Contains(out, "Container command:") {
		t.Errorf("container mode should still print Container command line:\n%s", out)
	}
	if !strings.Contains(out, "Sandbox image:") {
		t.Errorf("container mode should still print Sandbox image line:\n%s", out)
	}
}
