package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestClaudeEntrypointAddsFastPromptByDefault verifies that the Claude sandbox
// entrypoint script injects the /fast system prompt by default (WALLFACER_SANDBOX_FAST
// not explicitly disabled).
func TestClaudeEntrypointAddsFastPromptByDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix shell")
	}
	tempDir := t.TempDir()
	argsPath := filepath.Join(tempDir, "claude.args")
	fakeClaudePath := filepath.Join(tempDir, "claude")
	fakeClaude := `#!/bin/bash
set -euo pipefail
printf '%s\n' "$@" > "` + argsPath + `"
`
	if err := os.WriteFile(fakeClaudePath, []byte(fakeClaude), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	cmd := exec.Command("/bin/bash", filepath.Join("testdata", "entrypoints", "claude.sh"), "-p", "test prompt")
	cmd.Env = append(os.Environ(), "PATH="+tempDir+":"+os.Getenv("PATH"))
	if err := cmd.Run(); err != nil {
		t.Fatalf("run entrypoint: %v", err)
	}

	argsRaw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	if !strings.Contains(string(argsRaw), "--append-system-prompt\n/fast\n") {
		t.Fatalf("expected /fast prompt in args, got:\n%s", string(argsRaw))
	}
}

// TestClaudeEntrypointSkipsFastPromptWhenDisabled verifies that setting
// WALLFACER_SANDBOX_FAST=false prevents the /fast system prompt from being added.
func TestClaudeEntrypointSkipsFastPromptWhenDisabled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix shell")
	}
	tempDir := t.TempDir()
	argsPath := filepath.Join(tempDir, "claude.args")
	fakeClaudePath := filepath.Join(tempDir, "claude")
	fakeClaude := `#!/bin/bash
set -euo pipefail
printf '%s\n' "$@" > "` + argsPath + `"
`
	if err := os.WriteFile(fakeClaudePath, []byte(fakeClaude), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	cmd := exec.Command("/bin/bash", filepath.Join("testdata", "entrypoints", "claude.sh"), "-p", "test prompt")
	cmd.Env = append(os.Environ(),
		"PATH="+tempDir+":"+os.Getenv("PATH"),
		"WALLFACER_SANDBOX_FAST=false",
	)
	if err := cmd.Run(); err != nil {
		t.Fatalf("run entrypoint: %v", err)
	}

	argsRaw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	if strings.Contains(string(argsRaw), "--append-system-prompt") {
		t.Fatalf("did not expect /fast prompt in args, got:\n%s", string(argsRaw))
	}
}
