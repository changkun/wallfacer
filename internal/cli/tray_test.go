//go:build desktop

package cli

import (
	"testing"
)

// TestTrayManagerNew verifies that NewTrayManager initializes without panic
// and stores the callbacks.
func TestTrayManagerNew(t *testing.T) {
	called := false
	showFn := func() { called = true }
	quitFn := func() {}

	tm := NewTrayManager(showFn, quitFn)
	if tm == nil {
		t.Fatal("expected non-nil TrayManager")
	}
	if tm.showWindow == nil {
		t.Fatal("expected non-nil showWindow callback")
	}
	if tm.quit == nil {
		t.Fatal("expected non-nil quit callback")
	}

	// Verify the stored callback is the one we passed.
	tm.showWindow()
	if !called {
		t.Fatal("showWindow callback was not invoked")
	}
}
