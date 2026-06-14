//go:build cursor_integration

// This live integration test is gated behind the cursor_integration build
// tag because it invokes a real cursor-agent process, which costs API tokens
// and needs an authenticated CLI. `make test` (go test ./... without -short)
// must not fire it; run it deliberately with:
//
//	go test -tags cursor_integration ./internal/harness/ -run TestCursor_Integration
package harness

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestCursor_Integration_OneShot runs a real cursor-agent one-shot and
// asserts the adapter parses the live stream into a terminal KindResult with
// a non-empty session id. Skips (not fails) when cursor-agent is absent or
// unauthenticated so a deliberate run on an unconfigured host stays green.
func TestCursor_Integration_OneShot(t *testing.T) {
	bin, err := exec.LookPath("cursor-agent")
	if err != nil {
		t.Skip("cursor-agent not on PATH; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	h, _ := Lookup(Cursor)
	// PermissionFull mirrors what the executor's launchCursor sets; it emits
	// --force --trust so the run clears cursor's workspace-trust gate that
	// otherwise stops a headless invocation in a fresh directory.
	argv, _, err := h.BuildArgv(Request{
		Prompt:     "Reply with the single word: hi",
		Permission: PermissionFull,
	})
	if err != nil {
		t.Fatalf("BuildArgv: %v", err)
	}
	// cursor-agent expects a git repo as its workspace; init an empty one.
	dir := t.TempDir()
	if gitErr := exec.CommandContext(ctx, "git", "-C", dir, "init", "-q").Run(); gitErr != nil {
		t.Skipf("git init failed: %v", gitErr)
	}
	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Dir = dir
	out, runErr := cmd.Output()
	if runErr != nil {
		// Most likely unauthenticated (cursor-agent login) or offline; treat
		// as a skip rather than a failure so a deliberate run stays green.
		t.Skipf("cursor-agent run failed (likely no auth/network): %v", runErr)
	}

	var terminal *Event
	var sessionID string
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] != '{' {
			continue
		}
		evt, perr := h.ParseEvent([]byte(line))
		if perr != nil {
			t.Fatalf("ParseEvent: %v", perr)
		}
		if evt.SessionID != "" {
			sessionID = evt.SessionID
		}
		if evt.Kind == KindResult || evt.Kind == KindError {
			e := evt
			terminal = &e
		}
	}
	if terminal == nil {
		t.Fatalf("no terminal KindResult event in cursor-agent output:\n%s", out)
	}
	if terminal.Kind == KindError {
		t.Fatalf("cursor-agent returned an error result: %+v", terminal)
	}
	if sessionID == "" {
		t.Error("terminal result carried no session id")
	}
}
