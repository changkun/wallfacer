package runner

import (
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/executor"
)

func TestContainerRegistry_SetGetDelete(t *testing.T) {
	r := &containerRegistry{}
	id := uuid.New()
	name := "wallfacer-test-container"

	r.Set(id, name)

	got, ok := r.Get(id)
	if !ok {
		t.Fatal("expected to find entry after Set, got ok=false")
	}
	if got != name {
		t.Fatalf("expected %q, got %q", name, got)
	}

	r.Delete(id)

	got, ok = r.Get(id)
	if ok {
		t.Fatalf("expected entry to be deleted, got %q", got)
	}
}

func TestContainerRegistry_GetMissing(t *testing.T) {
	r := &containerRegistry{}
	id := uuid.New()

	got, ok := r.Get(id)
	if ok {
		t.Fatalf("expected ok=false for missing entry, got %q", got)
	}
	if got != "" {
		t.Fatalf("expected empty string for missing entry, got %q", got)
	}
}

func TestContainerRegistry_Range(t *testing.T) {
	r := &containerRegistry{}
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	names := []string{"container-a", "container-b", "container-c"}

	for i, id := range ids {
		r.Set(id, names[i])
	}

	seen := map[uuid.UUID]string{}
	for id, entry := range r.All() {
		seen[id] = entry.name
	}

	if len(seen) != len(ids) {
		t.Fatalf("expected %d entries from Range, got %d", len(ids), len(seen))
	}
	for i, id := range ids {
		if seen[id] != names[i] {
			t.Fatalf("for id %v: expected %q, got %q", id, names[i], seen[id])
		}
	}
}

func TestContainerRegistry_RangeEarlyStop(t *testing.T) {
	r := &containerRegistry{}
	for i := range 5 {
		r.Set(uuid.New(), fmt.Sprintf("container-%d", i))
	}

	count := 0
	for range r.All() {
		count++
		break // stop after first
	}

	if count != 1 {
		t.Fatalf("expected Range to stop after 1 iteration when fn returns false, got %d", count)
	}
}

func TestContainerRegistry_ConcurrentAccess(t *testing.T) {
	r := &containerRegistry{}
	const goroutines = 50

	var wg sync.WaitGroup
	ids := make([]uuid.UUID, goroutines)
	for i := range ids {
		ids[i] = uuid.New()
	}

	// Concurrent Set
	for i := range goroutines {
		wg.Go(func() {
			r.Set(ids[i], fmt.Sprintf("container-%d", i))
		})
	}
	wg.Wait()

	// Concurrent Get
	for i := range goroutines {
		wg.Go(func() {
			name, ok := r.Get(ids[i])
			if !ok {
				t.Errorf("goroutine %d: expected entry for id %v", i, ids[i])
				return
			}
			if name != fmt.Sprintf("container-%d", i) {
				t.Errorf("goroutine %d: expected 'container-%d', got %q", i, i, name)
			}
		})
	}
	wg.Wait()

	// Concurrent Delete
	for i := range goroutines {
		wg.Go(func() {
			r.Delete(ids[i])
		})
	}
	wg.Wait()

	// All entries should be gone
	for id, entry := range r.All() {
		t.Errorf("unexpected entry after all deletes: id=%v name=%q", id, entry.name)
	}
}

// ---------- Handle-based registry tests ----------

func TestContainerRegistry_SetHandleGetHandle(t *testing.T) {
	r := &containerRegistry{}
	id := uuid.New()
	h := &stubHandle{name: "wallfacer-handle-test"}

	r.SetHandle(id, h)

	// Get returns the name from the handle.
	name, ok := r.Get(id)
	if !ok || name != "wallfacer-handle-test" {
		t.Fatalf("Get after SetHandle: ok=%v, name=%q", ok, name)
	}

	// GetHandle returns the handle itself.
	got := r.GetHandle(id)
	if got != h {
		t.Fatal("GetHandle returned different handle")
	}
}

func TestContainerRegistry_GetHandleMissing(t *testing.T) {
	r := &containerRegistry{}
	if h := r.GetHandle(uuid.New()); h != nil {
		t.Fatalf("expected nil handle for missing entry, got %v", h)
	}
}

func TestContainerRegistry_SetNameGetHandleNil(t *testing.T) {
	r := &containerRegistry{}
	id := uuid.New()
	r.Set(id, "name-only")

	// GetHandle returns nil for name-only entries.
	if h := r.GetHandle(id); h != nil {
		t.Fatalf("expected nil handle for name-only entry, got %v", h)
	}
}

// stubHandle is a minimal SandboxHandle for registry tests.
type stubHandle struct {
	name   string
	killed bool
}

func (h *stubHandle) State() executor.BackendState { return executor.StateRunning }
func (h *stubHandle) Stdout() io.ReadCloser        { return nil }
func (h *stubHandle) Stderr() io.ReadCloser        { return nil }
func (h *stubHandle) Wait() (int, error)           { return 0, nil }
func (h *stubHandle) Kill() error                  { h.killed = true; return nil }
func (h *stubHandle) Name() string                 { return h.name }
