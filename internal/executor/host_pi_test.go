//go:build !windows

package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// buildFakePi compiles testdata/fakepi into a temp binary and returns its
// path. The binary scans pi argv loosely and echoes the prompt and the
// --tools allowlist, so tests can assert executor wiring.
func buildFakePi(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "pi")
	cmd := exec.Command("go", "build", "-o", bin, "testdata/fakepi/main.go")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fakepi: %v\n%s", err, out)
	}
	return bin
}

// launchPiAndDrain runs Launch for pi, drains both streams, and returns
// (all NDJSON records, final record).
func launchPiAndDrain(t *testing.T, b *HostBackend, spec ContainerSpec) ([]map[string]any, map[string]any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := b.Launch(ctx, spec)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	var wg sync.WaitGroup
	var stdoutBytes []byte
	wg.Add(2)
	go func() {
		defer wg.Done()
		stdoutBytes, _ = io.ReadAll(h.Stdout())
	}()
	go func() {
		defer wg.Done()
		_, _ = io.ReadAll(h.Stderr())
	}()
	if _, err := h.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	wg.Wait()

	var lines []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(string(stdoutBytes)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		lines = append(lines, m)
	}
	if len(lines) == 0 {
		t.Fatalf("no NDJSON lines in stdout: %q", stdoutBytes)
	}
	return lines, lines[len(lines)-1]
}

// TestHostBackend_LaunchPi_ForcesWritePermission verifies the executor
// forces Full permission for pi. requestFromClaudeSpec leaves Permission at
// its ReadOnly zero value, which would restrict pi to --tools Read and
// prevent any edit; launchPi must override it to Full (no --tools).
func TestHostBackend_LaunchPi_ForcesWritePermission(t *testing.T) {
	bin := buildFakePi(t)
	b, err := NewHostBackend(HostBackendConfig{PiBinary: bin})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	spec := ContainerSpec{
		Name:    "wallfacer-pi-force",
		Env:     map[string]string{"WALLFACER_AGENT": "pi"},
		Cmd:     []string{"-p", "do the thing", "--verbose", "--output-format", "stream-json"},
		WorkDir: t.TempDir(),
	}
	_, final := launchPiAndDrain(t, b, spec)

	if tools, _ := final["tools"].(string); tools != "" {
		t.Errorf("pi launch restricted tools to %q; Full permission should omit --tools so edits are allowed", tools)
	}
	if res, _ := final["result"].(string); !strings.Contains(res, "do the thing") {
		t.Errorf("result should echo prompt; got %q", res)
	}
}

// TestHostBackend_LaunchPi_InstructionsContentPrepended verifies the
// instructions file CONTENTS (not its path) are prepended into the prompt.
// Pi has no system-prompt flag in v1, so the harness prepends SystemPrompt
// into the prompt; requestFromClaudeSpec seeds SystemPrompt with the path,
// and launchPi must swap in the contents (mirrors launchCodex/launchCursor).
func TestHostBackend_LaunchPi_InstructionsContentPrepended(t *testing.T) {
	bin := buildFakePi(t)
	b, err := NewHostBackend(HostBackendConfig{PiBinary: bin})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	instr := filepath.Join(t.TempDir(), "instructions.md")
	if err := os.WriteFile(instr, []byte("REPO-GUIDELINES-MARKER"), 0o600); err != nil {
		t.Fatal(err)
	}

	spec := ContainerSpec{
		Name:    "wallfacer-pi-instr",
		Env:     map[string]string{"WALLFACER_AGENT": "pi", "WALLFACER_INSTRUCTIONS_PATH": instr},
		Cmd:     []string{"-p", "hello pi", "--output-format", "stream-json"},
		WorkDir: t.TempDir(),
	}
	_, final := launchPiAndDrain(t, b, spec)
	res, _ := final["result"].(string)
	if !strings.Contains(res, "REPO-GUIDELINES-MARKER") {
		t.Errorf("instructions content not prepended into prompt: %q", res)
	}
	if strings.Contains(res, instr) {
		t.Errorf("instructions file path should not appear, only its contents: %q", res)
	}
}

// TestHostBackend_LaunchPi_RequiresPrompt verifies launchPi rejects a spec
// whose Cmd has no -p <prompt> rather than execing pi with an empty prompt.
func TestHostBackend_LaunchPi_RequiresPrompt(t *testing.T) {
	bin := buildFakePi(t)
	b, err := NewHostBackend(HostBackendConfig{PiBinary: bin})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, lerr := b.Launch(context.Background(), ContainerSpec{
		Name:    "wallfacer-pi-noprompt",
		Env:     map[string]string{"WALLFACER_AGENT": "pi"},
		Cmd:     []string{"--verbose", "--output-format", "stream-json"},
		WorkDir: t.TempDir(),
	})
	if lerr == nil || !strings.Contains(lerr.Error(), "requires a -p") {
		t.Errorf("expected a missing-prompt error, got: %v", lerr)
	}
}

// TestHostBackend_LaunchPi_UnresolvedBinary verifies a clear error when the
// pi binary is unresolved.
func TestHostBackend_LaunchPi_UnresolvedBinary(t *testing.T) {
	b, err := NewHostBackend(HostBackendConfig{PiBinary: "/no/such/pi"})
	if err != nil {
		t.Fatalf("construction should be best-effort; got: %v", err)
	}
	_, lerr := b.Launch(context.Background(), ContainerSpec{
		Name:    "wallfacer-pi-missing",
		Env:     map[string]string{"WALLFACER_AGENT": "pi"},
		Cmd:     []string{"-p", "x"},
		WorkDir: t.TempDir(),
	})
	if lerr == nil || !strings.Contains(lerr.Error(), "pi") {
		t.Errorf("expected unresolved pi error, got: %v", lerr)
	}
}
