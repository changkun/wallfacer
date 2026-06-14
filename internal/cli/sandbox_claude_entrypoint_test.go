package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestClaudeEntrypointForwardsArgs verifies the Claude sandbox entrypoint
// execs claude with --dangerously-skip-permissions and forwards the wallfacer
// argv unchanged, without injecting a /fast system prompt.
func TestClaudeEntrypointForwardsArgs(t *testing.T) {
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
	args := string(argsRaw)
	if !strings.Contains(args, "--dangerously-skip-permissions") {
		t.Fatalf("expected --dangerously-skip-permissions in args, got:\n%s", args)
	}
	if !strings.Contains(args, "test prompt") {
		t.Fatalf("expected forwarded prompt in args, got:\n%s", args)
	}
	if strings.Contains(args, "/fast") {
		t.Fatalf("did not expect /fast prompt in args, got:\n%s", args)
	}
}
