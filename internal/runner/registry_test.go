package runner

import (
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/google/uuid"
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
	for i := 0; i < 5; i++ {
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

func TestContainerRegistry_Singleton(t *testing.T) {
	r := &containerRegistry{}
	name := "wallfacer-ideate-1234"

	r.SetSingleton(name)

	got, ok := r.GetSingleton()
	if !ok {
		t.Fatal("expected to find singleton entry after SetSingleton, got ok=false")
	}
	if got != name {
		t.Fatalf("expected %q, got %q", name, got)
	}

	r.DeleteSingleton()

	got, ok = r.GetSingleton()
	if ok {
		t.Fatalf("expected singleton to be deleted, got %q", got)
	}
}

func TestContainerRegistry_SingletonGetMissing(t *testing.T) {
	r := &containerRegistry{}

	got, ok := r.GetSingleton()
	if ok {
		t.Fatalf("expected ok=false for missing singleton, got %q", got)
	}
	if got != "" {
		t.Fatalf("expected empty string for missing singleton, got %q", got)
	}
}

func TestContainerRegistry_SingletonKeyDoesNotConflictWithRealUUID(t *testing.T) {
	r := &containerRegistry{}
	realID := uuid.New()

	r.Set(realID, "real-container")
	r.SetSingleton("singleton-container")

	got, ok := r.Get(realID)
	if !ok || got != "real-container" {
		t.Fatalf("real UUID entry corrupted: ok=%v, got=%q", ok, got)
	}
	gotSingleton, ok := r.GetSingleton()
	if !ok || gotSingleton != "singleton-container" {
		t.Fatalf("singleton entry corrupted: ok=%v, got=%q", ok, gotSingleton)
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
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.Set(ids[i], fmt.Sprintf("container-%d", i))
		}(i)
	}
	wg.Wait()

	// Concurrent Get
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name, ok := r.Get(ids[i])
			if !ok {
				t.Errorf("goroutine %d: expected entry for id %v", i, ids[i])
				return
			}
			if name != fmt.Sprintf("container-%d", i) {
				t.Errorf("goroutine %d: expected 'container-%d', got %q", i, i, name)
			}
		}(i)
	}
	wg.Wait()

	// Concurrent Delete
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.Delete(ids[i])
		}(i)
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

	r.SetHandle(id, h, nil)

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

func TestContainerRegistry_SingletonHandle(t *testing.T) {
	r := &containerRegistry{}
	h := &stubHandle{name: "wallfacer-ideate-handle"}

	r.SetSingletonHandle(h, nil)

	name, ok := r.GetSingleton()
	if !ok || name != "wallfacer-ideate-handle" {
		t.Fatalf("GetSingleton after SetSingletonHandle: ok=%v, name=%q", ok, name)
	}

	got := r.GetSingletonHandle()
	if got != h {
		t.Fatal("GetSingletonHandle returned different handle")
	}

	r.DeleteSingleton()
	if got := r.GetSingletonHandle(); got != nil {
		t.Fatal("expected nil after DeleteSingleton")
	}
}

// stubHandle is a minimal SandboxHandle for registry tests.
type stubHandle struct {
	name   string
	killed bool
}

func (h *stubHandle) State() SandboxState   { return SandboxRunning }
func (h *stubHandle) Stdout() io.ReadCloser { return nil }
func (h *stubHandle) Stderr() io.ReadCloser { return nil }
func (h *stubHandle) Wait() (int, error)    { return 0, nil }
func (h *stubHandle) Kill() error           { h.killed = true; return nil }
func (h *stubHandle) Name() string          { return h.name }
