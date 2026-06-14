//go:build pi_integration

// This live integration test is gated behind the pi_integration build tag
// because it invokes a real pi process, which costs API tokens and needs an
// authenticated provider. `make test` (go test ./... without -short) must
// not fire it; run it deliberately with:
//
//	go test -tags pi_integration ./internal/harness/ -run TestPi_Integration
package harness

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestPi_Integration_OneShot runs a real pi one-shot and asserts the adapter
// parses the live --mode json stream into a terminal KindResult with a
// non-empty session id. Skips (not fails) when pi is absent or
// unauthenticated so a deliberate run on an unconfigured host stays green.
func TestPi_Integration_OneShot(t *testing.T) {
	bin, err := exec.LookPath("pi")
	if err != nil {
		t.Skip("pi not on PATH; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	h, _ := Lookup(Pi)
	// PermissionFull mirrors what the executor's launchPi sets (all four tools
	// enabled). A model can be supplied via PI_INTEGRATION_MODEL ("provider/id");
	// otherwise pi falls back to its default provider.
	req := Request{
		Prompt:     "Reply with the single word: hi",
		Permission: PermissionFull,
	}
	argv, _, err := h.BuildArgv(req)
	if err != nil {
		t.Fatalf("BuildArgv: %v", err)
	}
	dir := t.TempDir()
	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Dir = dir
	out, runErr := cmd.Output()
	if runErr != nil {
		// Most likely no provider credentials in the environment or offline;
		// treat as a skip rather than a failure so a deliberate run stays green.
		t.Skipf("pi run failed (likely no provider auth/network): %v", runErr)
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
		t.Fatalf("no terminal KindResult event in pi output:\n%s", out)
	}
	if terminal.Kind == KindError {
		t.Fatalf("pi returned an error result: %+v", terminal)
	}
	if sessionID == "" {
		t.Error("terminal result carried no session id")
	}
}
