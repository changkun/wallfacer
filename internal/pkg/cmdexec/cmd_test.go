package cmdexec

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// TestNew_Run verifies that a successful command returns nil.
func TestNew_Run(t *testing.T) {
	if err := New("true").Run(); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

// TestNew_RunFail verifies that a failing command returns a non-nil error.
func TestNew_RunFail(t *testing.T) {
	if err := New("false").Run(); err == nil {
		t.Fatal("expected error from 'false'")
	}
}

// TestNew_Output verifies that Output captures and returns trimmed stdout.
func TestNew_Output(t *testing.T) {
	out, err := New("echo", "hello").Output()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello" {
		t.Fatalf("expected 'hello', got %q", out)
	}
}

// TestNew_Output_Trimmed verifies that Output strips leading/trailing whitespace.
func TestNew_Output_Trimmed(t *testing.T) {
	out, err := New("echo", "  spaces  ").Output()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// echo adds newline; Output trims
	if out != "spaces" {
		t.Fatalf("expected 'spaces', got %q", out)
	}
}

// TestNew_Combined verifies that Combined merges stdout and stderr into one string.
func TestNew_Combined(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash not available on windows")
	}
	out, err := New("bash", "-c", "echo out; echo err >&2").Combined()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "out\nerr" {
		t.Fatalf("expected 'out\\nerr', got %q", out)
	}
}

// TestNew_OutputBytes verifies that OutputBytes returns raw bytes without trimming.
func TestNew_OutputBytes(t *testing.T) {
	raw, err := New("echo", "raw").OutputBytes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// OutputBytes does NOT trim
	if string(raw) != "raw\n" {
		t.Fatalf("expected 'raw\\n', got %q", string(raw))
	}
}

// TestNew_Capture verifies that Capture returns stdout and stderr as separate byte slices.
func TestNew_Capture(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash not available on windows")
	}
	stdout, stderr, err := New("bash", "-c", "echo out; echo err >&2").Capture()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(stdout) != "out\n" {
		t.Fatalf("stdout: expected 'out\\n', got %q", string(stdout))
	}
	if string(stderr) != "err\n" {
		t.Fatalf("stderr: expected 'err\\n', got %q", string(stderr))
	}
}

// TestWithContext_Cancellation verifies that a pre-cancelled context causes Run to fail.
func TestWithContext_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // already cancelled

	err := New("sleep", "10").WithContext(ctx).Run()
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// TestWithContext_Timeout verifies that a short context timeout kills a long-running command.
func TestWithContext_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	err := New("sleep", "10").WithContext(ctx).Run()
	if err == nil {
		t.Fatal("expected error from timed-out context")
	}
}

// TestGit_PrependsArgs verifies that Git prepends "-C <dir>" to the argument list.
func TestGit_PrependsArgs(t *testing.T) {
	cmd := Git("/tmp", "status")
	if cmd.name != "git" {
		t.Fatalf("expected name 'git', got %q", cmd.name)
	}
	if len(cmd.args) != 3 || cmd.args[0] != "-C" || cmd.args[1] != "/tmp" || cmd.args[2] != "status" {
		t.Fatalf("unexpected args: %v", cmd.args)
	}
}
