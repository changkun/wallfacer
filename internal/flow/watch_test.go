package flow

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestWatch_FiresOnYAMLWrite smoke-checks that the flow watcher
// behaves like the agents watcher: a YAML drop triggers onChange.
// The heavier debounce / ignore / cancel cases are covered in
// agents/watch_test.go since the two watchers share the same
// implementation shape.
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

	body := "slug: custom-flow\nname: Custom\nsteps:\n  - agent_slug: impl\n"
	if err := os.WriteFile(filepath.Join(dir, "custom.yaml"), []byte(body), 0o644); err != nil {
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
