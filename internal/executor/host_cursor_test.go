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

// buildFakeCursor compiles testdata/fakecursor into a temp binary and returns
// its path. The binary scans cursor argv loosely and echoes the prompt and
// whether --force was present, so tests can assert executor wiring.
func buildFakeCursor(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "cursor-agent")
	cmd := exec.Command("go", "build", "-o", bin, "testdata/fakecursor/main.go")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fakecursor: %v\n%s", err, out)
	}
	return bin
}

// launchCursorAndDrain runs Launch for cursor, drains both streams, and
// returns (all NDJSON records, final record).
func launchCursorAndDrain(t *testing.T, b *HostBackend, spec ContainerSpec) ([]map[string]any, map[string]any) {
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
	// Drain to EOF before Wait, matching the runner (agent.go): cmd.Wait
	// closes the StdoutPipe, so waiting before the drain finishes can
	// truncate the read.
	wg.Wait()
	if _, err := h.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}

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

// TestHostBackend_LaunchCursor_ForcesWritePermission verifies the executor
// injects --force for cursor. Without it cursor only proposes edits and a
// task never commits. requestFromClaudeSpec leaves Permission at its
// ReadOnly zero value, so launchCursor must override it to Full.
func TestHostBackend_LaunchCursor_ForcesWritePermission(t *testing.T) {
	bin := buildFakeCursor(t)
	b, err := NewHostBackend(HostBackendConfig{CursorBinary: bin})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	spec := ContainerSpec{
		Name:    "wallfacer-cursor-force",
		Env:     map[string]string{"WALLFACER_AGENT": "cursor"},
		Cmd:     []string{"-p", "do the thing", "--verbose", "--output-format", "stream-json"},
		WorkDir: t.TempDir(),
	}
	_, final := launchCursorAndDrain(t, b, spec)

	if final["force"] != true {
		t.Errorf("cursor launch did not inject --force (got force=%v); edits would only be proposed", final["force"])
	}
	if res, _ := final["result"].(string); !strings.Contains(res, "do the thing") {
		t.Errorf("result should echo prompt; got %q", res)
	}
}

// TestHostBackend_LaunchCursor_UnresolvedBinary verifies a clear error when
// the cursor-agent binary is unresolved.
func TestHostBackend_LaunchCursor_UnresolvedBinary(t *testing.T) {
	b, err := NewHostBackend(HostBackendConfig{CursorBinary: "/no/such/cursor-agent"})
	if err != nil {
		t.Fatalf("construction should be best-effort; got: %v", err)
	}
	_, lerr := b.Launch(context.Background(), ContainerSpec{
		Name:    "wallfacer-cursor-missing",
		Env:     map[string]string{"WALLFACER_AGENT": "cursor"},
		Cmd:     []string{"-p", "x"},
		WorkDir: t.TempDir(),
	})
	if lerr == nil || !strings.Contains(lerr.Error(), "cursor-agent") {
		t.Errorf("expected unresolved cursor-agent error, got: %v", lerr)
	}
}
