//go:build !desktop

package cli

import (
	"strings"
	"testing"
)

// TestRunDesktopStub verifies that the non-desktop build returns a clear error
// message indicating the feature requires the -tags desktop build flag.
func TestRunDesktopStub(t *testing.T) {
	err := RunDesktop("", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error from desktop stub, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unsupported") && !strings.Contains(msg, "not yet implemented") {
		t.Fatalf("unexpected error message: %s", msg)
	}
}
