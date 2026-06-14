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

// buildFakeOpenCode compiles testdata/fakeopencode into a temp binary and
// returns its path. The binary emits opencode-style NDJSON (no terminal
// result) echoing the prompt and whether --dangerously-skip-permissions was
// present, so tests can assert the launcher synthesizes a result and injects
// write-mode flags.
func buildFakeOpenCode(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "opencode")
	cmd := exec.Command("go", "build", "-o", bin, "testdata/fakeopencode/main.go")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fakeopencode: %v\n%s", err, out)
	}
	return bin
}

// launchOpenCodeAndDrain runs Launch for opencode, drains both streams, and
// returns (all NDJSON records, final synthesized result record).
func launchOpenCodeAndDrain(t *testing.T, b *HostBackend, spec ContainerSpec) ([]map[string]any, map[string]any) {
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
			t.Logf("skipping non-JSON line: %q (%v)", line, err)
			continue
		}
		lines = append(lines, m)
	}
	if len(lines) == 0 {
		t.Fatalf("no NDJSON lines in stdout: %q", stdoutBytes)
	}
	return lines, lines[len(lines)-1]
}

func TestHostBackend_LaunchOpenCode_SynthesizesResult(t *testing.T) {
	bin := buildFakeOpenCode(t)
	b, err := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, OpenCodeBinary: bin})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	spec := ContainerSpec{
		Name:    "wallfacer-opencode-ok",
		Env:     map[string]string{"WALLFACER_AGENT": "opencode"},
		Cmd:     []string{"-p", "hello opencode", "--model", "anthropic/claude-sonnet-4-6"},
		WorkDir: t.TempDir(),
	}
	lines, final := launchOpenCodeAndDrain(t, b, spec)

	// The fake emits step_start/text/step_finish; the launcher appends the
	// synthesized result, so the final line is always type:"result".
	if len(lines) < 2 {
		t.Fatalf("expected tee'd events plus synthesized result; got %d lines", len(lines))
	}
	if final["type"] != "result" {
		t.Errorf("final type = %v; want result", final["type"])
	}
	if final["sessionID"] != "fake-opencode-session" {
		t.Errorf("sessionID = %v; want fake-opencode-session", final["sessionID"])
	}
	if final["is_error"] != false {
		t.Errorf("is_error = %v; want false", final["is_error"])
	}
	if final["stop_reason"] != "end_turn" {
		t.Errorf("stop_reason = %v; want end_turn", final["stop_reason"])
	}
	// Result echoes the prompt and proves write-mode flags were injected.
	res, _ := final["result"].(string)
	if !strings.Contains(res, "hello opencode") {
		t.Errorf("result should echo prompt; got %q", res)
	}
	if !strings.Contains(res, "[skip-permissions]") {
		t.Errorf("launcher should pass --dangerously-skip-permissions (write mode); got %q", res)
	}

	usage, _ := final["usage"].(map[string]any)
	if usage == nil {
		t.Fatalf("missing usage: %+v", final)
	}
	if got, want := usage["input"].(float64), float64(11); got != want {
		t.Errorf("usage.input = %v; want %v", got, want)
	}
	if got, want := usage["output"].(float64), float64(7); got != want {
		t.Errorf("usage.output = %v; want %v", got, want)
	}
	cache, _ := usage["cache"].(map[string]any)
	if cache == nil || cache["read"].(float64) != 3 || cache["write"].(float64) != 1 {
		t.Errorf("usage.cache = %v; want read=3 write=1", cache)
	}
	if got, want := final["cost"].(float64), 0.002; got != want {
		t.Errorf("cost = %v; want %v", got, want)
	}
}

// TestHostBackend_LaunchOpenCode_InstructionsContentPrepended verifies the
// instructions file *contents* (not its path) are prepended into the prompt.
func TestHostBackend_LaunchOpenCode_InstructionsContentPrepended(t *testing.T) {
	bin := buildFakeOpenCode(t)
	b, _ := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, OpenCodeBinary: bin})

	instr := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(instr, []byte("REPO-GUIDELINES-MARKER"), 0o600); err != nil {
		t.Fatal(err)
	}

	spec := ContainerSpec{
		Name: "wallfacer-opencode-instr",
		Env: map[string]string{
			"WALLFACER_AGENT":             "opencode",
			"WALLFACER_INSTRUCTIONS_PATH": instr,
		},
		Cmd:     []string{"-p", "the-task"},
		WorkDir: t.TempDir(),
	}
	_, final := launchOpenCodeAndDrain(t, b, spec)
	res, _ := final["result"].(string)
	if !strings.Contains(res, "REPO-GUIDELINES-MARKER") {
		t.Errorf("instructions content not prepended into prompt: %q", res)
	}
	if !strings.Contains(res, "the-task") {
		t.Errorf("original task should still appear in prompt: %q", res)
	}
	if strings.Contains(res, instr) {
		t.Errorf("instructions file path should not leak, only its contents: %q", res)
	}
}

func TestHostBackend_LaunchOpenCode_MissingPromptFails(t *testing.T) {
	bin := buildFakeOpenCode(t)
	b, _ := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, OpenCodeBinary: bin})

	spec := ContainerSpec{
		Name:    "wallfacer-opencode-noprompt",
		Env:     map[string]string{"WALLFACER_AGENT": "opencode"},
		Cmd:     []string{"--verbose"},
		WorkDir: t.TempDir(),
	}
	_, err := b.Launch(context.Background(), spec)
	if err == nil {
		t.Fatal("expected error for missing -p")
	}
	if !strings.Contains(err.Error(), "-p") {
		t.Errorf("error should cite missing -p flag; got %v", err)
	}
}

// TestHostBackend_LaunchOpenCode_UnrecognizedOutputIsError verifies the
// schema-drift guard: when opencode emits output but none of it matches the
// events the launcher parses, the synthesized result is an error rather than a
// silent empty success. This is the failure mode a future opencode schema
// change would trigger, and it must surface loudly.
func TestHostBackend_LaunchOpenCode_UnrecognizedOutputIsError(t *testing.T) {
	bin := buildFakeOpenCode(t)
	b, _ := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, OpenCodeBinary: bin})

	spec := ContainerSpec{
		Name:    "wallfacer-opencode-garbage",
		Env:     map[string]string{"WALLFACER_AGENT": "opencode", "FAKEOPENCODE_GARBAGE": "1"},
		Cmd:     []string{"-p", "x"},
		WorkDir: t.TempDir(),
	}
	_, final := launchOpenCodeAndDrain(t, b, spec)
	if final["type"] != "result" {
		t.Fatalf("final type = %v; want result", final["type"])
	}
	if final["is_error"] != true {
		t.Errorf("is_error = %v; want true (no recognised events)", final["is_error"])
	}
	if final["stop_reason"] != "error_during_execution" {
		t.Errorf("stop_reason = %v; want error_during_execution", final["stop_reason"])
	}
}

// TestHostBackend_LaunchOpenCode_NonZeroExitWithResult verifies that a non-zero
// process exit is still a success when the stream carried a final text (the
// drain tolerates the exit code; correctness comes from the events).
func TestHostBackend_LaunchOpenCode_NonZeroExitWithResult(t *testing.T) {
	bin := buildFakeOpenCode(t)
	b, _ := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, OpenCodeBinary: bin})

	spec := ContainerSpec{
		Name:    "wallfacer-opencode-exit1",
		Env:     map[string]string{"WALLFACER_AGENT": "opencode", "FAKEOPENCODE_EXIT_1": "1"},
		Cmd:     []string{"-p", "x"},
		WorkDir: t.TempDir(),
	}
	_, final := launchOpenCodeAndDrain(t, b, spec)
	if final["type"] != "result" {
		t.Errorf("final type = %v; want result", final["type"])
	}
	if final["is_error"] != false {
		t.Errorf("is_error = %v; want false (stream carried final text)", final["is_error"])
	}
}
