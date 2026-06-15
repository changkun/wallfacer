//go:build !windows

package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// launchCodexAndDrain runs Launch for codex, drains both streams, and
// returns (all NDJSON records, final synthesized Claude record).
func launchCodexAndDrain(t *testing.T, b *HostBackend, spec ContainerSpec) ([]map[string]any, map[string]any) {
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

func TestHostBackend_LaunchCodex_WrapsResult(t *testing.T) {
	bin := buildFakeAgent(t, "fakeagent")
	b, err := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	spec := ContainerSpec{
		Name:    "wallfacer-codex-ok",
		Env:     map[string]string{"WALLFACER_AGENT": "codex"},
		Cmd:     []string{"-p", "hello codex", "--verbose", "--output-format", "stream-json", "--model", "gpt-5"},
		WorkDir: t.TempDir(),
	}
	lines, final := launchCodexAndDrain(t, b, spec)

	// Expect at least: the codex events we emitted plus the synthesized
	// Claude record (so >= 2 lines for our fake, but the claude record is
	// always last regardless of how many codex events arrived).
	if len(lines) < 2 {
		t.Fatalf("expected tee'd events plus final record; got %d lines", len(lines))
	}

	// The final record must be the Claude-compatible envelope.
	if final["session_id"] != "fake-codex-session" {
		t.Errorf("session_id = %v; want fake-codex-session", final["session_id"])
	}
	if final["stop_reason"] != "end_turn" {
		t.Errorf("stop_reason = %v; want end_turn", final["stop_reason"])
	}
	if final["is_error"] != false {
		t.Errorf("is_error = %v; want false", final["is_error"])
	}
	if res, _ := final["result"].(string); !strings.Contains(res, "hello codex") {
		t.Errorf("result should echo prompt (fake behaviour); got %q", res)
	}

	usage, _ := final["usage"].(map[string]any)
	if usage == nil {
		t.Fatalf("missing usage: %+v", final)
	}
	if got, want := usage["input_tokens"].(float64), float64(7); got != want {
		t.Errorf("input_tokens = %v; want %v", got, want)
	}
	// cached_input_tokens (codex) → cache_read_input_tokens (Claude).
	if got, want := usage["cache_read_input_tokens"].(float64), float64(3); got != want {
		t.Errorf("cache_read_input_tokens = %v; want %v (mapped from cached_input_tokens)", got, want)
	}
}

func TestHostBackend_LaunchCodex_MissingPromptFails(t *testing.T) {
	bin := buildFakeAgent(t, "fakeagent")
	b, _ := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin})

	// No -p flag in Cmd.
	spec := ContainerSpec{
		Name:    "wallfacer-codex-noprompt",
		Env:     map[string]string{"WALLFACER_AGENT": "codex"},
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
