package agents

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestWatch_FiresOnYAMLWrite verifies that dropping a YAML file
// into the watched directory triggers onChange after the debounce
// window. The busy-wait uses the test timeout as a ceiling.
func TestWatch_FiresOnYAMLWrite(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var fired atomic.Int32
	stop, err := Watch(ctx, dir, func() { fired.Add(1) })
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer stop()

	if err := os.WriteFile(filepath.Join(dir, "new-agent.yaml"), []byte("slug: x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for fired.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if fired.Load() == 0 {
		t.Fatal("onChange never fired after YAML write")
	}
}

// TestWatch_IgnoresNonYAML verifies that a README or other
// extension in the watched directory does NOT trigger onChange.
func TestWatch_IgnoresNonYAML(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var fired atomic.Int32
	stop, err := Watch(ctx, dir, func() { fired.Add(1) })
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer stop()

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("notes"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Wait comfortably past the debounce window.
	time.Sleep(400 * time.Millisecond)
	if got := fired.Load(); got != 0 {
		t.Errorf("onChange fired %d times for README write, want 0", got)
	}
}

// TestWatch_DebouncesBursts verifies that a rapid burst of writes
// collapses into a small number of onChange calls rather than one
// per event.
func TestWatch_DebouncesBursts(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var fired atomic.Int32
	stop, err := Watch(ctx, dir, func() { fired.Add(1) })
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer stop()

	// Five rapid writes within one debounce window.
	for i := 0; i < 5; i++ {
		path := filepath.Join(dir, "a.yaml")
		if err := os.WriteFile(path, []byte("slug: x\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	// Wait for the debounce to trip.
	deadline := time.Now().Add(2 * time.Second)
	for fired.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := fired.Load(); got < 1 || got > 2 {
		t.Errorf("fired count = %d, want 1 or 2 (debounce should coalesce)", got)
	}
}

// TestWatch_CancelStopsGoroutine verifies cancelling ctx stops the
// background goroutine. Absence of leaks is implicit via the test
// harness — we just check that onChange stops firing.
func TestWatch_CancelStopsGoroutine(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())

	var fired atomic.Int32
	if _, err := Watch(ctx, dir, func() { fired.Add(1) }); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	cancel()
	// Give the goroutine a moment to exit.
	time.Sleep(100 * time.Millisecond)

	before := fired.Load()
	if err := os.WriteFile(filepath.Join(dir, "after-cancel.yaml"), []byte("slug: x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	if fired.Load() != before {
		t.Errorf("onChange fired after ctx cancel (before=%d, after=%d)", before, fired.Load())
	}
}
