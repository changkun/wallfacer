//go:build opencode_integration

// This live integration test is gated behind the opencode_integration build
// tag because it invokes a real opencode process, which costs API tokens and
// needs a provider configured via `opencode auth login`. `make test` (go test
// ./... without the tag) must not fire it; run it deliberately with:
//
//	go test -tags opencode_integration ./internal/executor/ -run TestOpenCode_Integration
//
// Unlike cursor, opencode emits no terminal result event in its JSON stream,
// so this drives the full launchOpenCode path (which synthesizes the result)
// rather than the harness ParseEvent in isolation — that is the only place a
// terminal KindResult-shaped line is produced.
package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestOpenCode_Integration_OneShot runs a real `opencode run` through the host
// backend and asserts the launcher synthesizes a non-error terminal result
// with a session id. Skips (not fails) when opencode is absent or the run
// errors (no provider auth / offline), so a deliberate run on an unconfigured
// host stays green.
func TestOpenCode_Integration_OneShot(t *testing.T) {
	bin, err := exec.LookPath("opencode")
	if err != nil {
		t.Skip("opencode not on PATH; skipping integration test")
	}

	b, err := NewHostBackend(HostBackendConfig{OpenCodeBinary: bin})
	if err != nil {
		t.Fatalf("new host backend: %v", err)
	}

	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if gitErr := exec.CommandContext(ctx, "git", "-C", dir, "init", "-q").Run(); gitErr != nil {
		t.Skipf("git init failed: %v", gitErr)
	}

	spec := ContainerSpec{
		Name:    "wallfacer-opencode-integration",
		Env:     map[string]string{"WALLFACER_AGENT": "opencode"},
		Cmd:     []string{"-p", "Reply with the single word: hi"},
		WorkDir: dir,
	}

	h, err := b.Launch(ctx, spec)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	var wg sync.WaitGroup
	var stdoutBytes []byte
	wg.Add(2)
	go func() { defer wg.Done(); stdoutBytes, _ = io.ReadAll(h.Stdout()) }()
	go func() { defer wg.Done(); _, _ = io.ReadAll(h.Stderr()) }()
	// Drain to EOF before Wait, matching the runner (agent.go): cmd.Wait
	// closes the opencode StdoutPipe the tee goroutine reads, so waiting
	// before the drain finishes truncates the tee'd output.
	wg.Wait()
	if _, err := h.Wait(); err != nil {
		t.Skipf("opencode run failed (likely no auth/network): %v", err)
	}

	var final map[string]any
	scanner := bufio.NewScanner(strings.NewReader(string(stdoutBytes)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] != '{' {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil {
			final = m
		}
	}
	if final == nil {
		t.Fatalf("no JSON lines in opencode output:\n%s", stdoutBytes)
	}
	if final["type"] != "result" {
		t.Fatalf("last line is not the synthesized result: %v", final)
	}
	// An error result almost always means no provider auth on this host; treat
	// it as a skip so a deliberate run without `opencode auth login` stays green.
	if isErr, _ := final["is_error"].(bool); isErr {
		t.Skipf("opencode produced an error result (likely no auth): %v", final)
	}
	if sid, _ := final["sessionID"].(string); sid == "" {
		t.Error("synthesized result carried no session id")
	}
}
